package httpx

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"customer-service/backend/internal/app"
	"customer-service/backend/internal/config"
	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

func TestAdminAgentLifecycle(t *testing.T) {
	server := newTestRouter()

	adminToken := loginForToken(t, server, "/api/admin/login", "superadmin", "123456")
	created := doJSON(t, server, http.MethodPost, "/api/admin/agents", adminToken, map[string]any{
		"account":           "kf_lifecycle",
		"password":          "AgentPass-123456",
		"name":              "生命周期客服",
		"group":             "售前组",
		"max_conversations": 6,
	}, http.StatusCreated)
	agentID := created["agent"].(map[string]any)["id"].(string)

	doJSON(t, server, http.MethodPost, "/api/admin/agents", adminToken, map[string]any{
		"account":  "kf_lifecycle",
		"password": "AgentPass-123456",
		"name":     "重复账号",
	}, http.StatusConflict)

	patched := doJSON(t, server, http.MethodPatch, "/api/admin/agents/"+agentID, adminToken, map[string]any{
		"name":              "改名客服",
		"group":             "售后组",
		"max_conversations": 3,
	}, http.StatusOK)
	if got := patched["agent"].(map[string]any)["name"]; got != "改名客服" {
		t.Fatalf("unexpected patched name: %v", got)
	}

	doJSON(t, server, http.MethodPost, "/api/admin/agents/"+agentID+"/reset-password", adminToken, map[string]any{
		"password": "AgentPass-654321",
	}, http.StatusOK)
	generatedReset := doJSON(t, server, http.MethodPost, "/api/admin/agents/"+agentID+"/reset-password", adminToken, map[string]any{}, http.StatusOK)
	generatedPassword, ok := generatedReset["temporary_password"].(string)
	if !ok || len(generatedPassword) < 12 || generatedPassword == "123456" {
		t.Fatalf("expected generated temporary password, got %#v", generatedReset)
	}
	doJSON(t, server, http.MethodPost, "/api/agent/login", "", map[string]any{
		"account":  "kf_lifecycle",
		"password": generatedPassword,
	}, http.StatusOK)
	doJSON(t, server, http.MethodPost, "/api/admin/agents/"+agentID+"/disable", adminToken, map[string]any{}, http.StatusOK)
	doJSON(t, server, http.MethodPost, "/api/agent/login", "", map[string]any{
		"account":  "kf_lifecycle",
		"password": generatedPassword,
	}, http.StatusForbidden)
}

func TestAdminCreateAgentRejectsWeakPassword(t *testing.T) {
	server := newTestRouter()
	adminToken := loginForToken(t, server, "/api/admin/login", "superadmin", "123456")
	doJSON(t, server, http.MethodPost, "/api/admin/agents", adminToken, map[string]any{
		"account":           "kf_weak",
		"password":          "123456",
		"name":              "弱密码客服",
		"group":             "售前组",
		"max_conversations": 6,
	}, http.StatusBadRequest)
}

func TestHealthReadyAndMetrics(t *testing.T) {
	server := newTestRouter()
	doJSON(t, server, http.MethodGet, "/healthz", "", nil, http.StatusOK)
	doJSON(t, server, http.MethodGet, "/readyz", "", nil, http.StatusOK)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"customer_service_ws_connections", "customer_service_go_goroutines"} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics missing %q: %s", want, body)
		}
	}
}

