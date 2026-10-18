package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"customer-service/backend/internal/ai"
	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/knowledge"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

// ReplySuggestion 是一条候选回复：只返回给客服选择/编辑，AI 不会直接发送。
type ReplySuggestion struct {
	Text     string   `json:"text"`
	EntryIDs []string `json:"entry_ids,omitempty"`
	Score    float64  `json:"score,omitempty"`
}

// SuggestReplyResult 是"知识库辅助推荐发送"的响应体。
type SuggestReplyResult struct {
	Suggestions []ReplySuggestion `json:"suggestions"`
	// Reason 为空表示正常；no_knowledge 表示检索不到知识，前端应提示客服自行作答。
	Reason string `json:"reason,omitempty"`
}

const (
	// maxSuggestMessages 是构造检索查询时使用的最近消息条数上限。
	maxSuggestMessages = 6
	// maxSuggestChars 是检索查询的总字符上限，避免提示词/检索词无限膨胀。
	maxSuggestChars = 2000
	// maxSuggestReplies 是单次推荐返回的候选回复条数上限（1-3 条）。
	maxSuggestReplies = 3
)

// suggestReplyInstruction 要求模型只输出 JSON 数组，便于稳定解析 1-3 条候选回复。
const suggestReplyInstruction = `你是资深客服助手。请基于上面的知识资料，为人工客服生成 1-3 条可以直接发送给访客的候选回复。
要求：
1. 只能使用知识资料中的信息，不要编造资料里没有的内容；
2. 使用访客当前语言，语气礼貌、简洁、口语自然；
3. 每条回复独立可用，彼此角度略有差异（如直接回答 / 补充说明 / 引导下一步）；
4. 只输出 JSON 字符串数组，例如 ["回复一","回复二"]，不要输出任何解释或多余文字。`

// knowledgeSearch 是一次知识库检索的结果。
type knowledgeSearch struct {
	// Active 表示知识库模式是否生效：仅当总开关开启且存在启用中的知识库时为 true。
	// 为 false 时调用方必须保持原有的自由回答行为。
	Active   bool
	Hits     []domain.KnowledgeHit
	ByID     map[string]domain.KnowledgeEntry
	Settings domain.KnowledgeSettings
}

// searchKnowledge 执行一次本地检索。
// baseIDs 为空表示全部启用库（AI 自主接待）；非空表示只在这些库里检索（客服侧限定）。
//
// 检索流程：倒排索引按查询词取候选（并集）→ 无候选直接返回空（不再对全表做 BM25）
// → 对候选跑 BM25（语料统计由索引预计算，结果与全表 BM25 完全一致）。
// 只有在候选库超过索引内存上限时，才回退为一次全量读取 + 全表 BM25。
func (s *Service) searchKnowledge(query string, baseIDs []string) knowledgeSearch {
	settings := s.store.KnowledgeSettings()
	if !settings.Enabled {
		return knowledgeSearch{Settings: settings}
	}
	// 硬性要求：没有任何启用中的知识库时，AI 行为必须与未接入知识库时完全一致。
	bases := s.store.KnowledgeBases()
	enabledBases := 0
	for _, base := range bases {
		if base.Enabled {
			enabledBases++
		}
	}
	if enabledBases == 0 {
		return knowledgeSearch{Settings: settings}
	}
	queryTokens := knowledgeQueryTokens(query)
	if len(queryTokens) == 0 {
		return knowledgeSearch{Active: true, Settings: settings}
	}
	scope := enabledKnowledgeBaseIDs(bases, baseIDs)
	if candidates, stats, ok := s.knowledgeIndex.search(s.store, scope, queryTokens, time.Now().UTC()); ok {
		hits := knowledge.FilterKnowledgeHits(
			knowledge.RankKnowledgeCandidates(query, candidates, stats, settings.MaxEntries),
			settings.ScoreThreshold,
		)
		return knowledgeSearch{Active: true, Hits: hits, ByID: knowledgeEntriesByID(candidates), Settings: settings}
	}
	// 回退：索引不可用（单库条目数超上限）。这里才读全量，结果与旧实现一致。
	entries := s.store.KnowledgeEntriesForBases(baseIDs)
	hits := knowledge.FilterKnowledgeHits(
		knowledge.RankKnowledgeEntries(query, entries, settings.MaxEntries),
		settings.ScoreThreshold,
	)
	return knowledgeSearch{Active: true, Hits: hits, ByID: knowledgeEntriesByID(entries), Settings: settings}
}

