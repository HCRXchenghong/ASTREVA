package openapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"customer-service/backend/internal/domain"
)

// scopeEndpoint 描述一个开放端点与它所需的权限范围，用于「缺范围 → 403，授权后 → 正常」的矩阵断言。
type scopeEndpoint struct {
	name     string
	scope    string
	method   string
	path     func(conversationID string) string
	body     any
	okStatus int
	isWS     bool
}

// scopeEndpoints 按「先读后写、最后关闭会话」的顺序排列（关闭会改变会话状态，必须放在最后）。
func scopeEndpoints() []scopeEndpoint {
	return []scopeEndpoint{
		{name: "GET /me", scope: domain.APIScopeMeRead, method: http.MethodGet, path: func(string) string { return "/open/v1/me" }, okStatus: http.StatusOK},
		{name: "GET /conversations", scope: domain.APIScopeConversationsRead, method: http.MethodGet, path: func(string) string { return "/open/v1/conversations" }, okStatus: http.StatusOK},
		{name: "GET /conversations/{id}", scope: domain.APIScopeConversationsRead, method: http.MethodGet, path: func(id string) string { return "/open/v1/conversations/" + id }, okStatus: http.StatusOK},
		{name: "GET messages", scope: domain.APIScopeMessagesRead, method: http.MethodGet, path: func(id string) string { return "/open/v1/conversations/" + id + "/messages" }, okStatus: http.StatusOK},
		{name: "POST /conversations", scope: domain.APIScopeConversationsCreate, method: http.MethodPost, path: func(string) string { return "/open/v1/conversations" }, body: map[string]any{"lang": "zh"}, okStatus: http.StatusCreated},
		{name: "POST messages", scope: domain.APIScopeMessagesWrite, method: http.MethodPost, path: func(id string) string { return "/open/v1/conversations/" + id + "/messages" }, body: map[string]any{"content": "范围测试消息"}, okStatus: http.StatusCreated},
		{name: "WS subscribe", scope: domain.APIScopeEventsSubscribe, isWS: true, path: func(string) string { return "/open/v1/ws" }, okStatus: http.StatusSwitchingProtocols},
		{name: "POST close", scope: domain.APIScopeConversationsClose, method: http.MethodPost, path: func(id string) string { return "/open/v1/conversations/" + id + "/close" }, okStatus: http.StatusOK},
	}
}

// createAppWithScopes 通过管理接口创建应用；scopes 为 nil 时不传该字段（默认全功能/不受限）。
func (env *testEnv) createAppWithScopes(t *testing.T, name string, scopes any) (appID, apiKey string) {
	t.Helper()
	body := map[string]any{"name": name, "note": "test"}
	if scopes != nil {
		body["scopes"] = scopes
	}
	status, payload := env.do(t, http.MethodPost, "/api/admin/api-apps", env.adminToken, body)
	if status != http.StatusCreated {
		t.Fatalf("创建应用失败 status = %d, payload = %v", status, payload)
	}
	apiKey, _ = payload["api_key"].(string)
	record := payload["app"].(map[string]any)
	return record["id"].(string), apiKey
}

// patchScopes 覆盖保存应用的权限范围（只传 scopes，其余字段应保持原值）。
func (env *testEnv) patchScopes(t *testing.T, appID string, scopes []string) {
	t.Helper()
	status, payload := env.do(t, http.MethodPatch, "/api/admin/api-apps/"+appID, env.adminToken, map[string]any{
		"scopes": scopes,
	})
	if status != http.StatusOK {
		t.Fatalf("更新范围失败 status = %d, payload = %v", status, payload)
	}
}

// newAppWithConversation 创建一个不受限应用并预置一条会话，返回应用标识、Key 与会话 ID。
func (env *testEnv) newAppWithConversation(t *testing.T, name string) (appID, apiKey, conversationID string) {
	t.Helper()
	appID, apiKey = env.createAppWithScopes(t, name, nil)
	status, payload := env.do(t, http.MethodPost, "/open/v1/conversations", apiKey, map[string]any{"lang": "zh"})
	if status != http.StatusCreated {
		t.Fatalf("创建会话失败 status = %d, payload = %v", status, payload)
	}
	return appID, apiKey, payload["conversation"].(map[string]any)["id"].(string)
}