func TestMetricsCanRequireBearerToken(t *testing.T) {
	server := newTestRouterWithConfig(config.Config{Env: "test", MetricsBearerToken: "metrics-secret"})
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("metrics without token status=%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer metrics-secret")
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics with token status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRateLimit(t *testing.T) {
	server := newTestRouterWithConfig(config.Config{Env: "test", RateLimitEnabled: true, RateLimitRPS: 0.1, RateLimitBurst: 1})
	doJSON(t, server, http.MethodGet, "/api/contact-settings", "", nil, http.StatusOK)
	req := httptest.NewRequest(http.MethodGet, "/api/contact-settings", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected rate limit, got status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRateLimitUsesRemoteIPWithoutPort(t *testing.T) {
	server := newTestRouterWithConfig(config.Config{Env: "test", RateLimitEnabled: true, RateLimitRPS: 0.1, RateLimitBurst: 1})

	req := httptest.NewRequest(http.MethodGet, "/api/contact-settings", nil)
	req.RemoteAddr = "203.0.113.10:30001"
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first request status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/contact-settings", nil)
	req.RemoteAddr = "203.0.113.10:30002"
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected same remote IP to be rate limited despite port change, got status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPublicContactSettingsIncludesEntryReplyAndQuickReplies(t *testing.T) {
	server := newTestRouter()
	adminToken := loginForToken(t, server, "/api/admin/login", "superadmin", "123456")

	doJSON(t, server, http.MethodPatch, "/api/admin/contact-settings", adminToken, map[string]any{
		"phone":       "400-000-0000",
		"entry_reply": "这里是后台配置的首条欢迎语。",
	}, http.StatusOK)

	created := doJSON(t, server, http.MethodPost, "/api/admin/keyword-rules", adminToken, map[string]any{
		"keyword":               "退款流程",
		"reply":                 "请提供订单号，我们为您处理退款。",
		"enabled":               true,
		"priority":              88,
		"action":                "text",
		"show_in_quick_replies": true,
		"quick_reply_text":      "怎么退款？",
	}, http.StatusCreated)
	ruleID := created["rule"].(map[string]any)["id"].(string)
	doJSON(t, server, http.MethodPatch, "/api/admin/keyword-rules/"+ruleID, adminToken, map[string]any{
		"keyword":               "退款流程",
		"reply":                 "请提供订单号，我们为您处理退款。",
		"enabled":               true,
		"priority":              88,
		"action":                "text",
		"show_in_quick_replies": true,
		"quick_reply_text":      "怎么退款？",
	}, http.StatusOK)

	req := httptest.NewRequest(http.MethodGet, "/api/contact-settings", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var out map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out["entry_reply"] != "这里是后台配置的首条欢迎语。" {
		t.Fatalf("unexpected entry_reply: %#v", out["entry_reply"])
	}
	quickReplies, ok := out["quick_replies"].([]any)
	if !ok || len(quickReplies) == 0 {
		t.Fatalf("expected quick_replies, got %#v", out["quick_replies"])
	}
	first, ok := quickReplies[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected quick reply shape: %#v", quickReplies[0])
	}
	if first["text"] != "怎么退款？" {
		t.Fatalf("unexpected quick reply text: %#v", first["text"])
	}
	if first["send_text"] != "退款流程" {
		t.Fatalf("unexpected quick reply send_text: %#v", first["send_text"])
	}
}

func TestVisitorMessagePageMasksRevokedAgentMessage(t *testing.T) {
	st := store.NewMemoryStore(store.Options{})
	service := app.NewService(st, realtime.NewHub(), slog.Default())
	router := NewRouter(config.Config{Env: "test"}, slog.Default(), service)

	agentSession, agent, err := st.LoginAgent("admin", "123456")
	if err != nil {
		t.Fatalf("login agent: %v", err)
	}
	if _, _, err := st.SetAgentStatus(agent.ID, domain.AgentOnline); err != nil {
		t.Fatalf("set agent online: %v", err)
	}
	visitor, err := st.CreateVisitorConversation("198.51.100.23", "web")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}

	result, err := st.AddAgentMessage(agent.ID, visitor.Conversation.ID, "agent-msg-1", "后台仍可见的原始内容", domain.MessageText)
	if err != nil {
		t.Fatalf("add agent message: %v", err)
	}
	if _, _, err := st.RevokeMessage(agentSession, visitor.Conversation.ID, result.Input.ServerMsgID); err != nil {
		t.Fatalf("revoke message: %v", err)
	}

	visitorOut := doJSON(t, router, http.MethodGet, "/api/conversations/"+visitor.Conversation.ID+"/messages?limit=20", visitor.Token, nil, http.StatusOK)
	visitorMessages, ok := visitorOut["messages"].([]any)
	if !ok || len(visitorMessages) == 0 {
		t.Fatalf("expected visitor messages, got %#v", visitorOut["messages"])
	}
	visitorMsg := visitorMessages[len(visitorMessages)-1].(map[string]any)
	if visitorMsg["message_type"] != string(domain.MessageRevoked) {
		t.Fatalf("expected revoked visitor message type, got %#v", visitorMsg["message_type"])
	}
	if visitorMsg["content"] != "对方撤回了一条消息" {
		t.Fatalf("unexpected visitor revoked content: %#v", visitorMsg["content"])
	}
	if _, ok := visitorMsg["revoked_by_id"]; ok {
		t.Fatalf("visitor payload should not expose revoke actor: %#v", visitorMsg)
	}

	adminSession, _, err := st.LoginAdmin("superadmin", "123456")
	if err != nil {
		t.Fatalf("login admin: %v", err)
	}
	adminOut := doJSON(t, router, http.MethodGet, "/api/conversations/"+visitor.Conversation.ID+"/messages?limit=20", adminSession.Token, nil, http.StatusOK)
	agentMessages, ok := adminOut["messages"].([]any)
	if !ok || len(agentMessages) == 0 {
		t.Fatalf("expected admin messages, got %#v", adminOut["messages"])
	}
	adminMsg := agentMessages[len(agentMessages)-1].(map[string]any)
	if adminMsg["content"] != "后台仍可见的原始内容" {
		t.Fatalf("admin should still see original content, got %#v", adminMsg["content"])
	}
	if adminMsg["message_type"] != string(domain.MessageText) {
		t.Fatalf("admin message type should remain text, got %#v", adminMsg["message_type"])
	}
	if _, ok := adminMsg["revoked_at"]; !ok {
		t.Fatalf("admin payload should include revoked_at: %#v", adminMsg)
	}
}

func TestLoginRateLimit(t *testing.T) {
	server := newTestRouterWithConfig(config.Config{Env: "test", RateLimitEnabled: true, RateLimitRPS: 100, RateLimitBurst: 100})
	for i := 0; i < 10; i++ {
		doJSON(t, server, http.MethodPost, "/api/admin/login", "", map[string]any{
			"account":  "superadmin",
			"password": "wrong-password",
		}, http.StatusUnauthorized)
	}
	doJSON(t, server, http.MethodPost, "/api/admin/login", "", map[string]any{
		"account":  "superadmin",
		"password": "wrong-password",
	}, http.StatusTooManyRequests)
}

func TestClientIPUsesFirstForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:30001"
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 198.51.100.22")
	if got := clientIP(req, parseTrustedProxyCIDRs(config.DefaultTrustedProxyCIDRs)); got != "203.0.113.9" {
		t.Fatalf("unexpected client IP: %q", got)
	}
}

func TestClientIPIgnoresForwardedForFromUntrustedRemote(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.1:30001"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := clientIP(req, parseTrustedProxyCIDRs(config.DefaultTrustedProxyCIDRs)); got != "198.51.100.1" {
		t.Fatalf("unexpected client IP: %q", got)
	}
}

func TestClientIPIgnoresInvalidForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:30001"
	req.Header.Set("X-Forwarded-For", "not-an-ip")
	if got := clientIP(req, parseTrustedProxyCIDRs(config.DefaultTrustedProxyCIDRs)); got != "127.0.0.1" {
		t.Fatalf("unexpected client IP: %q", got)
	}
}

func TestAdminActionWritesAuditEvent(t *testing.T) {
	server := newTestRouter()
	adminToken := loginForToken(t, server, "/api/admin/login", "superadmin", "123456")
	doJSON(t, server, http.MethodPatch, "/api/admin/business-hours", adminToken, map[string]any{
		"timezone": "Asia/Shanghai",
		"start":    "09:00",
		"end":      "18:00",
		"enabled":  true,
	}, http.StatusOK)
	out := doJSON(t, server, http.MethodGet, "/api/admin/audit-events?limit=5", adminToken, nil, http.StatusOK)
	events := out["events"].([]any)
	if len(events) == 0 {
		t.Fatal("expected audit events")
	}
	if got := events[0].(map[string]any)["action"]; got != "business_hours.update" {
		t.Fatalf("unexpected audit action: %v", got)
	}
}

func TestAccountPasswordChange(t *testing.T) {
	server := newTestRouter()
	adminToken := loginForToken(t, server, "/api/admin/login", "superadmin", "123456")

	doJSON(t, server, http.MethodPost, "/api/account/password", adminToken, map[string]any{
		"current_password": "bad-password",
		"new_password":     "NewAdminPass-0123",
	}, http.StatusUnauthorized)
	doJSON(t, server, http.MethodPost, "/api/account/password", adminToken, map[string]any{
		"current_password": "123456",
		"new_password":     "NewAdminPass-0123",
	}, http.StatusOK)
	doJSON(t, server, http.MethodGet, "/api/admin/conversations", adminToken, nil, http.StatusUnauthorized)
	doJSON(t, server, http.MethodPost, "/api/admin/login", "", map[string]any{
		"account":  "superadmin",
		"password": "123456",
	}, http.StatusUnauthorized)
	newAdminToken := loginForToken(t, server, "/api/admin/login", "superadmin", "NewAdminPass-0123")
	eventsOut := doJSON(t, server, http.MethodGet, "/api/admin/audit-events?limit=5", newAdminToken, nil, http.StatusOK)
	if events := eventsOut["events"].([]any); len(events) == 0 || events[0].(map[string]any)["action"] != "account.password_change" {
		t.Fatalf("expected password change audit event, got %#v", eventsOut)
	}

	agentToken := loginForToken(t, server, "/api/agent/login", "admin", "123456")
	doJSON(t, server, http.MethodPost, "/api/account/password", agentToken, map[string]any{
		"current_password": "123456",
		"new_password":     "NewAgentPass-0123",
	}, http.StatusOK)
	doJSON(t, server, http.MethodGet, "/api/agent/conversations", agentToken, nil, http.StatusUnauthorized)
	loginForToken(t, server, "/api/agent/login", "admin", "NewAgentPass-0123")
}

func TestAgentTokenRevokedAfterResetAndDisable(t *testing.T) {
	server := newTestRouter()
	adminToken := loginForToken(t, server, "/api/admin/login", "superadmin", "123456")
	created := doJSON(t, server, http.MethodPost, "/api/admin/agents", adminToken, map[string]any{
		"account":           "kf_revoke",
		"password":          "AgentPass-123456",
		"name":              "撤销测试客服",
		"group":             "售前组",
		"max_conversations": 6,
	}, http.StatusCreated)
	agentID := created["agent"].(map[string]any)["id"].(string)
	agentToken := loginForToken(t, server, "/api/agent/login", "kf_revoke", "AgentPass-123456")

	reset := doJSON(t, server, http.MethodPost, "/api/admin/agents/"+agentID+"/reset-password", adminToken, map[string]any{}, http.StatusOK)
	tempPassword := reset["temporary_password"].(string)
	doJSON(t, server, http.MethodGet, "/api/agent/conversations", agentToken, nil, http.StatusUnauthorized)

	nextAgentToken := loginForToken(t, server, "/api/agent/login", "kf_revoke", tempPassword)
	doJSON(t, server, http.MethodPost, "/api/admin/agents/"+agentID+"/disable", adminToken, map[string]any{}, http.StatusOK)
	doJSON(t, server, http.MethodGet, "/api/agent/conversations", nextAgentToken, nil, http.StatusUnauthorized)
	doJSON(t, server, http.MethodPost, "/api/agent/login", "", map[string]any{
		"account":  "kf_revoke",
		"password": tempPassword,
	}, http.StatusForbidden)
}

func TestAdminCSVExports(t *testing.T) {
	server := newTestRouter()
	adminToken := loginForToken(t, server, "/api/admin/login", "superadmin", "123456")
	doJSON(t, server, http.MethodPost, "/api/visitor/conversations", "", map[string]any{"source": "csv-test"}, http.StatusCreated)
	doJSON(t, server, http.MethodPatch, "/api/admin/business-hours", adminToken, map[string]any{
		"timezone": "Asia/Shanghai",
		"start":    "09:00",
		"end":      "18:00",
		"enabled":  true,
	}, http.StatusOK)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/conversations/export.csv", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("conversation csv status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/csv") {
		t.Fatalf("unexpected csv content-type: %q", got)
	}
	if body := rec.Body.String(); !strings.Contains(body, "visitor_id") || !strings.Contains(body, "csv-test") {
		t.Fatalf("unexpected conversation csv body: %s", body)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/audit-events/export.csv?limit=20", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit csv status=%d body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "action") || !strings.Contains(body, "business_hours.update") {
		t.Fatalf("unexpected audit csv body: %s", body)
	}
}

