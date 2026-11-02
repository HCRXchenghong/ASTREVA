package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"customer-service/backend/internal/ai"
	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/notify"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

// ===== 批次 4 第二波：接管 / 只读围观 / 排队出队 / 无人值守兜底 / 黑名单 / 提醒渠道派发 =====
//
// 本文件把"存储能力"接成真实业务语义，全部逻辑都受客服策略开关控制：
//   - 排队、无人值守、接管弹窗默认开启，但只有在"分配失败/开关打开"时才产生可见副作用；
//   - 关闭对应开关后行为与改造前完全一致（既有会话流转不受影响）。

// watchRegistry 登记"客服正在只读围观哪些会话"。
//
// 说明：围观状态放在进程内存里，重启后丢失（可接受：App 会重新发起围观，
// 且围观本身不分配、不计入并发、不产生任何持久化副作用）。
type watchRegistry struct {
	mu      sync.RWMutex
	byAgent map[string]map[string]struct{}
}

func newWatchRegistry() *watchRegistry {
	return &watchRegistry{byAgent: map[string]map[string]struct{}{}}
}

func (w *watchRegistry) add(agentID, conversationID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.byAgent[agentID] == nil {
		w.byAgent[agentID] = map[string]struct{}{}
	}
	w.byAgent[agentID][conversationID] = struct{}{}
}

func (w *watchRegistry) remove(agentID, conversationID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.byAgent[agentID], conversationID)
	if len(w.byAgent[agentID]) == 0 {
		delete(w.byAgent, agentID)
	}
}

func (w *watchRegistry) removeConversation(conversationID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for agentID, conversations := range w.byAgent {
		delete(conversations, conversationID)
		if len(conversations) == 0 {
			delete(w.byAgent, agentID)
		}
	}
}

func (w *watchRegistry) removeAgent(agentID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.byAgent, agentID)
}

func (w *watchRegistry) has(agentID, conversationID string) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	_, ok := w.byAgent[agentID][conversationID]
	return ok
}

// takeoverRequestPayload 是推送给在线客服的「接管提示」事件体。
type takeoverRequestPayload struct {
	ConversationID string `json:"conversation_id"`
	VisitorName    string `json:"visitor_name"`
	WaitingSeconds int    `json:"waiting_seconds"`
}

// SetAlertLinkHint 设置提醒渠道正文里的会话链接前缀（PUBLIC_ADMIN_URL）；为空表示不下发链接。
func (s *Service) SetAlertLinkHint(url string) {
	s.alertLinkHint = strings.TrimSpace(url)
}

// IsWatching 判断某客服是否正在只读围观某会话。
func (s *Service) IsWatching(agentID, conversationID string) bool {
	if s.watching == nil {
		return false
	}
	return s.watching.has(strings.TrimSpace(agentID), strings.TrimSpace(conversationID))
}

// StartWatching 进入只读围观：只登记进程内状态，不分配、不计入并发、不标记已读。
func (s *Service) StartWatching(agentID, conversationID string) (domain.Conversation, error) {
	conversation, ok := s.store.Conversation(conversationID)
	if !ok {
		return domain.Conversation{}, store.ErrNotFound
	}
	if conversation.Status == domain.ConversationClosed {
		return domain.Conversation{}, store.ErrForbidden
	}
	agentID = strings.TrimSpace(agentID)
	// 默认只允许围观未分配会话、自己负责的会话或本组队列中的会话。
	// 需要“任意客服围观任意会话”时，由后台“安全策略”把围观范围改为 all。
	allowAll := s.store.ServicePolicy().AgentWatchScope == domain.AgentWatchScopeAll
	var agent domain.Agent
	for _, item := range s.store.AllAgents() {
		if item.ID == agentID {
			agent = item
			break
		}
	}
	allowed := allowAll || conversation.AssignedAgentID == "" ||
		conversation.AssignedAgentID == agentID ||
		(agent.ID != "" && conversation.QueueGroup != "" && agent.Group == conversation.QueueGroup)
	if !allowed {
		return domain.Conversation{}, store.ErrForbidden
	}
	if s.watching == nil {
		s.watching = newWatchRegistry()
	}
	s.watching.add(agentID, conversationID)
	return conversation, nil
}

