package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

// newKnowledgeStub 起一个 OpenAI 兼容上游桩，记录命中次数与请求体。
func newKnowledgeStub(t *testing.T, reply string) (*httptest.Server, *int32, <-chan string) {
	t.Helper()
	var calls int32
	bodies := make(chan string, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		atomic.AddInt32(&calls, 1)
		raw, _ := io.ReadAll(r.Body)
		select {
		case bodies <- string(raw):
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": reply}},
			},
		})
	}))
	t.Cleanup(server.Close)
	return server, &calls, bodies
}

// newKnowledgeService 构造使用指定 AI 模式与上游地址的服务实例。
func newKnowledgeService(t *testing.T, mode domain.AICustomerMode, baseURL string) (*Service, store.Store) {
	t.Helper()
	st := store.NewMemoryStore(store.Options{})
	if _, err := st.UpdateAISettings(domain.AISettings{
		Enabled:        true,
		Mode:           mode,
		BaseURL:        baseURL,
		APIKey:         "test-key",
		Model:          "test-model",
		APIType:        domain.AIAPITypeChatCompletions,
		TimeoutSeconds: 5,
	}); err != nil {
		t.Fatalf("update ai settings: %v", err)
	}
	return NewService(st, realtime.NewHub(), slog.Default()), st
}

