package app

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

// ===== 批次 4 第三波（2b）：SLA 计时器 / 会话超时自动关闭 / AI 关键词优先级接线 =====
//
// RunPolicySweep 是确定性的巡检入口（测试直接以固定 now 调用），生产由
// StartPolicyLoop 的 ticker 周期触发；巡检只在"状态发生变化"时推送、派发渠道、写审计与记日志。

const (
	// slaKindFirstResponse / slaKindIdle 是 conversation.sla 事件的 kind 取值。
	slaKindFirstResponse = "first_response"
	slaKindIdle          = "idle"

	// policySweepActorID 是巡检写审计时的操作者标识。
	// audit_events.actor_kind 有 CHECK 约束（只允许 admin/agent/visitor），
	// 因此系统巡检统一以 admin 身份记录，并用 ActorID 标明来源。
	policySweepActorID = "policy_sweep"

	// DefaultPolicyTickSeconds / MinPolicyTickSeconds 是巡检间隔的默认值与最小值（秒）。
	DefaultPolicyTickSeconds = 30
	MinPolicyTickSeconds     = 5

	// policyTickEnvKey 是巡检间隔的环境变量名。
	policyTickEnvKey = "SERVICE_POLICY_TICK_SECONDS"
)

// PolicyTickInterval 解析巡检间隔：默认 30 秒、非法值回落默认、最小 5 秒（避免巡检打爆数据库）。
func PolicyTickInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv(policyTickEnvKey))
	if raw == "" {
		return DefaultPolicyTickSeconds * time.Second
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return DefaultPolicyTickSeconds * time.Second
	}
	if seconds < MinPolicyTickSeconds {
		seconds = MinPolicyTickSeconds
	}
	return time.Duration(seconds) * time.Second
}

// conversationSLAPayload 是推送给负责客服的 SLA 告警事件体。
type conversationSLAPayload struct {
	ConversationID string `json:"conversation_id"`
	Kind           string `json:"kind"`
	Minutes        int    `json:"minutes"`
}

// policyLoopRunner 管理巡检 ticker 的生命周期：StopPolicyLoop 会等待 goroutine 真正退出。
type policyLoopRunner struct {
	mu   sync.Mutex
	stop chan struct{}
	done chan struct{}
}

// StartPolicyLoop 启动后台 SLA 巡检：每 interval 执行一次 RunPolicySweep。
// interval <= 0 时回落到默认 30 秒；重复调用是幂等的（不会启动第二个 ticker）。
func (s *Service) StartPolicyLoop(interval time.Duration) {
	if s == nil || s.policyLoop == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultPolicyTickSeconds * time.Second
	}
	runner := s.policyLoop
	runner.mu.Lock()
	if runner.stop != nil {
		// 已在运行：保持单实例，避免重复巡检。
		runner.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	runner.stop, runner.done = stop, done
	runner.mu.Unlock()

	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-ticker.C:
				s.RunPolicySweep(now)
			}
		}
	}()
}

// StopPolicyLoop 优雅停止巡检并等待 goroutine 退出；未启动或重复调用都是安全的。
func (s *Service) StopPolicyLoop() {
	if s == nil || s.policyLoop == nil {
		return
	}
	runner := s.policyLoop
	runner.mu.Lock()
	stop, done := runner.stop, runner.done
	runner.stop, runner.done = nil, nil
	runner.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-done
}

// RunPolicySweep 执行一次客服策略巡检，内部完成 2a–2c：
//   - 2a 首响超时：提醒负责客服（弹窗事件 + 提醒渠道 + 审计 conversation.sla_first_response）；
//   - 2b 待回复超时：提醒后按 sla_idle_action 交回 AI（conversation.sla_idle.ai）或转接/入队
//     （conversation.sla_idle.reassign）；
//   - 2c 静默自动关闭：走既有 CloseConversation 流程并写审计 conversation.auto_closed。
//
// 幂等：同一会话的同一超时只告警一次，用 conversations.sla_state 记录 first_response / idle；
// 客服真实回复或接管时会清空该标记，重新开始计时。
// 遍历范围：所有未结束的会话（量小，直接用 AllConversations）；首响与空闲 SLA 仅处理已分配会话。
func (s *Service) RunPolicySweep(now time.Time) {
	now = now.UTC()
	s.purgeExpiredVisitorProfiles(now)
	policy := s.store.ServicePolicy()
	for _, conversation := range s.store.AllConversations() {
		if conversation.Status == domain.ConversationClosed {
			continue
		}
		current := conversation
		// 首响提醒和客服空闲回退要求已有负责客服；静默自动关闭适用于所有
		// 未结束会话，包括等待分配、无人在线创建的会话。
		if strings.TrimSpace(current.AssignedAgentID) != "" {
			if updated, acted := s.sweepFirstResponse(policy, current, now); acted {
				current = updated
			}
			if updated, acted := s.sweepIdle(policy, current, now); acted {
				current = updated
			}
		}
		s.sweepAutoClose(policy, current, now)
	}
}