func TestAdminEndpointsRequireAdminToken(t *testing.T) {
	server := newTestRouter()
	doJSON(t, server, http.MethodGet, "/api/admin/agents", "", nil, http.StatusForbidden)
	agentToken := loginForToken(t, server, "/api/agent/login", "admin", "123456")
	doJSON(t, server, http.MethodGet, "/api/admin/agents", agentToken, nil, http.StatusForbidden)
}

func TestHTTPAPIDoesNotAcceptQueryToken(t *testing.T) {
	server := newTestRouter()
	adminToken := loginForToken(t, server, "/api/admin/login", "superadmin", "123456")
	doJSON(t, server, http.MethodGet, "/api/admin/conversations?token="+url.QueryEscape(adminToken), "", nil, http.StatusUnauthorized)
}

func TestJSONRejectsUnknownFields(t *testing.T) {
	server := newTestRouter()
	adminToken := loginForToken(t, server, "/api/admin/login", "superadmin", "123456")
	doJSON(t, server, http.MethodPatch, "/api/admin/business-hours", adminToken, map[string]any{
		"timezone":   "Asia/Shanghai",
		"start":      "09:00",
		"end":        "18:00",
		"enabled":    true,
		"unexpected": "field",
	}, http.StatusBadRequest)
}

func TestBusinessHoursRejectInvalidTime(t *testing.T) {
	server := newTestRouter()
	adminToken := loginForToken(t, server, "/api/admin/login", "superadmin", "123456")
	doJSON(t, server, http.MethodPatch, "/api/admin/business-hours", adminToken, map[string]any{
		"timezone": "Asia/Shanghai",
		"start":    "25:99",
		"end":      "18:00",
		"enabled":  true,
	}, http.StatusBadRequest)
}

