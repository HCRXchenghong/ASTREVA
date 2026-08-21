package httpx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"customer-service/backend/internal/config"
)

func visitorSocket(t *testing.T, serverURL, token, conversationID string) (*websocket.Conn, *http.Response) {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") +
		"/ws?role=visitor&token=" + url.QueryEscape(token) +
		"&conversation_id=" + url.QueryEscape(conversationID)
	header := http.Header{}
	header.Set("Origin", "https://allowed.test")
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dial visitor websocket: %v", err)
	}
	return conn, resp
}

func sendSocketEvent(t *testing.T, conn *websocket.Conn, event string, data map[string]any) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"event": event, "data": data})
	if err != nil {
		t.Fatalf("marshal socket event: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("write socket event: %v", err)
	}
}

func TestEndToEndVisitorToAgentLoop(t *testing.T) {
	router := newTestRouterWithConfig(configForE2E(t))
	server := httptest.NewServer(router)
	defer server.Close()

	adminToken := loginForToken(t, router, "/api/admin/login", "superadmin", "123456")

	created := doJSON(t, router, http.MethodPost, "/api/admin/agents", adminToken, map[string]any{
		"account":           "kf_e2e",
		"password":          "AgentPass-123456",
		"name":              "端到端客服",
		"group":             "售前组",
		"max_conversations": 5,
	}, http.StatusCreated)
	agentID := created["agent"].(map[string]any)["id"].(string)

	agentToken := loginForToken(t, router, "/api/agent/login", "kf_e2e", "AgentPass-123456")
	doJSON(t, router, http.MethodPost, "/api/agent/status", agentToken, map[string]any{"status": "online"}, http.StatusOK)

	visitor := doJSON(t, router, http.MethodPost, "/api/visitor/conversations", "", map[string]any{"source": "e2e-test"}, http.StatusCreated)
	visitorToken := visitor["token"].(string)
	conversationID := visitor["conversation"].(map[string]any)["id"].(string)

	conn, _ := visitorSocket(t, server.URL, visitorToken, conversationID)
	defer conn.Close()
	sendSocketEvent(t, conn, "message.send", map[string]any{
		"client_msg_id":   "e2e-msg-1",
		"conversation_id": conversationID,
		"message_type":    "text",
		"content":         "你好，我想咨询产品报价",
	})
	sendSocketEvent(t, conn, "message.send", map[string]any{
		"client_msg_id":   "e2e-msg-1",
		"conversation_id": conversationID,
		"message_type":    "text",
		"content":         "重复消息应幂等",
	})
	time.Sleep(150 * time.Millisecond)

	page := doJSON(t, router, http.MethodGet, fmt.Sprintf("/api/conversations/%s/messages?limit=20", conversationID), visitorToken, nil, http.StatusOK)
	messages := page["messages"].([]any)
	if len(messages) == 0 {
		t.Fatalf("expected visitor message to be persisted, got %#v", page)
	}

	transfer := doJSON(t, router, http.MethodPost, fmt.Sprintf("/api/admin/conversations/%s/transfer", conversationID), adminToken, map[string]any{"agent_id": agentID}, http.StatusOK)
	if status := transfer["conversation"].(map[string]any)["status"]; status != "assigned" {
		t.Fatalf("expected assigned after transfer, got %v", status)
	}

	agentList := doJSON(t, router, http.MethodGet, "/api/agent/conversations", agentToken, nil, http.StatusOK)
	conversations := agentList["conversations"].([]any)
	if len(conversations) == 0 {
		t.Fatalf("expected assigned conversation in agent list: %#v", agentList)
	}

	closed := doJSON(t, router, http.MethodPost, fmt.Sprintf("/api/conversations/%s/close", conversationID), adminToken, map[string]any{}, http.StatusOK)
	if status := closed["conversation"].(map[string]any)["status"]; status != "closed" {
		t.Fatalf("expected closed conversation, got %v", status)
	}

	rating := doJSON(t, router, http.MethodPost, fmt.Sprintf("/api/visitor/conversations/%s/rating", conversationID), visitorToken, map[string]any{
		"score":   5,
		"tags":    []string{"非常满意"},
		"comment": "回复很快",
	}, http.StatusCreated)
	if score := rating["rating"].(map[string]any)["score"]; score != float64(5) {
		t.Fatalf("unexpected rating score: %v", score)
	}

	summary := doJSON(t, router, http.MethodGet, "/api/admin/ratings/summary", adminToken, nil, http.StatusOK)
	if total := summary["total"]; total != float64(1) {
		t.Fatalf("expected one rating in summary, got %v", total)
	}

	audit := doJSON(t, router, http.MethodGet, "/api/admin/audit-events?limit=50", adminToken, nil, http.StatusOK)
	events := audit["events"].([]any)
	if len(events) == 0 {
		t.Fatalf("expected audit events, got none")
	}
}

