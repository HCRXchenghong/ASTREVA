package store

import (
	"errors"
	"testing"
	"time"

	"customer-service/backend/internal/domain"
)

func TestRemarkUpdateKeepsOriginalIP(t *testing.T) {
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	visitor, err := s.CreateVisitorConversation("203.0.113.7:53100", "web_pc", "zh", "direct", "")
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
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	visitor, err := s.CreateVisitorConversation("198.51.100.9", "web", "zh", "direct", "")
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

func TestHumanKeywordSilentlyAssignsAndKeepsAIUntilAgentReplies(t *testing.T) {
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	openBusinessForTest(t, s)
	agentSession, agent, err := s.LoginAgent("admin", "123456")
	if err != nil {
		t.Fatalf("login agent: %v", err)
	}
	if _, _, err := s.SetAgentStatus(agent.ID, domain.AgentOnline); err != nil {
		t.Fatalf("set agent online: %v", err)
	}
	visitor, err := s.CreateVisitorConversation("192.0.2.18", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	if visitor.Conversation.AssignedAgentID != agent.ID {
		t.Fatalf("conversation was not assigned to online agent: %q", visitor.Conversation.AssignedAgentID)
	}
	if s.agentsByID[agent.ID].CurrentConversations != 1 {
		t.Fatalf("expected one active conversation before handoff, got %d", s.agentsByID[agent.ID].CurrentConversations)
	}

	result, err := s.AddVisitorMessage(visitor.Conversation.ID, "c1", "我要转人工客服", domain.MessageText, nil)
	if err != nil {
		t.Fatalf("add visitor message: %v", err)
	}

	if result.Conversation.AssignedAgentID != agentSession.AccountID {
		t.Fatalf("conversation assignment changed unexpectedly: %q", result.Conversation.AssignedAgentID)
	}
	if s.agentsByID[agent.ID].CurrentConversations != 1 {
		t.Fatalf("handoff double-counted current conversations: %d", s.agentsByID[agent.ID].CurrentConversations)
	}
	// 静默转人工：不产生任何对访客可见的转接系统消息，AI 继续接待直到客服真实回复。
	if len(result.Generated) != 0 {
		t.Fatalf("silent handoff must not emit visible system messages, got %#v", result.Generated)
	}
	if !result.NeedAI {
		t.Fatalf("AI must keep replying until the agent actually replies: %#v", result)
	}
	if result.Conversation.Status != domain.ConversationHumanRequested {
		t.Fatalf("expected human_requested status during silent handoff, got %s", result.Conversation.Status)
	}
}

func TestAgentOfflineKeepsTakenOverConversationInHumanQueue(t *testing.T) {
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	openBusinessForTest(t, s)
	agentSession, agent, err := s.LoginAgent("admin", "123456")
	if err != nil {
		t.Fatalf("login agent: %v", err)
	}
	if _, _, err := s.SetAgentStatus(agent.ID, domain.AgentOnline); err != nil {
		t.Fatalf("set agent online: %v", err)
	}
	visitor, err := s.CreateVisitorConversation("192.0.2.188", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	if visitor.Conversation.AssignedAgentID != agent.ID {
		t.Fatalf("expected conversation assigned to agent, got %q", visitor.Conversation.AssignedAgentID)
	}
	// 客服真实回复后，会话才进入人工接待（assigned）。
	if _, err := s.AddAgentMessage(agentSession.AccountID, visitor.Conversation.ID, "takeover-1", "您好，人工客服为您服务。", domain.MessageText, nil); err != nil {
		t.Fatalf("add agent message: %v", err)
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
	if changed[0].Status != domain.ConversationHumanRequested {
		t.Fatalf("expected conversation to remain in human queue, got %s", changed[0].Status)
	}
}

func TestAlwaysAIModeStillHonorsExplicitHumanHandoff(t *testing.T) {
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	openBusinessForTest(t, s)
	if _, err := s.CreateKeywordRule(domain.KeywordRule{
		Keyword: "电话", MatchType: "contains", Reply: "客服热线请以官网公示为准。", Enabled: true, Priority: 90, Action: "telephone",
	}); err != nil {
		t.Fatalf("create keyword rule: %v", err)
	}
	if _, err := s.UpdateAISettings(domain.AISettings{Enabled: true, Mode: domain.AIModeAlwaysAI, APIKey: "sk-test-123456", Model: "gpt-4o-mini", APIType: "chat_completions", TimeoutSeconds: 5}); err != nil {
		t.Fatalf("update AI settings: %v", err)
	}
	visitor, err := s.CreateVisitorConversation("192.0.2.210", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	result, err := s.AddVisitorMessage(visitor.Conversation.ID, "handoff-1", "转人工", domain.MessageText, nil)
	if err != nil {
		t.Fatalf("request human: %v", err)
	}
	// 显式转人工进入静默排队；客服真实回复前 AI 继续接待（无痕转人工）。
	if result.Conversation.Status != domain.ConversationHumanRequested || !result.NeedAI || len(result.Generated) != 0 {
		t.Fatalf("silent handoff must keep AI serving without visible messages: %#v", result)
	}
	blocked, err := s.AddVisitorMessage(visitor.Conversation.ID, "handoff-2", "电话是多少", domain.MessageText, nil)
	if err != nil {
		t.Fatalf("add queued visitor message: %v", err)
	}
	// 排队期间关键词自动回复仍然可用，保证访客问题不被晾着。
	if blocked.NeedAI || len(blocked.Generated) != 1 {
		t.Fatalf("queued conversation should still answer keywords: %#v", blocked)
	}
}

func TestDefaultContactSettingsAndKeywordRulesHaveNoPlaceholderData(t *testing.T) {
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})

	contacts := s.ContactSettings()
	if contacts.Telephone != "" || contacts.WhatsApp != "" || contacts.Telegram != "" {
		t.Fatalf("默认联系方式必须为空，不能内置任何占位号码: %#v", contacts)
	}
	if rules := s.KeywordRules(); len(rules) != 0 {
		t.Fatalf("默认关键词规则必须为空，不能内置假联系方式规则: %#v", rules)
	}
}

func TestUpdateContactSettingsReplacesWholeRecord(t *testing.T) {
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})

	if _, err := s.UpdateContactSettings(domain.ContactSettings{
		Telephone:  "021-8888-0000",
		WhatsApp:   "wa-real-account",
		Telegram:   "tg-real-account",
		EntryReply: "欢迎咨询",
	}); err != nil {
		t.Fatalf("update contact settings: %v", err)
	}

	// PATCH 为整体替换语义：未传字段即清空，允许管理员显式删除联系方式。
	cleared, err := s.UpdateContactSettings(domain.ContactSettings{EntryReply: "欢迎咨询"})
	if err != nil {
		t.Fatalf("clear contact settings: %v", err)
	}
	if cleared.Telephone != "" || cleared.WhatsApp != "" || cleared.Telegram != "" {
		t.Fatalf("联系方式应可被显式清空: %#v", cleared)
	}
	if cleared.EntryReply != "欢迎咨询" {
		t.Fatalf("欢迎语不应被意外清空: %q", cleared.EntryReply)
	}
}

func TestUpdateAppReleaseKeepsReleaseNotesWhenEmpty(t *testing.T) {
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})

	release, err := s.CreateAppRelease(domain.AppRelease{
		Platform:     "android",
		VersionName:  "1.0.0",
		VersionCode:  1,
		FilePath:     "releases/1.0.0.apk",
		ReleaseNotes: "首个版本",
	})
	if err != nil {
		t.Fatalf("create app release: %v", err)
	}

	// 与 Postgres 对齐：未带 release_notes 的 PATCH 不应清空已有说明。
	updated, err := s.UpdateAppRelease(release.ID, domain.AppRelease{})
	if err != nil {
		t.Fatalf("update app release without notes: %v", err)
	}
	if updated.ReleaseNotes != "首个版本" {
		t.Fatalf("release_notes 不应被空值清空: %q", updated.ReleaseNotes)
	}

	updated, err = s.UpdateAppRelease(release.ID, domain.AppRelease{ReleaseNotes: "修复若干问题"})
	if err != nil {
		t.Fatalf("update app release with notes: %v", err)
	}
	if updated.ReleaseNotes != "修复若干问题" {
		t.Fatalf("release_notes 未按预期更新: %q", updated.ReleaseNotes)
	}
}

