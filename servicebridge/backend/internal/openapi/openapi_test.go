package openapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"customer-service/backend/internal/app"
	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

type testEnv struct {
	server     *httptest.Server
	store      store.Store
	adminToken string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	memoryStore := store.NewMemoryStore(store.Options{})
	hub := realtime.NewHub()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := app.NewService(memoryStore, hub, logger)
	openService := NewService(Options{
		Store:            memoryStore,
		App:              service,
		Logger:           logger,
		Enabled:          true,
		DefaultRateLimit: DefaultRateLimit,
		ResolveIP:        func(r *http.Request) (string, string) { return "203.0.113.9", "direct" },
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/api-apps", openService.HandleAdminApps)
	mux.HandleFunc("POST /api/admin/api-apps", openService.HandleAdminApps)
	mux.HandleFunc("PATCH /api/admin/api-apps/{id}", openService.HandleAdminApp)
	mux.HandleFunc("DELETE /api/admin/api-apps/{id}", openService.HandleAdminApp)
	mux.HandleFunc("POST /api/admin/api-apps/{id}/reset-key", openService.HandleAdminAppResetKey)
	mux.HandleFunc("OPTIONS /open/", openService.HandleOpenCORS)
	mux.HandleFunc("GET /open/v1/me", openService.HandleMe)
	mux.HandleFunc("GET /open/v1/conversations", openService.HandleConversations)
	mux.HandleFunc("POST /open/v1/conversations", openService.HandleConversations)
	mux.HandleFunc("GET /open/v1/conversations/{id}", openService.HandleConversation)
	mux.HandleFunc("GET /open/v1/conversations/{id}/messages", openService.HandleConversationMessages)
	mux.HandleFunc("POST /open/v1/conversations/{id}/messages", openService.HandleConversationMessages)
	mux.HandleFunc("POST /open/v1/conversations/{id}/close", openService.HandleConversationClose)
	mux.HandleFunc("GET /open/v1/ws", openService.HandleWebSocket)

	session, _, err := memoryStore.LoginAdmin("superadmin", "123456")
	if err != nil {
		t.Fatalf("login admin: %v", err)
	}
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &testEnv{server: server, store: memoryStore, adminToken: session.Token}
}

func (env *testEnv) do(t *testing.T, method, path, token string, body any) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, env.server.URL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	out := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("unmarshal %s %s response %q: %v", method, path, string(raw), err)
		}
	}
	return resp.StatusCode, out
}

func (env *testEnv) createApp(t *testing.T, name string) (appID, apiKey string) {
	t.Helper()
	status, payload := env.do(t, http.MethodPost, "/api/admin/api-apps", env.adminToken, map[string]any{"name": name, "note": "test"})
	if status != http.StatusCreated {
		t.Fatalf("create app status = %d, payload = %v", status, payload)
	}
	apiKey, _ = payload["api_key"].(string)
	record := payload["app"].(map[string]any)
	return record["id"].(string), apiKey
}

func TestGenerateAPIKey(t *testing.T) {
	plaintext, prefix, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	if !strings.HasPrefix(plaintext, KeyPrefix) || len(plaintext) != len(KeyPrefix)+keyRandomBytes*2 {
		t.Fatalf("unexpected plaintext shape: %s", plaintext)
	}
	if !strings.HasPrefix(prefix, KeyPrefix) {
		t.Fatalf("prefix should start with %s, got %s", KeyPrefix, prefix)
	}
	if HashAPIKey(plaintext) != hash {
		t.Fatal("hash mismatch")
	}
	other, _, otherHash, _ := GenerateAPIKey()
	if other == plaintext || otherHash == hash {
		t.Fatal("generated keys must be unique")
	}
}

