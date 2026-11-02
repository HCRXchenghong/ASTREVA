package app

import (
	"encoding/json"
	"fmt"
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

// newTranslationStub 起一个 OpenAI 兼容的翻译上游，按翻译调用次数返回不同译文。
// 计数只统计翻译请求（system prompt 含"翻译专家"），避免 AI 自动回复并发打到同一上游时
// 干扰"第几次翻译"的断言。
func newTranslationStub(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		content := "AI 自动回复"
		if strings.Contains(string(raw), "翻译专家") {
			count := atomic.AddInt32(&calls, 1)
			content = fmt.Sprintf("中文译文%d", count)
		}
		payload := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": content}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

func newTranslationService(t *testing.T) (*Service, store.Store, *int32) {
	t.Helper()
	server, calls := newTranslationStub(t)
	st := store.NewMemoryStore(store.Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
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
	return NewService(st, realtime.NewHub(), slog.Default()), st, calls
}

// waitForTranslation 轮询等待异步翻译落地（不阻塞消息投递是本期的设计目标）。
func waitForTranslation(t *testing.T, st store.Store, conversationID, serverMsgID, targetLang string) domain.Message {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, msg := range st.Messages(conversationID) {
			if msg.ServerMsgID != serverMsgID {
				continue
			}
			if text := strings.TrimSpace(msg.Translations[targetLang]); text != "" {
				return msg
			}
			if msg.TranslationStatus == "failed" {
				t.Fatalf("翻译状态为 failed：%+v", msg)
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等待 %s 译文超时（server_msg_id=%s）", targetLang, serverMsgID)
	return domain.Message{}
}

func TestVisitorMessageTranslatedWithDetectedLanguage(t *testing.T) {
	service, st, calls := newTranslationService(t)
	visitor, err := st.CreateVisitorConversation("192.0.2.90", "web", "es", "direct", "")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	conversationID := visitor.Conversation.ID

	result, err := service.AddVisitorMessage(conversationID, "c1", "Hola, ¿cuál es el precio de este producto?", domain.MessageText)
	if err != nil {
		t.Fatalf("add visitor message: %v", err)
	}
	// 消息先落库并即时下发：此时译文尚未就绪，状态必须是 pending（访客不会被翻译阻塞）。
	if result.Input.TranslationStatus != "pending" {
		t.Fatalf("初始 translation_status = %q, want pending", result.Input.TranslationStatus)
	}
	if result.Input.DetectedLang != ai.LangEs {
		t.Fatalf("detected_lang = %q, want %q", result.Input.DetectedLang, ai.LangEs)
	}
	translated := waitForTranslation(t, st, conversationID, result.Input.ServerMsgID, ai.LangZh)
	if atomic.LoadInt32(calls) == 0 {
		t.Fatal("西班牙语消息应触发一次翻译调用")
	}
	if got := translated.Translations[ai.LangZh]; got != "中文译文1" {
		t.Fatalf("中文译文 = %q, want %q", got, "中文译文1")
	}
	if translated.TranslationStatus != "done" {
		t.Fatalf("translation_status = %q, want done", translated.TranslationStatus)
	}
	result.Input = translated
	// 客服侧视图：默认中文译文 + 原文放到 OriginalContent（供「查看原文」）。
	agentView := AgentFacingMessage(result.Input)
	if agentView.Content != "中文译文1" {
		t.Fatalf("客服视图正文 = %q, want 中文译文", agentView.Content)
	}
	if agentView.OriginalContent != "Hola, ¿cuál es el precio de este producto?" {
		t.Fatalf("客服视图原文 = %q", agentView.OriginalContent)
	}
	// 访客侧视图：始终是访客自己的语言（原文），不带译文。
	visitorView := VisitorFacingMessageForLang(result.Input, ai.LangEs)
	if visitorView.Content != "Hola, ¿cuál es el precio de este producto?" {
		t.Fatalf("访客视图正文 = %q, want 原文", visitorView.Content)
	}
	if visitorView.Translations != nil {
		t.Fatal("访客视图不应携带译文映射")
	}
	// 会话语言状态已更新
	stored, ok := st.Conversation(conversationID)
	if !ok {
		t.Fatal("conversation missing")
	}
	if stored.DetectedLang != ai.LangEs {
		t.Fatalf("会话 detected_lang = %q, want %q", stored.DetectedLang, ai.LangEs)
	}
	if stored.LanguageStats[ai.LangEs] != 1 {
		t.Fatalf("会话语种统计 = %v, want es:1", stored.LanguageStats)
	}
}

func TestVisitorChineseMessageSkipsTranslation(t *testing.T) {
	service, st, calls := newTranslationService(t)
	visitor, _ := st.CreateVisitorConversation("192.0.2.91", "web", "zh", "direct", "")
	result, err := service.AddVisitorMessage(visitor.Conversation.ID, "c1", "你好，这个多少钱？", domain.MessageText)
	if err != nil {
		t.Fatalf("add visitor message: %v", err)
	}
	if atomic.LoadInt32(calls) != 0 {
		t.Fatal("中文消息不应调用翻译上游")
	}
	if result.Input.TranslationStatus != "skipped" {
		t.Fatalf("translation_status = %q, want skipped", result.Input.TranslationStatus)
	}
}

func TestReplyLanguageInstructionPersistsForConversation(t *testing.T) {
	service, st, _ := newTranslationService(t)
	visitor, _ := st.CreateVisitorConversation("192.0.2.92", "web", "ar", "direct", "")
	conversationID := visitor.Conversation.ID

	if _, err := service.AddVisitorMessage(conversationID, "c1", "مرحبا، أريد معرفة السعر", domain.MessageText); err != nil {
		t.Fatalf("add arabic message: %v", err)
	}
	if _, err := service.AddVisitorMessage(conversationID, "c2", "Please reply in Spanish from now on", domain.MessageText); err != nil {
		t.Fatalf("add instruction message: %v", err)
	}
	stored, _ := st.Conversation(conversationID)
	if stored.ReplyLangOverride != ai.LangEs {
		t.Fatalf("reply_lang_override = %q, want %q", stored.ReplyLangOverride, ai.LangEs)
	}
	if stored.ReplyLangOverrideAt == nil {
		t.Fatal("显式指令应记录生效时间")
	}
	// 后续消息继续遵循显式指令（整会话生效）。
	if got := conversationReplyLang(stored); got != ai.LangEs {
		t.Fatalf("会话回复语言 = %q, want %q", got, ai.LangEs)
	}
}

func TestDominantLanguageSwitchesReplyLanguage(t *testing.T) {
	service, st, _ := newTranslationService(t)
	visitor, _ := st.CreateVisitorConversation("192.0.2.93", "web", "en", "direct", "")
	conversationID := visitor.Conversation.ID

	messages := []string{
		"Hola, necesito una cotización",
		"¿Cuál es el precio? Gracias",
		"Hello, could you also send the catalogue?",
		"Gracias, quedo atento a su respuesta",
		"¿Me pueden enviar la ficha técnica?",
	}
	for index, content := range messages {
		if _, err := service.AddVisitorMessage(conversationID, fmt.Sprintf("m%d", index), content, domain.MessageText); err != nil {
			t.Fatalf("add message %d: %v", index, err)
		}
	}
	stored, _ := st.Conversation(conversationID)
	// 西班牙语 4 条 vs 英语 1 条 → 回复语言应为西语
	if stored.VisitorLang != ai.LangEs {
		t.Fatalf("会话回复语言 = %q, want %q（按用得多的语言）", stored.VisitorLang, ai.LangEs)
	}
	if stored.LanguageStats[ai.LangEs] != 4 || stored.LanguageStats[ai.LangEn] != 1 {
		t.Fatalf("语种统计 = %v, want es:4 en:1", stored.LanguageStats)
	}
}

func TestAgentMessageTranslatedFromDetectedAuthorLanguage(t *testing.T) {
	service, st, _ := newTranslationService(t)
	visitor, _ := st.CreateVisitorConversation("192.0.2.94", "web", "en", "direct", "")
	conversationID := visitor.Conversation.ID
	agent, err := st.CreateAgent(domain.Agent{Account: "lang_agent", Name: "语言客服", Group: "default", Status: domain.AgentOnline}, "AgentPass-123456")
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	adminSession, _, err := st.LoginAdmin("superadmin", "123456")
	if err != nil {
		t.Fatalf("login admin: %v", err)
	}
	// 先接管，再验证客服消息的自动翻译（未接管的会话不允许客服发言）。
	if _, err := service.TransferConversation(adminSession, conversationID, agent.ID, ""); err != nil {
		t.Fatalf("transfer conversation: %v", err)
	}
	// 客服写中文，访客语言是英语 → 应翻译成英语下发给访客。
	result, err := service.AddAgentMessage(agent.ID, conversationID, "a1", "您好，这款产品支持定制尺寸。", domain.MessageText)
	if err != nil {
		t.Fatalf("add agent message: %v", err)
	}
	if result.Input.AuthorLang != ai.LangZh {
		t.Fatalf("客服消息 author_lang = %q, want zh", result.Input.AuthorLang)
	}
	if result.Input.TranslationStatus != "pending" {
		t.Fatalf("初始 translation_status = %q, want pending", result.Input.TranslationStatus)
	}
	translated := waitForTranslation(t, st, conversationID, result.Input.ServerMsgID, ai.LangEn)
	view := VisitorFacingMessageForLang(translated, ai.LangEn)
	if view.Content == "您好，这款产品支持定制尺寸。" {
		t.Fatal("访客不应看到客服的中文原文")
	}
}
