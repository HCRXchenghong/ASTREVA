package app

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/notify"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

// ===== 批次 4 第二波：接管 / 只读围观 / 排队 / 无人值守 / 黑名单 / 提醒渠道 的应用层测试 =====

// markReadCounter 包装存储，统计 MarkConversationRead 被真正调用的次数，
// 用于断言"只读围观期间不调用 MarkConversationRead"。
type markReadCounter struct {
	store.Store
	calls int32
}

func (c *markReadCounter) MarkConversationRead(actor domain.AuthSession, conversationID string) (domain.Conversation, error) {
	atomic.AddInt32(&c.calls, 1)
	return c.Store.MarkConversationRead(actor, conversationID)
}

// recordedEvent 是 hub 发布事件的一条记录。
type recordedEvent struct {
	target string
	key    string
	event  string
	data   any
}

// eventRecorder 通过 Hub.SetPublisher 记录所有本地/跨节点发布的 WebSocket 事件。
type eventRecorder struct {
	mu     sync.Mutex
	events []recordedEvent
}

func newEventRecorder(hub *realtime.Hub) *eventRecorder {
	recorder := &eventRecorder{}
	hub.SetPublisher(func(wire realtime.WireEvent) {
		recorder.mu.Lock()
		defer recorder.mu.Unlock()
		recorder.events = append(recorder.events, recordedEvent{
			target: string(wire.Target),
			key:    wire.Key,
			event:  wire.Event.Event,
			data:   wire.Event.Data,
		})
	})
	return recorder
}

func (r *eventRecorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = nil
}

func (r *eventRecorder) byEvent(name string) []recordedEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []recordedEvent{}
	for _, event := range r.events {
		if event.event == name {
			out = append(out, event)
		}
	}
	return out
}

// fakeAlertNotifier 记录提醒渠道派发调用，可注入失败以验证"失败不影响主流程"。
type fakeAlertNotifier struct {
	mu        sync.Mutex
	calls     []fakeAlertCall
	sendError error
}

type fakeAlertCall struct {
	channels int
	payload  notify.AlertPayload
}

func (f *fakeAlertNotifier) Notify(context.Context, domain.PushSettings, notify.Message) error {
	return nil
}

func (f *fakeAlertNotifier) SendAlertChannels(_ context.Context, channels []domain.AgentAlertChannel, payload notify.AlertPayload) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeAlertCall{channels: len(channels), payload: payload})
	return f.sendError
}

func (f *fakeAlertNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeAlertNotifier) snapshot() []fakeAlertCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeAlertCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func (f *fakeAlertNotifier) setError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendError = err
}

// createFlowAgent 直接经存储层建一个客服账号（测试用，不经过管理员接口）。
func createFlowAgent(t *testing.T, st store.Store, account string, maxConversations int) domain.Agent {
	t.Helper()
	agent, err := st.CreateAgent(domain.Agent{
		Account:          account,
		Name:             "客服-" + account,
		Group:            "default",
		MaxConversations: maxConversations,
	}, "AgentPass-123456")
	if err != nil {
		t.Fatalf("create agent %s: %v", account, err)
	}
	return agent
}

// newFlowService 构造关闭营业时间限制、关闭排队的服务实例（避免无关副作用干扰用例）。
func newFlowService(t *testing.T) (*Service, *store.MemoryStore, *eventRecorder) {
	t.Helper()
	st := store.NewMemoryStore(store.Options{})
	if _, err := st.UpdateBusinessHours(domain.BusinessHours{Timezone: "UTC", Enabled: false}); err != nil {
		t.Fatalf("update business hours: %v", err)
	}
	policy := domain.DefaultServicePolicy()
	policy.QueueEnabled = false
	if _, err := st.UpdateServicePolicy(policy); err != nil {
		t.Fatalf("update service policy: %v", err)
	}
	hub := realtime.NewHub()
	recorder := newEventRecorder(hub)
	return NewService(st, hub, slog.Default()), st, recorder
}

// flowConversation 读取会话并断言存在。
func flowConversation(t *testing.T, st store.Store, conversationID string) domain.Conversation {
	t.Helper()
	conversation, ok := st.Conversation(conversationID)
	if !ok {
		t.Fatalf("会话不存在：%s", conversationID)
	}
	return conversation
}