// purgeExpiredVisitorProfiles 按「访客画像保留天数」清理超期画像（0=永久保留）。
// 放在巡检里而不是单独定时器：保留天数本身就是一项后台配置，改完最迟一个巡检周期生效。
// 清理失败只记日志，绝不影响 SLA 巡检本身。
func (s *Service) purgeExpiredVisitorProfiles(now time.Time) {
	settings := s.store.VisitorProfileSettings()
	if settings.RetentionDays <= 0 {
		return
	}
	cutoff := now.AddDate(0, 0, -settings.RetentionDays)
	removed, err := s.store.PurgeVisitorProfilesBefore(cutoff)
	if err != nil {
		s.logger.Warn("访客画像过期清理失败", "retention_days", settings.RetentionDays, "error", err)
		return
	}
	if removed > 0 {
		s.logger.Info("访客画像过期清理完成", "removed", removed, "retention_days", settings.RetentionDays, "cutoff", cutoff.Format(time.RFC3339))
	}
}

// sweepFirstResponse 处理首响超时（2a）：已分配、尚无人工首响且超过阈值时提醒负责客服。
func (s *Service) sweepFirstResponse(policy domain.ServicePolicy, conversation domain.Conversation, now time.Time) (domain.Conversation, bool) {
	if policy.SLAFirstResponseSeconds <= 0 {
		return conversation, false
	}
	// 幂等：已有人工首响或已有 SLA 标记时不再告警；客服回复/接管会清空标记。
	if conversation.FirstResponseAt != nil || conversation.SLAState != "" {
		return conversation, false
	}
	base := firstResponseBase(conversation, lastVisitorMessageAt(s.store.Messages(conversation.ID)))
	elapsed := now.Sub(base)
	if elapsed <= time.Duration(policy.SLAFirstResponseSeconds)*time.Second {
		return conversation, false
	}
	updated, err := s.store.SetConversationSLAState(conversation.ID, domain.ConversationSLAStateFirstResponse)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.logger.Warn("SLA 首响状态写入失败", "conversation_id", conversation.ID, "error", err)
		}
		return conversation, false
	}
	minutes := elapsedMinutes(elapsed)
	s.hub.SendToAgent(updated.AssignedAgentID, realtime.Event{Event: "conversation.sla", Data: conversationSLAPayload{
		ConversationID: updated.ID,
		Kind:           slaKindFirstResponse,
		Minutes:        minutes,
	}})
	s.dispatchAgentAlert(updated.AssignedAgentID, "首响超时",
		fmt.Sprintf("访客「%s」的会话已等待 %d 分钟仍无人工首次回复。", visitorDisplayName(updated), minutes), updated.ID)
	s.recordPolicyAudit("conversation.sla_first_response", updated,
		fmt.Sprintf("首响超时：已等待 %d 分钟（阈值 %d 秒）", minutes, policy.SLAFirstResponseSeconds))
	s.logger.Info("SLA 首响超时", "conversation_id", updated.ID, "agent_id", updated.AssignedAgentID, "minutes", minutes)
	return updated, true
}

