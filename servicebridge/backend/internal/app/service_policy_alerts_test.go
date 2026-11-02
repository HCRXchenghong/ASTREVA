package app

import (
	"log/slog"
	"testing"
	"time"

	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

// newPolicyAlertFixture 构造告警场景：
//   - 两个 waiting 会话进入队列（第二个优先级更高、等待更久）；
//   - 一个 human_requested 且已分配客服、尚未人工首响的会话；
//   - sla_state=first_response / idle 各一个（状态为 ai_serving，不产生 takeover 告警）。
func newPolicyAlertFixture(t *testing.T) (*Service, store.Store, domain.Agent, map[string]string) {
	t.Helper()
	st := store.NewMemoryStore(store.Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	// 关闭营业时间限制，保证 RequestHumanHandoff 的静默分配可用。
	if _, err := st.UpdateBusinessHours(domain.BusinessHours{Timezone: "Asia/Shanghai", Enabled: false}); err != nil {
		t.Fatalf("update business hours: %v", err)
	}
	service := NewService(st, realtime.NewHub(), slog.Default())

	policy := domain.DefaultServicePolicy()
	policy.SLAFirstResponseSeconds = 90 // 向上取整 => minutes=2
	policy.SLAIdleMinutes = 7
	if _, err := st.UpdateServicePolicy(policy); err != nil {
		t.Fatalf("update service policy: %v", err)
	}

	agent, err := st.CreateAgent(domain.Agent{
		Account: "alert_agent",
		Name:    "告警客服",
		Group:   "默认组",
		Status:  domain.AgentOnline,
	}, "AgentPass-123456")
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	admin, _, err := st.LoginAdmin("superadmin", "123456")
	if err != nil {
		t.Fatalf("login admin: %v", err)
	}

	newConversation := func(ip, remark string) domain.Conversation {
		session, err := st.CreateVisitorConversation(ip, "web", "zh", "direct", remark)
		if err != nil {
			t.Fatalf("create conversation: %v", err)
		}
		return session.Conversation
	}

	// 队列：convQueueHigh 优先级高且先入队（等待更久），convQueueLow 后入队。
	convQueueHigh := newConversation("203.0.113.11", "高优先访客")
	if _, err := st.EnqueueConversation(domain.QueuedConversation{ConversationID: convQueueHigh.ID, Priority: 5, GroupName: "售前"}); err != nil {
		t.Fatalf("enqueue high: %v", err)
	}
	time.Sleep(1100 * time.Millisecond)
	convQueueLow := newConversation("203.0.113.12", "普通访客")
	if _, err := st.EnqueueConversation(domain.QueuedConversation{ConversationID: convQueueLow.ID, Priority: 1, GroupName: "售前"}); err != nil {
		t.Fatalf("enqueue low: %v", err)
	}

	// human_requested 且已分配客服、尚无人工首响。
	convHuman := newConversation("203.0.113.13", "转人工访客")
	if _, err := st.RequestHumanHandoff(convHuman.ID); err != nil {
		t.Fatalf("request human handoff: %v", err)
	}

	// SLA 告警：转接后交回 AI（状态 ai_serving），再打上 sla_state 标记。
	prepareSLA := func(ip, remark, state string) domain.Conversation {
		conv := newConversation(ip, remark)
		if _, _, err := st.TransferConversation(admin, conv.ID, agent.ID, ""); err != nil {
			t.Fatalf("transfer: %v", err)
		}
		if _, err := st.ReturnConversationToAI(conv.ID); err != nil {
			t.Fatalf("return to ai: %v", err)
		}
		if _, err := st.SetConversationSLAState(conv.ID, state); err != nil {
			t.Fatalf("set sla state: %v", err)
		}
		return conv
	}
	convIdle := prepareSLA("203.0.113.14", "空闲访客", domain.ConversationSLAStateIdle)
	convFirst := prepareSLA("203.0.113.15", "首响访客", domain.ConversationSLAStateFirstResponse)

	return service, st, agent, map[string]string{
		"queue_high": convQueueHigh.ID,
		"queue_low":  convQueueLow.ID,
		"human":      convHuman.ID,
		"idle":       convIdle.ID,
		"first":      convFirst.ID,
	}
}

// TestPolicyAlertsFieldsAndOrder：
// 三类告警的字段与紧急度排序：takeover（同级等待更久在前）→ sla_idle → sla_first_response。
func TestPolicyAlertsFieldsAndOrder(t *testing.T) {
	service, _, agent, ids := newPolicyAlertFixture(t)

	alerts := service.PolicyAlerts(time.Now().UTC())
	if len(alerts) != 5 {
		t.Fatalf("应产生 5 条告警（3 takeover + 1 idle + 1 first_response）：%+v", alerts)
	}

	// 排序：kind 紧急度单调不减；同级按等待时长倒序。
	urgency := map[string]int{PolicyAlertTakeover: 0, PolicyAlertSLAIdle: 1, PolicyAlertSLAFirstResponse: 2}
	for i := 1; i < len(alerts); i++ {
		if urgency[alerts[i-1].Kind] > urgency[alerts[i].Kind] {
			t.Fatalf("告警排序错误：%+v", alerts)
		}
		if alerts[i-1].Kind == alerts[i].Kind && alerts[i-1].WaitingSeconds < alerts[i].WaitingSeconds {
			t.Fatalf("同级告警应按等待时间倒序：%+v", alerts)
		}
	}
	if alerts[0].Kind != PolicyAlertTakeover || alerts[0].ConversationID != ids["queue_high"] {
		t.Fatalf("最紧急应是队列中等待最久的会话：%+v", alerts[0])
	}

	byID := map[string]PolicyAlert{}
	for _, alert := range alerts {
		byID[alert.ConversationID] = alert
	}

	// 队列中的 waiting 会话：位次按 priority DESC, enqueued_at ASC 计算为 1-based。
	high := byID[ids["queue_high"]]
	if high.Kind != PolicyAlertTakeover || high.QueuePosition != 1 || high.Status != string(domain.ConversationWaiting) {
		t.Fatalf("高优先会话字段不符：%+v", high)
	}
	if high.VisitorName != "高优先访客" || high.WaitingSeconds < 1 {
		t.Fatalf("高优先会话访客名/等待时长不符：%+v", high)
	}
	low := byID[ids["queue_low"]]
	if low.QueuePosition != 2 || low.WaitingSeconds != 0 {
		t.Fatalf("低优先会话位次/等待时长不符：%+v", low)
	}

	// human_requested 且已分配客服：takeover 告警，无队列位次，带负责客服名。
	human := byID[ids["human"]]
	if human.Kind != PolicyAlertTakeover || human.Status != string(domain.ConversationHumanRequested) {
		t.Fatalf("human_requested 会话应产生 takeover 告警：%+v", human)
	}
	if human.QueuePosition != 0 {
		t.Fatalf("未入队会话不应有队列位次：%+v", human)
	}
	if human.AssignedAgentName != agent.Name {
		t.Fatalf("应带负责客服名 %q：%+v", agent.Name, human)
	}
	if human.VisitorName != "转人工访客" {
		t.Fatalf("访客名应取备注：%+v", human)
	}

	// sla_idle：minutes 取策略阈值；sla_first_response：阈值秒数向上取整为分钟。
	idle := byID[ids["idle"]]
	if idle.Kind != PolicyAlertSLAIdle || idle.Minutes != 7 || idle.Status != string(domain.ConversationAIServing) {
		t.Fatalf("sla_idle 告警字段不符：%+v", idle)
	}
	first := byID[ids["first"]]
	if first.Kind != PolicyAlertSLAFirstResponse || first.Minutes != 2 {
		t.Fatalf("sla_first_response 告警字段不符：%+v", first)
	}
	if idle.AssignedAgentName != agent.Name || first.AssignedAgentName != agent.Name {
		t.Fatalf("SLA 告警应带负责客服名：idle=%+v first=%+v", idle, first)
	}

	// idle 必须排在 first_response 之前。
	idleIndex, firstIndex := -1, -1
	for i, alert := range alerts {
		switch alert.Kind {
		case PolicyAlertSLAIdle:
			idleIndex = i
		case PolicyAlertSLAFirstResponse:
			firstIndex = i
		}
	}
	if idleIndex < 0 || firstIndex < 0 || idleIndex > firstIndex {
		t.Fatalf("idle 应排在 first_response 之前：%+v", alerts)
	}
}

// TestPolicyAlertsSkipsFirstRespondedAndClosed：
// 已有人工首响的 waiting 会话、已结束会话都不产生告警。
func TestPolicyAlertsSkipsFirstRespondedAndClosed(t *testing.T) {
	st := store.NewMemoryStore(store.Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	if _, err := st.UpdateBusinessHours(domain.BusinessHours{Timezone: "Asia/Shanghai", Enabled: false}); err != nil {
		t.Fatalf("update business hours: %v", err)
	}
	service := NewService(st, realtime.NewHub(), slog.Default())

	// 已首响：走一次真实客服回复（AddAgentMessage 会写 first_response_at）。
	session, err := st.CreateVisitorConversation("203.0.113.21", "web", "zh", "direct", "已回复访客")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	agent, err := st.CreateAgent(domain.Agent{Account: "alert_agent2", Name: "客服", Group: "默认组", Status: domain.AgentOnline}, "AgentPass-123456")
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if _, err := st.TakeoverConversation(agent.ID, session.Conversation.ID); err != nil {
		t.Fatalf("takeover: %v", err)
	}
	if _, err := st.AddAgentMessage(agent.ID, session.Conversation.ID, "m1", "您好，请问有什么可以帮您？", domain.MessageText, nil); err != nil {
		t.Fatalf("add agent message: %v", err)
	}
	conversation, ok := st.Conversation(session.Conversation.ID)
	if !ok || conversation.FirstResponseAt == nil {
		t.Fatalf("客服回复后应有首响时间：%+v", conversation)
	}

	// 已结束会话：即便保留 sla_state 也不应告警。
	closedSession, err := st.CreateVisitorConversation("203.0.113.22", "web", "zh", "direct", "已结束访客")
	if err != nil {
		t.Fatalf("create closed conversation: %v", err)
	}
	if _, err := st.SetConversationSLAState(closedSession.Conversation.ID, domain.ConversationSLAStateIdle); err != nil {
		t.Fatalf("set sla state: %v", err)
	}
	admin, _, err := st.LoginAdmin("superadmin", "123456")
	if err != nil {
		t.Fatalf("login admin: %v", err)
	}
	if _, _, err := st.CloseConversation(admin, closedSession.Conversation.ID); err != nil {
		t.Fatalf("close conversation: %v", err)
	}

	alerts := service.PolicyAlerts(time.Now().UTC())
	if len(alerts) != 0 {
		t.Fatalf("已首响/已结束会话不应产生告警：%+v", alerts)
	}
}
