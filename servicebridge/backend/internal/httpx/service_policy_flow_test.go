package httpx

import (
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"customer-service/backend/internal/app"
	"customer-service/backend/internal/config"
	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

// newPolicyFlowRouter 构造带 app.Service 句柄的测试路由（接管/围观用例需要直接造消息）。
func newPolicyFlowRouter(t *testing.T) (http.Handler, *store.MemoryStore, *app.Service) {
	t.Helper()
	st := store.NewMemoryStore(store.Options{})
	// 关闭营业时间限制，避免用例依赖运行时刻。
	_, _ = st.UpdateBusinessHours(domain.BusinessHours{Timezone: "Asia/Shanghai", Enabled: false})
	service := app.NewService(st, realtime.NewHub(), slog.Default())
	return NewRouter(config.Config{Env: "test"}, slog.Default(), service), st, service
}

// TestHTTPAgentTakeoverAndWatch 覆盖接管/只读围观的接口语义、鉴权、审计与已读守卫。
func TestHTTPAgentTakeoverAndWatch(t *testing.T) {
	server, st, service := newPolicyFlowRouter(t)
	adminToken := loginForToken(t, server, "/api/admin/login", "superadmin", "123456")
	agentAID, tokenA := createPolicyTestAgent(t, server, adminToken, "flow_takeover_a")
	agentBID, tokenB := createPolicyTestAgent(t, server, adminToken, "flow_takeover_b")
	_, tokenC := createPolicyTestAgent(t, server, adminToken, "flow_takeover_c")

	// AI 关闭自主接待：避免 AI 回复改变未读数。
	if _, err := st.UpdateAISettings(domain.AISettings{Enabled: true, Mode: domain.AIModeManualOnly}); err != nil {
		t.Fatalf("update ai settings: %v", err)
	}
	// 关闭排队，聚焦接管/围观本身。
	policy := domain.DefaultServicePolicy()
	policy.QueueEnabled = false
	if _, err := st.UpdateServicePolicy(policy); err != nil {
		t.Fatalf("update service policy: %v", err)
	}

	session, err := service.CreateVisitorConversation("203.0.113.90", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	conversationID := session.Conversation.ID

	// 鉴权：匿名 401、管理员/访客 403、不存在的会话 404。
	doJSON(t, server, http.MethodPost, "/api/agent/conversations/"+conversationID+"/takeover", "", nil, http.StatusUnauthorized)
	doJSON(t, server, http.MethodPost, "/api/agent/conversations/"+conversationID+"/takeover", adminToken, nil, http.StatusForbidden)
	doJSON(t, server, http.MethodPost, "/api/agent/conversations/"+conversationID+"/takeover", session.Token, nil, http.StatusUnauthorized)
	doJSON(t, server, http.MethodPost, "/api/agent/conversations/conv_missing/takeover", tokenA, nil, http.StatusNotFound)

	// A 先围观（允许围观他人/未分配会话）。
	watching := doJSON(t, server, http.MethodPost, "/api/agent/conversations/"+conversationID+"/watch", tokenA, nil, http.StatusOK)
	if watching["watching"] != true {
		t.Fatalf("围观接口应返回 watching=true：%#v", watching)
	}
	if !service.IsWatching(agentAID, conversationID) {
		t.Fatal("围观登记未生效")
	}
	if assigned := flowConversationByID(t, st, conversationID).AssignedAgentID; assigned != "" {
		t.Fatalf("围观不得分配会话：%q", assigned)
	}

	// B 正式接管：状态置 assigned，返回会话；A 再接管被拒且带可读 message。
	taken := doJSON(t, server, http.MethodPost, "/api/agent/conversations/"+conversationID+"/takeover", tokenB, nil, http.StatusOK)
	conversation, _ := taken["conversation"].(map[string]any)
	if conversation["status"] != string(domain.ConversationAssigned) {
		t.Fatalf("接管后状态应为 assigned：%#v", conversation)
	}
	if conversation["assigned_agent_id"] != agentBID {
		t.Fatalf("接管后应分配给 B：%#v", conversation["assigned_agent_id"])
	}
	conflict := doJSON(t, server, http.MethodPost, "/api/agent/conversations/"+conversationID+"/takeover", tokenA, nil, http.StatusForbidden)
	if message, _ := conflict["message"].(string); strings.TrimSpace(message) == "" {
		t.Fatalf("接管他人负责的会话应返回可读 message：%#v", conflict)
	}
	// 重复接管幂等。
	doJSON(t, server, http.MethodPost, "/api/agent/conversations/"+conversationID+"/takeover", tokenB, nil, http.StatusOK)

	// 访客发消息 → 未读 +1。
	if _, err := service.AddVisitorMessage(conversationID, "takeover-msg-1", "你好", domain.MessageText); err != nil {
		t.Fatalf("add visitor message: %v", err)
	}
	unread := flowConversationByID(t, st, conversationID).UnreadForAgent
	if unread == 0 {
		t.Fatal("访客消息应产生未读")
	}

	// 围观者可读消息（只读围观），但不能把会话标记已读。
	doJSON(t, server, http.MethodGet, "/api/conversations/"+conversationID+"/messages", tokenA, nil, http.StatusOK)
	doJSON(t, server, http.MethodPost, "/api/conversations/"+conversationID+"/read", tokenA, nil, http.StatusOK)
	if got := flowConversationByID(t, st, conversationID).UnreadForAgent; got != unread {
		t.Fatalf("围观期间不得标记已读：before=%d after=%d", unread, got)
	}
	// 与围观无关的第三个客服仍不可读。
	doJSON(t, server, http.MethodGet, "/api/conversations/"+conversationID+"/messages", tokenC, nil, http.StatusForbidden)
	// 负责客服 B 可以正常已读。
	doJSON(t, server, http.MethodPost, "/api/conversations/"+conversationID+"/read", tokenB, nil, http.StatusOK)
	if got := flowConversationByID(t, st, conversationID).UnreadForAgent; got != 0 {
		t.Fatalf("负责客服已读后未读数应为 0：%d", got)
	}

	// 退出围观 + 审计。
	stopped := doJSON(t, server, http.MethodDelete, "/api/agent/conversations/"+conversationID+"/watch", tokenA, nil, http.StatusOK)
	if stopped["watching"] != false {
		t.Fatalf("退出围观应返回 watching=false：%#v", stopped)
	}
	if service.IsWatching(agentAID, conversationID) {
		t.Fatal("退出围观未生效")
	}
	doJSON(t, server, http.MethodDelete, "/api/agent/conversations/"+conversationID+"/watch", tokenA, nil, http.StatusOK)
	assertAuditAction(t, st, "conversation.takeover")
	assertAuditAction(t, st, "conversation.watch")
	assertAuditAction(t, st, "conversation.watch_stop")
}

// TestHTTPQueueVisitorName 覆盖队列列表新增的 visitor_name（保留 wait_seconds）。
func TestHTTPQueueVisitorName(t *testing.T) {
	server, st, service := newPolicyFlowRouter(t)
	adminToken := loginForToken(t, server, "/api/admin/login", "superadmin", "123456")

	session, err := service.CreateVisitorConversation("203.0.113.101", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	// 覆盖分组/优先级，便于断言分组字段。
	if _, err := st.EnqueueConversation(domain.QueuedConversation{
		ConversationID: session.Conversation.ID,
		GroupName:      "售后",
		Priority:       7,
	}); err != nil {
		t.Fatalf("enqueue conversation: %v", err)
	}

	out := doJSON(t, server, http.MethodGet, "/api/admin/service-queue", adminToken, nil, http.StatusOK)
	if out["total"] != float64(1) {
		t.Fatalf("排队总数不符：%#v", out["total"])
	}
	head := out["queue"].([]any)[0].(map[string]any)
	if name, _ := head["visitor_name"].(string); name == "" {
		t.Fatalf("队列项必须包含 visitor_name：%#v", head)
	}
	if head["visitor_name"] != session.Conversation.VisitorRemark {
		t.Fatalf("visitor_name 应取访客备注：%#v", head["visitor_name"])
	}
	if _, ok := head["wait_seconds"]; !ok {
		t.Fatalf("必须保留 wait_seconds 字段：%#v", head)
	}
	if head["group_name"] != "售后" || head["priority"] != float64(7) {
		t.Fatalf("队列分组/优先级不符：%#v", head)
	}
}

// TestHTTPAlertChannelMaskedField 覆盖渠道列表新增的稳定掩码字段 masked（保留 config_masked 与 max_channels）。
func TestHTTPAlertChannelMaskedField(t *testing.T) {
	server, _, _ := newPolicyFlowRouter(t)
	adminToken := loginForToken(t, server, "/api/admin/login", "superadmin", "123456")
	agentID, _ := createPolicyTestAgent(t, server, adminToken, "masked_http_agent")

	secret := "123456:AAbbCCddEEffGGhh"
	created := doJSON(t, server, http.MethodPost, "/api/admin/agents/"+agentID+"/alert-channels", adminToken, map[string]any{
		"kind":   "telegram",
		"config": map[string]string{"token": secret, "chat_id": "10086"},
	}, http.StatusCreated)
	channel := created["channel"].(map[string]any)
	masked, _ := channel["masked"].(string)
	if !strings.HasPrefix(masked, "****") || strings.Contains(masked, "AAbb") {
		t.Fatalf("telegram masked 字段不符：%#v", channel["masked"])
	}
	if _, ok := channel["config_masked"]; !ok {
		t.Fatalf("必须保留 config_masked：%#v", channel)
	}

	// email 渠道：掩码保留域名。
	created = doJSON(t, server, http.MethodPost, "/api/admin/agents/"+agentID+"/alert-channels", adminToken, map[string]any{
		"kind":   "email",
		"config": map[string]string{"address": "support@example.com"},
	}, http.StatusCreated)
	emailMasked, _ := created["channel"].(map[string]any)["masked"].(string)
	if !strings.Contains(emailMasked, "@example.com") || strings.Contains(emailMasked, "support@") {
		t.Fatalf("email masked 字段不符：%q", emailMasked)
	}

	// 列表同时返回渠道数与上限；密钥与明文都不出现。
	list := doJSON(t, server, http.MethodGet, "/api/agent/alert-channels", loginForToken(t, server, "/api/agent/login", "masked_http_agent", "AgentPass-123456"), nil, http.StatusOK)
	if list["max_channels"] != float64(domain.AlertChannelMaxPerAgent) {
		t.Fatalf("max_channels 不符：%#v", list["max_channels"])
	}
	channels := list["channels"].([]any)
	if len(channels) != 2 {
		t.Fatalf("渠道数量不符：%d", len(channels))
	}
	for _, raw := range channels {
		item := raw.(map[string]any)
		if _, ok := item["masked"].(string); !ok {
			t.Fatalf("每个渠道都必须带字符串 masked：%#v", item)
		}
		if strings.Contains(recString(item), secret) {
			t.Fatalf("渠道列表泄漏密钥：%#v", item)
		}
	}
}

// flowConversationByID 读取会话并断言存在（测试辅助）。
func flowConversationByID(t *testing.T, st store.Store, conversationID string) domain.Conversation {
	t.Helper()
	conversation, ok := st.Conversation(conversationID)
	if !ok {
		t.Fatalf("会话不存在：%s", conversationID)
	}
	return conversation
}