// ClearWatchers 在会话转接/结束/删除或客服停用后清理围观登记，避免旧权限继续读消息。
func (s *Service) ClearWatchers(conversationID string) {
	if s.watching != nil {
		s.watching.removeConversation(conversationID)
	}
}

func (s *Service) ClearAgentWatchers(agentID string) {
	if s.watching != nil {
		s.watching.removeAgent(agentID)
	}
}

// StopWatching 退出只读围观；会话已删除时返回 ErrNotFound。
func (s *Service) StopWatching(agentID, conversationID string) error {
	if _, ok := s.store.Conversation(conversationID); !ok {
		return store.ErrNotFound
	}
	if s.watching != nil {
		s.watching.remove(agentID, conversationID)
	}
	return nil
}

// MarkConversationRead 标记会话已读。客服处于只读围观时直接跳过：
// 围观不产生任何"已读"副作用（不清零 unread_for_agent），这是围观与正式负责的核心区别。
func (s *Service) MarkConversationRead(session domain.AuthSession, conversationID string) (domain.Conversation, error) {
	if session.Kind == domain.AccountAgent && s.IsWatching(session.AccountID, conversationID) {
		conversation, ok := s.store.Conversation(conversationID)
		if !ok {
			return domain.Conversation{}, store.ErrNotFound
		}
		return conversation, nil
	}
	return s.store.MarkConversationRead(session, conversationID)
}

// TakeoverConversation 客服正式接管会话：分配给自己、状态置 assigned、停止 AI 自动回复
// （assigned 状态在存储层会直接判定为不需要 AI），并广播状态变化。
func (s *Service) TakeoverConversation(agentID, conversationID string) (domain.Conversation, error) {
	conversation, err := s.store.TakeoverConversation(agentID, conversationID)
	if err != nil {
		return domain.Conversation{}, err
	}
	// 接管后所有围观者都失去读权限（包括原围观者），避免越权继续读消息。
	s.ClearWatchers(conversationID)
	agentID = strings.TrimSpace(agentID)
	s.hub.SendToAgent(agentID, realtime.Event{Event: "conversation.assigned", Data: conversation})
	// 该客服也需要 status_changed（App 用它刷新会话状态与 AI 接待标记）。
	s.hub.SendToAgent(agentID, realtime.Event{Event: "conversation.status_changed", Data: conversation})
	s.hub.SendToVisitor(conversation.ID, realtime.Event{Event: "conversation.status_changed", Data: conversation})
	s.hub.BroadcastAdmins(realtime.Event{Event: "conversation.status_changed", Data: conversation})
	s.dispatchAgentAlert(agentID, "会话已接管",
		"您已接管访客「"+visitorDisplayName(conversation)+"」的会话。", conversation.ID)
	return conversation, nil
}