// callEndpoint 调用一个开放端点；WebSocket 端点返回握手响应状态与响应体。
func (env *testEnv) callEndpoint(t *testing.T, ep scopeEndpoint, apiKey, conversationID string) (status int, payload map[string]any) {
	t.Helper()
	if ep.isWS {
		wsURL := "ws" + strings.TrimPrefix(env.server.URL, "http") + ep.path(conversationID) + "?conversation_id=" + conversationID + "&key=" + apiKey
		conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err == nil {
			defer conn.Close()
			var connected map[string]any
			_ = conn.ReadJSON(&connected)
			return http.StatusSwitchingProtocols, connected
		}
		if resp == nil {
			t.Fatalf("WS 握手失败且无响应：%v", err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		out := map[string]any{}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &out)
		}
		return resp.StatusCode, out
	}
	return env.do(t, ep.method, ep.path(conversationID), apiKey, ep.body)
}

// assertDenied 断言端点因缺权限范围被拒：403 + {"error":"forbidden","message":含 scope 名}。
func (env *testEnv) assertDenied(t *testing.T, ep scopeEndpoint, apiKey, conversationID string) {
	t.Helper()
	status, payload := env.callEndpoint(t, ep, apiKey, conversationID)
	if status != http.StatusForbidden {
		t.Fatalf("%s 缺 %s 时应返回 403，实际 %d（%v）", ep.name, ep.scope, status, payload)
	}
	if payload["error"] != "forbidden" {
		t.Fatalf("%s 错误码应为 forbidden，实际 %v", ep.name, payload["error"])
	}
	message, _ := payload["message"].(string)
	if !strings.Contains(message, ep.scope) {
		t.Fatalf("%s 错误信息应包含范围名 %s，实际 %q", ep.name, ep.scope, message)
	}
}

// assertAllowed 断言端点被授权后正常工作。
func (env *testEnv) assertAllowed(t *testing.T, ep scopeEndpoint, apiKey, conversationID string) {
	t.Helper()
	status, payload := env.callEndpoint(t, ep, apiKey, conversationID)
	if status != ep.okStatus {
		t.Fatalf("%s 授权后应返回 %d，实际 %d（%v）", ep.name, ep.okStatus, status, payload)
	}
}

func (env *testEnv) countScopeDeniedAudits() int {
	count := 0
	for _, event := range env.store.RecentAuditEvents(200) {
		if event.Action == "openapi.scope_denied" {
			count++
		}
	}
	return count
}

// TestOpenAPIScopeEnforcementEveryEndpoint 覆盖验收 1：逐端点断言「缺范围 → 403 带范围名；授权 → 正常」。
// me:read 按规格不可取消，因此 GET /me 在任何收窄组合下都必须保持可用。
func TestOpenAPIScopeEnforcementEveryEndpoint(t *testing.T) {
	env := newTestEnv(t)
	appID, apiKey, conversationID := env.newAppWithConversation(t, "范围矩阵平台")
	endpoints := scopeEndpoints()

	// 收窄到仅 events:subscribe（me:read 自动补齐）：除 /me 与 WS 外全部 403。
	env.patchScopes(t, appID, []string{domain.APIScopeEventsSubscribe})
	for _, ep := range endpoints {
		if ep.scope == domain.APIScopeMeRead || ep.scope == domain.APIScopeEventsSubscribe {
			env.assertAllowed(t, ep, apiKey, conversationID)
			continue
		}
		env.assertDenied(t, ep, apiKey, conversationID)
	}

	// 再收窄到仅 conversations:read：仅 me:read 与 conversations:read 端点可用。
	env.patchScopes(t, appID, []string{domain.APIScopeConversationsRead})
	for _, ep := range endpoints {
		if ep.scope == domain.APIScopeMeRead || ep.scope == domain.APIScopeConversationsRead {
			env.assertAllowed(t, ep, apiKey, conversationID)
			continue
		}
		env.assertDenied(t, ep, apiKey, conversationID)
	}

	// 授予全部范围后所有端点恢复正常。
	env.patchScopes(t, appID, domain.AllAPIScopes())
	for _, ep := range endpoints {
		env.assertAllowed(t, ep, apiKey, conversationID)
	}
}

