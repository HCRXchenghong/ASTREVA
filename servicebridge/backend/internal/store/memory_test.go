package store

import (
	"errors"
	"testing"

	"customer-service/backend/internal/domain"
)

func TestRemarkUpdateKeepsOriginalIP(t *testing.T) {
	s := NewMemoryStore(Options{})
	visitor, err := s.CreateVisitorConversation("203.0.113.7:53100", "web_pc")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	adminSession, _, err := s.LoginAdmin("superadmin", "123456")
	if err != nil {
		t.Fatalf("login admin: %v", err)
	}

	updated, err := s.UpdateRemark(adminSession, visitor.Conversation.ID, "意向客户-王总")
	if err != nil {
		t.Fatalf("update remark: %v", err)
	}

	if updated.VisitorIP != "203.0.113.7" {
		t.Fatalf("visitor ip was changed: %q", updated.VisitorIP)
	}
	if updated.VisitorRemark != "意向客户-王总" {
		t.Fatalf("visitor remark not updated: %q", updated.VisitorRemark)
	}
}

func TestAgentCannotEditUnassignedConversation(t *testing.T) {
	s := NewMemoryStore(Options{})
	visitor, err := s.CreateVisitorConversation("198.51.100.9", "web")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	agentSession, _, err := s.LoginAgent("admin", "123456")
	if err != nil {
		t.Fatalf("login agent: %v", err)
	}

	_, err = s.UpdateRemark(agentSession, visitor.Conversation.ID, "越权备注")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestHumanKeywordDoesNotDoubleCountAssignedAgent(t *testing.T) {
	s := NewMemoryStore(Options{})
	agentSession, agent, err := s.LoginAgent("admin", "123456")
	if err != nil {
		t.Fatalf("login agent: %v", err)
	}
	if _, _, err := s.SetAgentStatus(agent.ID, domain.AgentOnline); err != nil {
		t.Fatalf("set agent online: %v", err)
	}
	visitor, err := s.CreateVisitorConversation("192.0.2.18", "web")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	if visitor.Conversation.AssignedAgentID != agent.ID {
		t.Fatalf("conversation was not assigned to online agent: %q", visitor.Conversation.AssignedAgentID)
	}
	if s.agentsByID[agent.ID].CurrentConversations != 1 {
		t.Fatalf("expected one active conversation before handoff, got %d", s.agentsByID[agent.ID].CurrentConversations)
	}

	result, err := s.AddVisitorMessage(visitor.Conversation.ID, "c1", "我要转人工客服", domain.MessageText)
	if err != nil {
		t.Fatalf("add visitor message: %v", err)
	}

	if result.Conversation.AssignedAgentID != agentSession.AccountID {
		t.Fatalf("conversation assignment changed unexpectedly: %q", result.Conversation.AssignedAgentID)
	}
	if s.agentsByID[agent.ID].CurrentConversations != 1 {
		t.Fatalf("handoff double-counted current conversations: %d", s.agentsByID[agent.ID].CurrentConversations)
	}
	if len(result.Generated) != 1 || result.Generated[0].MessageType != domain.MessageHandoff {
		t.Fatalf("expected one handoff system message, got %#v", result.Generated)
	}
}

func TestAgentOfflineReleasesConversationBackToAI(t *testing.T) {
	s := NewMemoryStore(Options{})
	_, agent, err := s.LoginAgent("admin", "123456")
	if err != nil {
		t.Fatalf("login agent: %v", err)
	}
	if _, _, err := s.SetAgentStatus(agent.ID, domain.AgentOnline); err != nil {
		t.Fatalf("set agent online: %v", err)
	}
	visitor, err := s.CreateVisitorConversation("192.0.2.188", "web")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	if visitor.Conversation.AssignedAgentID != agent.ID {
		t.Fatalf("expected conversation assigned to agent, got %q", visitor.Conversation.AssignedAgentID)
	}

	updatedAgent, changed, err := s.SetAgentStatus(agent.ID, domain.AgentOffline)
	if err != nil {
		t.Fatalf("set agent offline: %v", err)
	}

	if updatedAgent.CurrentConversations != 0 {
		t.Fatalf("expected no current conversations, got %d", updatedAgent.CurrentConversations)
	}
	if len(changed) != 1 {
		t.Fatalf("expected one changed conversation, got %d", len(changed))
	}
	if changed[0].AssignedAgentID != "" {
		t.Fatalf("conversation should be released from agent, got %q", changed[0].AssignedAgentID)
	}
	if changed[0].Status != domain.ConversationAIServing {
		t.Fatalf("expected conversation back to AI, got %s", changed[0].Status)
	}
}

func TestPhoneKeywordGeneratesStructuredContactReply(t *testing.T) {
	s := NewMemoryStore(Options{})
	visitor, err := s.CreateVisitorConversation("192.0.2.19", "web")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}

	result, err := s.AddVisitorMessage(visitor.Conversation.ID, "c1", "客服电话是多少？", domain.MessageText)
	if err != nil {
		t.Fatalf("add visitor message: %v", err)
	}

	if result.NeedAI {
		t.Fatal("keyword reply should not call AI")
	}
	if len(result.Generated) != 1 {
		t.Fatalf("expected one generated keyword reply, got %d", len(result.Generated))
	}
	if result.Generated[0].MessageType != domain.MessageContactPhone {
		t.Fatalf("expected phone contact message, got %s", result.Generated[0].MessageType)
	}
	if result.Generated[0].Content != "客服电话：400-123-4567" {
		t.Fatalf("unexpected reply content: %q", result.Generated[0].Content)
	}
}