func TestKeywordRulesCRUD(t *testing.T) {
	router := newTestRouter()
	adminToken := loginForToken(t, router, "/api/admin/login", "superadmin", "123456")

	created := doJSON(t, router, http.MethodPost, "/api/admin/keyword-rules", adminToken, map[string]any{
		"keyword":               "报价",
		"match_type":            "contains",
		"reply":                 "请留下联系方式，销售会尽快联系您。",
		"enabled":               true,
		"priority":              60,
		"action":                "text",
		"show_in_quick_replies": true,
		"quick_reply_text":      "咨询报价",
	}, http.StatusCreated)
	ruleID := created["rule"].(map[string]any)["id"].(string)

	patched := doJSON(t, router, http.MethodPatch, "/api/admin/keyword-rules/"+ruleID, adminToken, map[string]any{
		"keyword":  "报价单",
		"enabled":  false,
		"priority": 70,
	}, http.StatusOK)
	if keyword := patched["rule"].(map[string]any)["keyword"]; keyword != "报价单" {
		t.Fatalf("unexpected keyword after patch: %v", keyword)
	}

	deleted := doJSON(t, router, http.MethodDelete, "/api/admin/keyword-rules/"+ruleID, adminToken, nil, http.StatusOK)
	if deleted["rule"] == nil {
		t.Fatalf("expected deleted rule payload")
	}
	doJSON(t, router, http.MethodDelete, "/api/admin/keyword-rules/"+ruleID, adminToken, nil, http.StatusNotFound)
}

func TestAppReleaseUploadAndUpdateCheck(t *testing.T) {
	router := newTestRouterWithConfig(configForE2E(t))
	server := httptest.NewServer(router)
	defer server.Close()

	adminToken := loginForToken(t, router, "/api/admin/login", "superadmin", "123456")

	apkBytes := []byte{0x50, 0x4b, 0x03, 0x04, 0x00, 0x00, 0x00, 0x00, 0x01, 0x02, 0x03, 0x04}
	body, contentType := multipartBody(t, map[string]string{
		"platform":      "android",
		"version_name":  "1.1.0",
		"version_code":  "2",
		"release_notes": "修复若干问题",
		"force_update":  "true",
	}, "file", "astreva-1.1.0.apk", apkBytes)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/app-releases", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload release status=%d body=%s", rec.Code, rec.Body.String())
	}

	list := doJSON(t, router, http.MethodGet, "/api/admin/app-releases?platform=android", adminToken, nil, http.StatusOK)
	releases := list["releases"].([]any)
	if len(releases) != 1 {
		t.Fatalf("expected one release, got %#v", list)
	}
	release := releases[0].(map[string]any)
	releaseID := release["id"].(string)

	latest := doJSON(t, router, http.MethodGet, "/api/app-update/latest?platform=android&version_code=1", "", nil, http.StatusOK)
	if latest["update_available"] != true || latest["force_update"] != true {
		t.Fatalf("expected forced update for older client, got %#v", latest)
	}

	noUpdate := doJSON(t, router, http.MethodGet, "/api/app-update/latest?platform=android&version_code=2", "", nil, http.StatusOK)
	if noUpdate["update_available"] != false {
		t.Fatalf("expected no update for current client, got %#v", noUpdate)
	}

	downloadReq := httptest.NewRequest(http.MethodGet, "/api/app-update/download/"+releaseID, nil)
	downloadRec := httptest.NewRecorder()
	router.ServeHTTP(downloadRec, downloadReq)
	if downloadRec.Code != http.StatusOK {
		t.Fatalf("download status=%d", downloadRec.Code)
	}
	if !strings.Contains(downloadRec.Header().Get("Content-Type"), "vnd.android.package-archive") {
		t.Fatalf("unexpected download content type: %s", downloadRec.Header().Get("Content-Type"))
	}
	if downloadRec.Body.Len() != len(apkBytes) {
		t.Fatalf("unexpected download size: %d want %d", downloadRec.Body.Len(), len(apkBytes))
	}

	patched := doJSON(t, router, http.MethodPatch, "/api/admin/app-releases/"+releaseID, adminToken, map[string]any{
		"force_update": false,
	}, http.StatusOK)
	if patched["release"].(map[string]any)["force_update"] != false {
		t.Fatalf("expected force_update cleared, got %#v", patched)
	}

	doJSON(t, router, http.MethodDelete, "/api/admin/app-releases/"+releaseID, adminToken, nil, http.StatusOK)
	doJSON(t, router, http.MethodGet, "/api/app-update/download/"+releaseID, "", nil, http.StatusNotFound)
}

func configForE2E(t *testing.T) config.Config {
	return config.Config{
		Env:                "test",
		CORSAllowedOrigins: "https://allowed.test",
		UploadDir:          t.TempDir(),
		UploadMaxBytes:     10 * 1024 * 1024,
	}
}

func multipartBody(t *testing.T, fields map[string]string, fileField, fileName string, fileBytes []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("write multipart field: %v", err)
		}
	}
	part, err := writer.CreateFormFile(fileField, fileName)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(fileBytes); err != nil {
		t.Fatalf("write file bytes: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return &body, writer.FormDataContentType()
}