func TestAdminAppLifecycleAndOpenFlow(t *testing.T) {
	env := newTestEnv(t)
	appID, apiKey := env.createApp(t, "合作平台 A")

	status, payload := env.do(t, http.MethodGet, "/open/v1/me", apiKey, nil)
	if status != http.StatusOK {
		t.Fatalf("me status = %d", status)
	}
	if payload["app"].(map[string]any)["id"] != appID {
		t.Fatalf("me returned wrong app: %v", payload)
	}

	status, _ = env.do(t, http.MethodGet, "/open/v1/me", "ast_live_bogus", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("invalid key status = %d, want 401", status)
	}

	status, payload = env.do(t, http.MethodPost, "/open/v1/conversations", apiKey, map[string]any{
		"external_user_id": "partner-user-1",
		"lang":             "en",
	})
	if status != http.StatusCreated {
		t.Fatalf("create conversation status = %d, payload = %v", status, payload)
	}
	conversation := payload["conversation"].(map[string]any)
	conversationID := conversation["id"].(string)
	if conversation["source"] != "api:"+appID {
		t.Fatalf("conversation source = %v", conversation["source"])
	}
	if conversation["visitor_remark"] != "partner-user-1" {
		t.Fatalf("visitor remark = %v", conversation["visitor_remark"])
	}
	if _, hasToken := payload["token"]; hasToken {
		t.Fatal("open api must not leak visitor tokens")
	}

	status, payload = env.do(t, http.MethodGet, "/open/v1/conversations", apiKey, nil)
	if status != http.StatusOK {
		t.Fatalf("list status = %d", status)
	}
	if list := payload["conversations"].([]any); len(list) != 1 {
		t.Fatalf("list len = %d", len(list))
	}

	status, payload = env.do(t, http.MethodPost, "/open/v1/conversations/"+conversationID+"/messages", apiKey, map[string]any{
		"client_msg_id": "m-1",
		"content":       "Hello, I want a quote",
	})
	if status != http.StatusCreated {
		t.Fatalf("send message status = %d, payload = %v", status, payload)
	}
	if payload["message"].(map[string]any)["content"] != "Hello, I want a quote" {
		t.Fatalf("message echo = %v", payload["message"])
	}

	// AI 接待回复由服务端异步生成（见 app.AddVisitorMessage 里的 go generateAIReply），
	// 这里必须轮询等待落库，不能在发出消息后立刻断言消息条数（否则是竞态）。
	var messages []any
	deadline := time.Now().Add(2 * time.Second)
	for {
		status, payload = env.do(t, http.MethodGet, "/open/v1/conversations/"+conversationID+"/messages?limit=50", apiKey, nil)
		if status != http.StatusOK {
			t.Fatalf("messages status = %d", status)
		}
		messages = payload["messages"].([]any)
		if len(messages) >= 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(messages) < 2 {
		t.Fatalf("expected entry reply + visitor message, got %d", len(messages))
	}

	otherAppID, otherKey := env.createApp(t, "合作平台 B")
	status, _ = env.do(t, http.MethodGet, "/open/v1/conversations/"+conversationID, otherKey, nil)
	if status != http.StatusNotFound {
		t.Fatalf("cross-app access status = %d, want 404", status)
	}
	_ = otherAppID

	status, payload = env.do(t, http.MethodPost, "/open/v1/conversations/"+conversationID+"/close", apiKey, nil)
	if status != http.StatusOK {
		t.Fatalf("close status = %d", status)
	}
	if payload["conversation"].(map[string]any)["status"] != "closed" {
		t.Fatalf("close result = %v", payload["conversation"])
	}

	status, payload = env.do(t, http.MethodPatch, "/api/admin/api-apps/"+appID, env.adminToken, map[string]any{
		"name": "合作平台 A 改名", "enabled": false, "rate_limit": 0, "note": "",
	})
	if status != http.StatusOK {
		t.Fatalf("patch status = %d, payload = %v", status, payload)
	}
	status, _ = env.do(t, http.MethodGet, "/open/v1/me", apiKey, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("disabled app status = %d, want 401", status)
	}

	status, payload = env.do(t, http.MethodPost, "/api/admin/api-apps/"+appID+"/reset-key", env.adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("reset key status = %d", status)
	}
	newKey := payload["api_key"].(string)
	status, _ = env.do(t, http.MethodGet, "/open/v1/me", newKey, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("reset key on disabled app should stay unauthorized, got %d", status)
	}

	status, _ = env.do(t, http.MethodDelete, "/api/admin/api-apps/"+appID, env.adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("delete status = %d", status)
	}
	status, payload = env.do(t, http.MethodGet, "/api/admin/api-apps", env.adminToken, nil)
	apps := payload["apps"].([]any)
	if status != http.StatusOK || len(apps) != 1 {
		t.Fatalf("list after delete status = %d apps = %d", status, len(apps))
	}

	audited := false
	for _, event := range env.store.RecentAuditEvents(50) {
		if strings.HasPrefix(event.Action, "api_app.") {
			audited = true
		}
	}
	if !audited {
		t.Fatal("expected api_app audit events")
	}
}

func TestOpenAPIRateLimit(t *testing.T) {
	env := newTestEnv(t)
	appID, apiKey := env.createApp(t, "限速平台")
	if status, payload := env.do(t, http.MethodPatch, "/api/admin/api-apps/"+appID, env.adminToken, map[string]any{
		"name": "限速平台", "rate_limit": 2, "note": "",
	}); status != http.StatusOK {
		t.Fatalf("patch rate limit status = %d, payload = %v", status, payload)
	}

	var lastStatus int
	for i := 0; i < 3; i++ {
		lastStatus, _ = env.do(t, http.MethodGet, "/open/v1/me", apiKey, nil)
	}
	if lastStatus != http.StatusTooManyRequests {
		t.Fatalf("third rapid request status = %d, want 429", lastStatus)
	}
}

func TestOpenAPIWebSocketSubscription(t *testing.T) {
	env := newTestEnv(t)
	appID, apiKey := env.createApp(t, "实时平台")

	status, payload := env.do(t, http.MethodPost, "/open/v1/conversations", apiKey, map[string]any{"lang": "zh"})
	if status != http.StatusCreated {
		t.Fatalf("create conversation status = %d", status)
	}
	conversationID := payload["conversation"].(map[string]any)["id"].(string)

	wsURL := "ws" + strings.TrimPrefix(env.server.URL, "http") + "/open/v1/ws?conversation_id=" + conversationID + "&key=" + apiKey
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial open ws: %v", err)
	}
	defer conn.Close()

	var connected map[string]any
	if err := conn.ReadJSON(&connected); err != nil {
		t.Fatalf("read connected event: %v", err)
	}
	if connected["event"] != "connected" {
		t.Fatalf("first event = %v", connected)
	}

	if status, _ := env.do(t, http.MethodPost, "/open/v1/conversations/"+conversationID+"/messages", apiKey, map[string]any{
		"content": "实时性测试消息",
	}); status != http.StatusCreated {
		t.Fatalf("send message status = %d", status)
	}

	deadline := time.Now().Add(5 * time.Second)
	_ = conn.SetReadDeadline(deadline)
	gotAck := false
	for !gotAck {
		var event struct {
			Event string         `json:"event"`
			Data  map[string]any `json:"data"`
		}
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatalf("read ws event: %v", err)
		}
		if event.Event == "message.ack" && event.Data["content"] == "实时性测试消息" {
			gotAck = true
		}
	}

	// 上行事件应被拒绝（只读订阅）。
	if err := conn.WriteJSON(map[string]any{"event": "message.send", "data": map[string]any{"content": "hack"}}); err != nil {
		t.Fatalf("write upstream event: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		var event struct {
			Event string         `json:"event"`
			Data  map[string]any `json:"data"`
		}
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatalf("read rejection event: %v", err)
		}
		if event.Event == "error" {
			break
		}
	}
	_ = appID
}