func TestAdminTransferConversation(t *testing.T) {
	server := newTestRouter()

	adminToken := loginForToken(t, server, "/api/admin/login", "superadmin", "123456")
	visitor := doJSON(t, server, http.MethodPost, "/api/visitor/conversations", "", map[string]any{"source": "test"}, http.StatusCreated)
	conversationID := visitor["conversation"].(map[string]any)["id"].(string)

	out := doJSON(t, server, http.MethodPost, "/api/admin/conversations/"+conversationID+"/transfer", adminToken, map[string]any{
		"agent_id": "agent_lixue",
	}, http.StatusOK)
	conversation := out["conversation"].(map[string]any)
	if got := conversation["assigned_agent_id"]; got != "agent_lixue" {
		t.Fatalf("expected transfer to agent_lixue, got %v", got)
	}
}

func TestAgentPushDevice(t *testing.T) {
	server := newTestRouter()
	agentToken := loginForToken(t, server, "/api/agent/login", "admin", "123456")

	out := doJSON(t, server, http.MethodPost, "/api/agent/push-device", agentToken, map[string]any{
		"platform": "ios",
		"provider": "uni-push",
		"token":    "push-token-1",
	}, http.StatusOK)
	device := out["device"].(map[string]any)
	if got := device["token"]; got != "push-token-1" {
		t.Fatalf("unexpected push token: %v", got)
	}
}