// PreviewKnowledgeSearch 是后台"检索预览"入口：走同一套倒排索引，但不做阈值过滤，
// 便于管理员对照阈值调参。limit<=0 时使用知识库配置的 MaxEntries。
func (s *Service) PreviewKnowledgeSearch(query string, baseIDs []string, limit int) []domain.KnowledgeHit {
	if limit <= 0 {
		limit = s.store.KnowledgeSettings().MaxEntries
	}
	queryTokens := knowledgeQueryTokens(query)
	if len(queryTokens) == 0 {
		return nil
	}
	scope := enabledKnowledgeBaseIDs(s.store.KnowledgeBases(), baseIDs)
	if candidates, stats, ok := s.knowledgeIndex.search(s.store, scope, queryTokens, time.Now().UTC()); ok {
		return knowledge.RankKnowledgeCandidates(query, candidates, stats, limit)
	}
	return knowledge.RankKnowledgeEntries(query, s.store.KnowledgeEntriesForBases(baseIDs), limit)
}

// enabledKnowledgeBaseIDs 计算检索实际覆盖的启用知识库集合：
// baseIDs 为空 = 全部启用库；非空 = 其中存在且启用的库（显式指定无效库时不回落全部库）。
func enabledKnowledgeBaseIDs(bases []domain.KnowledgeBase, baseIDs []string) []string {
	requested := cleanKnowledgeBaseIDs(baseIDs)
	restricted := len(requested) > 0
	allowed := make(map[string]bool, len(requested))
	for _, id := range requested {
		allowed[id] = true
	}
	out := make([]string, 0, len(bases))
	for _, base := range bases {
		if !base.Enabled {
			continue
		}
		if restricted && !allowed[base.ID] {
			continue
		}
		out = append(out, base.ID)
	}
	return out
}

// cleanKnowledgeBaseIDs 去空去重，与存储层 KnowledgeEntriesForBases 的清洗规则保持一致。
func cleanKnowledgeBaseIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// knowledgeQueryTokens 按检索词规则切分查询并去重，顺序与 RankKnowledgeEntries 内部一致。
func knowledgeQueryTokens(query string) []string {
	tokens := knowledge.Tokenize(query)
	if len(tokens) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(tokens))
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token == "" || seen[token] {
			continue
		}
		seen[token] = true
		out = append(out, token)
	}
	return out
}

// knowledgeEntriesByID 建立条目 ID -> 条目的映射（用于拼装回答依据 prompt）。
func knowledgeEntriesByID(entries []domain.KnowledgeEntry) map[string]domain.KnowledgeEntry {
	if len(entries) == 0 {
		return nil
	}
	byID := make(map[string]domain.KnowledgeEntry, len(entries))
	for _, entry := range entries {
		byID[entry.ID] = entry
	}
	return byID
}

// knowledgeHitIDs 按分数顺序返回命中条目 ID，用于落库"回答依据"。
func knowledgeHitIDs(hits []domain.KnowledgeHit) []string {
	if len(hits) == 0 {
		return nil
	}
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		out = append(out, hit.EntryID)
	}
	return out
}

// stopAITyping 关闭访客端的"AI 正在回复"输入指示。
func (s *Service) stopAITyping(conversationID string) {
	s.hub.SendToVisitor(conversationID, realtime.Event{Event: "conversation.typing", Data: map[string]any{"sender_type": "ai", "typing": false}})
}