func TestOpenAPIWebSocketGuards(t *testing.T) {
	env := newTestEnv(t)
	appID, apiKey := env.createApp(t, "守卫平台")

	_, payload := env.do(t, http.MethodPost, "/open/v1/conversations", apiKey, nil)
	conversationID := payload["conversation"].(map[string]any)["id"].(string)

	otherAppID, otherKey := env.createApp(t, "其他平台")
	_, otherPayload := env.do(t, http.MethodPost, "/open/v1/conversations", otherKey, nil)
	otherConversationID := otherPayload["conversation"].(map[string]any)["id"].(string)

	base := "ws" + strings.TrimPrefix(env.server.URL, "http") + "/open/v1/ws"
	if _, resp, err := websocket.DefaultDialer.Dial(base+"?conversation_id="+otherConversationID+"&key="+apiKey, nil); err == nil {
		t.Fatal("expected subscription to foreign conversation to fail")
	} else if resp != nil && resp.StatusCode != http.StatusNotFound {
		t.Fatalf("foreign subscription status = %d", resp.StatusCode)
	}
	if _, resp, err := websocket.DefaultDialer.Dial(base+"?key="+apiKey, nil); err == nil {
		t.Fatal("expected missing conversation_id to fail")
	} else if resp != nil && resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing conversation status = %d", resp.StatusCode)
	}
	_ = appID
	_ = otherAppID
	_ = conversationID
}

