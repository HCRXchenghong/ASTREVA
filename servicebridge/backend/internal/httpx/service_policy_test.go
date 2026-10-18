package httpx

import (
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"customer-service/backend/internal/app"
	"customer-service/backend/internal/config"
	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

// newServicePolicyRouter 构造可访问底层存储的测试路由（批次 4 接口测试用）。
func newServicePolicyRouter(t *testing.T) (http.Handler, *store.MemoryStore) {
	t.Helper()
	st := store.NewMemoryStore(store.Options{})
	_, _ = st.UpdateBusinessHours(domain.BusinessHours{Timezone: "Asia/Shanghai", Enabled: false})
	service := app.NewService(st, realtime.NewHub(), slog.Default())
	return NewRouter(config.Config{Env: "test"}, slog.Default(), service), st
}

// createPolicyTestAgent 通过管理员接口建客服并登录，返回 agentID 与 token。
func createPolicyTestAgent(t *testing.T, server http.Handler, adminToken, account string) (string, string) {
	t.Helper()
	created := doJSON(t, server, http.MethodPost, "/api/admin/agents", adminToken, map[string]any{
		"account":           account,
		"password":          "AgentPass-123456",
		"name":              "渠道客服",
		"group":             "default",
		"max_conversations": 5,
	}, http.StatusCreated)
	agentID, _ := created["agent"].(map[string]any)["id"].(string)
	if agentID == "" {
		t.Fatalf("创建客服响应缺少 ID：%#v", created)
	}
	token := loginForToken(t, server, "/api/agent/login", account, "AgentPass-123456")
	return agentID, token
}

// TestHTTPServicePolicyAuthAndPatch 覆盖策略接口鉴权、局部更新与非法枚举拒绝。
func TestHTTPServicePolicyAuthAndPatch(t *testing.T) {
	server, st := newServicePolicyRouter(t)
	adminToken := loginForToken(t, server, "/api/admin/login", "superadmin", "123456")
	_, agentToken := createPolicyTestAgent(t, server, adminToken, "policy_http_agent")

	// 非管理员（含未登录）一律 403。
	doJSON(t, server, http.MethodGet, "/api/admin/service-policy", "", nil, http.StatusForbidden)
	doJSON(t, server, http.MethodGet, "/api/admin/service-policy", agentToken, nil, http.StatusForbidden)
	doJSON(t, server, http.MethodPatch, "/api/admin/service-policy", agentToken, map[string]any{"queue_enabled": false}, http.StatusForbidden)

	defaults := doJSON(t, server, http.MethodGet, "/api/admin/service-policy", adminToken, nil, http.StatusOK)
	policy := defaults["policy"].(map[string]any)
	if policy["max_wait_seconds"] != float64(domain.DefaultServicePolicy().MaxWaitSeconds) {
		t.Fatalf("默认 max_wait_seconds 不符：%#v", policy["max_wait_seconds"])
	}
	if policy["ai_keyword_priority"] != domain.AIKeywordPriorityKeywordFirst {
		t.Fatalf("默认优先级不符：%#v", policy["ai_keyword_priority"])
	}

	// 非法枚举显式 400（不静默回落）。
	doJSON(t, server, http.MethodPatch, "/api/admin/service-policy", adminToken,
		map[string]any{"sla_idle_action": "maybe"}, http.StatusBadRequest)
	doJSON(t, server, http.MethodPatch, "/api/admin/service-policy", adminToken,
		map[string]any{"ai_keyword_priority": "keyword"}, http.StatusBadRequest)

	updated := doJSON(t, server, http.MethodPatch, "/api/admin/service-policy", adminToken, map[string]any{
		"queue_enabled":           false,
		"max_wait_seconds":        120,
		"sla_idle_action":         domain.SLAIdleActionReassign,
		"ai_keyword_priority":     domain.AIKeywordPriorityAIFirst,
		"unattended_reply":        "客服稍后回复您。",
		"auto_close_idle_minutes": 0,
	}, http.StatusOK)
	policy = updated["policy"].(map[string]any)
	if policy["queue_enabled"] != false || policy["max_wait_seconds"] != float64(120) {
		t.Fatalf("策略更新未生效：%#v", policy)
	}
	if policy["ai_keyword_priority"] != domain.AIKeywordPriorityAIFirst {
		t.Fatalf("优先级更新未生效：%#v", policy["ai_keyword_priority"])
	}
	if policy["unattended_reply"] != "客服稍后回复您。" {
		t.Fatalf("兜底话术未生效：%#v", policy["unattended_reply"])
	}

	// 读回一致 + 审计落库。
	if readBack := st.ServicePolicy(); readBack.QueueEnabled || readBack.MaxWaitSeconds != 120 {
		t.Fatalf("存储层策略与接口不一致：%+v", readBack)
	}
	assertAuditAction(t, st, "service_policy.update")
}

// TestHTTPServiceQueueList 覆盖排队列表与等待时长。
func TestHTTPServiceQueueList(t *testing.T) {
	server, st := newServicePolicyRouter(t)
	adminToken := loginForToken(t, server, "/api/admin/login", "superadmin", "123456")
	_, agentToken := createPolicyTestAgent(t, server, adminToken, "queue_http_agent")

	first, err := st.CreateVisitorConversation("203.0.113.31", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	second, err := st.CreateVisitorConversation("203.0.113.32", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	if _, err := st.EnqueueConversation(domain.QueuedConversation{ConversationID: first.Conversation.ID, Priority: 1, GroupName: "售前"}); err != nil {
		t.Fatalf("入队失败：%v", err)
	}
	if _, err := st.EnqueueConversation(domain.QueuedConversation{ConversationID: second.Conversation.ID, Priority: 5, GroupName: "售后"}); err != nil {
		t.Fatalf("入队失败：%v", err)
	}

	doJSON(t, server, http.MethodGet, "/api/admin/service-queue", agentToken, nil, http.StatusForbidden)
	out := doJSON(t, server, http.MethodGet, "/api/admin/service-queue", adminToken, nil, http.StatusOK)
	if out["total"] != float64(2) {
		t.Fatalf("排队总数不符：%#v", out["total"])
	}
	queue := out["queue"].([]any)
	head := queue[0].(map[string]any)
	if head["conversation_id"] != second.Conversation.ID {
		t.Fatalf("应按优先级降序：%#v", queue)
	}
	if _, ok := head["wait_seconds"]; !ok {
		t.Fatalf("排队项应包含 wait_seconds：%#v", head)
	}
}

// TestHTTPBlacklistLifecycle 覆盖黑名单增删查、重复冲突、过期时间校验与鉴权。
func TestHTTPBlacklistLifecycle(t *testing.T) {
	server, st := newServicePolicyRouter(t)
	adminToken := loginForToken(t, server, "/api/admin/login", "superadmin", "123456")
	_, agentToken := createPolicyTestAgent(t, server, adminToken, "blacklist_http_agent")

	doJSON(t, server, http.MethodGet, "/api/admin/blacklist", agentToken, nil, http.StatusForbidden)
	doJSON(t, server, http.MethodPost, "/api/admin/blacklist", agentToken,
		map[string]any{"kind": "visitor", "value": "v1"}, http.StatusForbidden)
	doJSON(t, server, http.MethodPost, "/api/admin/blacklist", adminToken,
		map[string]any{"kind": "sms", "value": "v1"}, http.StatusBadRequest)
	doJSON(t, server, http.MethodPost, "/api/admin/blacklist", adminToken,
		map[string]any{"kind": "visitor", "value": "v1", "expires_at": time.Now().UTC().Add(-time.Hour)}, http.StatusBadRequest)

	created := doJSON(t, server, http.MethodPost, "/api/admin/blacklist", adminToken,
		map[string]any{"kind": "visitor", "value": "vis_http", "reason": "刷屏"}, http.StatusCreated)
	entry := created["entry"].(map[string]any)
	entryID, _ := entry["id"].(string)
	if entryID == "" || entry["created_by"] == "" {
		t.Fatalf("黑名单应带 ID 与创建人：%#v", entry)
	}

	doJSON(t, server, http.MethodPost, "/api/admin/blacklist", adminToken,
		map[string]any{"kind": "visitor", "value": "vis_http"}, http.StatusConflict)
	doJSON(t, server, http.MethodGet, "/api/admin/blacklist?kind=bogus", adminToken, nil, http.StatusBadRequest)

	list := doJSON(t, server, http.MethodGet, "/api/admin/blacklist?kind=visitor", adminToken, nil, http.StatusOK)
	if list["total"] != float64(1) {
		t.Fatalf("黑名单总数不符：%#v", list["total"])
	}
	doJSON(t, server, http.MethodDelete, "/api/admin/blacklist/"+entryID, agentToken, nil, http.StatusForbidden)
	doJSON(t, server, http.MethodDelete, "/api/admin/blacklist/"+entryID, adminToken, nil, http.StatusOK)
	doJSON(t, server, http.MethodDelete, "/api/admin/blacklist/"+entryID, adminToken, nil, http.StatusNotFound)
	assertAuditAction(t, st, "blacklist.add")
	assertAuditAction(t, st, "blacklist.delete")
}

// TestHTTPAlertChannelsAdminAndAgent 覆盖渠道上限、密钥掩码、PATCH 密钥语义与归属校验。
func TestHTTPAlertChannelsAdminAndAgent(t *testing.T) {
	server, st := newServicePolicyRouter(t)
	adminToken := loginForToken(t, server, "/api/admin/login", "superadmin", "123456")
	agentA, tokenA := createPolicyTestAgent(t, server, adminToken, "channel_http_a")
	_, tokenB := createPolicyTestAgent(t, server, adminToken, "channel_http_b")

	token := "123456:AAbbCCddEEffGGhh"
	created := doJSON(t, server, http.MethodPost, "/api/admin/agents/"+agentA+"/alert-channels", adminToken, map[string]any{
		"kind":   "telegram",
		"config": map[string]string{"token": token, "chat_id": "10086"},
	}, http.StatusCreated)
	// 响应绝不能包含明文 token。
	if strings.Contains(recString(created), token) {
		t.Fatalf("创建渠道响应泄漏密钥：%s", recString(created))
	}
	channel := created["channel"].(map[string]any)
	channelID, _ := channel["id"].(string)
	masked := channel["config_masked"].(map[string]any)
	if !strings.HasPrefix(masked["token"].(string), "****") || strings.Contains(masked["token"].(string), "AAbb") {
		t.Fatalf("token 掩码不符：%#v", masked)
	}

	// 客服只能看到自己的渠道。
	own := doJSON(t, server, http.MethodGet, "/api/agent/alert-channels", tokenA, nil, http.StatusOK)
	if len(own["channels"].([]any)) != 1 {
		t.Fatalf("客服 A 应有 1 个渠道：%#v", own)
	}
	if strings.Contains(recString(own), token) {
		t.Fatalf("渠道列表泄漏密钥：%s", recString(own))
	}
	if other := doJSON(t, server, http.MethodGet, "/api/agent/alert-channels", tokenB, nil, http.StatusOK); len(other["channels"].([]any)) != 0 {
		t.Fatalf("客服 B 不应看到 A 的渠道：%#v", other)
	}
	// 管理员专属接口不接受客服身份；客服接口不接受管理员身份。
	doJSON(t, server, http.MethodGet, "/api/admin/agents/"+agentA+"/alert-channels", tokenA, nil, http.StatusForbidden)
	doJSON(t, server, http.MethodGet, "/api/agent/alert-channels", adminToken, nil, http.StatusForbidden)

	// 跨账号操作返回 404。
	doJSON(t, server, http.MethodPatch, "/api/agent/alert-channels/"+channelID, tokenB,
		map[string]any{"enabled": false}, http.StatusNotFound)
	doJSON(t, server, http.MethodDelete, "/api/agent/alert-channels/"+channelID, tokenB, nil, http.StatusNotFound)

	// 只改 enabled：密钥必须保持不变。
	doJSON(t, server, http.MethodPatch, "/api/admin/agents/"+agentA+"/alert-channels/"+channelID, adminToken,
		map[string]any{"enabled": false}, http.StatusOK)
	channels := st.AgentAlertChannels(agentA)
	if len(channels) != 1 || channels[0].Enabled {
		t.Fatalf("PATCH enabled 未生效：%+v", channels)
	}
	if channels[0].Config["token"] != token {
		t.Fatalf("未显式传 config 时不应改密钥：%+v", channels[0].Config)
	}

	// 显式传 config：合并语义——只覆盖出现的键，未出现的键（含密钥）保留。
	newToken := "999999:ZZyyXXwwVVuuTTss"
	patched := doJSON(t, server, http.MethodPatch, "/api/admin/agents/"+agentA+"/alert-channels/"+channelID, adminToken,
		map[string]any{"config": map[string]string{"token": newToken}}, http.StatusOK)
	if strings.Contains(recString(patched), newToken) {
		t.Fatalf("PATCH 响应泄漏密钥：%s", recString(patched))
	}
	merged := st.AgentAlertChannels(agentA)[0].Config
	if merged["token"] != newToken {
		t.Fatalf("PATCH config 未覆盖 token：%+v", merged)
	}
	if merged["chat_id"] != "10086" {
		t.Fatalf("未出现的键应保留旧值：%+v", merged)
	}
	// 只传 chat_id 时密钥保持不变。
	doJSON(t, server, http.MethodPatch, "/api/admin/agents/"+agentA+"/alert-channels/"+channelID, adminToken,
		map[string]any{"config": map[string]string{"chat_id": "10011"}}, http.StatusOK)
	merged = st.AgentAlertChannels(agentA)[0].Config
	if merged["token"] != newToken || merged["chat_id"] != "10011" {
		t.Fatalf("合并语义不符：%+v", merged)
	}
	// 显式空串清空必填键 → 400。
	doJSON(t, server, http.MethodPatch, "/api/admin/agents/"+agentA+"/alert-channels/"+channelID, adminToken,
		map[string]any{"config": map[string]string{"token": ""}}, http.StatusBadRequest)

	// 上限 3 个：第 4 个 400。
	doJSON(t, server, http.MethodPost, "/api/admin/agents/"+agentA+"/alert-channels", adminToken,
		map[string]any{"kind": "bark", "config": map[string]string{"key": "bark-key-0001"}}, http.StatusCreated)
	doJSON(t, server, http.MethodPost, "/api/admin/agents/"+agentA+"/alert-channels", adminToken,
		map[string]any{"kind": "email", "config": map[string]string{"address": "a@example.com"}}, http.StatusCreated)
	doJSON(t, server, http.MethodPost, "/api/admin/agents/"+agentA+"/alert-channels", adminToken,
		map[string]any{"kind": "bark", "config": map[string]string{"key": "bark-key-0002"}}, http.StatusBadRequest)
	// 配置不全 400；客服不存在 404。
	doJSON(t, server, http.MethodPost, "/api/admin/agents/"+agentA+"/alert-channels", adminToken,
		map[string]any{"kind": "telegram", "config": map[string]string{"token": "only"}}, http.StatusBadRequest)
	doJSON(t, server, http.MethodPost, "/api/admin/agents/agent_missing/alert-channels", adminToken,
		map[string]any{"kind": "bark", "config": map[string]string{"key": "k"}}, http.StatusNotFound)

	// 客服删除自己的渠道。
	doJSON(t, server, http.MethodDelete, "/api/agent/alert-channels/"+channelID, tokenA, nil, http.StatusOK)
	doJSON(t, server, http.MethodDelete, "/api/agent/alert-channels/"+channelID, tokenA, nil, http.StatusNotFound)
	assertAuditAction(t, st, "alert_channel.create")
	assertAuditAction(t, st, "alert_channel.delete")
}

// TestHTTPAlertSMTPSettings 覆盖 SMTP 配置鉴权、密码掩码与 password 语义。
func TestHTTPAlertSMTPSettings(t *testing.T) {
	server, st := newServicePolicyRouter(t)
	adminToken := loginForToken(t, server, "/api/admin/login", "superadmin", "123456")
	_, agentToken := createPolicyTestAgent(t, server, adminToken, "smtp_http_agent")

	doJSON(t, server, http.MethodGet, "/api/admin/alert-smtp", "", nil, http.StatusForbidden)
	doJSON(t, server, http.MethodGet, "/api/admin/alert-smtp", agentToken, nil, http.StatusForbidden)

	defaults := doJSON(t, server, http.MethodGet, "/api/admin/alert-smtp", adminToken, nil, http.StatusOK)
	if defaults["settings"].(map[string]any)["port"] != float64(domain.DefaultAlertSMTPPort) {
		t.Fatalf("默认端口不符：%#v", defaults)
	}

	password := "smtp-secret-1234"
	updated := doJSON(t, server, http.MethodPatch, "/api/admin/alert-smtp", adminToken, map[string]any{
		"host":         "smtp.example.com",
		"port":         587,
		"username":     "bot@example.com",
		"password":     password,
		"from_address": "bot@example.com",
		"use_tls":      true,
	}, http.StatusOK)
	if strings.Contains(recString(updated), password) {
		t.Fatalf("SMTP 响应泄漏密码：%s", recString(updated))
	}
	settings := updated["settings"].(map[string]any)
	if settings["password_masked"] != domain.MaskSecretValue(password) {
		t.Fatalf("密码掩码不符：%#v", settings["password_masked"])
	}
	if st.AlertSMTPSettings().Password != password {
		t.Fatalf("存储层内部应保留明文密码：%+v", st.AlertSMTPSettings())
	}

	// 不传 password：保持原密码。
	doJSON(t, server, http.MethodPatch, "/api/admin/alert-smtp", adminToken, map[string]any{
		"host": "smtp2.example.com",
	}, http.StatusOK)
	if st.AlertSMTPSettings().Password != password {
		t.Fatalf("不传 password 时不应改密码：%+v", st.AlertSMTPSettings())
	}
	if st.AlertSMTPSettings().Host != "smtp2.example.com" {
		t.Fatalf("host 应更新：%+v", st.AlertSMTPSettings())
	}

	// 传 ""：清除密码（空掩码因 omitempty 不会出现在 JSON 里）。
	cleared := doJSON(t, server, http.MethodPatch, "/api/admin/alert-smtp", adminToken, map[string]any{
		"password": "",
	}, http.StatusOK)
	if value, ok := cleared["settings"].(map[string]any)["password_masked"]; ok && value != "" {
		t.Fatalf("清除后掩码应为空：%#v", cleared["settings"])
	}
	if st.AlertSMTPSettings().Password != "" {
		t.Fatalf("密码应被清除：%+v", st.AlertSMTPSettings())
	}

	// 非法端口 400。
	doJSON(t, server, http.MethodPatch, "/api/admin/alert-smtp", adminToken, map[string]any{"port": 70000}, http.StatusBadRequest)
	assertAuditAction(t, st, "alert_smtp.update")
}

// TestHTTPAlertChannelTestEndpoints 覆盖管理员与客服的测试发送接口（即时配置 + 已保存配置两种形式）。
func TestHTTPAlertChannelTestEndpoints(t *testing.T) {
	server, _ := newServicePolicyRouter(t)
	adminToken := loginForToken(t, server, "/api/admin/login", "superadmin", "123456")
	agentA, agentToken := createPolicyTestAgent(t, server, adminToken, "alerttest_http_agent")
	_, tokenB := createPolicyTestAgent(t, server, adminToken, "alerttest_http_agent_b")

	// 鉴权：匿名/客服访问管理员接口 403；管理员访问客服接口 403。
	doJSON(t, server, http.MethodPost, "/api/admin/alert-channels/test", "", nil, http.StatusForbidden)
	doJSON(t, server, http.MethodPost, "/api/admin/alert-channels/test", agentToken, nil, http.StatusForbidden)
	doJSON(t, server, http.MethodPost, "/api/agent/alert-channels/test", adminToken, nil, http.StatusForbidden)

	// 配置不全或非法种类 400。
	doJSON(t, server, http.MethodPost, "/api/admin/alert-channels/test", adminToken,
		map[string]any{"kind": "telegram", "config": map[string]string{}}, http.StatusBadRequest)
	doJSON(t, server, http.MethodPost, "/api/admin/alert-channels/test", adminToken,
		map[string]any{"kind": "sms", "config": map[string]string{"value": "x"}}, http.StatusBadRequest)

	// 测试路由使用 DisabledNotifier，因此直接返回 ok=true。
	adminResult := doJSON(t, server, http.MethodPost, "/api/admin/alert-channels/test", adminToken,
		map[string]any{"kind": "telegram", "config": map[string]string{"token": "t-1234", "chat_id": "1"}}, http.StatusOK)
	if adminResult["ok"] != true {
		t.Fatalf("管理员测试发送应成功：%#v", adminResult)
	}
	agentResult := doJSON(t, server, http.MethodPost, "/api/agent/alert-channels/test", agentToken,
		map[string]any{"kind": "bark", "config": map[string]string{"key": "bark-key-1"}}, http.StatusOK)
	if agentResult["ok"] != true {
		t.Fatalf("客服测试发送应成功：%#v", agentResult)
	}

	// {id} 形式：读取服务端已保存配置发送（密钥不必回传前端）。
	created := doJSON(t, server, http.MethodPost, "/api/admin/agents/"+agentA+"/alert-channels", adminToken, map[string]any{
		"kind":   "telegram",
		"config": map[string]string{"token": "555555:ServerSideToken", "chat_id": "10086"},
	}, http.StatusCreated)
	channelID := created["channel"].(map[string]any)["id"].(string)

	byID := doJSON(t, server, http.MethodPost, "/api/agent/alert-channels/test", agentToken,
		map[string]any{"id": channelID}, http.StatusOK)
	if byID["ok"] != true || byID["message"] == "" {
		t.Fatalf("客服按 ID 测试发送应成功并带 message：%#v", byID)
	}
	if strings.Contains(recString(byID), "ServerSideToken") {
		t.Fatalf("按 ID 测试响应泄漏密钥：%s", recString(byID))
	}
	adminByID := doJSON(t, server, http.MethodPost, "/api/admin/alert-channels/test", adminToken,
		map[string]any{"id": channelID}, http.StatusOK)
	if adminByID["ok"] != true {
		t.Fatalf("管理员按 ID 测试发送应成功：%#v", adminByID)
	}
	// 别的客服不能用别人的渠道 ID；不存在的 ID 返回 404。
	doJSON(t, server, http.MethodPost, "/api/agent/alert-channels/test", tokenB,
		map[string]any{"id": channelID}, http.StatusNotFound)
	doJSON(t, server, http.MethodPost, "/api/agent/alert-channels/test", agentToken,
		map[string]any{"id": "alc_missing"}, http.StatusNotFound)
	doJSON(t, server, http.MethodPost, "/api/admin/alert-channels/test", adminToken,
		map[string]any{"id": "alc_missing"}, http.StatusNotFound)
}

// assertAuditAction 断言最近审计里存在指定 action。
func assertAuditAction(t *testing.T, st *store.MemoryStore, action string) {
	t.Helper()
	for _, event := range st.RecentAuditEvents(50) {
		if event.Action == action {
			return
		}
	}
	t.Fatalf("未找到审计事件 %q", action)
}