// drainQueue 按 priority DESC / enqueued_at ASC 顺序尝试出队分配。
// 触发点：客服上线、会话结束/转出释放并发、以及离线释放后的重新排队。
// 非营业时间遵循既有语义不自动接单（队列保留，等恢复营业后由下一次触发分配）。
func (s *Service) drainQueue() []domain.Conversation {
	entries := s.store.QueueList()
	if len(entries) == 0 {
		return nil
	}
	if !s.store.BusinessHours().IsOpenAt(time.Now().UTC()) {
		return nil
	}
	assigned := make([]domain.Conversation, 0, len(entries))
	for _, entry := range entries {
		conversation, ok := s.store.Conversation(entry.ConversationID)
		if !ok || conversation.Status == domain.ConversationClosed {
			// 会话已被删除/已结束：顺手清理悬挂队列记录，避免队列永远排不空。
			_ = s.store.DequeueConversation(entry.ConversationID)
			continue
		}
		if conversation.AssignedAgentID != "" {
			// 已由别的路径分配：出队即可。
			_ = s.store.DequeueConversation(entry.ConversationID)
			continue
		}
		target, ok := s.selectQueueTarget(entry.GroupName)
		if !ok {
			// 当前没有可用客服；队列有序，后面的也不会成功，直接结束本轮。
			break
		}
		updated, err := s.store.AssignConversationToAgent(conversation.ID, target.ID)
		if err != nil {
			// 竞态下该条分不出去（例如刚被别的路径抢走）属于预期；其它错误只记一条告警。
			if !errors.Is(err, store.ErrConflict) {
				s.logger.Warn("排队出队分配失败", "conversation_id", conversation.ID, "agent_id", target.ID, "error", err)
			}
			continue
		}
		assigned = append(assigned, updated)
		s.hub.SendToAgent(updated.AssignedAgentID, realtime.Event{Event: "conversation.assigned", Data: updated})
		s.hub.SendToVisitor(updated.ID, realtime.Event{Event: "conversation.status_changed", Data: updated})
		s.hub.BroadcastAdmins(realtime.Event{Event: "conversation.status_changed", Data: updated})
		s.notifyAgent(updated)
		s.dispatchAgentAlert(updated.AssignedAgentID, "新会话分配",
			"访客「"+visitorDisplayName(updated)+"」已排队分配给您。", updated.ID)
	}
	return assigned
}

// selectQueueTarget 选择一个可承接该分组的在线客服：优先并发数最少，其次 ID 稳定排序。
// 分组为空表示不限制分组。
func (s *Service) selectQueueTarget(group string) (domain.Agent, bool) {
	return s.selectAvailableAgent(group, "")
}

// selectReassignTarget 为 SLA 空闲转接选择目标客服：优先同分组，其次不限分组；
// 永远排除当前负责客服（转接语义是"转给其他在线客服"）。
func (s *Service) selectReassignTarget(group, excludeAgentID string) (domain.Agent, bool) {
	if target, ok := s.selectAvailableAgent(group, excludeAgentID); ok {
		return target, true
	}
	group = strings.TrimSpace(group)
	if group == "" {
		return domain.Agent{}, false
	}
	return s.selectAvailableAgent("", excludeAgentID)
}

// selectAvailableAgent 选择一个在线、未禁用、未达并发的客服；excludeAgentID 非空时排除该客服。
// 选择规则：并发数最少优先，其次 ID 稳定排序（保证可预测）。
func (s *Service) selectAvailableAgent(group, excludeAgentID string) (domain.Agent, bool) {
	group = strings.TrimSpace(group)
	excludeAgentID = strings.TrimSpace(excludeAgentID)
	var selected domain.Agent
	found := false
	for _, agent := range s.store.AllAgents() {
		if agent.Status != domain.AgentOnline || agent.DisabledAt != nil {
			continue
		}
		if excludeAgentID != "" && agent.ID == excludeAgentID {
			continue
		}
		if agent.CurrentConversations >= agent.MaxConversations {
			continue
		}
		if group != "" && strings.TrimSpace(agent.Group) != group {
			continue
		}
		if !found || agent.CurrentConversations < selected.CurrentConversations ||
			(agent.CurrentConversations == selected.CurrentConversations && agent.ID < selected.ID) {
			selected, found = agent, true
		}
	}
	return selected, found
}