func waitForFlowCondition(t *testing.T, description string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待条件超时：%s", description)
}

func countAuditAction(st store.Store, action string) int {
	count := 0
	for _, event := range st.RecentAuditEvents(100) {
		if event.Action == action {
			count++
		}
	}
	return count
}

// TestTakeoverAssignsAgentAndStopsAI 覆盖接管的核心语义：
// 分配给自己、状态置 assigned、清空 SLA 状态、停止 AI 自动回复、拒绝跨客服接管。
func TestTakeoverAssignsAgentAndStopsAI(t *testing.T) {
	service, st, recorder := newFlowService(t)
	if _, err := st.UpdateAISettings(domain.AISettings{Enabled: true, Mode: domain.AIModeHumanFirst}); err != nil {
		t.Fatalf("update ai settings: %v", err)
	}
	agentA := createFlowAgent(t, st, "takeover_a", 5)
	agentB := createFlowAgent(t, st, "takeover_b", 5)
	if _, _, err := st.SetAgentStatus(agentA.ID, domain.AgentOnline); err != nil {
		t.Fatalf("set agent online: %v", err)
	}
	session, err := service.CreateVisitorConversation("203.0.113.10", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	conversationID := session.Conversation.ID
	if session.Conversation.AssignedAgentID != agentA.ID {
		t.Fatalf("会话应被静默分配给在线客服 A：%+v", session.Conversation)
	}
	if session.Conversation.Status == domain.ConversationAssigned {
		t.Fatalf("未接管前不应是 assigned：%+v", session.Conversation.Status)
	}

	recorder.reset()
	conversation, err := service.TakeoverConversation(agentA.ID, conversationID)
	if err != nil {
		t.Fatalf("takeover: %v", err)
	}
	if conversation.Status != domain.ConversationAssigned {
		t.Fatalf("接管后状态应为 assigned，实际 %q", conversation.Status)
	}
	if conversation.SLAState != "" || conversation.FirstResponseAt != nil {
		t.Fatalf("接管后应清空 SLA 状态与首响时间：%+v", conversation)
	}
	if conversation.AssignedAgentID != agentA.ID {
		t.Fatalf("接管后应分配给发起客服：%+v", conversation.AssignedAgentID)
	}
	// 广播给访客与管理员（该客服自身也会收到 assigned）。
	if len(recorder.byEvent("conversation.status_changed")) == 0 {
		t.Fatal("接管应广播 conversation.status_changed")
	}

	// 接管后 AI 不再自动回复：不触发 NeedAI，且不产生任何上游调用。
	result, err := service.AddVisitorMessage(conversationID, "m-after-takeover", "你们在吗", domain.MessageText)
	if err != nil {
		t.Fatalf("add visitor message: %v", err)
	}
	if result.NeedAI {
		t.Fatal("接管后不得再触发 AI 自动回复")
	}

	// 他人负责的会话不能接管（403 语义）。
	if _, err := service.TakeoverConversation(agentB.ID, conversationID); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("跨客服接管应返回 ErrForbidden，实际 %v", err)
	}
	// 重复接管同一会话幂等，且不重复计并发。
	before := agentCurrentConversations(t, st, agentA.ID)
	if _, err := service.TakeoverConversation(agentA.ID, conversationID); err != nil {
		t.Fatalf("重复接管应幂等成功：%v", err)
	}
	if after := agentCurrentConversations(t, st, agentA.ID); after != before {
		t.Fatalf("重复接管不得重复计并发：before=%d after=%d", before, after)
	}
	// 已结束会话不能接管。
	if _, err := service.CloseConversation(domain.AuthSession{Kind: domain.AccountAdmin, AccountID: "admin_super"}, conversationID); err != nil {
		t.Fatalf("close conversation: %v", err)
	}
	if _, err := service.TakeoverConversation(agentA.ID, conversationID); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("已结束会话应拒绝接管，实际 %v", err)
	}
}

func agentCurrentConversations(t *testing.T, st store.Store, agentID string) int {
	t.Helper()
	for _, agent := range st.AllAgents() {
		if agent.ID == agentID {
			return agent.CurrentConversations
		}
	}
	t.Fatalf("未找到客服 %s", agentID)
	return 0
}