func TestVisitorRatingAndAdminSummary(t *testing.T) {
	server := newTestRouter()
	adminToken := loginForToken(t, server, "/api/admin/login", "superadmin", "123456")
	visitor := doJSON(t, server, http.MethodPost, "/api/visitor/conversations", "", map[string]any{"source": "rating-test"}, http.StatusCreated)
	visitorToken := visitor["token"].(string)
	conversationID := visitor["conversation"].(map[string]any)["id"].(string)

	out := doJSON(t, server, http.MethodPost, "/api/visitor/conversations/"+conversationID+"/rating", visitorToken, map[string]any{
		"score":   5,
		"tags":    []string{"响应快", "专业"},
		"comment": "体验很好",
	}, http.StatusCreated)
	if got := out["rating"].(map[string]any)["score"]; got != float64(5) {
		t.Fatalf("unexpected rating score: %v", got)
	}
	doJSON(t, server, http.MethodPost, "/api/visitor/conversations/"+conversationID+"/rating", visitorToken, map[string]any{
		"score":   4,
		"comment": "重复评价",
	}, http.StatusConflict)

	summary := doJSON(t, server, http.MethodGet, "/api/admin/ratings/summary", adminToken, nil, http.StatusOK)
	if got := summary["total"]; got != float64(1) {
		t.Fatalf("unexpected rating total: %v", got)
	}
	dashboard := doJSON(t, server, http.MethodGet, "/api/admin/dashboard", adminToken, nil, http.StatusOK)
	if got := dashboard["rating"].(map[string]any)["total"]; got != float64(1) {
		t.Fatalf("unexpected dashboard rating total: %v", got)
	}
}