// sweepIdle 处理待回复超时（2b）：最后一条访客消息超过 sla_idle_minutes 仍无人工回复时，
// 先告警（幂等，sla_state=idle），再按 sla_idle_action 交回 AI 或转接其他客服。
func (s *Service) sweepIdle(policy domain.ServicePolicy, conversation domain.Conversation, now time.Time) (domain.Conversation, bool) {
	if policy.SLAIdleMinutes <= 0 {
		return conversation, false
	}
	if conversation.SLAState == domain.ConversationSLAStateIdle {
		// 同一会话的待回复超时只告警/回退一次；客服回复或接管后重新计时。
		return conversation, false
	}
	visitorAt := lastVisitorMessageAt(s.store.Messages(conversation.ID))
	if visitorAt == nil {
		return conversation, false
	}
	// 客服已在访客最后一条消息之后回复过 → 不算"待回复"。
	if conversation.LastAgentReplyAt != nil && !visitorAt.After(*conversation.LastAgentReplyAt) {
		return conversation, false
	}
	waited := now.Sub(*visitorAt)
	if waited <= time.Duration(policy.SLAIdleMinutes)*time.Minute {
		return conversation, false
	}
	updated, err := s.store.SetConversationSLAState(conversation.ID, domain.ConversationSLAStateIdle)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.logger.Warn("SLA 待回复状态写入失败", "conversation_id", conversation.ID, "error", err)
		}
		return conversation, false
	}
	minutes := elapsedMinutes(waited)
	s.hub.SendToAgent(updated.AssignedAgentID, realtime.Event{Event: "conversation.sla", Data: conversationSLAPayload{
		ConversationID: updated.ID,
		Kind:           slaKindIdle,
		Minutes:        minutes,
	}})
	s.dispatchAgentAlert(updated.AssignedAgentID, "待回复超时",
		fmt.Sprintf("访客「%s」已等待 %d 分钟未收到人工回复。", visitorDisplayName(updated), minutes), updated.ID)
	s.recordPolicyAudit("conversation.sla_idle", updated,
		fmt.Sprintf("待回复超时：访客已等待 %d 分钟（阈值 %d 分钟）", minutes, policy.SLAIdleMinutes))
	s.logger.Info("SLA 待回复超时", "conversation_id", updated.ID, "agent_id", updated.AssignedAgentID, "minutes", minutes)

	if domain.NormalizeSLAIdleAction(policy.SLAIdleAction) == domain.SLAIdleActionReassign {
		s.slaReassignIdleConversation(updated, minutes)
	} else {
		s.slaReturnIdleConversationToAI(updated, minutes)
	}
	// 动作可能改变状态/分配，回读一次让后续 2c 基于最新会话判断。
	if latest, ok := s.store.Conversation(updated.ID); ok {
		return latest, true
	}
	return updated, true
}

// slaReturnIdleConversationToAI 执行 sla_idle_action=ai：把会话交回 AI 接待（保留负责客服便于追溯）。
func (s *Service) slaReturnIdleConversationToAI(conversation domain.Conversation, minutes int) {
	updated, err := s.store.ReturnConversationToAI(conversation.ID)
	if err != nil {
		s.logger.Warn("SLA 空闲回退 AI 失败", "conversation_id", conversation.ID, "error", err)
		return
	}
	s.hub.SendToAgent(updated.AssignedAgentID, realtime.Event{Event: "conversation.status_changed", Data: updated})
	s.hub.SendToVisitor(updated.ID, realtime.Event{Event: "conversation.status_changed", Data: updated})
	s.hub.BroadcastAdmins(realtime.Event{Event: "conversation.status_changed", Data: updated})
	s.recordPolicyAudit("conversation.sla_idle.ai", updated,
		fmt.Sprintf("待回复超时交回 AI 接待：访客已等待 %d 分钟", minutes))
	s.logger.Info("SLA 空闲回退到 AI 接待", "conversation_id", updated.ID, "agent_id", updated.AssignedAgentID)
}

// slaReassignIdleConversation 执行 sla_idle_action=reassign：优先同分组转给其他在线且未达并发的客服；
// 没有候选（或目标客服在竞态中被占满）时释放当前客服并重新入队。
func (s *Service) slaReassignIdleConversation(conversation domain.Conversation, minutes int) {
	target, ok := s.selectReassignTarget(conversation.QueueGroup, conversation.AssignedAgentID)
	if !ok {
		s.slaReleaseIdleConversationToQueue(conversation, minutes, "没有其他可转接的在线客服")
		return
	}
	updated, err := s.store.ReassignConversation(conversation.ID, target.ID)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			// 竞态：目标客服刚被占满/离线 → 回落为释放并重新入队。
			s.slaReleaseIdleConversationToQueue(conversation, minutes, "目标客服已不可用")
			return
		}
		s.logger.Warn("SLA 空闲转接失败", "conversation_id", conversation.ID, "target_agent_id", target.ID, "error", err)
		return
	}
	s.hub.SendToAgent(updated.AssignedAgentID, realtime.Event{Event: "conversation.assigned", Data: updated})
	s.hub.SendToAgent(updated.AssignedAgentID, realtime.Event{Event: "conversation.status_changed", Data: updated})
	s.hub.SendToVisitor(updated.ID, realtime.Event{Event: "conversation.status_changed", Data: updated})
	s.hub.BroadcastAdmins(realtime.Event{Event: "conversation.status_changed", Data: updated})
	s.notifyAgent(updated)
	s.dispatchAgentAlert(updated.AssignedAgentID, "会话转接",
		fmt.Sprintf("访客「%s」的会话因待回复超时转接到您名下。", visitorDisplayName(updated)), updated.ID)
	s.recordPolicyAudit("conversation.sla_idle.reassign", updated,
		fmt.Sprintf("待回复超时转接：访客已等待 %d 分钟，已转给客服「%s」", minutes, updated.AssignedAgentName))
	s.logger.Info("SLA 空闲转接", "conversation_id", updated.ID, "target_agent_id", updated.AssignedAgentID)
}