// TestWatchIsReadOnlyAndSkipsMarkRead 覆盖只读围观：
// 不改变分配与并发、不计入客服会话列表、围观期间不调用 MarkConversationRead（未读数不变）。
func TestWatchIsReadOnlyAndSkipsMarkRead(t *testing.T) {
	base := store.NewMemoryStore(store.Options{})
	counter := &markReadCounter{Store: base}
	service := NewService(counter, realtime.NewHub(), slog.Default())
	if _, err := base.UpdateBusinessHours(domain.BusinessHours{Timezone: "UTC", Enabled: false}); err != nil {
		t.Fatalf("update business hours: %v", err)
	}
	// AI 关闭自主接待，避免 AI 回复把未读数清零。
	if _, err := base.UpdateAISettings(domain.AISettings{Enabled: true, Mode: domain.AIModeManualOnly}); err != nil {
		t.Fatalf("update ai settings: %v", err)
	}
	policy := domain.DefaultServicePolicy()
	policy.QueueEnabled = false
	if _, err := base.UpdateServicePolicy(policy); err != nil {
		t.Fatalf("update service policy: %v", err)
	}
	agentA := createFlowAgent(t, base, "watch_a", 5)
	agentB := createFlowAgent(t, base, "watch_b", 5)
	if _, _, err := base.SetAgentStatus(agentA.ID, domain.AgentOnline); err != nil {
		t.Fatalf("set agent online: %v", err)
	}

	session, err := service.CreateVisitorConversation("203.0.113.20", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	conversationID := session.Conversation.ID
	if session.Conversation.AssignedAgentID != agentA.ID {
		t.Fatalf("会话应分配给 A：%+v", session.Conversation)
	}
	for index, content := range []string{"你好", "在吗"} {
		if _, err := service.AddVisitorMessage(conversationID, "watch-msg-"+string(rune('a'+index)), content, domain.MessageText); err != nil {
			t.Fatalf("add visitor message: %v", err)
		}
	}
	unreadBefore := flowConversation(t, base, conversationID).UnreadForAgent
	if unreadBefore == 0 {
		t.Fatal("访客消息应产生未读")
	}

	if _, err := service.StartWatching(agentB.ID, conversationID); err != nil {
		t.Fatalf("start watching: %v", err)
	}
	if !service.IsWatching(agentB.ID, conversationID) {
		t.Fatal("围观登记未生效")
	}
	// 不改变分配与并发计数。
	after := flowConversation(t, base, conversationID)
	if after.AssignedAgentID != agentA.ID {
		t.Fatalf("围观不得改变分配：%+v", after.AssignedAgentID)
	}
	if got := agentCurrentConversations(t, base, agentA.ID); got != 1 {
		t.Fatalf("A 的并发应保持 1，实际 %d", got)
	}
	if got := agentCurrentConversations(t, base, agentB.ID); got != 0 {
		t.Fatalf("围观不得计入 B 的并发，实际 %d", got)
	}
	if list, err := base.ConversationsForAgent(agentB.ID); err != nil || len(list) != 0 {
		t.Fatalf("围观会话不得进入 B 的会话列表：%v %+v", err, list)
	}

	// 围观期间"标记已读"必须被跳过：不调用存储层，未读数不变。
	if _, err := service.MarkConversationRead(domain.AuthSession{Kind: domain.AccountAgent, AccountID: agentB.ID}, conversationID); err != nil {
		t.Fatalf("围观期间标记已读应静默跳过：%v", err)
	}
	if calls := atomic.LoadInt32(&counter.calls); calls != 0 {
		t.Fatalf("围观期间不得调用 MarkConversationRead，实际调用 %d 次", calls)
	}
	if got := flowConversation(t, base, conversationID).UnreadForAgent; got != unreadBefore {
		t.Fatalf("围观期间未读数不得变化：before=%d after=%d", unreadBefore, got)
	}

	// 退出围观后：负责客服（A）可正常已读，非负责客服（B）仍然 403。
	if err := service.StopWatching(agentB.ID, conversationID); err != nil {
		t.Fatalf("stop watching: %v", err)
	}
	if service.IsWatching(agentB.ID, conversationID) {
		t.Fatal("退出围观未生效")
	}
	read, err := service.MarkConversationRead(domain.AuthSession{Kind: domain.AccountAgent, AccountID: agentA.ID}, conversationID)
	if err != nil {
		t.Fatalf("负责客服已读应成功：%v", err)
	}
	if read.UnreadForAgent != 0 {
		t.Fatalf("负责客服已读后未读数应为 0：%d", read.UnreadForAgent)
	}
	if calls := atomic.LoadInt32(&counter.calls); calls == 0 {
		t.Fatal("退出围观后应走正常已读路径")
	}
	if _, err := service.MarkConversationRead(domain.AuthSession{Kind: domain.AccountAgent, AccountID: agentB.ID}, conversationID); !errors.Is(err, store.ErrForbidden) {
		t.Fatalf("未负责的客服已读应 403，实际 %v", err)
	}
}

// TestTakeoverRequestPopupRespectsPreferences 覆盖接管提示事件：
// 只推给"在线 + 未关闭接管弹窗"的客服，且受全局开关控制。
func TestTakeoverRequestPopupRespectsPreferences(t *testing.T) {
	service, st, recorder := newFlowService(t)
	agentA := createFlowAgent(t, st, "popup_a", 5)
	agentB := createFlowAgent(t, st, "popup_b", 5)
	// B 关闭接管弹窗（默认开启）。
	off := false
	if _, err := st.UpdateAgentPreferences(agentB.ID, domain.AgentPreferences{TakeoverPopupEnabled: &off}); err != nil {
		t.Fatalf("update agent preferences: %v", err)
	}
	for _, agent := range []domain.Agent{agentA, agentB} {
		if _, _, err := st.SetAgentStatus(agent.ID, domain.AgentOnline); err != nil {
			t.Fatalf("set agent online: %v", err)
		}
	}

	recorder.reset()
	if _, err := service.CreateVisitorConversation("203.0.113.30", "web", "zh", "direct", ""); err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	requests := recorder.byEvent("conversation.takeover_request")
	if len(requests) != 1 {
		t.Fatalf("应只向开启接管弹窗的在线客服推送一次，实际 %d 次：%+v", len(requests), requests)
	}
	if requests[0].key != agentA.ID {
		t.Fatalf("接管提示应收件人是 A，实际 %q", requests[0].key)
	}
	payload, ok := requests[0].data.(takeoverRequestPayload)
	if !ok {
		t.Fatalf("接管提示事件体类型不符：%#v", requests[0].data)
	}
	if payload.ConversationID == "" || payload.VisitorName == "" {
		t.Fatalf("接管提示缺少会话/访客信息：%+v", payload)
	}
	// 不得影响既有事件语义：会话创建/静默分配的既有广播仍然照常发出。
	if len(recorder.byEvent("conversation.created")) == 0 {
		t.Fatal("会话创建仍应广播 conversation.created")
	}
	if len(recorder.byEvent("conversation.assigned")) == 0 {
		t.Fatal("静默分配仍应给客服推送 conversation.assigned")
	}

	// 全局开关关闭后，任何客服都收不到接管提示。
	policy := st.ServicePolicy()
	policy.TakeoverPopupEnabled = false
	if _, err := st.UpdateServicePolicy(policy); err != nil {
		t.Fatalf("update service policy: %v", err)
	}
	recorder.reset()
	if _, err := service.CreateVisitorConversation("203.0.113.31", "web", "zh", "direct", ""); err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if requests := recorder.byEvent("conversation.takeover_request"); len(requests) != 0 {
		t.Fatalf("关闭全局开关后不得推送接管提示：%+v", requests)
	}
}

// TestQueueDrainByPriorityAndOfflineReenqueue 覆盖排队：
// 无人可分配 → 入队并推送排队位置；客服上线 → 按优先级出队分配并移出队列；离线释放且未首响 → 重新入队。
func TestQueueDrainByPriorityAndOfflineReenqueue(t *testing.T) {
	service, st, recorder := newFlowService(t)
	policy := domain.DefaultServicePolicy()
	policy.QueueEnabled = true
	if _, err := st.UpdateServicePolicy(policy); err != nil {
		t.Fatalf("update service policy: %v", err)
	}
	policy.TakeoverPopupEnabled = false // 避免接管提示干扰断言
	if _, err := st.UpdateServicePolicy(policy); err != nil {
		t.Fatalf("update service policy: %v", err)
	}

	first, err := service.CreateVisitorConversation("203.0.113.41", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create first conversation: %v", err)
	}
	second, err := service.CreateVisitorConversation("203.0.113.42", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create second conversation: %v", err)
	}
	queue := st.QueueList()
	if len(queue) != 2 {
		t.Fatalf("无在线客服时应全部入队，实际 %d 条", len(queue))
	}
	positions := recorder.byEvent("conversation.queue_position")
	if len(positions) != 2 {
		t.Fatalf("入队应给访客推送排队位置，实际 %d 次", len(positions))
	}
	for _, event := range positions {
		data, ok := event.data.(map[string]any)
		if !ok || data["position"] == nil {
			t.Fatalf("排队位置事件体不符：%#v", event.data)
		}
	}

	// 让第二个会话优先级更高：出队时必须先分配它。
	if _, err := st.EnqueueConversation(domain.QueuedConversation{ConversationID: second.Conversation.ID, Priority: 5}); err != nil {
		t.Fatalf("re-enqueue second: %v", err)
	}
	if _, err := st.EnqueueConversation(domain.QueuedConversation{ConversationID: first.Conversation.ID, Priority: 1}); err != nil {
		t.Fatalf("re-enqueue first: %v", err)
	}

	agent := createFlowAgent(t, st, "queue_agent", 1)
	recorder.reset()
	if _, err := service.SetAgentStatus(agent.ID, domain.AgentOnline); err != nil {
		t.Fatalf("set agent online: %v", err)
	}
	assigned := flowConversation(t, st, second.Conversation.ID)
	if assigned.AssignedAgentID != agent.ID {
		t.Fatalf("应按优先级先分配第二个会话，实际 %+v", assigned.AssignedAgentID)
	}
	if got := flowConversation(t, st, first.Conversation.ID); got.AssignedAgentID != "" {
		t.Fatalf("并发已满，第一个会话不应被分配：%+v", got.AssignedAgentID)
	}
	queue = st.QueueList()
	if len(queue) != 1 || queue[0].ConversationID != first.Conversation.ID {
		t.Fatalf("分配成功后必须移出队列，剩余队列不符：%+v", queue)
	}
	if len(recorder.byEvent("conversation.assigned")) == 0 {
		t.Fatal("出队分配应通知客服与访客")
	}

	// 离线释放：尚未人工首响 → 重新入队。
	recorder.reset()
	released, err := service.SetAgentStatus(agent.ID, domain.AgentOffline)
	if err != nil {
		t.Fatalf("set agent offline: %v", err)
	}
	if released.Status != domain.AgentOffline {
		t.Fatalf("客服应离线：%+v", released.Status)
	}
	if got := flowConversation(t, st, second.Conversation.ID); got.AssignedAgentID != "" {
		t.Fatalf("离线后会话应被释放：%+v", got.AssignedAgentID)
	}
	found := false
	for _, entry := range st.QueueList() {
		if entry.ConversationID == second.Conversation.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("离线释放且未首响的会话必须重新入队：%+v", st.QueueList())
	}
}

// TestUnattendedFallbackSentOnceAndQueueRecovered 覆盖无人值守兜底：
// 非营业时间只发一次（按访客语言翻译）；恢复营业后队列中的会话被分配。
func TestUnattendedFallbackSentOnceAndQueueRecovered(t *testing.T) {
	stub, _, _ := newKnowledgeStub(t, "Our team will get back to you shortly.")
	service, st := newKnowledgeService(t, domain.AIModeManualOnly, stub.URL)
	// 营业时间窗口设为 00:00–00:00（长度为零）→ 任何时刻都是非营业时间。
	if _, err := st.UpdateBusinessHours(domain.BusinessHours{Timezone: "UTC", Start: "00:00", End: "00:00", Enabled: true}); err != nil {
		t.Fatalf("update business hours: %v", err)
	}
	replyText := "客服稍后回复您。"
	policy := domain.DefaultServicePolicy()
	policy.QueueEnabled = true
	policy.UnattendedEnabled = true
	policy.UnattendedReply = replyText
	policy.TakeoverPopupEnabled = false
	if _, err := st.UpdateServicePolicy(policy); err != nil {
		t.Fatalf("update service policy: %v", err)
	}

	session, err := service.CreateVisitorConversation("203.0.113.60", "web", "en", "direct", "")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	conversationID := session.Conversation.ID
	notices := func() []domain.Message {
		out := []domain.Message{}
		for _, msg := range st.Messages(conversationID) {
			if msg.Content == replyText {
				out = append(out, msg)
			}
		}
		return out
	}
	waitForFlowCondition(t, "无人值守兜底话术落库", func() bool { return len(notices()) == 1 })
	notice := notices()[0]
	if notice.MessageType != domain.MessageSystem {
		t.Fatalf("兜底话术应按系统消息落库：%+v", notice.MessageType)
	}
	if got := notice.Translations["en"]; got != "Our team will get back to you shortly." {
		t.Fatalf("兜底话术译文不符：%q", got)
	}
	// 会话应已进入队列（排队开关打开）。
	if queued := st.QueueList(); len(queued) != 1 || queued[0].ConversationID != conversationID {
		t.Fatalf("非营业时间仍应排队等待恢复：%+v", queued)
	}

	// 第二条访客消息不得重复发送兜底话术。
	if _, err := service.AddVisitorMessage(conversationID, "unattended-2", "hello again", domain.MessageText); err != nil {
		t.Fatalf("add visitor message: %v", err)
	}
	if got := len(notices()); got != 1 {
		t.Fatalf("同一会话只允许发一次兜底话术，实际 %d 次", got)
	}

	// 恢复营业 + 客服上线 → 队列中的会话被分配。
	if _, err := st.UpdateBusinessHours(domain.BusinessHours{Timezone: "UTC", Enabled: false}); err != nil {
		t.Fatalf("update business hours: %v", err)
	}
	agent := createFlowAgent(t, st, "unattended_agent", 5)
	if _, err := service.SetAgentStatus(agent.ID, domain.AgentOnline); err != nil {
		t.Fatalf("set agent online: %v", err)
	}
	if got := flowConversation(t, st, conversationID); got.AssignedAgentID != agent.ID {
		t.Fatalf("营业恢复后队列会话应被分配：%+v", got.AssignedAgentID)
	}
	if queue := st.QueueList(); len(queue) != 0 {
		t.Fatalf("分配成功后队列应清空：%+v", queue)
	}
}

// TestBlacklistBlocksCreationAndMessages 覆盖黑名单三类命中、审计与过期放行。
func TestBlacklistBlocksCreationAndMessages(t *testing.T) {
	service, st, _ := newFlowService(t)

	// ip 命中：建会话被拒，且错误里不得包含被拉黑的值。
	if _, err := st.AddBlacklist(domain.BlacklistEntry{Kind: domain.BlacklistKindIP, Value: "203.0.113.70"}); err != nil {
		t.Fatalf("add ip blacklist: %v", err)
	}
	_, err := service.CreateVisitorConversation("203.0.113.70", "web", "zh", "direct", "")
	if !errors.Is(err, store.ErrBlacklisted) {
		t.Fatalf("ip 命中应拒绝建会话，实际 %v", err)
	}
	if strings.Contains(err.Error(), "203.0.113.70") {
		t.Fatalf("错误文本不得泄露被拉黑的 ip：%v", err)
	}
	if got := store.BlacklistMessage(err); got != domain.DefaultBlacklistMessage {
		t.Fatalf("黑名单文案不符：%q", got)
	}

	// account 命中：开放 API 的外部用户 ID（落在 remark 上）被拒。
	if _, err := st.AddBlacklist(domain.BlacklistEntry{Kind: domain.BlacklistKindAccount, Value: "ext-user-9"}); err != nil {
		t.Fatalf("add account blacklist: %v", err)
	}
	if _, err := service.CreateVisitorConversation("203.0.113.71", "web", "zh", "direct", "ext-user-9"); !errors.Is(err, store.ErrBlacklisted) {
		t.Fatalf("account 命中应拒绝建会话，实际 %v", err)
	}

	// visitor 命中：发消息被拒。
	session, err := service.CreateVisitorConversation("203.0.113.72", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if _, err := st.AddBlacklist(domain.BlacklistEntry{Kind: domain.BlacklistKindVisitor, Value: session.Conversation.VisitorID}); err != nil {
		t.Fatalf("add visitor blacklist: %v", err)
	}
	if _, err := service.AddVisitorMessage(session.Conversation.ID, "blocked-1", "你好", domain.MessageText); !errors.Is(err, store.ErrBlacklisted) {
		t.Fatalf("visitor 命中应拒绝发消息，实际 %v", err)
	}
	if len(st.Messages(session.Conversation.ID)) != 0 {
		t.Fatal("被拦截的消息不得落库")
	}
	if count := countAuditAction(st, "blacklist.blocked"); count < 3 {
		t.Fatalf("三类命中都应写审计，实际 %d 条", count)
	}

	// 过期条目放行。
	expired := time.Now().UTC().Add(-time.Hour)
	if _, err := st.AddBlacklist(domain.BlacklistEntry{Kind: domain.BlacklistKindIP, Value: "203.0.113.73", ExpiresAt: &expired}); err != nil {
		t.Fatalf("add expired blacklist: %v", err)
	}
	if _, err := service.CreateVisitorConversation("203.0.113.73", "web", "zh", "direct", ""); err != nil {
		t.Fatalf("过期黑名单应放行，实际 %v", err)
	}
}

// TestAgentAlertDispatchOnAssignAndVisitorMessage 覆盖提醒渠道派发：
// 分配/访客消息触发派发、渠道为空不调用、发送失败不影响主流程。
func TestAgentAlertDispatchOnAssignAndVisitorMessage(t *testing.T) {
	service, st, _ := newFlowService(t)
	notifier := &fakeAlertNotifier{}
	service.SetNotifier(notifier)
	agent := createFlowAgent(t, st, "alert_agent", 5)
	if _, _, err := st.SetAgentStatus(agent.ID, domain.AgentOnline); err != nil {
		t.Fatalf("set agent online: %v", err)
	}
	// 渠道为空：分配不应产生任何派发。
	if _, err := service.CreateVisitorConversation("203.0.113.80", "web", "zh", "direct", ""); err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	time.Sleep(80 * time.Millisecond)
	if notifier.count() != 0 {
		t.Fatalf("渠道为空时不得发起派发，实际 %d 次", notifier.count())
	}

	// 配置一个渠道后：新会话分配 + 访客新消息都应派发。
	if _, err := st.CreateAgentAlertChannel(agent.ID, domain.AlertChannelKindBark, map[string]string{"key": "bark-key-0001"}); err != nil {
		t.Fatalf("create alert channel: %v", err)
	}
	session, err := service.CreateVisitorConversation("203.0.113.81", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	waitForFlowCondition(t, "分配触发提醒派发", func() bool { return notifier.count() >= 1 })

	longContent := strings.Repeat("很长的消息内容", 40)
	if _, err := service.AddVisitorMessage(session.Conversation.ID, "alert-msg", longContent, domain.MessageText); err != nil {
		t.Fatalf("add visitor message: %v", err)
	}
	waitForFlowCondition(t, "访客消息触发提醒派发", func() bool { return notifier.count() >= 2 })
	calls := notifier.snapshot()
	var messageCall *fakeAlertCall
	for index := range calls {
		if strings.Contains(calls[index].payload.Title, "新访客消息") {
			messageCall = &calls[index]
		}
	}
	if messageCall == nil {
		t.Fatalf("未找到访客消息派发：%+v", calls)
	}
	if messageCall.payload.ConversationID != session.Conversation.ID {
		t.Fatalf("派发应带 conversation_id：%+v", messageCall.payload)
	}
	if !strings.Contains(messageCall.payload.Body, "访客") {
		t.Fatalf("派发正文应包含访客名：%+v", messageCall.payload)
	}
	preview := alertPreview(longContent)
	if len([]rune(preview)) > 81 {
		t.Fatalf("消息摘要应截断到 80 字：%d", len([]rune(preview)))
	}
	if !strings.Contains(messageCall.payload.Body, preview) {
		t.Fatalf("派发正文应包含截断后的消息摘要：%+v", messageCall.payload)
	}

	// 发送失败只记日志：消息主流程不受影响。
	notifier.setError(errors.New("smtp down"))
	before := notifier.count()
	result, err := service.AddVisitorMessage(session.Conversation.ID, "alert-msg-2", "第二条", domain.MessageText)
	if err != nil {
		t.Fatalf("派发失败不得影响消息主流程：%v", err)
	}
	if result.Input.ServerMsgID == "" {
		t.Fatal("消息应正常落库")
	}
	waitForFlowCondition(t, "失败派发已记录", func() bool { return notifier.count() > before })
}