func TestBusinessHoursBlocksAssignmentAndEmitsOfflineReplyOnce(t *testing.T) {
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	_, agent, err := s.LoginAgent("admin", "123456")
	if err != nil {
		t.Fatalf("login agent: %v", err)
	}
	// 非营业时间窗口：以当前时刻为基准避开现在，避免用例受运行时间影响。
	setTestBusinessWindow(t, s, time.Hour, 2*time.Hour)
	if _, _, err := s.SetAgentStatus(agent.ID, domain.AgentOnline); err != nil {
		t.Fatalf("set agent online off-hours: %v", err)
	}

	visitor, err := s.CreateVisitorConversation("192.0.2.77", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	if visitor.Conversation.AssignedAgentID != "" {
		t.Fatalf("非营业时间不应自动分配人工: %q", visitor.Conversation.AssignedAgentID)
	}

	result, err := s.AddVisitorMessage(visitor.Conversation.ID, "c1", "我要转人工", domain.MessageText, nil)
	if err != nil {
		t.Fatalf("add handoff message: %v", err)
	}
	if result.Conversation.AssignedAgentID != "" {
		t.Fatalf("非营业时间转人工不应自动分配客服: %q", result.Conversation.AssignedAgentID)
	}
	if result.Conversation.Status != domain.ConversationHumanRequested {
		t.Fatalf("非营业时间应进入人工队列: %s", result.Conversation.Status)
	}
	if len(result.Generated) != 1 {
		t.Fatalf("非营业时间应给出一次留言提示: %#v", result.Generated)
	}
	hint := result.Generated[0]
	if hint.SenderType != domain.SenderSystem || hint.MessageType != domain.MessageSystem {
		t.Fatalf("留言提示应为 system 消息: %#v", hint)
	}
	if hint.Content != defaultOfflineReply {
		t.Fatalf("未配置时留言提示应使用内置文案: %q", hint.Content)
	}
	// AI 仍继续接待：非营业时间只拦截人工分配，不打断 AI。
	if !result.NeedAI {
		t.Fatal("非营业时间 AI 应继续接待")
	}

	again, err := s.AddVisitorMessage(visitor.Conversation.ID, "c2", "我要转人工", domain.MessageText, nil)
	if err != nil {
		t.Fatalf("add repeated handoff message: %v", err)
	}
	if len(again.Generated) != 0 {
		t.Fatalf("留言提示同一会话只应给出一次: %#v", again.Generated)
	}
}

func TestBusinessHoursUsesConfiguredOfflineReply(t *testing.T) {
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	if _, err := s.UpdateBusinessHours(domain.BusinessHours{
		Timezone:     "UTC",
		Enabled:      true,
		OfflineReply: "现在是休息时间，请留言，我们稍后回复。",
	}); err != nil {
		t.Fatalf("update business hours: %v", err)
	}
	setTestBusinessWindow(t, s, time.Hour, 2*time.Hour)

	visitor, err := s.CreateVisitorConversation("192.0.2.78", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	result, err := s.AddVisitorMessage(visitor.Conversation.ID, "c1", "转人工", domain.MessageText, nil)
	if err != nil {
		t.Fatalf("add handoff message: %v", err)
	}
	if len(result.Generated) != 1 || result.Generated[0].Content != "现在是休息时间，请留言，我们稍后回复。" {
		t.Fatalf("应使用后台配置的留言文案: %#v", result.Generated)
	}
}

func TestBusinessHoursAllowsAssignmentDuringOpenHours(t *testing.T) {
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	_, agent, err := s.LoginAgent("admin", "123456")
	if err != nil {
		t.Fatalf("login agent: %v", err)
	}
	// 营业时间窗口覆盖当前时刻。
	setTestBusinessWindow(t, s, -time.Hour, time.Hour)
	if _, _, err := s.SetAgentStatus(agent.ID, domain.AgentOnline); err != nil {
		t.Fatalf("set agent online: %v", err)
	}

	visitor, err := s.CreateVisitorConversation("192.0.2.79", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	if visitor.Conversation.AssignedAgentID != agent.ID {
		t.Fatalf("营业时间内应自动分配在线客服: %q", visitor.Conversation.AssignedAgentID)
	}
}

func TestConversationCarriesAssignedAgentIdentity(t *testing.T) {
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	openBusinessForTest(t, s)
	_, agent, err := s.LoginAgent("admin", "123456")
	if err != nil {
		t.Fatalf("login agent: %v", err)
	}
	if _, _, err := s.SetAgentStatus(agent.ID, domain.AgentOnline); err != nil {
		t.Fatalf("set agent online: %v", err)
	}
	if _, err := s.UpdateAgentAvatar(agent.ID, "https://cdn.example.com/avatar.png"); err != nil {
		t.Fatalf("update agent avatar: %v", err)
	}

	visitor, err := s.CreateVisitorConversation("192.0.2.91", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	if visitor.Conversation.AssignedAgentID != agent.ID {
		t.Fatalf("expected assignment to online agent, got %q", visitor.Conversation.AssignedAgentID)
	}
	if visitor.Conversation.AssignedAgentName != "客服-李雪" {
		t.Fatalf("会话应带上客服名称: %q", visitor.Conversation.AssignedAgentName)
	}
	if visitor.Conversation.AssignedAgentAvatar != "https://cdn.example.com/avatar.png" {
		t.Fatalf("会话应带上客服头像: %q", visitor.Conversation.AssignedAgentAvatar)
	}

	fetched, ok := s.Conversation(visitor.Conversation.ID)
	if !ok {
		t.Fatal("conversation not found")
	}
	if fetched.AssignedAgentName != "客服-李雪" || fetched.AssignedAgentAvatar != "https://cdn.example.com/avatar.png" {
		t.Fatalf("读取路径应补齐客服信息: %#v", fetched)
	}
}

// setTestBusinessWindow 把营业时间设为以当前时刻为基准的相对窗口，让用例不受运行时间影响。
func setTestBusinessWindow(t *testing.T, s *MemoryStore, startOffset, endOffset time.Duration) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := s.UpdateBusinessHours(domain.BusinessHours{
		Timezone: "UTC",
		Start:    now.Add(startOffset).Format("15:04"),
		End:      now.Add(endOffset).Format("15:04"),
		Enabled:  true,
	}); err != nil {
		t.Fatalf("set business window: %v", err)
	}
}

// openBusinessForTest 关闭营业时间限制（Enabled=false 表示不限制），让既有用例保持随时可分配的行为。
func openBusinessForTest(t *testing.T, s *MemoryStore) {
	t.Helper()
	if _, err := s.UpdateBusinessHours(domain.BusinessHours{Timezone: "Asia/Shanghai", Enabled: false}); err != nil {
		t.Fatalf("open business hours: %v", err)
	}
}

func TestClientMessageIDIsIdempotent(t *testing.T) {
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	visitor, err := s.CreateVisitorConversation("192.0.2.21", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}

	first, err := s.AddVisitorMessage(visitor.Conversation.ID, "client-1", "重复发送测试", domain.MessageText, nil)
	if err != nil {
		t.Fatalf("add visitor message: %v", err)
	}
	second, err := s.AddVisitorMessage(visitor.Conversation.ID, "client-1", "重复发送测试", domain.MessageText, nil)
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
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	visitor, err := s.CreateVisitorConversation("192.0.2.22", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	for idx := 0; idx < 5; idx++ {
		if _, err := s.AddVisitorMessage(visitor.Conversation.ID, "client-page-"+string(rune('a'+idx)), "消息", domain.MessageText, nil); err != nil {
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

func TestHumanTakeoverBlocksKeywordAndAIReplies(t *testing.T) {
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	openBusinessForTest(t, s)
	if _, err := s.CreateKeywordRule(domain.KeywordRule{
		Keyword: "电话", MatchType: "contains", Reply: "客服热线请以官网公示为准。", Enabled: true, Priority: 90, Action: "telephone",
	}); err != nil {
		t.Fatalf("create keyword rule: %v", err)
	}
	agentSession, agent, err := s.LoginAgent("admin", "123456")
	if err != nil {
		t.Fatalf("login agent: %v", err)
	}
	if _, _, err := s.SetAgentStatus(agent.ID, domain.AgentOnline); err != nil {
		t.Fatalf("set agent online: %v", err)
	}
	visitor, err := s.CreateVisitorConversation("192.0.2.20", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	result, err := s.AddVisitorMessage(visitor.Conversation.ID, "c1", "电话是多少", domain.MessageText, nil)
	if err != nil {
		t.Fatalf("add visitor message: %v", err)
	}

	// 客服尚未真实回复时，静默分配的会话仍由 AI/关键词接待。
	if result.NeedAI || len(result.Generated) != 1 {
		t.Fatalf("silently assigned conversation must still auto-reply: %#v", result)
	}
	if _, err := s.AddAgentMessage(agentSession.AccountID, visitor.Conversation.ID, "takeover", "您好，人工客服为您服务。", domain.MessageText, nil); err != nil {
		t.Fatalf("agent takeover: %v", err)
	}
	if _, _, err := s.AddAIMessage(visitor.Conversation.ID, "这是一条迟到的 AI 回复", nil); err != ErrConflict {
		t.Fatalf("late AI reply must be rejected after human takeover, got %v", err)
	}

	result, err = s.AddVisitorMessage(visitor.Conversation.ID, "c2", "转人工", domain.MessageText, nil)
	if err != nil {
		t.Fatalf("add second visitor message: %v", err)
	}
	if result.NeedAI || len(result.Generated) != 0 || result.Conversation.Status != domain.ConversationAssigned {
		t.Fatalf("assigned conversation must remain fully human-controlled: %#v", result)
	}
}

func TestUpdateAISettingsMasksAndPreservesAPIKey(t *testing.T) {
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})

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
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	openBusinessForTest(t, s)

	agentSession, agent, err := s.LoginAgent("admin", "123456")
	if err != nil {
		t.Fatalf("login agent: %v", err)
	}
	if _, _, err := s.SetAgentStatus(agent.ID, domain.AgentOnline); err != nil {
		t.Fatalf("set agent online: %v", err)
	}

	visitor, err := s.CreateVisitorConversation("192.0.2.30", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	if visitor.Conversation.AssignedAgentID != agent.ID {
		t.Fatalf("expected assigned agent %q, got %q", agent.ID, visitor.Conversation.AssignedAgentID)
	}

	result, err := s.AddAgentMessage(agent.ID, visitor.Conversation.ID, "agent-msg-1", "这是一条可撤回消息", domain.MessageText, nil)
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
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})

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

func TestPrefixKeywordOnlyMatchesMessageStart(t *testing.T) {
	rule := domain.KeywordRule{Keyword: "订单", MatchType: "prefix", Reply: "请提供订单号", Enabled: true, Action: "text"}
	if err := validateKeywordRule(rule); err != nil {
		t.Fatalf("prefix rule should be valid: %v", err)
	}
	if !keywordMatches(rule, "订单怎么查询") {
		t.Fatal("prefix keyword should match the beginning of a message")
	}
	if keywordMatches(rule, "我想查询订单") {
		t.Fatal("prefix keyword must not match text in the middle")
	}
}

func TestCreateVisitorConversationUsesConfiguredEntryReply(t *testing.T) {
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})

	if _, err := s.UpdateContactSettings(domain.ContactSettings{
		EntryReply: "欢迎进入会话，这里是后台配置的首条回复。",
	}); err != nil {
		t.Fatalf("update contact settings: %v", err)
	}

	visitor, err := s.CreateVisitorConversation("192.0.2.88", "web", "zh", "direct", "")
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
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})

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

func TestAdminTransferConversationReleasesOldAgent(t *testing.T) {
	s := NewMemoryStore(Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	openBusinessForTest(t, s)
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
	visitor, err := s.CreateVisitorConversation("192.0.2.30", "web", "zh", "direct", "")
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
	// 无痕转人工：转接提示为仅客服端可见的 handoff 消息，且会话状态不因转接而改变。
	if msg.MessageType != domain.MessageHandoff {
		t.Fatalf("expected agent-side handoff message, got %s", msg.MessageType)
	}
	if conversation.Status != visitor.Conversation.Status {
		t.Fatalf("transfer must not change conversation status: %s -> %s", visitor.Conversation.Status, conversation.Status)
	}
	if s.agentsByID[oldAgent.ID].CurrentConversations != 0 {
		t.Fatalf("old agent load not released: %d", s.agentsByID[oldAgent.ID].CurrentConversations)
	}
	if s.agentsByID[newAgent.ID].CurrentConversations != 1 {
		t.Fatalf("new agent load not incremented: %d", s.agentsByID[newAgent.ID].CurrentConversations)
	}
}