func TestClientMessageIDIsIdempotent(t *testing.T) {
	s := NewMemoryStore(Options{})
	visitor, err := s.CreateVisitorConversation("192.0.2.21", "web")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}

	first, err := s.AddVisitorMessage(visitor.Conversation.ID, "client-1", "重复发送测试", domain.MessageText)
	if err != nil {
		t.Fatalf("add visitor message: %v", err)
	}
	second, err := s.AddVisitorMessage(visitor.Conversation.ID, "client-1", "重复发送测试", domain.MessageText)
	if err != nil {
		t.Fatalf("add duplicate visitor message: %v", err)
	}

	if first.Input.ServerMsgID != second.Input.ServerMsgID {
		t.Fatalf("duplicate client_msg_id created a new message: %s != %s", first.Input.ServerMsgID, second.Input.ServerMsgID)
	}
	messages := s.Messages(visitor.Conversation.ID)
	visitorMessages := 0
	for _, msg := range messages {
		if msg.SenderType == domain.SenderVisitor && msg.ClientMsgID == "client-1" {
			visitorMessages++
		}
	}
	if visitorMessages != 1 {
		t.Fatalf("expected one deduplicated visitor message, got %d", visitorMessages)
	}
}

func TestPagedMessagesReturnsNewestPageAndCursor(t *testing.T) {
	s := NewMemoryStore(Options{})
	visitor, err := s.CreateVisitorConversation("192.0.2.22", "web")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	for idx := 0; idx < 5; idx++ {
		if _, err := s.AddVisitorMessage(visitor.Conversation.ID, "client-page-"+string(rune('a'+idx)), "消息", domain.MessageText); err != nil {
			t.Fatalf("add visitor message %d: %v", idx, err)
		}
	}

	first := s.PagedMessages(visitor.Conversation.ID, 3, "")
	if len(first.Messages) != 3 || !first.HasMore || first.NextBefore == "" {
		t.Fatalf("unexpected first page: %#v", first)
	}
	second := s.PagedMessages(visitor.Conversation.ID, 3, first.NextBefore)
	if len(second.Messages) == 0 {
		t.Fatalf("expected older page, got %#v", second)
	}
	if second.Messages[len(second.Messages)-1].ServerMsgID == first.Messages[0].ServerMsgID {
		t.Fatal("older page repeated boundary message")
	}
}