// handleUnassignedConversation 处理"创建/收到消息后仍未分配客服"的会话：
// 按策略入队（含排队位置推送）；满足无人值守条件时补发一次兜底话术。
// notifyPopup 表示"本次入队"是否要补发接管提示（新建会话已在创建时推过，避免重复）。
func (s *Service) handleUnassignedConversation(conversation domain.Conversation, notifyPopup bool) {
	policy := s.store.ServicePolicy()
	entry, queued, newlyQueued := s.enqueueIfNeeded(conversation)
	if newlyQueued && notifyPopup {
		// 会话重新进入排队（例如客服离线释放后）时也要让在线客服看到接管提示。
		s.notifyTakeoverRequest(conversation, entry.EnqueuedAt)
	}
	if !policy.UnattendedEnabled {
		return
	}
	open := s.store.BusinessHours().IsOpenAt(time.Now().UTC())
	if !open || !queued {
		// 非营业时间，或"无在线客服且未入队成功"（排队关闭/入队失败）→ 无人值守兜底。
		s.sendUnattendedNotice(conversation)
	}
}

// unattendedEligible 判断是否该发无人值守兜底话术。
//
// 除了策略开关与"每会话一次"之外，还要求 **AI 兜底接待不可用**：
// AI 已开启时访客并不是无人搭理，再补一条离线话术会与 AI 回复重复；
// 同时这也是为了保持既有会话流转（AI 自主接待）行为与测试不变——冲突时优先保证既有行为，
// 该取舍已在交付说明中记录。
func (s *Service) unattendedEligible(conversation domain.Conversation) bool {
	policy := s.store.ServicePolicy()
	if !policy.UnattendedEnabled || conversation.UnattendedNotifiedAt != nil {
		return false
	}
	settings := s.store.AISettings()
	if settings.Enabled && settings.Mode != domain.AIModeManualOnly {
		return false
	}
	return true
}

// enqueueIfNeeded 在排队开关打开且会话尚未在队列时入队，并向访客推送排队位置。
// 后两个返回值分别是"本次调用后该会话是否已在队列中"（用于判断"未入队成功"要不要兜底）
// 与"本次是否新入队"（用于只在进入队列的那一刻补发接管提示）。
func (s *Service) enqueueIfNeeded(conversation domain.Conversation) (domain.QueuedConversation, bool, bool) {
	policy := s.store.ServicePolicy()
	if !policy.QueueEnabled {
		return domain.QueuedConversation{}, false, false
	}
	for _, existing := range s.store.QueueList() {
		if existing.ConversationID == conversation.ID {
			return existing, true, false
		}
	}
	reason := domain.QueueReasonOffline
	if s.onlineAgentCount() > 0 {
		reason = domain.QueueReasonCapacity
	}
	entry, err := s.store.EnqueueConversation(domain.QueuedConversation{
		ConversationID: conversation.ID,
		GroupName:      strings.TrimSpace(conversation.QueueGroup),
		Priority:       0,
		Reason:         reason,
	})
	if err != nil {
		s.logger.Warn("会话入队失败", "conversation_id", conversation.ID, "error", err)
		return domain.QueuedConversation{}, false, false
	}
	s.pushQueuePosition(entry)
	return entry, true, true
}

// pushQueuePosition 向访客推送"排队中，前方 N 位"。
func (s *Service) pushQueuePosition(entry domain.QueuedConversation) {
	position := 0
	for index, item := range s.store.QueueList() {
		if item.ConversationID == entry.ConversationID {
			position = index + 1
			break
		}
	}
	if position == 0 {
		return
	}
	waitingSeconds := int(time.Since(entry.EnqueuedAt).Seconds())
	if waitingSeconds < 0 {
		waitingSeconds = 0
	}
	data := map[string]any{
		"position":        position,
		"waiting_seconds": waitingSeconds,
	}
	if strings.TrimSpace(entry.GroupName) != "" {
		data["group"] = entry.GroupName
	}
	s.hub.SendToVisitor(entry.ConversationID, realtime.Event{Event: "conversation.queue_position", Data: data})
}

