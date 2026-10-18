package httpx

import (
	"net/http"
	"testing"

	"customer-service/backend/internal/domain"
)

// TestHTTPPolicyAlertsAdminOnlyAndShape：
// /api/admin/policy-alerts 仅管理员可访问（客服 403），返回 waiting 会话的 takeover 告警，
// 含队列位次、访客名与状态。
func TestHTTPPolicyAlertsAdminOnlyAndShape(t *testing.T) {
	router, st := newKnowledgeRouter(t)

	// 未登录 → 403。
	doJSON(t, router, http.MethodGet, "/api/admin/policy-alerts", "", nil, http.StatusForbidden)

	session, err := st.CreateVisitorConversation("203.0.113.31", "web", "zh", "direct", "排队访客")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if _, err := st.EnqueueConversation(domain.QueuedConversation{
		ConversationID: session.Conversation.ID,
		Priority:       3,
		GroupName:      "售前",
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// 客服账号登录后仍不可访问（非管理员）。
	agent, err := st.CreateAgent(domain.Agent{
		Account: "policy_alert_agent",
		Name:    "告警客服",
		Group:   "默认组",
		Status:  domain.AgentOnline,
	}, "AgentPass-123456")
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	agentToken := loginForToken(t, router, "/api/agent/login", agent.Account, "AgentPass-123456")
	doJSON(t, router, http.MethodGet, "/api/admin/policy-alerts", agentToken, nil, http.StatusForbidden)

	// 管理员可访问：waiting + 队列中的会话产生 takeover 告警。
	adminToken := loginForToken(t, router, "/api/admin/login", "superadmin", "123456")
	out := doJSON(t, router, http.MethodGet, "/api/admin/policy-alerts", adminToken, nil, http.StatusOK)
	alerts, ok := out["alerts"].([]any)
	if !ok || len(alerts) != 1 {
		t.Fatalf("应有 1 条告警：%#v", out)
	}
	alert, ok := alerts[0].(map[string]any)
	if !ok {
		t.Fatalf("告警结构不符：%#v", alerts[0])
	}
	if alert["kind"] != "takeover" || alert["conversation_id"] != session.Conversation.ID {
		t.Fatalf("告警类型/会话不符：%#v", alert)
	}
	if alert["queue_position"] != float64(1) {
		t.Fatalf("队列位次应为 1：%#v", alert)
	}
	if alert["visitor_name"] != "排队访客" {
		t.Fatalf("访客名应取备注：%#v", alert)
	}
	if alert["status"] != string(domain.ConversationWaiting) {
		t.Fatalf("状态应为 waiting：%#v", alert)
	}
}
