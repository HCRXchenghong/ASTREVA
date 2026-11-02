package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"customer-service/backend/internal/app"
	"customer-service/backend/internal/config"
	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

// newAgentPreferencesRouter 构建一个可访问底层 store 的测试路由。
func newAgentPreferencesRouter(t *testing.T) (http.Handler, store.Store) {
	t.Helper()
	st := store.NewMemoryStore(store.Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	// 关闭营业时间限制，避免用例依赖运行时刻。
	_, _ = st.UpdateBusinessHours(domain.BusinessHours{Timezone: "Asia/Shanghai", Enabled: false})
	service := app.NewService(st, realtime.NewHub(), slog.Default())
	return NewRouter(config.Config{Env: "test", InternalAPIToken: "test-internal-token"}, slog.Default(), service), st
}

// loginAgentPreferencesFixture 创建并登录一个客服账号。
func loginAgentPreferencesFixture(t *testing.T, router http.Handler, account string) string {
	t.Helper()
	adminToken := loginForToken(t, router, "/api/admin/login", "superadmin", "123456")
	doJSON(t, router, http.MethodPost, "/api/admin/agents", adminToken, map[string]any{
		"account":           account,
		"password":          "AgentPass-123456",
		"name":              "偏好接口客服",
		"group":             "售前组",
		"max_conversations": 5,
	}, http.StatusCreated)
	return loginForToken(t, router, "/api/agent/login", account, "AgentPass-123456")
}

// TestAgentPreferencesDefaultAndPartialPatch：
// 默认值、局部更新、非法 dialect 400。
func TestAgentPreferencesDefaultAndPartialPatch(t *testing.T) {
	router, st := newAgentPreferencesRouter(t)
	agentToken := loginAgentPreferencesFixture(t, router, "kf_pref")

	initial := doJSON(t, router, http.MethodGet, "/api/agent/preferences", agentToken, nil, http.StatusOK)
	preferences, ok := initial["preferences"].(map[string]any)
	if !ok {
		t.Fatalf("preferences 字段缺失：%#v", initial)
	}
	if preferences["auto_translate_incoming"] != true ||
		preferences["auto_translate_outgoing"] != true ||
		preferences["review_translation_before_send"] != true ||
		preferences["ai_suggest_replies"] != true {
		t.Fatalf("布尔偏好默认值应为 true：%#v", preferences)
	}
	if preferences["translation_dialect_mode"] != domain.DialectModeStandard {
		t.Fatalf("默认方言模式 = %v, want standard", preferences["translation_dialect_mode"])
	}

	patched := doJSON(t, router, http.MethodPatch, "/api/agent/preferences", agentToken, map[string]any{
		"auto_translate_incoming":  false,
		"translation_dialect_mode": "dialect",
	}, http.StatusOK)
	updated, ok := patched["preferences"].(map[string]any)
	if !ok {
		t.Fatalf("patched preferences 缺失：%#v", patched)
	}
	// 局部更新：只改提供的字段，其它字段保持原值（默认 true）。
	if updated["auto_translate_incoming"] != false {
		t.Fatalf("auto_translate_incoming 应更新为 false：%#v", updated)
	}
	if updated["auto_translate_outgoing"] != true || updated["review_translation_before_send"] != true {
		t.Fatalf("未提供的字段不应被修改：%#v", updated)
	}
	if updated["translation_dialect_mode"] != domain.DialectModeDialect {
		t.Fatalf("translation_dialect_mode = %v, want dialect", updated["translation_dialect_mode"])
	}

	// 非法方言模式必须 400，且不改变已保存的值。
	doJSON(t, router, http.MethodPatch, "/api/agent/preferences", agentToken, map[string]any{
		"translation_dialect_mode": "klingon",
	}, http.StatusBadRequest)
	stored := doJSON(t, router, http.MethodGet, "/api/agent/preferences", agentToken, nil, http.StatusOK)
	if got := stored["preferences"].(map[string]any)["translation_dialect_mode"]; got != domain.DialectModeDialect {
		t.Fatalf("非法值不应写入：%v", got)
	}

	// 非客服身份不能读写偏好。
	adminToken := loginForToken(t, router, "/api/admin/login", "superadmin", "123456")
	doJSON(t, router, http.MethodGet, "/api/agent/preferences", adminToken, nil, http.StatusForbidden)

	// 底层存储与接口返回一致。
	agents := st.AllAgents()
	if len(agents) == 0 {
		t.Fatal("agent missing in store")
	}
	readBack, err := st.AgentPreferences(agents[0].ID)
	if err != nil {
		t.Fatalf("read preferences: %v", err)
	}
	if readBack.AutoTranslateIncoming == nil || *readBack.AutoTranslateIncoming {
		t.Fatalf("store 读回 auto_translate_incoming 应为 false：%+v", readBack)
	}
	if readBack.DialectMode != domain.DialectModeDialect {
		t.Fatalf("store 读回方言模式 = %q, want dialect", readBack.DialectMode)
	}
}

// TestAgentTranslatePreviewReturnsTranslationAndPersistsNothing：
// 预览返回译文，且不产生任何新消息。
func TestAgentTranslatePreviewReturnsTranslationAndPersistsNothing(t *testing.T) {
	router, st := newAgentPreferencesRouter(t)
	upstream := newPreviewUpstream(t, "Hola, ¿en qué puedo ayudarte?", http.StatusOK)
	if _, err := st.UpdateAISettings(domain.AISettings{
		Enabled:        true,
		Mode:           domain.AIModeHumanFirst,
		BaseURL:        upstream.URL,
		APIKey:         "test-key",
		Model:          "test-model",
		APIType:        "chat_completions",
		TimeoutSeconds: 5,
	}); err != nil {
		t.Fatalf("update ai settings: %v", err)
	}
	agentToken := loginAgentPreferencesFixture(t, router, "kf_preview")

	visitor := doJSON(t, router, http.MethodPost, "/api/visitor/conversations", "", map[string]any{
		"source": "preview-test",
		"lang":   "es",
	}, http.StatusCreated)
	conversationID, ok := visitor["conversation"].(map[string]any)["id"].(string)
	if !ok || conversationID == "" {
		t.Fatalf("conversation id missing: %#v", visitor)
	}
	before := len(st.Messages(conversationID))

	preview := doJSON(t, router, http.MethodPost, "/api/agent/translate-preview", agentToken, map[string]any{
		"conversation_id": conversationID,
		"content":         "你好，请问有什么可以帮您？",
	}, http.StatusOK)
	if preview["target_lang"] != "es" || preview["source_lang"] != "zh" {
		t.Fatalf("预览语言 = %v->%v, want zh->es", preview["source_lang"], preview["target_lang"])
	}
	if preview["translated_text"] != "Hola, ¿en qué puedo ayudarte?" {
		t.Fatalf("预览译文 = %v", preview["translated_text"])
	}
	if after := len(st.Messages(conversationID)); after != before {
		t.Fatalf("预览不应产生新消息：before=%d after=%d", before, after)
	}
}

// TestAgentTranslatePreviewFailureReturnsBadGateway：
// 翻译上游失败返回 502，且不写库。
func TestAgentTranslatePreviewFailureReturnsBadGateway(t *testing.T) {
	router, st := newAgentPreferencesRouter(t)
	upstream := newPreviewUpstream(t, "boom", http.StatusInternalServerError)
	if _, err := st.UpdateAISettings(domain.AISettings{
		Enabled:        true,
		Mode:           domain.AIModeHumanFirst,
		BaseURL:        upstream.URL,
		APIKey:         "test-key",
		Model:          "test-model",
		APIType:        "chat_completions",
		TimeoutSeconds: 5,
	}); err != nil {
		t.Fatalf("update ai settings: %v", err)
	}
	agentToken := loginAgentPreferencesFixture(t, router, "kf_preview_fail")

	visitor := doJSON(t, router, http.MethodPost, "/api/visitor/conversations", "", map[string]any{
		"source": "preview-test",
		"lang":   "es",
	}, http.StatusCreated)
	conversationID := visitor["conversation"].(map[string]any)["id"].(string)
	before := len(st.Messages(conversationID))

	failed := doJSON(t, router, http.MethodPost, "/api/agent/translate-preview", agentToken, map[string]any{
		"conversation_id": conversationID,
		"content":         "你好",
	}, http.StatusBadGateway)
	if failed["error"] != "translation_failed" {
		t.Fatalf("502 响应应带错误信息：%#v", failed)
	}
	if after := len(st.Messages(conversationID)); after != before {
		t.Fatalf("失败的预览不应写库：before=%d after=%d", before, after)
	}
}

// newPreviewUpstream 起一个 OpenAI 兼容的测试上游。
func newPreviewUpstream(t *testing.T, content string, status int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			http.Error(w, content, status)
			return
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
	return server
}