func TestUploadImage(t *testing.T) {
	uploadDir := t.TempDir()
	server := newTestRouterWithConfig(config.Config{Env: "test", UploadDir: uploadDir, UploadMaxBytes: 1024 * 1024})
	visitor := doJSON(t, server, http.MethodPost, "/api/visitor/conversations", "", map[string]any{"source": "upload-test"}, http.StatusCreated)
	visitorToken := visitor["token"].(string)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "avatar.png")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
		0x42, 0x60, 0x82,
	}); err != nil {
		t.Fatalf("write png: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/uploads", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+visitorToken)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out uploadResponse
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if out.URL == "" || out.Path == "" {
		t.Fatalf("missing upload fields: %#v", out)
	}
	fullPath := filepath.Join(uploadDir, filepath.FromSlash(out.Path))
	if _, err := os.Stat(fullPath); err != nil {
		t.Fatalf("uploaded file missing: %v", err)
	}
}

func TestWebSocketOriginWhitelist(t *testing.T) {
	router := newTestRouterWithConfig(config.Config{Env: "test", CORSAllowedOrigins: "https://allowed.test"})
	server := httptest.NewServer(router)
	defer server.Close()
	visitor := doJSON(t, router, http.MethodPost, "/api/visitor/conversations", "", map[string]any{"source": "origin-test"}, http.StatusCreated)
	token := visitor["token"].(string)
	conversationID := visitor["conversation"].(map[string]any)["id"].(string)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") +
		"/ws?role=visitor&token=" + url.QueryEscape(token) +
		"&conversation_id=" + url.QueryEscape(conversationID)

	evilHeader := http.Header{}
	evilHeader.Set("Origin", "https://evil.test")
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, evilHeader)
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected disallowed websocket origin to fail")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("expected 403 for disallowed origin, got status=%d err=%v", status, err)
	}

	allowedHeader := http.Header{}
	allowedHeader.Set("Origin", "https://allowed.test")
	conn, resp, err = websocket.DefaultDialer.Dial(wsURL, allowedHeader)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("expected allowed origin to connect, status=%d err=%v", status, err)
	}
	_ = conn.Close()
}

func newTestRouter() http.Handler {
	return newTestRouterWithConfig(config.Config{Env: "test"})
}

func newTestRouterWithConfig(cfg config.Config) http.Handler {
	st := store.NewMemoryStore(store.Options{})
	service := app.NewService(st, realtime.NewHub(), slog.Default())
	return NewRouter(cfg, slog.Default(), service)
}

func loginForToken(t *testing.T, server http.Handler, path, account, password string) string {
	t.Helper()
	out := doJSON(t, server, http.MethodPost, path, "", map[string]any{"account": account, "password": password}, http.StatusOK)
	token, ok := out["token"].(string)
	if !ok || token == "" {
		t.Fatalf("missing token in response: %#v", out)
	}
	return token
}

func doJSON(t *testing.T, server http.Handler, method, path, token string, payload any, wantStatus int) map[string]any {
	t.Helper()
	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			t.Fatalf("encode payload: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &body)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		t.Fatalf("%s %s status=%d want=%d body=%s", method, path, rec.Code, wantStatus, rec.Body.String())
	}
	var out map[string]any
	if rec.Body.Len() > 0 {
		if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
			t.Fatalf("decode response: %v body=%s", err, rec.Body.String())
		}
	}
	return out
}