// TestOpenAPILegacyAppWithoutScopes 覆盖验收 2：空 scopes 的旧应用等同于全部范围，所有端点可用。
func TestOpenAPILegacyAppWithoutScopes(t *testing.T) {
	env := newTestEnv(t)
	// 不加 scopes 字段创建（历史应用形态），存储层应为空数组。
	appID, apiKey := env.createAppWithScopes(t, "历史应用", nil)
	appRecord, ok := env.findApp(appID)
	if !ok {
		t.Fatal("应用未找到")
	}
	if appRecord["scopes"] != nil {
		if list, isList := appRecord["scopes"].([]any); !isList || len(list) != 0 {
			t.Fatalf("历史应用 scopes 应为空数组，实际 %v", appRecord["scopes"])
		}
	}

	status, payload := env.do(t, http.MethodPost, "/open/v1/conversations", apiKey, map[string]any{"lang": "zh"})
	if status != http.StatusCreated {
		t.Fatalf("旧应用创建会话失败 status = %d payload = %v", status, payload)
	}
	conversationID := payload["conversation"].(map[string]any)["id"].(string)

	for _, ep := range scopeEndpoints() {
		env.assertAllowed(t, ep, apiKey, conversationID)
	}

	// /me 应把「不受限」展开为全部 7 个范围。
	status, payload = env.do(t, http.MethodGet, "/open/v1/me", apiKey, nil)
	if status != http.StatusOK {
		t.Fatalf("me status = %d", status)
	}
	scopes := payload["scopes"].([]any)
	if len(scopes) != len(domain.AllAPIScopes()) {
		t.Fatalf("旧应用 /me scopes 应为全部 %d 个，实际 %v", len(domain.AllAPIScopes()), scopes)
	}
}

// findApp 从管理接口读取应用记录（JSON 形态）。
func (env *testEnv) findApp(appID string) (map[string]any, bool) {
	for _, raw := range env.listApps() {
		record, _ := raw.(map[string]any)
		if record["id"] == appID {
			return record, true
		}
	}
	return nil, false
}

func (env *testEnv) listApps() []any {
	req, err := http.NewRequest(http.MethodGet, env.server.URL+"/api/admin/api-apps", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+env.adminToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	list, _ := out["apps"].([]any)
	return list
}

// TestOpenAPIScopePresets 覆盖验收 3：四套预置组合逐一断言可访问与被拒的端点集合。
func TestOpenAPIScopePresets(t *testing.T) {
	cases := []struct {
		name    string
		scopes  []string
		allowed []string
		denied  []string
	}{
		{
			name:    "全功能",
			scopes:  domain.AllAPIScopes(),
			allowed: []string{"GET /me", "GET /conversations", "GET /conversations/{id}", "GET messages", "POST /conversations", "POST messages", "WS subscribe", "POST close"},
			denied:  nil,
		},
		{
			name:    "只读订阅",
			scopes:  []string{domain.APIScopeConversationsRead, domain.APIScopeMessagesRead, domain.APIScopeEventsSubscribe},
			allowed: []string{"GET /me", "GET /conversations", "GET /conversations/{id}", "GET messages", "WS subscribe"},
			denied:  []string{"POST /conversations", "POST messages", "POST close"},
		},
		{
			name:    "仅创建与发消息",
			scopes:  []string{domain.APIScopeConversationsCreate, domain.APIScopeMessagesWrite},
			allowed: []string{"GET /me", "POST /conversations", "POST messages"},
			denied:  []string{"GET /conversations", "GET /conversations/{id}", "GET messages", "WS subscribe", "POST close"},
		},
		{
			name:    "客服机器人",
			scopes:  []string{domain.APIScopeConversationsRead, domain.APIScopeMessagesRead, domain.APIScopeMessagesWrite, domain.APIScopeConversationsClose},
			allowed: []string{"GET /me", "GET /conversations", "GET /conversations/{id}", "GET messages", "POST messages", "POST close"},
			denied:  []string{"POST /conversations", "WS subscribe"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t)
			appID, apiKey, conversationID := env.newAppWithConversation(t, "预置组合-"+tc.name)
			env.patchScopes(t, appID, tc.scopes)

			allowed := map[string]bool{}
			for _, name := range tc.allowed {
				allowed[name] = true
			}
			denied := map[string]bool{}
			for _, name := range tc.denied {
				denied[name] = true
			}
			for _, ep := range scopeEndpoints() {
				if !allowed[ep.name] && !denied[ep.name] {
					t.Fatalf("用例 %s 未覆盖端点 %s", tc.name, ep.name)
				}
				if denied[ep.name] {
					env.assertDenied(t, ep, apiKey, conversationID)
					continue
				}
				env.assertAllowed(t, ep, apiKey, conversationID)
			}
		})
	}
}