func TestEscalateNoReplyToAIRequiresSameLastVisitorMessage(t *testing.T) {
	s := NewMemoryStore(Options{})
	_, agent, err := s.LoginAgent("admin", "123456")
	if err != nil {
		t.Fatalf("login agent: %v", err)
	}
	if _, _, err := s.SetAgentStatus(agent.ID, domain.AgentOnline); err != nil {
		t.Fatalf("set agent online: %v", err)
	}
	visitor, err := s.CreateVisitorConversation("192.0.2.20", "web")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	result, err := s.AddVisitorMessage(visitor.Conversation.ID, "c1", "价格多少", domain.MessageText)
	if err != nil {
		t.Fatalf("add visitor message: %v", err)
	}

	if _, err := s.AddAgentMessage(agent.ID, visitor.Conversation.ID, "a1", "您好，正在为您查询。", domain.MessageText); err != nil {
		t.Fatalf("add agent message: %v", err)
	}
	if _, ok := s.EscalateNoReplyToAI(visitor.Conversation.ID, result.VisitorMessageSentAt); ok {
		t.Fatal("agent reply should prevent no-reply escalation")
	}

	result, err = s.AddVisitorMessage(visitor.Conversation.ID, "c2", "还在吗", domain.MessageText)
	if err != nil {
		t.Fatalf("add second visitor message: %v", err)
	}
	conversation, ok := s.EscalateNoReplyToAI(visitor.Conversation.ID, result.VisitorMessageSentAt)
	if !ok {
		t.Fatal("expected no-reply escalation")
	}
	if conversation.Status != domain.ConversationAIServing {
		t.Fatalf("unexpected status after escalation: %s", conversation.Status)
	}
}

func TestUpdateAISettingsMasksAndPreservesAPIKey(t *testing.T) {
	s := NewMemoryStore(Options{})

	updated, err := s.UpdateAISettings(domain.AISettings{
		Enabled: true,
		APIKey:  "sk-test-123456",
		Model:   "gpt-4o-mini",
	})
	if err != nil {
		t.Fatalf("update AI settings: %v", err)
	}
	if updated.APIKey != "sk-test-123456" {
		t.Fatal("api key was not stored")
	}
	if updated.APIKeyMasked != "sk-t****3456" {
		t.Fatalf("unexpected api key mask: %q", updated.APIKeyMasked)
	}

	updated, err = s.UpdateAISettings(domain.AISettings{
		Enabled: true,
		Model:   "gpt-4.1-mini",
	})
	if err != nil {
		t.Fatalf("update AI settings preserving key: %v", err)
	}
	if updated.APIKey != "sk-test-123456" {
		t.Fatal("empty patch should preserve api key")
	}
	if updated.Model != "gpt-4.1-mini" {
		t.Fatalf("model was not updated: %q", updated.Model)
	}
}

func TestRevokeAgentMessageUpdatesConversationAndPreservesOriginalContent(t *testing.T) {
	s := NewMemoryStore(Options{})

	agentSession, agent, err := s.LoginAgent("admin", "123456")
	if err != nil {
		t.Fatalf("login agent: %v", err)
	}
	if _, _, err := s.SetAgentStatus(agent.ID, domain.AgentOnline); err != nil {
		t.Fatalf("set agent online: %v", err)
	}

	visitor, err := s.CreateVisitorConversation("192.0.2.30", "web")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	if visitor.Conversation.AssignedAgentID != agent.ID {
		t.Fatalf("expected assigned agent %q, got %q", agent.ID, visitor.Conversation.AssignedAgentID)
	}

	result, err := s.AddAgentMessage(agent.ID, visitor.Conversation.ID, "agent-msg-1", "这是一条可撤回消息", domain.MessageText)
	if err != nil {
		t.Fatalf("add agent message: %v", err)
	}
	if result.Conversation.UnreadForVisitor != 1 {
		t.Fatalf("expected visitor unread count to increase, got %d", result.Conversation.UnreadForVisitor)
	}

	msg, conversation, err := s.RevokeMessage(agentSession, visitor.Conversation.ID, result.Input.ServerMsgID)
	if err != nil {
		t.Fatalf("revoke message: %v", err)
	}
	if msg.RevokedAt == nil {
		t.Fatal("expected revoked_at to be set")
	}
	if msg.Content != "这是一条可撤回消息" {
		t.Fatalf("expected original content to remain stored, got %q", msg.Content)
	}
	if msg.RevokedByKind != domain.AccountAgent || msg.RevokedByID != agentSession.AccountID {
		t.Fatalf("unexpected revoke actor: kind=%q id=%q", msg.RevokedByKind, msg.RevokedByID)
	}
	if conversation.LastMessage != "已撤回一条消息" {
		t.Fatalf("unexpected conversation preview: %q", conversation.LastMessage)
	}
	if conversation.UnreadForVisitor != 0 {
		t.Fatalf("expected visitor unread count to be decremented, got %d", conversation.UnreadForVisitor)
	}

	messages := s.Messages(visitor.Conversation.ID)
	if len(messages) == 0 {
		t.Fatal("expected stored messages")
	}
	found := false
	for _, item := range messages {
		if item.ServerMsgID != result.Input.ServerMsgID {
			continue
		}
		found = true
		if item.RevokedAt == nil {
			t.Fatal("stored message missing revoked_at")
		}
		if item.Content != "这是一条可撤回消息" {
			t.Fatalf("stored message content changed unexpectedly: %q", item.Content)
		}
	}
	if !found {
		t.Fatalf("revoked message %q not found in store", result.Input.ServerMsgID)
	}
}