// suggestQuery 用最近若干条会话消息构造检索查询：优先使用客服视角的中文译文，
// 没有译文时回退原文；总长度受 maxSuggestChars 限制。
func (s *Service) suggestQuery(conversationID string) string {
	messages := s.store.Messages(conversationID)
	parts := make([]string, 0, maxSuggestMessages)
	total := 0
	for index := len(messages) - 1; index >= 0 && len(parts) < maxSuggestMessages; index-- {
		msg := messages[index]
		if msg.MessageType != domain.MessageText && msg.MessageType != domain.MessageAIText {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if translated := strings.TrimSpace(msg.Translations[ai.LangZh]); translated != "" {
			content = translated
		}
		if content == "" {
			continue
		}
		if total+len([]rune(content)) > maxSuggestChars {
			break
		}
		parts = append(parts, content)
		total += len([]rune(content))
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, "\n")
}

// SuggestReply 生成 1-3 条候选回复供客服选择发送。
// 该接口只生成候选，绝不落库任何消息；AI 不直接发送。
//
//   - 客服关闭 knowledge_suggest_enabled 时直接返回空数组，且不调用任何上游；
//   - 检索不到知识（或知识库未启用）时返回空数组并带 reason=no_knowledge；
//   - 客服配置了 knowledge_base_ids 时只在这些库里检索。
func (s *Service) SuggestReply(ctx context.Context, agentID, conversationID string) (SuggestReplyResult, error) {
	agentID = strings.TrimSpace(agentID)
	conversationID = strings.TrimSpace(conversationID)
	if agentID == "" || conversationID == "" {
		return SuggestReplyResult{}, store.ErrInvalidInput
	}
	conversation, ok := s.store.Conversation(conversationID)
	if !ok {
		return SuggestReplyResult{}, store.ErrNotFound
	}
	// 只允许会话的负责客服生成建议，避免跨客服泄露会话内容。
	if conversation.AssignedAgentID != agentID {
		return SuggestReplyResult{}, store.ErrForbidden
	}
	preferences := s.agentPreferences(agentID)
	// 两个开关共同门控：ai_suggest_replies 是"AI 推荐发送"总开关，
	// knowledge_suggest_enabled 是"是否用知识库辅助推荐"。任一关闭都不生成候选，
	// 且不调用上游（避免客服关掉后仍产生费用与上下文外泄）。
	if !prefBool(preferences.AISuggestReplies) || !prefBool(preferences.KnowledgeSuggestEnabled) {
		return SuggestReplyResult{Suggestions: []ReplySuggestion{}, Reason: "disabled_by_preference"}, nil
	}
	query := s.suggestQuery(conversationID)
	if query == "" {
		return SuggestReplyResult{Suggestions: []ReplySuggestion{}, Reason: "no_knowledge"}, nil
	}
	search := s.searchKnowledge(query, preferences.KnowledgeBaseIDs)
	if !search.Active || len(search.Hits) == 0 {
		return SuggestReplyResult{Suggestions: []ReplySuggestion{}, Reason: "no_knowledge"}, nil
	}
	// 推荐生成属于接待能力；绑定不可用（含白名单拒绝）时直接报错，不静默改投。
	settings, err := s.aiSettingsForCapability(domain.AICapabilityReception)
	if err != nil {
		return SuggestReplyResult{}, err
	}
	if !settings.Enabled {
		return SuggestReplyResult{Suggestions: []ReplySuggestion{}, Reason: "ai_disabled"}, nil
	}
	visitorLang := conversationReplyLang(conversation)
	settings.SystemPrompt = knowledge.BuildKnowledgePrompt(settings.SystemPrompt, search.Hits, search.ByID) +
		"\n\n" + suggestReplyInstruction
	history := s.conversationAITurns(conversationID, "", visitorLang)
	raw, err := s.ai.Reply(ctx, settings, s.store.ContactSettings(), history,
		"请基于知识资料给出候选回复。", visitorLang, conversationID)
	if err != nil {
		return SuggestReplyResult{}, err
	}
	entryIDs := knowledgeHitIDs(search.Hits)
	score := search.Hits[0].Score
	suggestions := make([]ReplySuggestion, 0, maxSuggestReplies)
	for _, text := range parseSuggestions(raw, maxSuggestReplies) {
		suggestions = append(suggestions, ReplySuggestion{Text: text, EntryIDs: entryIDs, Score: score})
	}
	if len(suggestions) == 0 {
		return SuggestReplyResult{Suggestions: []ReplySuggestion{}, Reason: "no_knowledge"}, nil
	}
	return SuggestReplyResult{Suggestions: suggestions}, nil
}

// parseSuggestions 把模型输出解析为最多 max 条候选回复。
// 优先按 JSON 字符串数组解析；失败时回退为按行拆分，保证上游不守格式也能用。
func parseSuggestions(raw string, max int) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || max <= 0 {
		return nil
	}
	if start := strings.Index(raw, "["); start >= 0 {
		if end := strings.LastIndex(raw, "]"); end > start {
			var items []string
			if err := json.Unmarshal([]byte(raw[start:end+1]), &items); err == nil {
				return cleanSuggestions(items, max)
			}
		}
	}
	return cleanSuggestions(strings.Split(raw, "\n"), max)
}

// cleanSuggestions 清洗候选文本：去引号、去列表前缀、去空行，最多保留 max 条。
func cleanSuggestions(items []string, max int) []string {
	out := make([]string, 0, max)
	for _, item := range items {
		text := strings.TrimSpace(item)
		text = strings.Trim(text, "\"'“”「」")
		text = stripListMarker(strings.TrimSpace(text))
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		out = append(out, text)
		if len(out) >= max {
			break
		}
	}
	return out
}

// stripListMarker 去掉 "1. " / "1、" / "- " / "• " 这类列表前缀，不动正文里的数字。
func stripListMarker(text string) string {
	trimmed := strings.TrimLeft(text, "-•*")
	if trimmed != text {
		return strings.TrimSpace(trimmed)
	}
	digits := 0
	for digits < len(text) && text[digits] >= '0' && text[digits] <= '9' {
		digits++
	}
	if digits > 0 && digits < len(text) {
		remainder := text[digits:]
		for _, marker := range []string{".", "、", ")", "）", ":", "："} {
			if strings.HasPrefix(remainder, marker) {
				return strings.TrimSpace(remainder[len(marker):])
			}
		}
	}
	return text
}

// knowledgeRefuseText 返回配置的库外拒答文案；未配置时使用中文默认文案。
func knowledgeRefuseText(settings domain.KnowledgeSettings) string {
	text := strings.TrimSpace(settings.RefuseReply)
	if text == "" {
		return domain.DefaultKnowledgeRefuseReply
	}
	return text
}