// slaReleaseIdleConversationToQueue 释放当前客服并把会话重新入队（没有可转接候选时的兜底）。
// 复用既有入队与排队位置推送；此处刻意不调用 drainQueue——刚刚释放的并发会让原客服重新成为
// 唯一候选，立即出队等于把会话原样分回原客服，违背"转给其他客服"的语义（队列由客服上线、
// 会话结束、转接等既有触发点出队）。
func (s *Service) slaReleaseIdleConversationToQueue(conversation domain.Conversation, minutes int, reason string) {
	released, err := s.store.ReassignConversation(conversation.ID, "")
	if err != nil {
		s.logger.Warn("SLA 空闲释放会话失败", "conversation_id", conversation.ID, "error", err)
		return
	}
	s.hub.SendToVisitor(released.ID, realtime.Event{Event: "conversation.status_changed", Data: released})
	s.hub.BroadcastAdmins(realtime.Event{Event: "conversation.status_changed", Data: released})
	s.handleUnassignedConversation(released, false)
	s.recordPolicyAudit("conversation.sla_idle.reassign", released,
		fmt.Sprintf("待回复超时转接：访客已等待 %d 分钟，%s，已重新排队", minutes, reason))
	s.logger.Info("SLA 空闲回退排队", "conversation_id", released.ID, "reason", reason)
}

// sweepAutoClose 处理会话超时自动关闭（2c）：auto_close_idle_minutes>0 且最近活动距今超过该值时，
// 走既有 CloseConversation 流程（访客能收到"会话已结束"与评价入口），并写审计。
// 已结束的会话在 RunPolicySweep 入口被跳过，不会重复执行。
func (s *Service) sweepAutoClose(policy domain.ServicePolicy, conversation domain.Conversation, now time.Time) {
	if policy.AutoCloseIdleMinutes <= 0 || conversation.Status == domain.ConversationClosed {
		return
	}
	lastActivity := conversation.CreatedAt
	if conversation.LastMessageAt != nil && conversation.LastMessageAt.After(lastActivity) {
		lastActivity = *conversation.LastMessageAt
	}
	if now.Sub(lastActivity) <= time.Duration(policy.AutoCloseIdleMinutes)*time.Minute {
		return
	}
	if _, err := s.CloseConversation(domain.AuthSession{Kind: domain.AccountAdmin, AccountID: policySweepActorID}, conversation.ID); err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.logger.Warn("会话自动关闭失败", "conversation_id", conversation.ID, "error", err)
		}
		return
	}
	s.recordPolicyAudit("conversation.auto_closed", conversation,
		fmt.Sprintf("双方静默超过 %d 分钟，已自动结束会话", policy.AutoCloseIdleMinutes))
	s.logger.Info("会话超时自动关闭", "conversation_id", conversation.ID, "idle_minutes", policy.AutoCloseIdleMinutes)
}

// recordPolicyAudit 记录巡检产生的审计事件。
func (s *Service) recordPolicyAudit(action string, conversation domain.Conversation, description string) {
	s.store.RecordAuditEvent(domain.AuditEvent{
		ActorKind:   domain.AccountAdmin,
		ActorID:     policySweepActorID,
		Action:      action,
		Resource:    "conversation",
		ResourceID:  conversation.ID,
		Description: description,
		CreatedAt:   time.Now().UTC(),
	})
}

// firstResponseBase 计算首响计时起点：分配时间（近似取会话创建时间）与最后一条访客消息时间的较晚者。
// 库中没有独立的"分配时间"列；接管会重置 first_response_at 并清空 sla_state，从而重新开始计时。
func firstResponseBase(conversation domain.Conversation, visitorAt *time.Time) time.Time {
	base := conversation.CreatedAt
	if visitorAt != nil && visitorAt.After(base) {
		base = *visitorAt
	}
	return base
}

// lastVisitorMessageAt 返回最后一条访客消息的时间；没有访客消息时返回 nil。
// 待回复判定以消息时间为基准，不新增列。
func lastVisitorMessageAt(messages []domain.Message) *time.Time {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].SenderType != domain.SenderVisitor {
			continue
		}
		at := messages[index].CreatedAt
		return &at
	}
	return nil
}

// elapsedMinutes 把等待时长换算成分钟数（向上取整到至少 1 分钟），用于告警文案与事件体。
func elapsedMinutes(elapsed time.Duration) int {
	if elapsed <= 0 {
		return 0
	}
	minutes := int(elapsed / time.Minute)
	if minutes < 1 {
		minutes = 1
	}
	return minutes
}