func TestKeywordRuleCreateAndUpdate(t *testing.T) {
	s := NewMemoryStore(Options{})

	rule, err := s.CreateKeywordRule(domain.KeywordRule{
		Keyword:  "售后",
		Reply:    "售后客服会尽快为您处理。",
		Enabled:  true,
		Priority: 70,
	})
	if err != nil {
		t.Fatalf("create keyword rule: %v", err)
	}
	if rule.ID == "" || rule.MatchType != "contains" || rule.Action != "text" {
		t.Fatalf("unexpected created rule: %#v", rule)
	}

	updated, err := s.UpdateKeywordRule(rule.ID, domain.KeywordRule{
		Keyword:            "售后",
		Reply:              "已为您记录售后问题。",
		Enabled:            false,
		Priority:           60,
		Action:             "text",
		ShowInQuickReplies: true,
		QuickReplyText:     "售后处理",
	})
	if err != nil {
		t.Fatalf("update keyword rule: %v", err)
	}
	if updated.Enabled {
		t.Fatal("rule should be disabled")
	}
	if updated.Reply != "已为您记录售后问题。" {
		t.Fatalf("reply was not updated: %q", updated.Reply)
	}
	if !updated.ShowInQuickReplies || updated.QuickReplyText != "售后处理" {
		t.Fatalf("quick reply config was not updated: %#v", updated)
	}
}

func TestCreateVisitorConversationUsesConfiguredEntryReply(t *testing.T) {
	s := NewMemoryStore(Options{})

	if _, err := s.UpdateContactSettings(domain.ContactSettings{
		EntryReply: "欢迎进入会话，这里是后台配置的首条回复。",
	}); err != nil {
		t.Fatalf("update contact settings: %v", err)
	}

	visitor, err := s.CreateVisitorConversation("192.0.2.88", "web")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	if len(visitor.InitialMessages) != 1 {
		t.Fatalf("expected one initial message, got %d", len(visitor.InitialMessages))
	}
	if visitor.InitialMessages[0].Content != "欢迎进入会话，这里是后台配置的首条回复。" {
		t.Fatalf("unexpected initial reply: %q", visitor.InitialMessages[0].Content)
	}
	if visitor.InitialMessages[0].SenderType != domain.SenderAI {
		t.Fatalf("unexpected initial sender type: %s", visitor.InitialMessages[0].SenderType)
	}
}

