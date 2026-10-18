package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

// newChatCompletionStub 起一个 OpenAI 兼容上游，统计 /chat/completions 命中次数。
func newChatCompletionStub(t *testing.T, reply string) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": reply}},
			},
		})
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

func newAIProviderService(t *testing.T, globalURL string) (*Service, store.Store) {
	t.Helper()
	st := store.NewMemoryStore(store.Options{})
	if _, err := st.UpdateAISettings(domain.AISettings{
		Enabled:        true,
		Mode:           domain.AIModeHumanFirst,
		BaseURL:        globalURL,
		APIKey:         "global-key",
		Model:          "global-model",
		APIType:        domain.AIAPITypeChatCompletions,
		TimeoutSeconds: 5,
	}); err != nil {
		t.Fatalf("update ai settings: %v", err)
	}
	return NewService(st, realtime.NewHub(), slog.Default()), st
}

// TestAIBindingPriorityAndFallback：绑定供应商时请求打到绑定桩；
// 禁用/删除供应商后回退到 ai_settings 的桩。
func TestAIBindingPriorityAndFallback(t *testing.T) {
	globalStub, globalCalls := newChatCompletionStub(t, "global-reply")
	boundStub, boundCalls := newChatCompletionStub(t, "bound-reply")
	service, st := newAIProviderService(t, globalStub.URL)

	provider, err := st.CreateAIProvider(domain.AIProvider{
		Name:    "绑定供应商",
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
		Capability: domain.AICapabilityTranslation,
		ProviderID: provider.ID,
		Model:      "bound-model",
	}); err != nil {
		t.Fatalf("upsert binding: %v", err)
	}

	translated := service.translateMessageWithStyle("es", "zh", domain.MessageText, "Hola, necesito ayuda", "")
	if got := translated["zh"]; got != "bound-reply" {
		t.Fatalf("绑定供应商译文 = %q, want bound-reply", got)
	}
	if atomic.LoadInt32(boundCalls) != 1 || atomic.LoadInt32(globalCalls) != 0 {
		t.Fatalf("首次调用命中次数 bound=%d global=%d，期望 1/0",
			atomic.LoadInt32(boundCalls), atomic.LoadInt32(globalCalls))
	}

	// 禁用供应商 → 自动回退 ai_settings。
	disabled := provider
	disabled.Enabled = false
	if _, err := st.UpdateAIProvider(provider.ID, disabled, nil); err != nil {
		t.Fatalf("disable provider: %v", err)
	}
	translated = service.translateMessageWithStyle("es", "zh", domain.MessageText, "Hola de nuevo", "")
	if got := translated["zh"]; got != "global-reply" {
		t.Fatalf("禁用后译文 = %q, want global-reply", got)
	}
	if atomic.LoadInt32(boundCalls) != 1 || atomic.LoadInt32(globalCalls) != 1 {
		t.Fatalf("禁用后命中次数 bound=%d global=%d，期望 1/1",
			atomic.LoadInt32(boundCalls), atomic.LoadInt32(globalCalls))
	}

	// 重新启用并删除供应商 → 绑定被清理并回退。
	enabled := provider
	enabled.Enabled = true
	if _, err := st.UpdateAIProvider(provider.ID, enabled, nil); err != nil {
		t.Fatalf("re-enable provider: %v", err)
	}
	if err := st.DeleteAIProvider(provider.ID); err != nil {
		t.Fatalf("delete provider: %v", err)
	}
	translated = service.translateMessageWithStyle("es", "zh", domain.MessageText, "Hola otra vez", "")
	if got := translated["zh"]; got != "global-reply" {
		t.Fatalf("删除后译文 = %q, want global-reply", got)
	}
	if atomic.LoadInt32(boundCalls) != 1 || atomic.LoadInt32(globalCalls) != 2 {
		t.Fatalf("删除后命中次数 bound=%d global=%d，期望 1/2",
			atomic.LoadInt32(boundCalls), atomic.LoadInt32(globalCalls))
	}
}

// TestAIProviderHostWhitelistRejectsCall：绑定供应商 allowed_hosts 与 base_url 不匹配时，
// 调用必须返回错误且两个上游都不会收到请求。
func TestAIProviderHostWhitelistRejectsCall(t *testing.T) {
	globalStub, globalCalls := newChatCompletionStub(t, "global-reply")
	boundStub, boundCalls := newChatCompletionStub(t, "bound-reply")
	service, st := newAIProviderService(t, globalStub.URL)

	provider, err := st.CreateAIProvider(domain.AIProvider{
		Name:         "受限供应商",
		Kind:         domain.AIProviderKindText,
		APIType:      domain.AIAPITypeChatCompletions,
		BaseURL:      boundStub.URL, // 127.0.0.1
		Models:       []string{"bound-model"},
		AllowedHosts: "example.com",
		Enabled:      true,
	}, "bound-key")
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if _, err := st.UpsertAIBinding(domain.AIBinding{
		Capability: domain.AICapabilityTranslation,
		ProviderID: provider.ID,
		Model:      "bound-model",
	}); err != nil {
		t.Fatalf("upsert binding: %v", err)
	}

	visitor, err := st.CreateVisitorConversation("192.0.2.10", "web", "en", "direct", "")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	_, err = service.TranslatePreview(context.Background(), "agent_missing", visitor.Conversation.ID, "Hola, necesito ayuda")
	if !errors.Is(err, ErrAIProviderHostNotAllowed) {
		t.Fatalf("白名单不通过应返回 ErrAIProviderHostNotAllowed，实际 %v", err)
	}
	if atomic.LoadInt32(boundCalls) != 0 || atomic.LoadInt32(globalCalls) != 0 {
		t.Fatalf("白名单拒绝后不应发出任何请求：bound=%d global=%d",
			atomic.LoadInt32(boundCalls), atomic.LoadInt32(globalCalls))
	}

	// aiSettingsFor 是宽容版本：记录日志并回退到全局配置。
	fallback := service.aiSettingsFor(domain.AICapabilityTranslation)
	if fallback.BaseURL != globalStub.URL {
		t.Fatalf("aiSettingsFor 应回退到全局 base_url = %q，实际 %q", globalStub.URL, fallback.BaseURL)
	}

	// 白名单改为匹配 127.0.0.1 后，请求正常打到绑定供应商。
	allowed := provider
	allowed.AllowedHosts = "127.0.0.1"
	if _, err := st.UpdateAIProvider(provider.ID, allowed, nil); err != nil {
		t.Fatalf("update allowed hosts: %v", err)
	}
	preview, err := service.TranslatePreview(context.Background(), "agent_missing", visitor.Conversation.ID, "Hola, necesito ayuda")
	if err != nil {
		t.Fatalf("白名单匹配后翻译失败：%v", err)
	}
	if preview.TranslatedText != "bound-reply" {
		t.Fatalf("译文 = %q, want bound-reply", preview.TranslatedText)
	}
	if atomic.LoadInt32(boundCalls) != 1 || atomic.LoadInt32(globalCalls) != 0 {
		t.Fatalf("白名单匹配后命中次数 bound=%d global=%d，期望 1/0",
			atomic.LoadInt32(boundCalls), atomic.LoadInt32(globalCalls))
	}
}