// sendUnattendedNotice 发送一次无人值守兜底话术：
// 文案取策略配置（空则内置中文兜底），按访客语言翻译（翻译失败回退中文原文）；
// 每个会话只发一次，由 conversations.unattended_notified_at 保证（重启后依然成立）。
func (s *Service) sendUnattendedNotice(conversation domain.Conversation) {
	if !s.unattendedEligible(conversation) {
		return
	}
	policy := s.store.ServicePolicy()
	text := strings.TrimSpace(policy.UnattendedReply)
	if text == "" {
		text = domain.DefaultUnattendedReply
	}
	visitorLang := conversationReplyLang(conversation)
	// 复用既有翻译链路：静态映射优先，其次翻译上游；翻译失败（或 AI 已关闭）时回退中文原文。
	var translations map[string]string
	if visitorLang != ai.LangZh {
		if translated, ok := knownSystemText(text, visitorLang); ok {
			translations = map[string]string{visitorLang: translated}
		} else if s.store.AISettings().Enabled {
			translations = s.translateMessage(ai.LangZh, visitorLang, domain.MessageSystem, text)
		}
	}
	msg, updated, err := s.store.AddUnattendedNotice(conversation.ID, text, translations)
	if err != nil {
		if !errors.Is(err, store.ErrConflict) {
			s.logger.Warn("发送无人值守兜底话术失败", "conversation_id", conversation.ID, "error", err)
		}
		return
	}
	s.hub.SendToVisitor(updated.ID, realtime.Event{Event: "message.receive", Data: VisitorFacingMessageForLang(msg, visitorLang)})
	if updated.AssignedAgentID != "" {
		s.hub.SendToAgent(updated.AssignedAgentID, realtime.Event{Event: "message.receive", Data: AgentFacingMessage(msg)})
	}
	s.hub.SendToVisitor(updated.ID, realtime.Event{Event: "conversation.status_changed", Data: updated})
	s.hub.BroadcastAdmins(realtime.Event{Event: "conversation.status_changed", Data: updated})
}

// notifyTakeoverRequest 向"在线且未关闭接管弹窗"的客服推送接管提示。
// waitingSince 为会话创建或入队时间；为便于调用方，零值回退到会话创建时间。
func (s *Service) notifyTakeoverRequest(conversation domain.Conversation, waitingSince time.Time) {
	if !s.store.ServicePolicy().TakeoverPopupEnabled {
		return
	}
	if waitingSince.IsZero() {
		waitingSince = conversation.CreatedAt
	}
	waitingSeconds := int(time.Since(waitingSince).Seconds())
	if waitingSeconds < 0 {
		waitingSeconds = 0
	}
	payload := takeoverRequestPayload{
		ConversationID: conversation.ID,
		VisitorName:    visitorDisplayName(conversation),
		WaitingSeconds: waitingSeconds,
	}
	for _, agent := range s.store.AllAgents() {
		if agent.Status != domain.AgentOnline || agent.DisabledAt != nil {
			continue
		}
		// 客服可自行关闭接管弹窗（默认开启）。
		if !prefBool(s.agentPreferences(agent.ID).TakeoverPopupEnabled) {
			continue
		}
		s.hub.SendToAgent(agent.ID, realtime.Event{Event: "conversation.takeover_request", Data: payload})
	}
}

// onlineAgentCount 返回当前在线且未禁用的客服数（不校验并发与分组）。
func (s *Service) onlineAgentCount() int {
	count := 0
	for _, agent := range s.store.AllAgents() {
		if agent.Status == domain.AgentOnline && agent.DisabledAt == nil {
			count++
		}
	}
	return count
}

