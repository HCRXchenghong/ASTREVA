package app

import (
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

// ===== 批次 4 第三波（2b）：SLA 计时器 / 自动关闭 / AI 关键词优先级 的应用层测试 =====

// setServicePolicy 在当前策略基础上做局部修改并保存。
func setServicePolicy(t *testing.T, st store.Store, mutate func(*domain.ServicePolicy)) {
	t.Helper()
	policy := st.ServicePolicy()
	mutate(&policy)
	if _, err := st.UpdateServicePolicy(policy); err != nil {
		t.Fatalf("更新客服策略失败：%v", err)
	}
}

// slaEvents 过滤出推送给客服的 conversation.sla 事件。
func slaEvents(recorder *eventRecorder) []recordedEvent {
	return recorder.byEvent("conversation.sla")
}

// slaPayload 断言事件体是 conversationSLAPayload 并返回。
func slaPayload(t *testing.T, event recordedEvent) conversationSLAPayload {
	t.Helper()
	payload, ok := event.data.(conversationSLAPayload)
	if !ok {
		t.Fatalf("conversation.sla 事件体类型不符：%#v", event.data)
	}
	return payload
}

// TestPolicySweepFirstResponseTimeoutOnceAndClearedByReply 覆盖 2a：
// 超时前不告警；超时后只推一次事件、渠道派发一次、审计一条；再巡检不重复；客服回复后清空状态。
func TestPolicySweepFirstResponseTimeoutOnceAndClearedByReply(t *testing.T) {
	service, st, recorder := newFlowService(t)
	setServicePolicy(t, st, func(policy *domain.ServicePolicy) {
		policy.SLAFirstResponseSeconds = 60
		policy.SLAIdleMinutes = 0       // 关闭 idle，隔离首响用例
		policy.AutoCloseIdleMinutes = 0 // 关闭自动关闭
		policy.TakeoverPopupEnabled = false
	})
	agent := createFlowAgent(t, st, "sla_fr_agent", 5)
	if _, _, err := st.SetAgentStatus(agent.ID, domain.AgentOnline); err != nil {
		t.Fatalf("客服上线失败：%v", err)
	}
	// 先注入 notifier（后端 push 的 goroutine 会读取它，避免测试期并发写）。
	notifier := &fakeAlertNotifier{}
	service.SetNotifier(notifier)
	session, err := service.CreateVisitorConversation("203.0.113.201", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	conversationID := session.Conversation.ID
	if session.Conversation.AssignedAgentID != agent.ID {
		t.Fatalf("会话应分配给在线客服：%+v", session.Conversation)
	}
	// 提醒渠道在建会话之后创建：分配时渠道为空，不会产生派发，计数只反映巡检结果。
	if _, err := st.CreateAgentAlertChannel(agent.ID, domain.AlertChannelKindBark, map[string]string{"key": "bark-key-sla"}); err != nil {
		t.Fatalf("创建提醒渠道失败：%v", err)
	}
	createdAt := session.Conversation.CreatedAt
	recorder.reset()

	// 超时前：仅过去 30 秒，不告警。
	service.RunPolicySweep(createdAt.Add(30 * time.Second))
	if events := slaEvents(recorder); len(events) != 0 {
		t.Fatalf("未超时不得告警：%+v", events)
	}
	if got := countAuditAction(st, "conversation.sla_first_response"); got != 0 {
		t.Fatalf("未超时不得写审计，实际 %d 条", got)
	}
	if got := notifier.count(); got != 0 {
		t.Fatalf("未超时不得派发渠道，实际 %d 次", got)
	}

	// 超过阈值：只推一次事件 + 一次渠道派发 + 一条审计。
	service.RunPolicySweep(createdAt.Add(90 * time.Second))
	events := slaEvents(recorder)
	if len(events) != 1 {
		t.Fatalf("首响超时应只推一次事件，实际 %d 次：%+v", len(events), events)
	}
	if events[0].key != agent.ID {
		t.Fatalf("首响事件应推给负责客服 %q，实际 %q", agent.ID, events[0].key)
	}
	payload := slaPayload(t, events[0])
	if payload.Kind != "first_response" || payload.ConversationID != conversationID || payload.Minutes < 1 {
		t.Fatalf("首响事件体不符：%+v", payload)
	}
	waitForFlowCondition(t, "首响超时派发提醒渠道", func() bool { return notifier.count() >= 1 })
	if got := notifier.count(); got != 1 {
		t.Fatalf("首响超时应只派发一次，实际 %d 次", got)
	}
	if got := notifier.snapshot()[0].payload.Title; got != "首响超时" {
		t.Fatalf("渠道派发标题应为「首响超时」，实际 %q", got)
	}
	if got := countAuditAction(st, "conversation.sla_first_response"); got != 1 {
		t.Fatalf("首响超时应只写一条审计，实际 %d 条", got)
	}
	if state := flowConversation(t, st, conversationID).SLAState; state != domain.ConversationSLAStateFirstResponse {
		t.Fatalf("首响超时后 sla_state 应为 first_response，实际 %q", state)
	}

	// 再巡检：幂等，不重复告警/派发/审计。
	recorder.reset()
	service.RunPolicySweep(createdAt.Add(120 * time.Second))
	if events := slaEvents(recorder); len(events) != 0 {
		t.Fatalf("同一状态只应告警一次：%+v", events)
	}
	if got := countAuditAction(st, "conversation.sla_first_response"); got != 1 {
		t.Fatalf("重复巡检不得重复写审计，实际 %d 条", got)
	}

	// 客服真实回复：写入 first_response_at / last_agent_reply_at，并清空 sla_state。
	if _, err := service.AddAgentMessage(agent.ID, conversationID, "sla-fr-reply", "您好，已为您跟进。", domain.MessageText); err != nil {
		t.Fatalf("客服回复失败：%v", err)
	}
	replied := flowConversation(t, st, conversationID)
	if replied.FirstResponseAt == nil {
		t.Fatal("客服首次回复后应写入 first_response_at")
	}
	if replied.LastAgentReplyAt == nil {
		t.Fatal("客服回复后应更新 last_agent_reply_at")
	}
	if replied.SLAState != "" {
		t.Fatalf("客服回复后应清空 sla_state，实际 %q", replied.SLAState)
	}
}

// TestPolicySweepIdleReturnsToAIAndAIResponds 覆盖 2b 的 ai 分支：
// 待回复超时只告警一次，状态回到 ai_serving 且保留负责客服；随后访客再发消息确实调用上游。
func TestPolicySweepIdleReturnsToAIAndAIResponds(t *testing.T) {
	stub, calls, _ := newKnowledgeStub(t, "AI 已接手回复。")
	service, st := newKnowledgeService(t, domain.AIModeAlwaysAI, stub.URL)
	if _, err := st.UpdateBusinessHours(domain.BusinessHours{Timezone: "UTC", Enabled: false}); err != nil {
		t.Fatalf("更新营业时间失败：%v", err)
	}
	setServicePolicy(t, st, func(policy *domain.ServicePolicy) {
		policy.QueueEnabled = false
		policy.SLAFirstResponseSeconds = 0
		policy.SLAIdleMinutes = 5
		policy.SLAIdleAction = domain.SLAIdleActionAI
		policy.AutoCloseIdleMinutes = 0
		policy.AIKeywordPriority = domain.AIKeywordPriorityKeywordFirst
		policy.TakeoverPopupEnabled = false
	})
	agent := createFlowAgent(t, st, "sla_idle_ai", 5)
	if _, _, err := st.SetAgentStatus(agent.ID, domain.AgentOnline); err != nil {
		t.Fatalf("客服上线失败：%v", err)
	}
	session, err := service.CreateVisitorConversation("203.0.113.202", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	conversationID := session.Conversation.ID
	// 接管：状态置为 assigned（AI 暂时静默），模拟"客服负责但一直未回复"。
	if _, err := service.TakeoverConversation(agent.ID, conversationID); err != nil {
		t.Fatalf("接管失败：%v", err)
	}
	recorder := newEventRecorder(service.Hub())
	recorder.reset()

	result, err := service.AddVisitorMessage(conversationID, "sla-idle-1", "请问什么时候发货", domain.MessageText)
	if err != nil {
		t.Fatalf("访客发消息失败：%v", err)
	}
	if result.NeedAI {
		t.Fatal("人工已接管（assigned）时不得触发 AI")
	}
	if got := atomic.LoadInt32(calls); got != 0 {
		t.Fatalf("人工已接管时不得调用上游，实际 %d 次", got)
	}
	visitorAt := result.Input.CreatedAt

	// 超过 idle 分钟：告警 + 审计 + 交回 AI。
	service.RunPolicySweep(visitorAt.Add(6 * time.Minute))
	events := slaEvents(recorder)
	if len(events) != 1 {
		t.Fatalf("待回复超时应只推一次事件，实际 %d 次：%+v", len(events), events)
	}
	if payload := slaPayload(t, events[0]); payload.Kind != "idle" || payload.Minutes < 5 {
		t.Fatalf("待回复事件体不符：%+v", payload)
	}
	if got := countAuditAction(st, "conversation.sla_idle"); got != 1 {
		t.Fatalf("待回复超时应写一条审计，实际 %d 条", got)
	}
	if got := countAuditAction(st, "conversation.sla_idle.ai"); got != 1 {
		t.Fatalf("交回 AI 应写一条审计，实际 %d 条", got)
	}
	updated := flowConversation(t, st, conversationID)
	if updated.Status != domain.ConversationAIServing {
		t.Fatalf("待回复超时应交回 AI 接待（ai_serving），实际 %q", updated.Status)
	}
	if updated.AssignedAgentID != agent.ID {
		t.Fatalf("交回 AI 后应保留负责客服便于追溯：%+v", updated.AssignedAgentID)
	}

	// 幂等：同一状态再巡检不重复告警。
	recorder.reset()
	service.RunPolicySweep(visitorAt.Add(12 * time.Minute))
	if events := slaEvents(recorder); len(events) != 0 {
		t.Fatalf("同一状态只应告警一次：%+v", events)
	}
	if got := countAuditAction(st, "conversation.sla_idle"); got != 1 {
		t.Fatalf("重复巡检不得重复写审计，实际 %d 条", got)
	}

	// 后续访客消息确实走 AI（上游被调用）。
	if _, err := service.AddVisitorMessage(conversationID, "sla-idle-2", "那我先下单了", domain.MessageText); err != nil {
		t.Fatalf("访客发消息失败：%v", err)
	}
	waitForFlowCondition(t, "交回 AI 后访客消息调用上游", func() bool { return atomic.LoadInt32(calls) >= 1 })
	waitForAIMessage(t, st, conversationID)
}

// TestPolicySweepIdleReassignToOtherAgent 覆盖 2b 的 reassign 分支（有候选）：
// 转给其他在线且未达并发的客服，并写转接审计。
func TestPolicySweepIdleReassignToOtherAgent(t *testing.T) {
	service, st, recorder := newFlowService(t)
	setServicePolicy(t, st, func(policy *domain.ServicePolicy) {
		policy.QueueEnabled = false
		policy.SLAFirstResponseSeconds = 0
		policy.SLAIdleMinutes = 5
		policy.SLAIdleAction = domain.SLAIdleActionReassign
		policy.AutoCloseIdleMinutes = 0
		policy.UnattendedEnabled = false
		policy.TakeoverPopupEnabled = false
	})
	agentA := createFlowAgent(t, st, "sla_reassign_a", 5)
	agentB := createFlowAgent(t, st, "sla_reassign_b", 5)
	// 先只让 A 上线：会话确定分配给 A，再让 B 上线作为候选。
	if _, _, err := st.SetAgentStatus(agentA.ID, domain.AgentOnline); err != nil {
		t.Fatalf("A 上线失败：%v", err)
	}
	session, err := service.CreateVisitorConversation("203.0.113.203", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	conversationID := session.Conversation.ID
	if _, err := service.TakeoverConversation(agentA.ID, conversationID); err != nil {
		t.Fatalf("A 接管失败：%v", err)
	}
	if _, _, err := st.SetAgentStatus(agentB.ID, domain.AgentOnline); err != nil {
		t.Fatalf("B 上线失败：%v", err)
	}
	result, err := service.AddVisitorMessage(conversationID, "sla-reassign-1", "还在吗", domain.MessageText)
	if err != nil {
		t.Fatalf("访客发消息失败：%v", err)
	}
	recorder.reset()

	service.RunPolicySweep(result.Input.CreatedAt.Add(6 * time.Minute))
	updated := flowConversation(t, st, conversationID)
	if updated.AssignedAgentID != agentB.ID {
		t.Fatalf("待回复超时应转给其他在线客服 B，实际 %q", updated.AssignedAgentID)
	}
	if got := agentCurrentConversations(t, st, agentA.ID); got != 0 {
		t.Fatalf("转接后原客服并发应释放为 0，实际 %d", got)
	}
	if got := agentCurrentConversations(t, st, agentB.ID); got != 1 {
		t.Fatalf("转接后目标客服并发应为 1，实际 %d", got)
	}
	if got := countAuditAction(st, "conversation.sla_idle.reassign"); got != 1 {
		t.Fatalf("转接应写一条审计，实际 %d 条", got)
	}
	assigned := recorder.byEvent("conversation.assigned")
	if len(assigned) == 0 || assigned[0].key != agentB.ID {
		t.Fatalf("转接应通知目标客服：%+v", assigned)
	}
}

// TestPolicySweepIdleReassignWithoutCandidateQueues 覆盖 2b 的 reassign 分支（无候选）：
// 释放当前客服并重新入队，访客收到排队位置。
func TestPolicySweepIdleReassignWithoutCandidateQueues(t *testing.T) {
	service, st, recorder := newFlowService(t)
	setServicePolicy(t, st, func(policy *domain.ServicePolicy) {
		policy.QueueEnabled = true
		policy.SLAFirstResponseSeconds = 0
		policy.SLAIdleMinutes = 5
		policy.SLAIdleAction = domain.SLAIdleActionReassign
		policy.AutoCloseIdleMinutes = 0
		policy.UnattendedEnabled = false
		policy.TakeoverPopupEnabled = false
	})
	agent := createFlowAgent(t, st, "sla_queue_agent", 5)
	if _, _, err := st.SetAgentStatus(agent.ID, domain.AgentOnline); err != nil {
		t.Fatalf("客服上线失败：%v", err)
	}
	session, err := service.CreateVisitorConversation("203.0.113.204", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	conversationID := session.Conversation.ID
	if _, err := service.TakeoverConversation(agent.ID, conversationID); err != nil {
		t.Fatalf("接管失败：%v", err)
	}
	result, err := service.AddVisitorMessage(conversationID, "sla-queue-1", "麻烦回我一下", domain.MessageText)
	if err != nil {
		t.Fatalf("访客发消息失败：%v", err)
	}
	recorder.reset()

	service.RunPolicySweep(result.Input.CreatedAt.Add(6 * time.Minute))
	updated := flowConversation(t, st, conversationID)
	if updated.AssignedAgentID != "" {
		t.Fatalf("没有候选客服时应释放分配，实际 %q", updated.AssignedAgentID)
	}
	if got := agentCurrentConversations(t, st, agent.ID); got != 0 {
		t.Fatalf("释放后客服并发应为 0，实际 %d", got)
	}
	queue := st.QueueList()
	if len(queue) != 1 || queue[0].ConversationID != conversationID {
		t.Fatalf("没有候选客服时应重新入队：%+v", queue)
	}
	positions := recorder.byEvent("conversation.queue_position")
	if len(positions) == 0 {
		t.Fatal("重新入队应给访客推送排队位置")
	}
	if got := countAuditAction(st, "conversation.sla_idle.reassign"); got != 1 {
		t.Fatalf("回退排队应写一条审计，实际 %d 条", got)
	}
}

// TestPolicySweepAutoCloseOnceAndDisabled 覆盖 2c：
// 超时关闭一次（访客收到"会话已结束"），再巡检不重复；阈值为 0 时不关闭。
func TestPolicySweepAutoCloseOnceAndDisabled(t *testing.T) {
	service, st, _ := newFlowService(t)
	setServicePolicy(t, st, func(policy *domain.ServicePolicy) {
		policy.QueueEnabled = false
		policy.SLAFirstResponseSeconds = 0
		policy.SLAIdleMinutes = 0
		policy.AutoCloseIdleMinutes = 30
		policy.TakeoverPopupEnabled = false
	})
	agent := createFlowAgent(t, st, "sla_close_agent", 5)
	if _, _, err := st.SetAgentStatus(agent.ID, domain.AgentOnline); err != nil {
		t.Fatalf("客服上线失败：%v", err)
	}
	session, err := service.CreateVisitorConversation("203.0.113.205", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	conversationID := session.Conversation.ID
	lastActivity := session.Conversation.LastMessageAt
	if lastActivity == nil {
		t.Fatalf("会话应记录最后消息时间：%+v", session.Conversation)
	}

	// 未到阈值：不关闭。
	service.RunPolicySweep(lastActivity.Add(10 * time.Minute))
	if got := flowConversation(t, st, conversationID); got.Status == domain.ConversationClosed {
		t.Fatal("未到阈值不得自动关闭")
	}

	// 超过阈值：关闭一次并写审计，访客收到"会话已结束"。
	service.RunPolicySweep(lastActivity.Add(31 * time.Minute))
	closed := flowConversation(t, st, conversationID)
	if closed.Status != domain.ConversationClosed {
		t.Fatalf("超过阈值应自动关闭，实际 %q", closed.Status)
	}
	if got := countAuditAction(st, "conversation.auto_closed"); got != 1 {
		t.Fatalf("自动关闭应写一条审计，实际 %d 条", got)
	}
	if ended := countMessageContent(st, conversationID, "会话已结束"); ended != 1 {
		t.Fatalf("访客应收到一条结束提示，实际 %d 条", ended)
	}

	// 再巡检：已结束的会话不再处理。
	service.RunPolicySweep(lastActivity.Add(120 * time.Minute))
	if got := countAuditAction(st, "conversation.auto_closed"); got != 1 {
		t.Fatalf("已结束会话不得重复关闭，实际审计 %d 条", got)
	}
	if ended := countMessageContent(st, conversationID, "会话已结束"); ended != 1 {
		t.Fatalf("已结束会话不得重复下发结束提示，实际 %d 条", ended)
	}

	// 阈值为 0：即使静默很久也不关闭。
	setServicePolicy(t, st, func(policy *domain.ServicePolicy) { policy.AutoCloseIdleMinutes = 0 })
	other, err := service.CreateVisitorConversation("203.0.113.206", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	otherActivity := other.Conversation.LastMessageAt
	service.RunPolicySweep(otherActivity.Add(48 * time.Hour))
	if got := flowConversation(t, st, other.Conversation.ID); got.Status == domain.ConversationClosed {
		t.Fatal("auto_close_idle_minutes=0 时不得自动关闭")
	}
	if got := countAuditAction(st, "conversation.auto_closed"); got != 1 {
		t.Fatalf("阈值为 0 时不得新增关闭审计，实际 %d 条", got)
	}
}

// countMessageContent 统计会话内指定内容的消息条数。
func countMessageContent(st store.Store, conversationID, content string) int {
	count := 0
	for _, msg := range st.Messages(conversationID) {
		if msg.Content == content {
			count++
		}
	}
	return count
}

// TestAIKeywordPriorityModes 覆盖四种 AI/关键词优先级模式：
// 通过直接调用消息处理（service.AddVisitorMessage）断言关键词回复是否生成与上游调用次数。
func TestAIKeywordPriorityModes(t *testing.T) {
	const keywordReply = "价格请咨询在线客服"
	cases := []struct {
		name        string
		priority    string
		aiEnabled   bool
		withStub    bool
		wantKeyword bool // 命中关键词时是否生成关键词回复
		wantNeedAI  bool // 命中关键词时是否触发 AI
		plainNeedAI bool // 未命中关键词时是否触发 AI
	}{
		{"keyword_first", domain.AIKeywordPriorityKeywordFirst, true, true, true, false, true},
		{"keyword_first_ai_disabled", domain.AIKeywordPriorityKeywordFirst, false, false, true, false, false},
		{"ai_first", domain.AIKeywordPriorityAIFirst, true, true, false, true, true},
		{"ai_first_ai_unavailable", domain.AIKeywordPriorityAIFirst, false, false, true, false, false},
		{"keyword_only", domain.AIKeywordPriorityKeywordOnly, true, true, true, false, false},
		{"ai_only", domain.AIKeywordPriorityAIOnly, true, true, false, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls *int32
			baseURL := ""
			if tc.withStub {
				stub, counter, _ := newKnowledgeStub(t, "AI 自动回复内容")
				baseURL = stub.URL
				calls = counter
			}
			st := store.NewMemoryStore(store.Options{})
			if _, err := st.UpdateBusinessHours(domain.BusinessHours{Timezone: "UTC", Enabled: false}); err != nil {
				t.Fatalf("更新营业时间失败：%v", err)
			}
			settings := domain.AISettings{Enabled: tc.aiEnabled, Mode: domain.AIModeAlwaysAI}
			if baseURL != "" {
				settings.BaseURL = baseURL
				settings.APIKey = "test-key"
				settings.Model = "test-model"
				settings.APIType = domain.AIAPITypeChatCompletions
				settings.TimeoutSeconds = 5
			}
			if _, err := st.UpdateAISettings(settings); err != nil {
				t.Fatalf("更新 AI 设置失败：%v", err)
			}
			setServicePolicy(t, st, func(policy *domain.ServicePolicy) {
				policy.QueueEnabled = false
				policy.AIKeywordPriority = tc.priority
				policy.SLAFirstResponseSeconds = 0
				policy.SLAIdleMinutes = 0
				policy.AutoCloseIdleMinutes = 0
				policy.UnattendedEnabled = false
				policy.TakeoverPopupEnabled = false
			})
			if _, err := st.CreateKeywordRule(domain.KeywordRule{
				Keyword: "价格", MatchType: "contains", Reply: keywordReply, Enabled: true, Action: "text",
			}); err != nil {
				t.Fatalf("创建关键词规则失败：%v", err)
			}
			service := NewService(st, realtime.NewHub(), slog.Default())

			keywordSession, err := service.CreateVisitorConversation("203.0.113.221", "web", "zh", "direct", "")
			if err != nil {
				t.Fatalf("创建会话失败：%v", err)
			}
			keywordResult, err := service.AddVisitorMessage(keywordSession.Conversation.ID, "kw-1", "这个价格多少", domain.MessageText)
			if err != nil {
				t.Fatalf("访客发消息失败：%v", err)
			}
			gotKeyword := false
			for _, msg := range keywordResult.Generated {
				if msg.Content == keywordReply {
					gotKeyword = true
				}
			}
			if gotKeyword != tc.wantKeyword {
				t.Fatalf("关键词回复生成 = %v，want %v（模式 %s）：%+v", gotKeyword, tc.wantKeyword, tc.name, keywordResult.Generated)
			}
			if keywordResult.NeedAI != tc.wantNeedAI {
				t.Fatalf("命中关键词时 NeedAI = %v，want %v（模式 %s）", keywordResult.NeedAI, tc.wantNeedAI, tc.name)
			}
			if tc.wantNeedAI {
				waitForFlowCondition(t, "关键词消息触发 AI 上游", func() bool { return atomic.LoadInt32(calls) >= 1 })
			}

			// 未命中关键词：只断言是否触发 AI 与上游调用次数，不改变其它语义。
			plainSession, err := service.CreateVisitorConversation("203.0.113.222", "web", "zh", "direct", "")
			if err != nil {
				t.Fatalf("创建会话失败：%v", err)
			}
			before := int32(0)
			if calls != nil {
				before = atomic.LoadInt32(calls)
			}
			plainResult, err := service.AddVisitorMessage(plainSession.Conversation.ID, "plain-1", "随便聊聊吧", domain.MessageText)
			if err != nil {
				t.Fatalf("访客发消息失败：%v", err)
			}
			for _, msg := range plainResult.Generated {
				if msg.Content == keywordReply {
					t.Fatalf("未命中关键词不得生成关键词回复：%+v", plainResult.Generated)
				}
			}
			if plainResult.NeedAI != tc.plainNeedAI {
				t.Fatalf("未命中关键词时 NeedAI = %v，want %v（模式 %s）", plainResult.NeedAI, tc.plainNeedAI, tc.name)
			}
			if tc.plainNeedAI {
				waitForFlowCondition(t, "普通消息触发 AI 上游", func() bool { return atomic.LoadInt32(calls) > before })
			} else if calls != nil && atomic.LoadInt32(calls) != before {
				t.Fatalf("不应调用上游，实际增加 %d 次", atomic.LoadInt32(calls)-before)
			}
		})
	}
}

// sweepCounter 包装存储，统计 ServicePolicy() 被调用次数（每次巡检调用一次）。
type sweepCounter struct {
	store.Store
	calls int32
}

func (c *sweepCounter) ServicePolicy() domain.ServicePolicy {
	atomic.AddInt32(&c.calls, 1)
	return c.Store.ServicePolicy()
}

// TestPolicyLoopTickIntervalAndGracefulStop 覆盖 ticker：
// 环境变量解析（默认 30、最小 5、非法回落）与 Start/Stop 的优雅停止。
func TestPolicyLoopTickIntervalAndGracefulStop(t *testing.T) {
	t.Setenv("SERVICE_POLICY_TICK_SECONDS", "7")
	if got := PolicyTickInterval(); got != 7*time.Second {
		t.Fatalf("SERVICE_POLICY_TICK_SECONDS=7 应解析为 7s，实际 %v", got)
	}
	t.Setenv("SERVICE_POLICY_TICK_SECONDS", "2")
	if got := PolicyTickInterval(); got != MinPolicyTickSeconds*time.Second {
		t.Fatalf("小于最小间隔应钳制到 %ds，实际 %v", MinPolicyTickSeconds, got)
	}
	t.Setenv("SERVICE_POLICY_TICK_SECONDS", "abc")
	if got := PolicyTickInterval(); got != DefaultPolicyTickSeconds*time.Second {
		t.Fatalf("非法值应回落默认 %ds，实际 %v", DefaultPolicyTickSeconds, got)
	}
	t.Setenv("SERVICE_POLICY_TICK_SECONDS", "")
	if got := PolicyTickInterval(); got != DefaultPolicyTickSeconds*time.Second {
		t.Fatalf("未配置应回落默认 %ds，实际 %v", DefaultPolicyTickSeconds, got)
	}

	counter := &sweepCounter{Store: store.NewMemoryStore(store.Options{})}
	service := NewService(counter, realtime.NewHub(), slog.Default())
	// 用远小于默认值的间隔验证"多次巡检"（避免长时间 sleep）。
	service.StartPolicyLoop(10 * time.Millisecond)
	service.StartPolicyLoop(10 * time.Millisecond) // 重复启动应幂等
	waitForFlowCondition(t, "ticker 触发多次巡检", func() bool { return atomic.LoadInt32(&counter.calls) >= 3 })

	service.StopPolicyLoop()
	stopped := atomic.LoadInt32(&counter.calls)
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&counter.calls); got != stopped {
		t.Fatalf("StopPolicyLoop 后不应继续巡检：%d → %d", stopped, got)
	}
	service.StopPolicyLoop() // 重复停止应幂等
}

// TestPolicySweepPurgesExpiredVisitorProfiles 验证「访客画像保留天数」真的接在巡检上：
// 设 30 天 → 巡检清掉 40 天前的画像、留下今天的；设 0（永久）→ 巡检不再删任何画像。
func TestPolicySweepPurgesExpiredVisitorProfiles(t *testing.T) {
	service, st, _ := newFlowService(t)
	setServicePolicy(t, st, func(policy *domain.ServicePolicy) {
		policy.SLAFirstResponseSeconds = 0
		policy.SLAIdleMinutes = 0
		policy.AutoCloseIdleMinutes = 0
		policy.TakeoverPopupEnabled = false
	})

	oldVisitor, err := st.CreateVisitorConversation("198.51.100.11", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("创建旧访客会话失败：%v", err)
	}
	newVisitor, err := st.CreateVisitorConversation("198.51.100.12", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("创建新访客会话失败：%v", err)
	}

	now := time.Now().UTC()
	stale := now.AddDate(0, 0, -40)
	if _, err := st.UpdateVisitorProfile(oldVisitor.Conversation.VisitorID, func(profile *domain.VisitorProfile) error {
		profile.LastSeenAt = &stale
		profile.UpdatedAt = stale
		return nil
	}); err != nil {
		t.Fatalf("写入超期画像失败：%v", err)
	}
	if _, err := st.UpdateVisitorProfile(newVisitor.Conversation.VisitorID, func(profile *domain.VisitorProfile) error {
		profile.LastSeenAt = &now
		profile.UpdatedAt = now
		return nil
	}); err != nil {
		t.Fatalf("写入未超期画像失败：%v", err)
	}

	// 保留 30 天：巡检应清掉 40 天前那条。
	thirty := 30
	if _, err := st.UpdateVisitorProfileSettings(domain.VisitorProfileSettingsPatch{RetentionDays: &thirty}); err != nil {
		t.Fatalf("设置保留天数失败：%v", err)
	}
	service.RunPolicySweep(now)
	if _, ok := st.VisitorProfile(oldVisitor.Conversation.VisitorID); ok {
		t.Fatal("巡检应清掉超过 30 天未活动的画像")
	}
	if _, ok := st.VisitorProfile(newVisitor.Conversation.VisitorID); !ok {
		t.Fatal("巡检不应清掉未超期的画像")
	}

	// 改成永久保留（0）：再造一条超期画像，巡检不得再删。
	permanent := 0
	if _, err := st.UpdateVisitorProfileSettings(domain.VisitorProfileSettingsPatch{RetentionDays: &permanent}); err != nil {
		t.Fatalf("设置永久保留失败：%v", err)
	}
	kept, err := st.CreateVisitorConversation("198.51.100.13", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	if _, err := st.UpdateVisitorProfile(kept.Conversation.VisitorID, func(profile *domain.VisitorProfile) error {
		profile.LastSeenAt = &stale
		profile.UpdatedAt = stale
		return nil
	}); err != nil {
		t.Fatalf("写入超期画像失败：%v", err)
	}
	service.RunPolicySweep(now)
	if _, ok := st.VisitorProfile(kept.Conversation.VisitorID); !ok {
		t.Fatal("保留天数=0（永久）时巡检不得删除任何画像")
	}
}