// seedKnowledge 建一个启用知识库并写入一条启用条目。
func seedKnowledge(t *testing.T, st store.Store, content string) (domain.KnowledgeBase, domain.KnowledgeEntry) {
	t.Helper()
	base, err := st.CreateKnowledgeBase(domain.KnowledgeBase{Name: "测试知识库", Enabled: true})
	if err != nil {
		t.Fatalf("create knowledge base: %v", err)
	}
	entry, err := st.CreateKnowledgeEntry(domain.KnowledgeEntry{
		BaseID:   base.ID,
		Title:    "排水沟槽",
		Content:  content,
		Keywords: []string{"排水沟槽"},
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("create knowledge entry: %v", err)
	}
	return base, entry
}

// waitForAIMessage 轮询等待会话内出现一条 AI 文本消息。
func waitForAIMessage(t *testing.T, st store.Store, conversationID string) domain.Message {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		messages := st.Messages(conversationID)
		for index := len(messages) - 1; index >= 0; index-- {
			msg := messages[index]
			if msg.SenderType == domain.SenderAI && msg.MessageType == domain.MessageAIText {
				return msg
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("等待 AI 消息超时")
	return domain.Message{}
}

// TestGenerateAIReplyInjectsKnowledgeAndRecordsEntryIDs：
// 命中知识时把资料注入 system prompt，并把命中条目 ID 落库到消息上。
func TestGenerateAIReplyInjectsKnowledgeAndRecordsEntryIDs(t *testing.T) {
	stub, calls, bodies := newKnowledgeStub(t, "不锈钢排水沟槽支持定制长度。")
	service, st := newKnowledgeService(t, domain.AIModeAlwaysAI, stub.URL)
	_, entry := seedKnowledge(t, st, "不锈钢排水沟槽支持定制长度与颜色，可按图纸生产。")

	visitor, err := st.CreateVisitorConversation("192.0.2.71", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	conversationID := visitor.Conversation.ID
	if _, err := service.AddVisitorMessage(conversationID, "m1", "排水沟槽有哪些规格？", domain.MessageText); err != nil {
		t.Fatalf("add visitor message: %v", err)
	}

	msg := waitForAIMessage(t, st, conversationID)
	if msg.Content != "不锈钢排水沟槽支持定制长度。" {
		t.Fatalf("AI 回复 = %q", msg.Content)
	}
	if len(msg.KnowledgeEntryIDs) != 1 || msg.KnowledgeEntryIDs[0] != entry.ID {
		t.Fatalf("回答依据应记录命中条目：%+v", msg.KnowledgeEntryIDs)
	}
	// 客服端视图保留"回答依据"，访客视图不下发内部条目 ID。
	if view := AgentFacingMessage(msg); len(view.KnowledgeEntryIDs) != 1 {
		t.Fatalf("客服视图应保留回答依据：%+v", view.KnowledgeEntryIDs)
	}
	if view := VisitorFacingMessageForLang(msg, "zh"); len(view.KnowledgeEntryIDs) != 0 {
		t.Fatalf("访客视图不应下发回答依据：%+v", view.KnowledgeEntryIDs)
	}
	if atomic.LoadInt32(calls) != 1 {
		t.Fatalf("命中知识时应调用一次上游，实际 %d", atomic.LoadInt32(calls))
	}
	select {
	case body := <-bodies:
		if !strings.Contains(body, "只依据这些资料") {
			t.Fatalf("system prompt 未要求只依据资料：%s", body)
		}
		if !strings.Contains(body, "可按图纸生产") {
			t.Fatalf("system prompt 未注入知识正文：%s", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("未捕获到上游请求体")
	}
}

// TestGenerateAIReplyKnowledgeMissHandoff：
// 未命中 + unknown_action=handoff：回复配置文案、状态转 human_requested、不调用上游。
func TestGenerateAIReplyKnowledgeMissHandoff(t *testing.T) {
	stub, calls, _ := newKnowledgeStub(t, "不应被调用")
	service, st := newKnowledgeService(t, domain.AIModeHumanFirst, stub.URL)
	_, _ = seedKnowledge(t, st, "不锈钢排水沟槽支持定制长度与颜色。")
	if _, err := st.UpdateKnowledgeSettings(domain.KnowledgeSettings{
		UnknownAction:  domain.KnowledgeUnknownHandoff,
		RefuseReply:    "抱歉，这个问题需要人工客服为您解答。",
		ScoreThreshold: domain.DefaultKnowledgeScoreThreshold,
		MaxEntries:     domain.DefaultKnowledgeMaxEntries,
		Enabled:        true,
	}); err != nil {
		t.Fatalf("update knowledge settings: %v", err)
	}

	visitor, err := st.CreateVisitorConversation("192.0.2.72", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	conversationID := visitor.Conversation.ID
	if _, err := service.AddVisitorMessage(conversationID, "m1", "如何申请退款？", domain.MessageText); err != nil {
		t.Fatalf("add visitor message: %v", err)
	}

	msg := waitForAIMessage(t, st, conversationID)
	if msg.Content != "抱歉，这个问题需要人工客服为您解答。" {
		t.Fatalf("未命中回复文案 = %q", msg.Content)
	}
	if len(msg.KnowledgeEntryIDs) != 0 {
		t.Fatalf("未命中不应记录回答依据：%+v", msg.KnowledgeEntryIDs)
	}
	conversation, ok := st.Conversation(conversationID)
	if !ok || conversation.Status != domain.ConversationHumanRequested {
		t.Fatalf("未命中 handoff 后会话状态应为 human_requested，实际 %+v", conversation.Status)
	}
	time.Sleep(150 * time.Millisecond)
	if got := atomic.LoadInt32(calls); got != 0 {
		t.Fatalf("未命中时不应调用上游自由作答，实际调用 %d 次", got)
	}
}

// TestGenerateAIReplyKnowledgeMissRefuse：
// 未命中 + unknown_action=refuse：只回复配置文案，不转人工、不调用上游。
func TestGenerateAIReplyKnowledgeMissRefuse(t *testing.T) {
	stub, calls, _ := newKnowledgeStub(t, "不应被调用")
	service, st := newKnowledgeService(t, domain.AIModeHumanFirst, stub.URL)
	_, _ = seedKnowledge(t, st, "不锈钢排水沟槽支持定制长度与颜色。")
	if _, err := st.UpdateKnowledgeSettings(domain.KnowledgeSettings{
		UnknownAction:  domain.KnowledgeUnknownRefuse,
		RefuseReply:    "抱歉，我暂时没有查到相关资料。",
		ScoreThreshold: domain.DefaultKnowledgeScoreThreshold,
		MaxEntries:     domain.DefaultKnowledgeMaxEntries,
		Enabled:        true,
	}); err != nil {
		t.Fatalf("update knowledge settings: %v", err)
	}

	visitor, err := st.CreateVisitorConversation("192.0.2.73", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	conversationID := visitor.Conversation.ID
	if _, err := service.AddVisitorMessage(conversationID, "m1", "如何申请退款？", domain.MessageText); err != nil {
		t.Fatalf("add visitor message: %v", err)
	}

	msg := waitForAIMessage(t, st, conversationID)
	if msg.Content != "抱歉，我暂时没有查到相关资料。" {
		t.Fatalf("未命中回复文案 = %q", msg.Content)
	}
	conversation, ok := st.Conversation(conversationID)
	if !ok || conversation.Status == domain.ConversationHumanRequested {
		t.Fatalf("refuse 分支不应转人工，实际状态 %s", conversation.Status)
	}
	time.Sleep(150 * time.Millisecond)
	if got := atomic.LoadInt32(calls); got != 0 {
		t.Fatalf("未命中时不应调用上游自由作答，实际调用 %d 次", got)
	}
}

// TestGenerateAIReplyWithoutKnowledgeKeepsLegacyBehavior：
// 硬性要求：没有任何知识库时，AI 行为与改造前完全一致（自由回答、无回答依据）。
func TestGenerateAIReplyWithoutKnowledgeKeepsLegacyBehavior(t *testing.T) {
	stub, calls, _ := newKnowledgeStub(t, "自由回答内容")
	service, st := newKnowledgeService(t, domain.AIModeAlwaysAI, stub.URL)

	visitor, err := st.CreateVisitorConversation("192.0.2.74", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	conversationID := visitor.Conversation.ID
	if _, err := service.AddVisitorMessage(conversationID, "m1", "随便问一个问题", domain.MessageText); err != nil {
		t.Fatalf("add visitor message: %v", err)
	}

	msg := waitForAIMessage(t, st, conversationID)
	if msg.Content != "自由回答内容" {
		t.Fatalf("无知识库时应走自由回答，实际 %q", msg.Content)
	}
	if len(msg.KnowledgeEntryIDs) != 0 {
		t.Fatalf("无知识库时不应有回答依据：%+v", msg.KnowledgeEntryIDs)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("无知识库时应正常调用上游一次，实际 %d", got)
	}
}

// newSuggestFixture 构造"客服已接管 + 会话内有访客消息"的场景，AI 模式为 manual_only。
func newSuggestFixture(t *testing.T, baseURL string, visitorText string) (*Service, store.Store, domain.Agent, string) {
	t.Helper()
	service, st := newKnowledgeService(t, domain.AIModeManualOnly, baseURL)
	agent := newAgentWithPreferences(t, st, "suggest_agent", domain.AgentPreferences{})
	visitor, err := st.CreateVisitorConversation("192.0.2.75", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	conversationID := visitor.Conversation.ID
	if _, err := service.AddVisitorMessage(conversationID, "m1", visitorText, domain.MessageText); err != nil {
		t.Fatalf("add visitor message: %v", err)
	}
	assignConversationTo(t, st, conversationID, agent.ID)
	return service, st, agent, conversationID
}

// TestSuggestReplyReturnsCandidatesWithoutPersisting：
// 命中知识时返回 1-3 条候选，且不产生任何消息。
func TestSuggestReplyReturnsCandidatesWithoutPersisting(t *testing.T) {
	stub, calls, bodies := newKnowledgeStub(t, `["不锈钢排水沟槽支持定制长度。","可按图纸生产，欢迎提供尺寸。"]`)
	service, st, agent, conversationID := newSuggestFixture(t, stub.URL, "排水沟槽怎么卖？")
	_, entry := seedKnowledge(t, st, "不锈钢排水沟槽支持定制长度与颜色。")

	before := len(st.Messages(conversationID))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := service.SuggestReply(ctx, agent.ID, conversationID)
	if err != nil {
		t.Fatalf("suggest reply: %v", err)
	}
	if len(result.Suggestions) != 2 {
		t.Fatalf("应返回 2 条候选，实际 %+v", result.Suggestions)
	}
	if result.Suggestions[0].Text != "不锈钢排水沟槽支持定制长度。" {
		t.Fatalf("候选内容不符：%+v", result.Suggestions[0])
	}
	if len(result.Suggestions[0].EntryIDs) != 1 || result.Suggestions[0].EntryIDs[0] != entry.ID {
		t.Fatalf("候选应带知识条目 ID：%+v", result.Suggestions[0])
	}
	if result.Suggestions[0].Score <= 0 {
		t.Fatalf("候选应带命中分数：%+v", result.Suggestions[0])
	}
	if after := len(st.Messages(conversationID)); after != before {
		t.Fatalf("推荐接口不应产生任何消息：before=%d after=%d", before, after)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("应调用一次上游生成候选，实际 %d", got)
	}
	select {
	case body := <-bodies:
		if !strings.Contains(body, "只依据这些资料") || !strings.Contains(body, "JSON 字符串数组") {
			t.Fatalf("推荐提示词不符：%s", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("未捕获到上游请求体")
	}
}

// TestSuggestReplyDisabledDoesNotCallUpstream：
// 客服关闭 knowledge_suggest_enabled 时返回空数组，且两个上游桩都不被调用。
func TestSuggestReplyDisabledDoesNotCallUpstream(t *testing.T) {
	globalStub, globalCalls, _ := newKnowledgeStub(t, `["全局候选"]`)
	boundStub, boundCalls, _ := newKnowledgeStub(t, `["绑定候选"]`)
	service, st, agent, conversationID := newSuggestFixture(t, globalStub.URL, "排水沟槽怎么卖？")
	_, _ = seedKnowledge(t, st, "不锈钢排水沟槽支持定制长度与颜色。")

	provider, err := st.CreateAIProvider(domain.AIProvider{
		Name:    "接待供应商",
		Kind:    domain.AIProviderKindText,
		APIType: domain.AIAPITypeChatCompletions,
		BaseURL: boundStub.URL,
		Models:  []string{"bound-model"},
		Enabled: true,
	}, "bound-key")
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if _, err := st.UpsertAIBinding(domain.AIBinding{
		Capability: domain.AICapabilityReception,
		ProviderID: provider.ID,
		Model:      "bound-model",
	}); err != nil {
		t.Fatalf("upsert binding: %v", err)
	}

	disabled := false
	if _, err := st.UpdateAgentPreferences(agent.ID, domain.AgentPreferences{KnowledgeSuggestEnabled: &disabled}); err != nil {
		t.Fatalf("update agent preferences: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := service.SuggestReply(ctx, agent.ID, conversationID)
	if err != nil {
		t.Fatalf("suggest reply: %v", err)
	}
	if len(result.Suggestions) != 0 {
		t.Fatalf("关闭推荐后应返回空数组：%+v", result.Suggestions)
	}
	if result.Reason != "" {
		t.Fatalf("关闭推荐不应带 reason，实际 %q", result.Reason)
	}
	time.Sleep(150 * time.Millisecond)
	if atomic.LoadInt32(boundCalls) != 0 || atomic.LoadInt32(globalCalls) != 0 {
		t.Fatalf("关闭推荐后不应调用任何上游：bound=%d global=%d",
			atomic.LoadInt32(boundCalls), atomic.LoadInt32(globalCalls))
	}

	// 打开开关后应命中绑定的接待供应商，且不落到全局配置。
	enabled := true
	if _, err := st.UpdateAgentPreferences(agent.ID, domain.AgentPreferences{KnowledgeSuggestEnabled: &enabled}); err != nil {
		t.Fatalf("update agent preferences: %v", err)
	}
	result, err = service.SuggestReply(ctx, agent.ID, conversationID)
	if err != nil {
		t.Fatalf("suggest reply after enable: %v", err)
	}
	if len(result.Suggestions) != 1 || result.Suggestions[0].Text != "绑定候选" {
		t.Fatalf("打开推荐后应命中绑定供应商：%+v", result.Suggestions)
	}
	if atomic.LoadInt32(boundCalls) != 1 || atomic.LoadInt32(globalCalls) != 0 {
		t.Fatalf("命中次数 bound=%d global=%d，期望 1/0",
			atomic.LoadInt32(boundCalls), atomic.LoadInt32(globalCalls))
	}
}

// TestSuggestReplyNoKnowledgeReturnsReason：检索不到知识时返回空数组 + no_knowledge，不调用上游。
func TestSuggestReplyNoKnowledgeReturnsReason(t *testing.T) {
	stub, calls, _ := newKnowledgeStub(t, `["不应出现"]`)
	service, st, agent, conversationID := newSuggestFixture(t, stub.URL, "如何申请退款？")
	_, _ = seedKnowledge(t, st, "不锈钢排水沟槽支持定制长度与颜色。")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := service.SuggestReply(ctx, agent.ID, conversationID)
	if err != nil {
		t.Fatalf("suggest reply: %v", err)
	}
	if len(result.Suggestions) != 0 || result.Reason != "no_knowledge" {
		t.Fatalf("检索不到知识时应返回空 + no_knowledge：%+v", result)
	}
	time.Sleep(150 * time.Millisecond)
	if got := atomic.LoadInt32(calls); got != 0 {
		t.Fatalf("检索不到知识时不应调用上游，实际 %d", got)
	}
}

// TestSuggestReplyRestrictedToAgentBases：客服限定的知识库之外的条目不参与检索。
func TestSuggestReplyRestrictedToAgentBases(t *testing.T) {
	stub, _, _ := newKnowledgeStub(t, `["候选"]`)
	service, st, agent, conversationID := newSuggestFixture(t, stub.URL, "排水沟槽怎么卖？")
	_, entry := seedKnowledge(t, st, "不锈钢排水沟槽支持定制长度与颜色。")

	otherBase, err := st.CreateKnowledgeBase(domain.KnowledgeBase{Name: "其它知识库", Enabled: true})
	if err != nil {
		t.Fatalf("create knowledge base: %v", err)
	}
	if _, err := st.UpdateAgentPreferences(agent.ID, domain.AgentPreferences{
		KnowledgeBaseIDs: []string{otherBase.ID},
	}); err != nil {
		t.Fatalf("update agent preferences: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := service.SuggestReply(ctx, agent.ID, conversationID)
	if err != nil {
		t.Fatalf("suggest reply: %v", err)
	}
	if len(result.Suggestions) != 0 || result.Reason != "no_knowledge" {
		t.Fatalf("限定到无条目的库时应返回空：%+v", result)
	}

	// 限定回含该条目的库后恢复命中。
	baseID := entry.BaseID
	if _, err := st.UpdateAgentPreferences(agent.ID, domain.AgentPreferences{
		KnowledgeBaseIDs: []string{baseID},
	}); err != nil {
		t.Fatalf("update agent preferences: %v", err)
	}
	result, err = service.SuggestReply(ctx, agent.ID, conversationID)
	if err != nil {
		t.Fatalf("suggest reply: %v", err)
	}
	if len(result.Suggestions) != 1 {
		t.Fatalf("限定到正确知识库后应命中：%+v", result)
	}
}

// TestSuggestReplyRejectsOtherAgentConversation：非负责客服不得生成建议。
func TestSuggestReplyRejectsOtherAgentConversation(t *testing.T) {
	stub, _, _ := newKnowledgeStub(t, `["候选"]`)
	service, st, _, conversationID := newSuggestFixture(t, stub.URL, "排水沟槽怎么卖？")
	_, _ = seedKnowledge(t, st, "不锈钢排水沟槽支持定制长度与颜色。")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := service.SuggestReply(ctx, "agent_other", conversationID); err == nil {
		t.Fatal("非负责客服应被拒绝")
	}
}

// TestParseSuggestionsHandlesJSONAndPlainLines：上游不守格式时按行回退解析。
func TestParseSuggestionsHandlesJSONAndPlainLines(t *testing.T) {
	jsonText := "```json\n[\"候选一\",\"候选二\",\"候选三\",\"候选四\"]\n```"
	items := parseSuggestions(jsonText, maxSuggestReplies)
	if len(items) != maxSuggestReplies || items[0] != "候选一" {
		t.Fatalf("JSON 解析失败：%+v", items)
	}
	plain := "1. 第一条回复\n2. 第二条回复\n- 第三条回复"
	items = parseSuggestions(plain, maxSuggestReplies)
	if len(items) != 3 || items[0] != "第一条回复" || items[2] != "第三条回复" {
		t.Fatalf("按行回退解析失败：%+v", items)
	}
	if got := parseSuggestions("", 3); len(got) != 0 {
		t.Fatalf("空输出应返回空：%+v", got)
	}
}
