package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"customer-service/backend/internal/domain"
)

// newServicePolicyTestStore 构造带一个客服与两个会话的内存存储。
func newServicePolicyTestStore(t *testing.T) (*MemoryStore, domain.Agent, string, string) {
	t.Helper()
	st := NewMemoryStore(Options{})
	agent, err := st.CreateAgent(domain.Agent{Account: "policy_agent", Name: "策略客服", Status: domain.AgentOnline}, "AgentPass-123456")
	if err != nil {
		t.Fatalf("创建客服失败：%v", err)
	}
	first, err := st.CreateVisitorConversation("203.0.113.10", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	second, err := st.CreateVisitorConversation("203.0.113.11", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	return st, agent, first.Conversation.ID, second.Conversation.ID
}

// TestMemoryServicePolicyDefaultsAndNormalize 覆盖策略默认值与归一。
func TestMemoryServicePolicyDefaultsAndNormalize(t *testing.T) {
	st := NewMemoryStore(Options{})

	defaults := st.ServicePolicy()
	want := domain.DefaultServicePolicy()
	if defaults.QueueEnabled != want.QueueEnabled ||
		defaults.MaxWaitSeconds != want.MaxWaitSeconds ||
		defaults.SLAFirstResponseSeconds != want.SLAFirstResponseSeconds ||
		defaults.SLAIdleMinutes != want.SLAIdleMinutes ||
		defaults.SLAIdleAction != want.SLAIdleAction ||
		defaults.AutoCloseIdleMinutes != want.AutoCloseIdleMinutes ||
		defaults.AIKeywordPriority != want.AIKeywordPriority ||
		defaults.UnattendedEnabled != want.UnattendedEnabled ||
		defaults.TakeoverPopupEnabled != want.TakeoverPopupEnabled {
		t.Fatalf("默认策略不符：got=%+v want=%+v", defaults, want)
	}
	if defaults.UpdatedAt != nil {
		t.Fatalf("未写入时 UpdatedAt 应为 nil：%v", defaults.UpdatedAt)
	}

	updated, err := st.UpdateServicePolicy(domain.ServicePolicy{
		QueueEnabled:         false,
		MaxWaitSeconds:       -5,
		SLAIdleAction:        "bad-value",
		AIKeywordPriority:    "bad-value",
		AutoCloseIdleMinutes: 99999999,
		UnattendedReply:      "  稍等  ",
	})
	if err != nil {
		t.Fatalf("更新策略失败：%v", err)
	}
	if updated.QueueEnabled {
		t.Fatal("QueueEnabled 未持久化")
	}
	if updated.MaxWaitSeconds != 0 {
		t.Fatalf("负数应归零，实际 %d", updated.MaxWaitSeconds)
	}
	if updated.SLAIdleAction != domain.SLAIdleActionAI {
		t.Fatalf("非法枚举应回落 ai，实际 %q", updated.SLAIdleAction)
	}
	if updated.AIKeywordPriority != domain.AIKeywordPriorityKeywordFirst {
		t.Fatalf("非法枚举应回落 keyword_first，实际 %q", updated.AIKeywordPriority)
	}
	if updated.AutoCloseIdleMinutes != domain.ServicePolicyMaxAutoCloseIdleMinutesLimit {
		t.Fatalf("超上限应收敛，实际 %d", updated.AutoCloseIdleMinutes)
	}
	if updated.UnattendedReply != "稍等" {
		t.Fatalf("话术应去空白，实际 %q", updated.UnattendedReply)
	}
	if updated.UpdatedAt == nil {
		t.Fatal("写入后 UpdatedAt 不应为 nil")
	}

	readBack := st.ServicePolicy()
	if readBack.QueueEnabled || readBack.UnattendedReply != "稍等" {
		t.Fatalf("读回策略与写入不一致：%+v", readBack)
	}
}

// TestMemoryConversationQueue 覆盖排队增删查与排序、重复入队保留原入队时间。
func TestMemoryConversationQueue(t *testing.T) {
	st, _, firstID, secondID := newServicePolicyTestStore(t)

	if _, err := st.EnqueueConversation(domain.QueuedConversation{ConversationID: "conv_missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的会话入队应返回 ErrNotFound，实际 %v", err)
	}
	if _, err := st.EnqueueConversation(domain.QueuedConversation{ConversationID: ""}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("空会话 ID 应返回 ErrInvalidInput，实际 %v", err)
	}

	low, err := st.EnqueueConversation(domain.QueuedConversation{
		ConversationID: firstID, GroupName: "售前", Priority: 1, Reason: "capacity",
	})
	if err != nil {
		t.Fatalf("入队失败：%v", err)
	}
	high, err := st.EnqueueConversation(domain.QueuedConversation{
		ConversationID: secondID, GroupName: "售后", Priority: 9, Reason: "offline",
	})
	if err != nil {
		t.Fatalf("入队失败：%v", err)
	}
	if low.Reason != domain.QueueReasonCapacity || high.Reason != domain.QueueReasonOffline {
		t.Fatalf("入队原因未保留：%+v %+v", low, high)
	}

	queue := st.QueueList()
	if len(queue) != 2 {
		t.Fatalf("排队列表应有 2 条，实际 %d", len(queue))
	}
	if queue[0].ConversationID != secondID || queue[1].ConversationID != firstID {
		t.Fatalf("应按优先级降序：%+v", queue)
	}

	// 重复入队：更新优先级/分组，但保留原入队时间。
	again, err := st.EnqueueConversation(domain.QueuedConversation{
		ConversationID: firstID, GroupName: "VIP", Priority: 99, Reason: "manual",
	})
	if err != nil {
		t.Fatalf("重复入队失败：%v", err)
	}
	if !again.EnqueuedAt.Equal(low.EnqueuedAt) {
		t.Fatalf("重复入队应保留原入队时间：%v -> %v", low.EnqueuedAt, again.EnqueuedAt)
	}
	if again.GroupName != "VIP" || again.Priority != 99 || again.Reason != domain.QueueReasonManual {
		t.Fatalf("重复入队应更新分组/优先级/原因：%+v", again)
	}
	queue = st.QueueList()
	if len(queue) != 2 || queue[0].ConversationID != firstID {
		t.Fatalf("重复入队后顺序不符：%+v", queue)
	}

	if err := st.DequeueConversation(firstID); err != nil {
		t.Fatalf("出队失败：%v", err)
	}
	if err := st.DequeueConversation(firstID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复出队应返回 ErrNotFound，实际 %v", err)
	}
	if queue = st.QueueList(); len(queue) != 1 {
		t.Fatalf("出队后应剩 1 条，实际 %d", len(queue))
	}
}

// TestMemoryBlacklist 覆盖三类命中、过期放行与清理、重复冲突、删除。
func TestMemoryBlacklist(t *testing.T) {
	st := NewMemoryStore(Options{})
	now := time.Now().UTC()

	visitor, err := st.AddBlacklist(domain.BlacklistEntry{Kind: "visitor", Value: "vis_1", Reason: "刷屏", CreatedBy: "admin_1"})
	if err != nil {
		t.Fatalf("新增访客黑名单失败：%v", err)
	}
	if visitor.ID == "" || visitor.CreatedAt.IsZero() {
		t.Fatalf("存储层应生成 ID 与创建时间：%+v", visitor)
	}
	if _, err := st.AddBlacklist(domain.BlacklistEntry{Kind: "ip", Value: "203.0.113.77:53100"}); err != nil {
		t.Fatalf("新增 IP 黑名单失败：%v", err)
	}
	if _, err := st.AddBlacklist(domain.BlacklistEntry{Kind: "account", Value: "acc_9"}); err != nil {
		t.Fatalf("新增账号黑名单失败：%v", err)
	}

	// 三类命中：IP 会按既有规则去掉端口。
	if entry, ok := st.MatchBlacklist("visitor", "vis_1"); !ok || entry.ID != visitor.ID {
		t.Fatalf("访客应命中：%+v %v", entry, ok)
	}
	if _, ok := st.MatchBlacklist("ip", "203.0.113.77"); !ok {
		t.Fatal("IP 应命中（应忽略端口）")
	}
	if _, ok := st.MatchBlacklist("account", "acc_9"); !ok {
		t.Fatal("账号应命中")
	}
	if _, ok := st.MatchBlacklist("account", "acc_missing"); ok {
		t.Fatal("未命中不应返回 true")
	}

	// 重复添加冲突。
	if _, err := st.AddBlacklist(domain.BlacklistEntry{Kind: "visitor", Value: "vis_1"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("重复添加应返回 ErrConflict，实际 %v", err)
	}
	// 非法入参。
	if _, err := st.AddBlacklist(domain.BlacklistEntry{Kind: "sms", Value: "x"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("非法种类应返回 ErrInvalidInput，实际 %v", err)
	}
	if _, err := st.AddBlacklist(domain.BlacklistEntry{Kind: "visitor", Value: "  "}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("空值应返回 ErrInvalidInput，实际 %v", err)
	}

	// 过期项：命中应放行并顺手清理。
	past := now.Add(-time.Minute)
	expired, err := st.AddBlacklist(domain.BlacklistEntry{Kind: "visitor", Value: "vis_expired", ExpiresAt: &past})
	if err != nil {
		t.Fatalf("新增过期黑名单失败：%v", err)
	}
	if _, ok := st.MatchBlacklist("visitor", "vis_expired"); ok {
		t.Fatal("过期项应视为未命中")
	}
	found := false
	for _, entry := range st.BlacklistList("visitor") {
		if entry.ID == expired.ID {
			found = true
		}
	}
	if found {
		t.Fatal("过期项应在命中时被清理")
	}

	// 列表过滤与删除。
	if list := st.BlacklistList("ip"); len(list) != 1 || list[0].Kind != "ip" {
		t.Fatalf("按 kind 过滤不符：%+v", list)
	}
	if list := st.BlacklistList(""); len(list) != 3 {
		t.Fatalf("全部黑名单应有 3 条，实际 %d", len(list))
	}
	if err := st.DeleteBlacklist(visitor.ID); err != nil {
		t.Fatalf("删除失败：%v", err)
	}
	if err := st.DeleteBlacklist(visitor.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除应返回 ErrNotFound，实际 %v", err)
	}
	if _, ok := st.MatchBlacklist("visitor", "vis_1"); ok {
		t.Fatal("删除后不应再命中")
	}
}

// TestMemoryAlertChannelsLimitMaskPatchDelete 覆盖渠道上限、掩码、PATCH 密钥语义与删除。
func TestMemoryAlertChannelsLimitMaskPatchDelete(t *testing.T) {
	st, agent, _, _ := newServicePolicyTestStore(t)

	if _, err := st.CreateAgentAlertChannel("agent_missing", "telegram", map[string]string{"token": "t", "chat_id": "c"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的客服应返回 ErrNotFound，实际 %v", err)
	}
	if _, err := st.CreateAgentAlertChannel(agent.ID, "sms", map[string]string{"value": "x"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("非法渠道种类应返回 ErrInvalidInput，实际 %v", err)
	}

	token := "123456:AAbbCCddEEffGGhhIIjj"
	first, err := st.CreateAgentAlertChannel(agent.ID, "telegram", map[string]string{"token": token, "chat_id": "10086"})
	if err != nil {
		t.Fatalf("创建 telegram 渠道失败：%v", err)
	}
	if first.Config["token"] != token {
		t.Fatalf("存储层内部应保留明文配置：%+v", first.Config)
	}
	if !strings.HasPrefix(first.ConfigMasked["token"], "****") || strings.Contains(first.ConfigMasked["token"], "GGhh") {
		t.Fatalf("响应掩码不应泄漏 token：%+v", first.ConfigMasked)
	}
	if first.ConfigMasked["chat_id"] != "10086" {
		t.Fatalf("chat_id 应保持可读：%+v", first.ConfigMasked)
	}
	if _, err := st.CreateAgentAlertChannel(agent.ID, "bark", map[string]string{"key": "bark-key-0001"}); err != nil {
		t.Fatalf("创建 bark 渠道失败：%v", err)
	}
	if _, err := st.CreateAgentAlertChannel(agent.ID, "email", map[string]string{"address": "agent@example.com"}); err != nil {
		t.Fatalf("创建 email 渠道失败：%v", err)
	}
	if _, err := st.CreateAgentAlertChannel(agent.ID, "bark", map[string]string{"key": "bark-key-0002"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("第 4 个渠道应返回 ErrInvalidInput，实际 %v", err)
	}
	if channels := st.AgentAlertChannels(agent.ID); len(channels) != 3 {
		t.Fatalf("应有 3 个渠道，实际 %d", len(channels))
	}
	// 按 ID 读取单个渠道（渠道测试发送复用已保存配置用）。
	single, ok := st.AgentAlertChannel(first.ID)
	if !ok || single.Config["token"] != token {
		t.Fatalf("按 ID 读取渠道失败：%+v %v", single, ok)
	}
	if _, ok := st.AgentAlertChannel("alc_missing"); ok {
		t.Fatal("不存在的渠道不应返回 true")
	}

	// PATCH 只改 enabled：配置（含密钥）必须保持不变。
	disabled := false
	updated, err := st.UpdateAgentAlertChannel(first.ID, domain.AgentAlertChannelPatch{Enabled: &disabled}, nil)
	if err != nil {
		t.Fatalf("更新渠道失败：%v", err)
	}
	if updated.Enabled {
		t.Fatal("enabled 应更新为 false")
	}
	if updated.Config["token"] != token {
		t.Fatalf("未显式传 config 时不应改密钥：%+v", updated.Config)
	}

	// PATCH 显式传 config：合并语义——未出现的键保留旧值，出现的键覆盖。
	newToken := "999999:ZZyyXXwwVVuuTTssRRqq"
	updated, err = st.UpdateAgentAlertChannel(first.ID, domain.AgentAlertChannelPatch{}, &map[string]string{
		"token": newToken,
	})
	if err != nil {
		t.Fatalf("合并配置失败：%v", err)
	}
	if updated.Config["token"] != newToken {
		t.Fatalf("token 应被覆盖：%+v", updated.Config)
	}
	if updated.Config["chat_id"] != "10086" {
		t.Fatalf("未出现的键应保留旧值：%+v", updated.Config)
	}
	// 只改非密钥键时，密钥同样保留。
	updated, err = st.UpdateAgentAlertChannel(first.ID, domain.AgentAlertChannelPatch{}, &map[string]string{"chat_id": "10010"})
	if err != nil {
		t.Fatalf("合并 chat_id 失败：%v", err)
	}
	if updated.Config["token"] != newToken || updated.Config["chat_id"] != "10010" {
		t.Fatalf("合并结果不符：%+v", updated.Config)
	}
	// 显式空串表示清空该键；清掉必填键应报错（配置整体不合法）。
	if _, err := st.UpdateAgentAlertChannel(first.ID, domain.AgentAlertChannelPatch{}, &map[string]string{"token": ""}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("清空必填键应返回 ErrInvalidInput，实际 %v", err)
	}
	if _, err := st.UpdateAgentAlertChannel(first.ID, domain.AgentAlertChannelPatch{}, &map[string]string{"chat_id": "only"}); err != nil {
		t.Fatalf("只改 chat_id 应成功：%v", err)
	}
	if _, err := st.UpdateAgentAlertChannel("alc_missing", domain.AgentAlertChannelPatch{}, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("更新不存在渠道应返回 ErrNotFound，实际 %v", err)
	}
	if err := st.DeleteAgentAlertChannel(first.ID); err != nil {
		t.Fatalf("删除渠道失败：%v", err)
	}
	if err := st.DeleteAgentAlertChannel(first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复删除应返回 ErrNotFound，实际 %v", err)
	}
	if channels := st.AgentAlertChannels(agent.ID); len(channels) != 2 {
		t.Fatalf("删除后应剩 2 个渠道，实际 %d", len(channels))
	}

	// 删除客服应级联清理其渠道。
	if _, err := st.DeleteAgent(agent.ID); err != nil {
		t.Fatalf("删除客服失败：%v", err)
	}
	if channels := st.AgentAlertChannels(agent.ID); len(channels) != 0 {
		t.Fatalf("删除客服后渠道应被清理：%+v", channels)
	}
}

// TestMemoryAlertSMTPSettings 覆盖 SMTP 默认值、密码掩码与 password 语义。
func TestMemoryAlertSMTPSettings(t *testing.T) {
	st := NewMemoryStore(Options{})

	defaults := st.AlertSMTPSettings()
	if defaults.Port != domain.DefaultAlertSMTPPort || defaults.PasswordMasked != "" {
		t.Fatalf("SMTP 默认值不符：%+v", defaults)
	}
	if _, err := st.UpdateAlertSMTPSettings(domain.AlertSMTPSettings{Host: "smtp.example.com", Port: 70000, FromAddress: "bot@example.com"}, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("非法端口应返回 ErrInvalidInput，实际 %v", err)
	}

	password := "smtp-secret-1234"
	updated, err := st.UpdateAlertSMTPSettings(domain.AlertSMTPSettings{
		Host:        "smtp.example.com",
		Port:        465,
		Username:    "bot@example.com",
		FromAddress: "bot@example.com",
		UseTLS:      true,
	}, &password)
	if err != nil {
		t.Fatalf("更新 SMTP 失败：%v", err)
	}
	if updated.PasswordMasked != domain.MaskSecretValue(password) {
		t.Fatalf("密码应只回掩码：%q", updated.PasswordMasked)
	}
	if updated.Host != "smtp.example.com" || updated.Port != 465 || !updated.UseTLS {
		t.Fatalf("SMTP 字段未保存：%+v", updated)
	}
	// 内部读取应拿到明文（json:"-" 保证不会序列化泄漏）。
	internal := st.AlertSMTPSettings()
	if internal.Password != password {
		t.Fatalf("存储层内部应保留明文密码：%q", internal.Password)
	}

	// password==nil 表示不改密码。
	withoutPassword, err := st.UpdateAlertSMTPSettings(domain.AlertSMTPSettings{
		Host: "smtp2.example.com", Port: 587, Username: "bot@example.com", FromAddress: "bot@example.com",
	}, nil)
	if err != nil {
		t.Fatalf("更新 SMTP（不改密码）失败：%v", err)
	}
	if internal := st.AlertSMTPSettings(); internal.Password != password {
		t.Fatalf("nil 表示不改密码，实际 %q", internal.Password)
	}
	if withoutPassword.Host != "smtp2.example.com" {
		t.Fatalf("其它字段应更新：%+v", withoutPassword)
	}

	// password=="" 表示清除。
	empty := ""
	cleared, err := st.UpdateAlertSMTPSettings(domain.AlertSMTPSettings{
		Host: "smtp2.example.com", Port: 587, FromAddress: "bot@example.com",
	}, &empty)
	if err != nil {
		t.Fatalf("清除密码失败：%v", err)
	}
	if cleared.PasswordMasked != "" {
		t.Fatalf("清除后掩码应为空：%q", cleared.PasswordMasked)
	}
	if internal := st.AlertSMTPSettings(); internal.Password != "" {
		t.Fatalf("清除后内部密码应为空，实际 %q", internal.Password)
	}
}

// TestMemoryQueueCascadeOnConversationDelete 覆盖会话删除时排队记录被级联清理。
func TestMemoryQueueCascadeOnConversationDelete(t *testing.T) {
	st := NewMemoryStore(Options{})
	visitor, err := st.CreateVisitorConversation("203.0.113.12", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("创建会话失败：%v", err)
	}
	conversationID := visitor.Conversation.ID
	if _, err := st.EnqueueConversation(domain.QueuedConversation{ConversationID: conversationID, Reason: "offline"}); err != nil {
		t.Fatalf("入队失败：%v", err)
	}
	if _, _, err := st.CloseConversation(domain.AuthSession{Kind: domain.AccountAdmin, AccountID: "admin_1"}, conversationID); err != nil {
		t.Fatalf("关闭会话失败：%v", err)
	}
	if err := st.DeleteConversation(domain.AuthSession{Kind: domain.AccountAdmin, AccountID: "admin_1"}, conversationID); err != nil {
		t.Fatalf("删除会话失败：%v", err)
	}
	if queue := st.QueueList(); len(queue) != 0 {
		t.Fatalf("会话删除后排队记录应被清理：%+v", queue)
	}
}