// TestOpenAPIScopeAdminValidation 覆盖验收 4：/me 返回 scopes；非法 scope → 400；保存时自动补 me:read。
func TestOpenAPIScopeAdminValidation(t *testing.T) {
	env := newTestEnv(t)

	// 4.1 非法范围：创建与更新都应返回 400 中文说明。
	status, payload := env.do(t, http.MethodPost, "/api/admin/api-apps", env.adminToken, map[string]any{
		"name": "非法范围应用", "scopes": []string{"conversations:read", "bogus:scope"},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("非法范围创建应 400，实际 %d（%v）", status, payload)
	}
	if payload["error"] != "invalid_scope" {
		t.Fatalf("非法范围错误码应为 invalid_scope，实际 %v", payload["error"])
	}
	if message, _ := payload["message"].(string); !strings.Contains(message, "不支持的权限范围") || !strings.Contains(message, "bogus:scope") {
		t.Fatalf("非法范围提示应带中文说明与非法值，实际 %q", message)
	}

	appID, apiKey := env.createAppWithScopes(t, "范围校验应用", []string{domain.APIScopeConversationsRead})
	status, payload = env.do(t, http.MethodPatch, "/api/admin/api-apps/"+appID, env.adminToken, map[string]any{
		"scopes": []string{"me:read", "nope"},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("非法范围更新应 400，实际 %d（%v）", status, payload)
	}

	// 4.2 保存时自动补齐 me:read（只传 conversations:read）。
	record, ok := env.findApp(appID)
	if !ok {
		t.Fatal("应用未找到")
	}
	saved := scopeStrings(t, record["scopes"])
	if !containsScope(saved, domain.APIScopeMeRead) {
		t.Fatalf("保存后应自动补齐 me:read，实际 %v", saved)
	}

	// 4.3 /me 返回实际生效范围。
	status, payload = env.do(t, http.MethodGet, "/open/v1/me", apiKey, nil)
	if status != http.StatusOK {
		t.Fatalf("me status = %d", status)
	}
	effective := scopeStrings(t, payload["scopes"])
	if len(effective) != 2 || !containsScope(effective, domain.APIScopeMeRead) || !containsScope(effective, domain.APIScopeConversationsRead) {
		t.Fatalf("/me 应返回 [me:read conversations:read]，实际 %v", effective)
	}

	// 4.4 显式传空数组 = 不受限，/me 展开为全部范围。
	env.patchScopes(t, appID, []string{})
	status, payload = env.do(t, http.MethodGet, "/open/v1/me", apiKey, nil)
	if status != http.StatusOK {
		t.Fatalf("me status = %d", status)
	}
	if all := scopeStrings(t, payload["scopes"]); len(all) != len(domain.AllAPIScopes()) {
		t.Fatalf("显式空数组应收敛为全部范围，实际 %v", all)
	}
}

// TestOpenAPIScopeDeniedAudit 覆盖验收 5：缺范围被拒写审计，且不含请求体敏感内容。
func TestOpenAPIScopeDeniedAudit(t *testing.T) {
	env := newTestEnv(t)
	const marker = "SECRET-VISITOR-ID-7f2a13"
	appID, apiKey := env.createAppWithScopes(t, "审计平台", []string{domain.APIScopeMessagesRead})

	before := env.countScopeDeniedAudits()
	status, payload := env.do(t, http.MethodPost, "/open/v1/conversations", apiKey, map[string]any{
		"external_user_id": marker,
		"lang":             "zh",
	})
	if status != http.StatusForbidden {
		t.Fatalf("缺 conversations:create 应 403，实际 %d（%v）", status, payload)
	}
	after := env.countScopeDeniedAudits()
	if after != before+1 {
		t.Fatalf("缺范围被拒应新增 1 条审计，实际 %d → %d", before, after)
	}

	var denied *domain.AuditEvent
	for _, event := range env.store.RecentAuditEvents(50) {
		if event.Action == "openapi.scope_denied" {
			copied := event
			denied = &copied
			break
		}
	}
	if denied == nil {
		t.Fatal("未找到 openapi.scope_denied 审计")
	}
	if denied.ResourceID != appID || !strings.Contains(denied.ActorID, appID) {
		t.Fatalf("审计应包含 app_id，实际 actor=%s resource=%s", denied.ActorID, denied.ResourceID)
	}
	if !strings.Contains(denied.Description, domain.APIScopeConversationsCreate) {
		t.Fatalf("审计应包含缺失的范围名，实际 %q", denied.Description)
	}
	if !strings.Contains(denied.Description, "/open/v1/conversations") {
		t.Fatalf("审计应包含请求路径，实际 %q", denied.Description)
	}
	if strings.Contains(denied.Description, marker) || strings.Contains(denied.Description, "external_user_id") {
		t.Fatalf("审计不得记录请求体内容，实际 %q", denied.Description)
	}
	if denied.IPAddress != "203.0.113.9" {
		t.Fatalf("审计应记录来源 IP，实际 %q", denied.IPAddress)
	}
}

// TestAdminAppPatchPartialUpdate 回归：PATCH 为指针语义，未提供的字段必须保持原值。
// 控制台切换「启用/禁用」只发 {"enabled": false}，不得把限速、备注与权限范围清零。
func TestAdminAppPatchPartialUpdate(t *testing.T) {
	env := newTestEnv(t)
	status, payload := env.do(t, http.MethodPost, "/api/admin/api-apps", env.adminToken, map[string]any{
		"name": "局部更新平台", "note": "保留我", "rate_limit": 42,
		"scopes": []string{domain.APIScopeConversationsRead, domain.APIScopeMessagesRead},
	})
	if status != http.StatusCreated {
		t.Fatalf("创建应用失败 status = %d payload = %v", status, payload)
	}
	created := payload["app"].(map[string]any)
	appID := created["id"].(string)
	apiKey, _ := payload["api_key"].(string)
	if apiKey == "" {
		t.Fatalf("创建应返回一次性明文 api_key，实际 %v", payload)
	}
	if _, legacy := payload["key"]; legacy {
		t.Fatalf("创建响应不应包含旧字段 key：%v", payload)
	}

	// 只发 enabled，其余字段保持不变。
	status, payload = env.do(t, http.MethodPatch, "/api/admin/api-apps/"+appID, env.adminToken, map[string]any{"enabled": false})
	if status != http.StatusOK {
		t.Fatalf("局部更新失败 status = %d payload = %v", status, payload)
	}
	updated := payload["app"].(map[string]any)
	if updated["enabled"] != false {
		t.Fatalf("enabled 应更新为 false，实际 %v", updated["enabled"])
	}
	if updated["rate_limit"] != float64(42) {
		t.Fatalf("未提供的 rate_limit 应保持 42，实际 %v", updated["rate_limit"])
	}
	if updated["note"] != "保留我" {
		t.Fatalf("未提供的 note 应保持不变，实际 %v", updated["note"])
	}
	if updated["name"] != "局部更新平台" {
		t.Fatalf("未提供的 name 应保持不变，实际 %v", updated["name"])
	}
	scopes := scopeStrings(t, updated["scopes"])
	want := []string{domain.APIScopeMeRead, domain.APIScopeConversationsRead, domain.APIScopeMessagesRead}
	if strings.Join(scopes, ",") != strings.Join(want, ",") {
		t.Fatalf("未提供的 scopes 应保持不变，实际 %v", scopes)
	}

	// 重置 Key 同样应保留限速、备注与范围，并继续返回 api_key。
	status, payload = env.do(t, http.MethodPost, "/api/admin/api-apps/"+appID+"/reset-key", env.adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("重置 Key 失败 status = %d payload = %v", status, payload)
	}
	resetApp := payload["app"].(map[string]any)
	if resetAPIKey, _ := payload["api_key"].(string); resetAPIKey == "" {
		t.Fatalf("重置 Key 应返回一次性明文 api_key，实际 %v", payload)
	}
	if resetApp["rate_limit"] != float64(42) || resetApp["note"] != "保留我" {
		t.Fatalf("重置 Key 不得清空限速/备注，实际 %v", resetApp)
	}
	if got := scopeStrings(t, resetApp["scopes"]); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("重置 Key 不得清空权限范围，实际 %v", got)
	}
}

// TestNormalizeAPIScopesRejectsUnknown 确认非法 scope 走 400 + invalid_scope 契约（控制台按此处理）。
func TestNormalizeAPIScopesRejectsUnknown(t *testing.T) {
	env := newTestEnv(t)
	status, payload := env.do(t, http.MethodPost, "/api/admin/api-apps", env.adminToken, map[string]any{
		"name": "契约校验", "scopes": []string{"unknown:scope"},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("未知 scope 应 400，实际 %d（%v）", status, payload)
	}
	if payload["error"] != "invalid_scope" {
		t.Fatalf("错误体应为 {\"error\":\"invalid_scope\"}，实际 %v", payload)
	}
	if message, _ := payload["message"].(string); !strings.HasPrefix(message, "不支持的权限范围") {
		t.Fatalf("message 应以「不支持的权限范围」开头，实际 %q", message)
	}
}

func containsScope(scopes []string, target string) bool {
	for _, scope := range scopes {
		if scope == target {
			return true
		}
	}
	return false
}

// scopeStrings 把 JSON 数组（[]any）转成字符串切片。
func scopeStrings(t *testing.T, raw any) []string {
	t.Helper()
	if raw == nil {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		t.Fatalf("scopes 应为数组，实际 %T", raw)
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		out = append(out, item.(string))
	}
	return out
}
