package app

import (
	"strings"

	"customer-service/backend/internal/ai"
	"customer-service/backend/internal/domain"
)

// languageDecision 是一次访客消息带来的语言状态变化。
type languageDecision struct {
	DetectedLang string
	ReplyLang    string
	Override     string
	Stats        map[string]int
	Changed      bool
}

// conversationReplyLang 返回会话当前的回复语言：显式指令覆盖优先，其次访客语言。
func conversationReplyLang(conversation domain.Conversation) string {
	if override := ai.NormalizeOptionalLang(conversation.ReplyLangOverride); override != "" {
		return override
	}
	return ai.NormalizeLang(conversation.VisitorLang, ai.LangZh)
}

// translationStatusFor 计算消息的翻译状态：skipped（无需翻译）/ done / failed。
func translationStatusFor(sourceLang, targetLang string, translations map[string]string) string {
	source := ai.NormalizeLang(sourceLang, ai.LangZh)
	target := ai.NormalizeLang(targetLang, ai.LangZh)
	if source == target {
		return "skipped"
	}
	if text, ok := translations[target]; ok && strings.TrimSpace(text) != "" {
		return "done"
	}
	return "failed"
}

// resolveConversationLanguage 处理一条访客消息带来的语言变化：
//  1. 逐条识别消息语言（脚本 + 词表，含方言标记）；
//  2. 解析"请用 X 语言回答我"这类显式指令，命中后整会话生效；
//  3. 统计最近若干条访客消息的语种分布，按"用得多的语言"决定回复语言（平局取最近一条）。
func (s *Service) resolveConversationLanguage(conversation domain.Conversation, content string, messageType domain.MessageType) languageDecision {
	decision := languageDecision{
		DetectedLang: ai.NormalizeOptionalLang(conversation.DetectedLang),
		ReplyLang:    conversationReplyLang(conversation),
		Override:     ai.NormalizeOptionalLang(conversation.ReplyLangOverride),
		Stats:        conversation.LanguageStats,
	}
	if !isTranslatableMessageType(messageType) || strings.TrimSpace(content) == "" {
		return decision
	}
	detection := ai.DetectLanguage(content)
	stats := map[string]int{}
	for lang, count := range conversation.LanguageStats {
		stats[lang] = count
	}
	if messageType == domain.MessageText {
		if detection.Code != "" {
			decision.DetectedLang = detection.Code
			stats[detection.Code]++
		}
		decision.Changed = true
	}
	decision.Stats = stats

	if code, ok := ai.ParseLanguageInstruction(content); ok {
		decision.Override = code
		decision.ReplyLang = code
		decision.Changed = true
		return decision
	}
	if messageType == domain.MessageText {
		recent := s.recentVisitorLanguages(conversation.ID, 10)
		recent = append(recent, detection.Code)
		dominant := ai.DetectDominantLanguage(recent, conversation.VisitorLang, 0.4)
		if dominant != "" {
			decision.ReplyLang = dominant
		}
	}
	return decision
}

// recentVisitorLanguages 读取会话内最近的访客文本消息语种（时间正序）。
func (s *Service) recentVisitorLanguages(conversationID string, limit int) []string {
	messages := s.store.Messages(conversationID)
	languages := make([]string, 0, limit)
	for index := len(messages) - 1; index >= 0 && len(languages) < limit; index-- {
		msg := messages[index]
		if msg.SenderType != domain.SenderVisitor || msg.MessageType != domain.MessageText {
			continue
		}
		if lang := ai.NormalizeOptionalLang(msg.DetectedLang); lang != "" {
			languages = append(languages, lang)
		}
	}
	for i, j := 0, len(languages)-1; i < j; i, j = i+1, j-1 {
		languages[i], languages[j] = languages[j], languages[i]
	}
	return languages
}