// checkBlacklist 检查 visitor | ip | account 三类黑名单，命中即写审计并返回统一文案的错误。
// 返回的 store.BlacklistError 不含被拉黑的值本身，可安全下发给访客。
func (s *Service) checkBlacklist(visitorID, ip, account string) error {
	probes := []struct {
		kind  string
		value string
	}{
		{domain.BlacklistKindVisitor, strings.TrimSpace(visitorID)},
		{domain.BlacklistKindIP, strings.TrimSpace(ip)},
		{domain.BlacklistKindAccount, strings.TrimSpace(account)},
	}
	for _, probe := range probes {
		if probe.value == "" {
			continue
		}
		entry, hit := s.store.MatchBlacklist(probe.kind, probe.value)
		if !hit {
			continue
		}
		s.recordBlacklistBlocked(entry, visitorID, ip)
		return &store.BlacklistError{Kind: entry.Kind, Message: domain.DefaultBlacklistMessage}
	}
	return nil
}

// recordBlacklistBlocked 写 blacklist.blocked 审计：内部审计保留 kind/value 与访客身份，
// 便于风控追溯；响应文案里绝不包含这些值。
func (s *Service) recordBlacklistBlocked(entry domain.BlacklistEntry, visitorID, ip string) {
	s.store.RecordAuditEvent(domain.AuditEvent{
		ActorKind:  domain.AccountVisitor,
		ActorID:    strings.TrimSpace(visitorID),
		Action:     "blacklist.blocked",
		Resource:   "blacklist",
		ResourceID: entry.ID,
		IPAddress:  strings.TrimSpace(ip),
		Description: fmt.Sprintf("黑名单拦截：kind=%s value=%s visitor_id=%s ip=%s",
			entry.Kind, entry.Value, strings.TrimSpace(visitorID), strings.TrimSpace(ip)),
		CreatedAt: time.Now().UTC(),
	})
}

// dispatchAgentAlert 异步把提醒推送到负责客服自配的渠道（telegram/bark/email）。
// 渠道为空时不开 goroutine；发送失败只记日志，绝不影响消息主流程。
func (s *Service) dispatchAgentAlert(agentID, title, body, conversationID string) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return
	}
	channels := s.store.AgentAlertChannels(agentID)
	enabled := make([]domain.AgentAlertChannel, 0, len(channels))
	for _, channel := range channels {
		if channel.Enabled {
			enabled = append(enabled, channel)
		}
	}
	if len(enabled) == 0 {
		return
	}
	if s.notifier == nil {
		return
	}
	payload := notify.AlertPayload{
		Title:          title,
		Body:           strings.TrimSpace(body),
		ConversationID: conversationID,
		LinkHint:       s.alertLinkHint,
	}
	go func() {
		// 提醒渠道是旁路能力：任何 panic/失败都不得影响消息主流程。
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Warn("提醒渠道派发 panic", "agent_id", agentID, "error", recovered)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), alertDispatchTimeout)
		defer cancel()
		if err := s.notifier.SendAlertChannels(ctx, enabled, payload); err != nil {
			s.logger.Warn("提醒渠道派发失败", "agent_id", agentID, "error", err)
		}
	}()
}

// alertDispatchTimeout 是单次提醒渠道派发（含重试）的外层超时。
const alertDispatchTimeout = 30 * time.Second

// visitorDisplayName 返回提醒/弹窗里展示的访客名：备注优先，为空回退「访客」。
func visitorDisplayName(conversation domain.Conversation) string {
	name := strings.TrimSpace(conversation.VisitorRemark)
	if name == "" {
		return "访客"
	}
	return name
}

// alertPreview 截取消息摘要的前 80 个字符（按 rune，避免截断多字节字符）。
func alertPreview(text string) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= 80 {
		return text
	}
	return string(runes[:80]) + "…"
}

// conversationBlacklistAccount 推断会话对应的"账号"身份：
// 开放 API 会把外部用户 ID 写进 visitor_remark；访客备注为空或等于 IP 时视为未提供账号。
func conversationBlacklistAccount(conversation domain.Conversation) string {
	remark := strings.TrimSpace(conversation.VisitorRemark)
	if remark == "" || remark == strings.TrimSpace(conversation.VisitorIP) {
		return ""
	}
	return remark
}
