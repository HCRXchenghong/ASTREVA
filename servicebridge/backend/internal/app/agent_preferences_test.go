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

	"customer-service/backend/internal/ai"
	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

// newPromptCaptureService 起一个翻译上游桩，把每次请求体原样送入 channel，
// 便于断言 system prompt 中是否带上方言风格提示。
func newPromptCaptureService(t *testing.T) (*Service, store.Store, <-chan string) {
	t.Helper()
	bodies := make(chan string, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		select {
		case bodies <- string(raw):
		default:
		}
		payload := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "translated-text"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(server.Close)

	st := store.NewMemoryStore(store.Options{})
	if _, err := st.UpdateAISettings(domain.AISettings{
		Enabled:        true,
		Mode:           domain.AIModeHumanFirst,
		BaseURL:        server.URL,
		APIKey:         "test-key",
		Model:          "test-model",
		APIType:        "chat_completions",
		TimeoutSeconds: 5,
	}); err != nil {
		t.Fatalf("update ai settings: %v", err)
	}
	return NewService(st, realtime.NewHub(), slog.Default()), st, bodies
}

// newAgentWithPreferences 直接落库一个客服账号，偏好按参数设置（nil 表示不设置该字段）。
func newAgentWithPreferences(t *testing.T, st store.Store, account string, preferences domain.AgentPreferences) domain.Agent {
	t.Helper()
	agent, err := st.CreateAgent(domain.Agent{
		Account:     account,
		Name:        "偏好客服",
		Group:       "默认组",
		Status:      domain.AgentOnline,
		Preferences: preferences,
	}, "AgentPass-123456")
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	return agent
}

// assignConversationTo 通过管理员转接把会话交给指定客服。
func assignConversationTo(t *testing.T, st store.Store, conversationID, agentID string) {
	t.Helper()
	admin, _, err := st.LoginAdmin("superadmin", "123456")
	if err != nil {
		t.Fatalf("login admin: %v", err)
	}
	if _, _, err := st.TransferConversation(admin, conversationID, agentID, ""); err != nil {
		t.Fatalf("transfer conversation: %v", err)
	}
}

func boolPtr(value bool) *bool { return &value }

// newManualModeTranslationService 与 newTranslationService 相同，但 AI 模式为 manual_only，
// 避免访客消息触发 AI 自动回复（那也会打到同一个上游，干扰翻译调用计数）。
func newManualModeTranslationService(t *testing.T) (*Service, store.Store, *int32) {
	t.Helper()
	server, calls := newTranslationStub(t)
	st := store.NewMemoryStore(store.Options{})
	if _, err := st.UpdateAISettings(domain.AISettings{
		Enabled:        true,
		Mode:           domain.AIModeManualOnly,
		BaseURL:        server.URL,
		APIKey:         "test-key",
		Model:          "test-model",
		APIType:        "chat_completions",
		TimeoutSeconds: 5,
	}); err != nil {
		t.Fatalf("update ai settings: %v", err)
	}
	return NewService(st, realtime.NewHub(), slog.Default()), st, calls
}

// waitForMessage 轮询等待指定消息落库可见。
func waitForMessage(t *testing.T, st store.Store, conversationID, serverMsgID string) domain.Message {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, msg := range st.Messages(conversationID) {
			if msg.ServerMsgID == serverMsgID {
				return msg
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待消息落库超时：%s", serverMsgID)
	return domain.Message{}
}

// TestIncomingTranslationSkippedWhenAgentDisabled：
// 负责客服关闭"访客消息自动译成中文"后，访客的西语消息不再翻译，也不触发上游调用。
func TestIncomingTranslationSkippedWhenAgentDisabled(t *testing.T) {
	service, st, calls := newManualModeTranslationService(t)
	agent := newAgentWithPreferences(t, st, "incoming_off", domain.AgentPreferences{
		AutoTranslateIncoming: boolPtr(false),
	})
	visitor, err := st.CreateVisitorConversation("192.0.2.201", "web", "es", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	conversationID := visitor.Conversation.ID
	assignConversationTo(t, st, conversationID, agent.ID)

	result, err := service.AddVisitorMessage(conversationID, "c1", "Hola, ¿cuál es el precio de este producto?", domain.MessageText)
	if err != nil {
		t.Fatalf("add visitor message: %v", err)
	}
	if result.Input.TranslationStatus != "skipped" {
		t.Fatalf("translation_status = %q, want skipped", result.Input.TranslationStatus)
	}
	if len(result.Input.Translations) != 0 {
		t.Fatalf("跳过翻译时不应有译文：%v", result.Input.Translations)
	}
	// 给异步任务留出窗口，确认没有 goroutine 被排队。
	time.Sleep(150 * time.Millisecond)
	if got := atomic.LoadInt32(calls); got != 0 {
		t.Fatalf("关闭入向翻译后不应调用上游，实际调用 %d 次", got)
	}
	// 语言状态照旧更新，只有翻译被跳过。
	stored, ok := st.Conversation(conversationID)
	if !ok || stored.DetectedLang != ai.LangEs {
		t.Fatalf("会话 detected_lang 应记录为 es，实际 %q", stored.DetectedLang)
	}
}

// TestOutgoingTranslationSkippedWhenAgentDisabled：
// 客服关闭"我的消息自动译成访客语言"后，中文原文直接投递给英语访客。
func TestOutgoingTranslationSkippedWhenAgentDisabled(t *testing.T) {
	service, st, calls := newTranslationService(t)
	agent := newAgentWithPreferences(t, st, "outgoing_off", domain.AgentPreferences{
		AutoTranslateOutgoing: boolPtr(false),
	})
	visitor, err := st.CreateVisitorConversation("192.0.2.202", "web", "en", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	conversationID := visitor.Conversation.ID
	assignConversationTo(t, st, conversationID, agent.ID)

	const content = "您好，这款产品支持定制尺寸。"
	result, err := service.AddAgentMessage(agent.ID, conversationID, "a1", content, domain.MessageText)
	if err != nil {
		t.Fatalf("add agent message: %v", err)
	}
	if result.Input.TranslationStatus != "skipped" {
		t.Fatalf("translation_status = %q, want skipped", result.Input.TranslationStatus)
	}
	if _, ok := result.Input.Translations["en"]; ok {
		t.Fatalf("关闭出向翻译后不应有 en 译文：%v", result.Input.Translations)
	}
	time.Sleep(150 * time.Millisecond)
	if got := atomic.LoadInt32(calls); got != 0 {
		t.Fatalf("关闭出向翻译后不应调用上游，实际调用 %d 次", got)
	}
	stored := waitForMessage(t, st, conversationID, result.Input.ServerMsgID)
	if stored.Content != content {
		t.Fatalf("落库内容 = %q, want 原文", stored.Content)
	}
	view := VisitorFacingMessageForLang(stored, "en")
	if view.Content != content {
		t.Fatalf("访客视图应为客服原文，实际 %q", view.Content)
	}
}

// TestDialectModeAddsStyleHintToTranslationPrompt：
// 方言模式下，翻译请求的 system prompt 必须包含风格提示。
func TestDialectModeAddsStyleHintToTranslationPrompt(t *testing.T) {
	service, st, bodies := newPromptCaptureService(t)
	agent := newAgentWithPreferences(t, st, "dialect_agent", domain.AgentPreferences{
		DialectMode: domain.DialectModeDialect,
	})
	visitor, err := st.CreateVisitorConversation("192.0.2.203", "web", "es", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	conversationID := visitor.Conversation.ID
	assignConversationTo(t, st, conversationID, agent.ID)
	dialect := "es-419"
	if _, err := st.UpdateConversationLanguage(conversationID, store.ConversationLanguagePatch{DetectedLang: &dialect}); err != nil {
		t.Fatalf("update conversation language: %v", err)
	}

	result, err := service.AddAgentMessage(agent.ID, conversationID, "a1", "您好，这款产品支持定制尺寸。", domain.MessageText)
	if err != nil {
		t.Fatalf("add agent message: %v", err)
	}
	if result.Input.TranslationStatus != "pending" {
		t.Fatalf("translation_status = %q, want pending", result.Input.TranslationStatus)
	}
	select {
	case body := <-bodies:
		if !strings.Contains(body, "风格要求") {
			t.Fatalf("翻译提示词缺少风格要求：%s", body)
		}
		if !strings.Contains(body, "拉丁美洲西班牙语") {
			t.Fatalf("翻译提示词缺少目标方言提示：%s", body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("等待翻译请求超时")
	}
}

// TestStandardModePromptHasNoStyleHint：标准模式下提示词不应出现风格要求。
func TestStandardModePromptHasNoStyleHint(t *testing.T) {
	service, st, bodies := newPromptCaptureService(t)
	agent := newAgentWithPreferences(t, st, "standard_agent", domain.AgentPreferences{})
	visitor, err := st.CreateVisitorConversation("192.0.2.204", "web", "es", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	conversationID := visitor.Conversation.ID
	assignConversationTo(t, st, conversationID, agent.ID)
	dialect := "es-419"
	if _, err := st.UpdateConversationLanguage(conversationID, store.ConversationLanguagePatch{DetectedLang: &dialect}); err != nil {
		t.Fatalf("update conversation language: %v", err)
	}
	if _, err := service.AddAgentMessage(agent.ID, conversationID, "a1", "您好，这款产品支持定制尺寸。", domain.MessageText); err != nil {
		t.Fatalf("add agent message: %v", err)
	}
	select {
	case body := <-bodies:
		if strings.Contains(body, "风格要求") {
			t.Fatalf("标准模式不应带风格要求：%s", body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("等待翻译请求超时")
	}
}

// TestTranslatePreviewReturnsTranslationWithoutPersisting：
// 预览只翻译，不产生任何新消息。
func TestTranslatePreviewReturnsTranslationWithoutPersisting(t *testing.T) {
	service, st, _ := newPromptCaptureService(t)
	agent := newAgentWithPreferences(t, st, "preview_agent", domain.AgentPreferences{})
	visitor, err := st.CreateVisitorConversation("192.0.2.205", "web", "es", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	conversationID := visitor.Conversation.ID
	before := len(st.Messages(conversationID))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	preview, err := service.TranslatePreview(ctx, agent.ID, conversationID, "你好，请问有什么可以帮您？")
	if err != nil {
		t.Fatalf("translate preview: %v", err)
	}
	if preview.TargetLang != "es" || preview.SourceLang != "zh" {
		t.Fatalf("预览语言 = %s->%s, want zh->es", preview.SourceLang, preview.TargetLang)
	}
	if preview.TranslatedText != "translated-text" {
		t.Fatalf("预览译文 = %q", preview.TranslatedText)
	}
	if after := len(st.Messages(conversationID)); after != before {
		t.Fatalf("预览不应产生新消息：before=%d after=%d", before, after)
	}
}

// TestTranslatePreviewSameLanguageReturnsOriginal：源语言与目标语言相同时原样返回。
func TestTranslatePreviewSameLanguageReturnsOriginal(t *testing.T) {
	service, st, _ := newPromptCaptureService(t)
	agent := newAgentWithPreferences(t, st, "preview_same", domain.AgentPreferences{})
	visitor, err := st.CreateVisitorConversation("192.0.2.206", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	preview, err := service.TranslatePreview(ctx, agent.ID, visitor.Conversation.ID, "你好")
	if err != nil {
		t.Fatalf("translate preview: %v", err)
	}
	if preview.TranslatedText != "你好" {
		t.Fatalf("同语言预览应原样返回，实际 %q", preview.TranslatedText)
	}
	if preview.TargetLang != "zh" || preview.SourceLang != "zh" {
		t.Fatalf("预览语言 = %s->%s, want zh->zh", preview.SourceLang, preview.TargetLang)
	}
}

// TestAgentPreferencesDialectModeDrivesPreviewDialect：
// 方言模式下预览返回会话检测到的方言标签，并把它带进翻译提示词。
func TestAgentPreferencesDialectModeDrivesPreviewDialect(t *testing.T) {
	service, st, bodies := newPromptCaptureService(t)
	agent := newAgentWithPreferences(t, st, "preview_dialect", domain.AgentPreferences{
		DialectMode: domain.DialectModeDialect,
	})
	visitor, err := st.CreateVisitorConversation("192.0.2.207", "web", "es", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	conversationID := visitor.Conversation.ID
	dialect := "es-419"
	if _, err := st.UpdateConversationLanguage(conversationID, store.ConversationLanguagePatch{DetectedLang: &dialect}); err != nil {
		t.Fatalf("update conversation language: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	preview, err := service.TranslatePreview(ctx, agent.ID, conversationID, "你好")
	if err != nil {
		t.Fatalf("translate preview: %v", err)
	}
	if preview.Dialect != dialect {
		t.Fatalf("预览方言 = %q, want %q", preview.Dialect, dialect)
	}
	select {
	case body := <-bodies:
		if !strings.Contains(body, "拉丁美洲西班牙语") {
			t.Fatalf("方言模式预览应带风格提示：%s", body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("等待翻译请求超时")
	}
}