func TestAgentCreateDisableAndLoginBoundary(t *testing.T) {
	s := NewMemoryStore(Options{})

	agent, err := s.CreateAgent(domain.Agent{
		Account:          "kf_003",
		Name:             "赵敏",
		Group:            "售前组",
		MaxConversations: 8,
	}, "AgentPass-123456")
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if agent.ID == "" || agent.Status != domain.AgentOffline {
		t.Fatalf("unexpected created agent: %#v", agent)
	}
	if _, err := s.CreateAgent(domain.Agent{Account: "kf_003", Name: "重复"}, "AgentPass-123456"); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected duplicate conflict, got %v", err)
	}
	if _, _, err := s.LoginAgent("kf_003", "AgentPass-123456"); err != nil {
		t.Fatalf("new agent should login before disabled: %v", err)
	}
	disabled, err := s.DisableAgent(agent.ID)
	if err != nil {
		t.Fatalf("disable agent: %v", err)
	}
	if disabled.DisabledAt == nil {
		t.Fatal("disabled_at should be set")
	}
	if _, _, err := s.LoginAgent("kf_003", "AgentPass-123456"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("disabled agent should not login, got %v", err)
	}
}

func TestRegisterAgentPushDeviceIsIdempotent(t *testing.T) {
	s := NewMemoryStore(Options{})
	_, agent, err := s.LoginAgent("admin", "123456")
	if err != nil {
		t.Fatalf("login agent: %v", err)
	}

	first, err := s.RegisterAgentPushDevice(agent.ID, domain.PushDevice{
		Platform: "ios",
		Provider: "uni-push",
		Token:    "token-1",
	})
	if err != nil {
		t.Fatalf("register push device: %v", err)
	}
	second, err := s.RegisterAgentPushDevice(agent.ID, domain.PushDevice{
		Platform: "ios",
		Provider: "uni-push",
		Token:    "token-1",
	})
	if err != nil {
		t.Fatalf("register duplicate push device: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("duplicate token should update existing device: %s != %s", first.ID, second.ID)
	}
	devices := s.PushDevicesForAgent(agent.ID)
	if len(devices) != 1 || devices[0].Token != "token-1" {
		t.Fatalf("unexpected push devices: %#v", devices)
	}
}

func TestAdminTransferConversationReleasesOldAgent(t *testing.T) {
	s := NewMemoryStore(Options{})
	adminSession, _, err := s.LoginAdmin("superadmin", "123456")
	if err != nil {
		t.Fatalf("login admin: %v", err)
	}
	_, oldAgent, err := s.LoginAgent("admin", "123456")
	if err != nil {
		t.Fatalf("login old agent: %v", err)
	}
	if _, _, err := s.SetAgentStatus(oldAgent.ID, domain.AgentOnline); err != nil {
		t.Fatalf("set old agent online: %v", err)
	}
	visitor, err := s.CreateVisitorConversation("192.0.2.30", "web")
	if err != nil {
		t.Fatalf("create visitor: %v", err)
	}
	if visitor.Conversation.AssignedAgentID != oldAgent.ID {
		t.Fatalf("expected initial assignment to old agent, got %q", visitor.Conversation.AssignedAgentID)
	}
	newAgent, err := s.CreateAgent(domain.Agent{
		Account:          "kf_004",
		Name:             "周宁",
		Group:            "售后组",
		MaxConversations: 10,
	}, "AgentPass-123456")
	if err != nil {
		t.Fatalf("create new agent: %v", err)
	}
	if _, _, err := s.SetAgentStatus(newAgent.ID, domain.AgentOnline); err != nil {
		t.Fatalf("set new agent online: %v", err)
	}

	conversation, msg, err := s.TransferConversation(adminSession, visitor.Conversation.ID, newAgent.ID, "")
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if conversation.AssignedAgentID != newAgent.ID {
		t.Fatalf("expected transfer to new agent, got %q", conversation.AssignedAgentID)
	}
	if msg.MessageType != domain.MessageSystem {
		t.Fatalf("expected system transfer message, got %s", msg.MessageType)
	}
	if s.agentsByID[oldAgent.ID].CurrentConversations != 0 {
		t.Fatalf("old agent load not released: %d", s.agentsByID[oldAgent.ID].CurrentConversations)
	}
	if s.agentsByID[newAgent.ID].CurrentConversations != 1 {
		t.Fatalf("new agent load not incremented: %d", s.agentsByID[newAgent.ID].CurrentConversations)
	}
}