func TestRequireConversationOwnership(t *testing.T) {
	other := domain.Conversation{ID: "conv_x", Source: "web"}
	apiApp := domain.APIApp{ID: "api_123"}
	if conversationOwnedBy(other, apiApp) {
		t.Fatal("web conversation must not be owned by api app")
	}
	owned := domain.Conversation{ID: "conv_y", Source: "api:api_123"}
	if !conversationOwnedBy(owned, apiApp) {
		t.Fatal("api conversation should be owned by its app")
	}
}

// TestOpenAPIConversationVisitorRef 覆盖开放 API 接受可选 visitor_ref 并立即建画像。
func TestOpenAPIConversationVisitorRef(t *testing.T) {
	env := newTestEnv(t)
	_, apiKey := env.createApp(t, "画像对接平台")
	const ref = "av_openapi_ref_0001"

	status, payload := env.do(t, http.MethodPost, "/open/v1/conversations", apiKey, map[string]any{
		"lang": "zh", "visitor_ref": ref,
	})
	if status != http.StatusCreated {
		t.Fatalf("create conversation status = %d payload = %v", status, payload)
	}
	conversation := payload["conversation"].(map[string]any)
	if conversation["profile_visitor_id"] != ref {
		t.Fatalf("会话画像键 = %v，want %q", conversation["profile_visitor_id"], ref)
	}
	profile, ok := env.store.VisitorProfile(ref)
	if !ok {
		t.Fatal("开放 API 建会话后画像应立即存在")
	}
	if profile.Visits != 1 {
		t.Fatalf("开放 API 建会话后 visits = %d，want 1", profile.Visits)
	}

	// 非法 visitor_ref：忽略且不影响建会话。
	status, payload = env.do(t, http.MethodPost, "/open/v1/conversations", apiKey, map[string]any{
		"lang": "zh", "visitor_ref": "av bad/ref!!",
	})
	if status != http.StatusCreated {
		t.Fatalf("非法 visitor_ref 不应影响建会话：status = %d payload = %v", status, payload)
	}
	if _, exists := payload["conversation"].(map[string]any)["profile_visitor_id"]; exists {
		t.Fatalf("非法 visitor_ref 应被忽略：%v", payload["conversation"])
	}
	if _, ok := env.store.VisitorProfile("av bad/ref!!"); ok {
		t.Fatal("非法 visitor_ref 不应创建画像")
	}
}
