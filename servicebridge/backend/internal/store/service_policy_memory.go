package store

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"customer-service/backend/internal/domain"
)

// ===== 客服策略 / 排队 / 黑名单 / 提醒渠道 / SMTP 的内存实现 =====
// 本批次只提供存储与接口能力，不接线 SLA 计时器、自动关闭、优先级切换等状态机。

// ServicePolicy 返回内存中的客服策略（启动时已填充默认值）。
func (s *MemoryStore) ServicePolicy() domain.ServicePolicy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.servicePolicy
}

// UpdateServicePolicy 覆盖保存客服策略；归一化在存储层统一完成。
func (s *MemoryStore) UpdateServicePolicy(next domain.ServicePolicy) (domain.ServicePolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized := next.Normalize()
	now := time.Now().UTC()
	normalized.UpdatedAt = &now
	s.servicePolicy = normalized
	return s.servicePolicy, nil
}

// QueueList 返回排队列表：按优先级降序、入队时间升序（同优先级先到先服务）。
func (s *MemoryStore) QueueList() []domain.QueuedConversation {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]domain.QueuedConversation, 0, len(s.queue))
	for _, entry := range s.queue {
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		if !out[i].EnqueuedAt.Equal(out[j].EnqueuedAt) {
			return out[i].EnqueuedAt.Before(out[j].EnqueuedAt)
		}
		return out[i].ConversationID < out[j].ConversationID
	})
	return out
}

// EnqueueConversation 写入排队队列；重复入队时更新分组/优先级/原因但保留原入队时间，
// 避免反复入队把会话挤到队尾。
func (s *MemoryStore) EnqueueConversation(entry domain.QueuedConversation) (domain.QueuedConversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	conversationID := strings.TrimSpace(entry.ConversationID)
	if conversationID == "" {
		return domain.QueuedConversation{}, fmt.Errorf("%w: conversation_id 不能为空", ErrInvalidInput)
	}
	if _, ok := s.conversations[conversationID]; !ok {
		return domain.QueuedConversation{}, ErrNotFound
	}
	groupName := strings.TrimSpace(entry.GroupName)
	reason := domain.NormalizeQueueReason(entry.Reason)
	if existing, ok := s.queue[conversationID]; ok {
		existing.GroupName = groupName
		existing.Priority = entry.Priority
		existing.Reason = reason
		s.queue[conversationID] = existing
		return existing, nil
	}
	entry.ConversationID = conversationID
	entry.GroupName = groupName
	entry.Reason = reason
	entry.EnqueuedAt = time.Now().UTC()
	s.queue[conversationID] = entry
	return entry, nil
}

// DequeueConversation 把会话移出排队队列。
func (s *MemoryStore) DequeueConversation(conversationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	conversationID = strings.TrimSpace(conversationID)
	if _, ok := s.queue[conversationID]; !ok {
		return ErrNotFound
	}
	delete(s.queue, conversationID)
	return nil
}

// BlacklistList 返回黑名单：kind 为空表示全部；按创建时间倒序。
func (s *MemoryStore) BlacklistList(kind string) []domain.BlacklistEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	kind = strings.ToLower(strings.TrimSpace(kind))
	out := make([]domain.BlacklistEntry, 0, len(s.blacklist))
	for _, entry := range s.blacklist {
		if kind != "" && entry.Kind != kind {
			continue
		}
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	return out
}

// AddBlacklist 新增黑名单项；(kind,value) 重复时返回 ErrConflict（过期项也需先删除再重建）。
func (s *MemoryStore) AddBlacklist(entry domain.BlacklistEntry) (domain.BlacklistEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized, err := normalizeBlacklistEntry(entry)
	if err != nil {
		return domain.BlacklistEntry{}, err
	}
	for _, existing := range s.blacklist {
		if existing.Kind == normalized.Kind && existing.Value == normalized.Value {
			return domain.BlacklistEntry{}, fmt.Errorf("%w: 该黑名单项已存在", ErrConflict)
		}
	}
	normalized.ID = "bl_" + randomID(8)
	normalized.CreatedAt = time.Now().UTC()
	if normalized.ExpiresAt != nil {
		value := normalized.ExpiresAt.UTC()
		normalized.ExpiresAt = &value
	}
	s.blacklist[normalized.ID] = normalized
	return normalized, nil
}

// DeleteBlacklist 删除黑名单项。
func (s *MemoryStore) DeleteBlacklist(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	id = strings.TrimSpace(id)
	if _, ok := s.blacklist[id]; !ok {
		return ErrNotFound
	}
	delete(s.blacklist, id)
	return nil
}

// MatchBlacklist 判断是否命中有效黑名单；命中过期项视为未命中，并顺手删除过期条目。
func (s *MemoryStore) MatchBlacklist(kind, value string) (domain.BlacklistEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized, err := normalizeBlacklistEntry(domain.BlacklistEntry{Kind: kind, Value: value})
	if err != nil {
		return domain.BlacklistEntry{}, false
	}
	now := time.Now().UTC()
	for id, entry := range s.blacklist {
		if entry.Kind != normalized.Kind || entry.Value != normalized.Value {
			continue
		}
		if entry.Expired(now) {
			delete(s.blacklist, id)
			return domain.BlacklistEntry{}, false
		}
		return entry, true
	}
	return domain.BlacklistEntry{}, false
}

// TakeoverConversation 客服正式接管会话（批次 4 第二波）。
// 语义：分配给该客服 + 状态置 assigned + 清空 sla_state + first_response_at 置空；
// 会话已被他人负责或已结束返回 ErrForbidden，重复接管同一会话幂等（不会重复计并发）。
func (s *MemoryStore) TakeoverConversation(agentID, conversationID string) (domain.Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	agentID = strings.TrimSpace(agentID)
	conversation, ok := s.conversations[strings.TrimSpace(conversationID)]
	if !ok {
		return domain.Conversation{}, ErrNotFound
	}
	if conversation.Status == domain.ConversationClosed {
		return domain.Conversation{}, ErrForbidden
	}
	if conversation.AssignedAgentID != "" && conversation.AssignedAgentID != agentID {
		// 已被他人负责：不允许抢占，交由管理员转接。
		return domain.Conversation{}, ErrForbidden
	}
	agent, ok := s.agentsByID[agentID]
	if !ok || agent.DisabledAt != nil {
		return domain.Conversation{}, ErrForbidden
	}
	now := time.Now().UTC()
	if conversation.AssignedAgentID != agentID {
		agent.CurrentConversations++
		conversation.AssignedAgentID = agentID
	}
	agent.Updated = now
	conversation.Status = domain.ConversationAssigned
	conversation.SLAState = ""
	conversation.FirstResponseAt = nil
	conversation.UpdatedAt = now
	s.fillAssignedAgentLocked(conversation)
	// 接管后不必再排队等待分配。
	delete(s.queue, conversation.ID)
	return *conversation, nil
}

// AssignConversationToAgent 静默分配（排队出队时使用）：只写 assigned_agent_id，
// 不改会话状态（保持无痕转人工语义），同时把会话移出排队队列。
func (s *MemoryStore) AssignConversationToAgent(conversationID, agentID string) (domain.Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	conversation, ok := s.conversations[strings.TrimSpace(conversationID)]
	if !ok {
		return domain.Conversation{}, ErrNotFound
	}
	agentID = strings.TrimSpace(agentID)
	if conversation.Status == domain.ConversationClosed {
		return domain.Conversation{}, ErrConflict
	}
	if conversation.AssignedAgentID == agentID {
		delete(s.queue, conversation.ID)
		return *conversation, nil
	}
	if conversation.AssignedAgentID != "" {
		return domain.Conversation{}, ErrConflict
	}
	agent, ok := s.agentsByID[agentID]
	if !ok || agent.DisabledAt != nil || agent.Status != domain.AgentOnline {
		return domain.Conversation{}, ErrConflict
	}
	if agent.CurrentConversations >= agent.MaxConversations {
		return domain.Conversation{}, ErrConflict
	}
	now := time.Now().UTC()
	agent.CurrentConversations++
	agent.Updated = now
	conversation.AssignedAgentID = agentID
	conversation.UpdatedAt = now
	s.fillAssignedAgentLocked(conversation)
	delete(s.queue, conversation.ID)
	return *conversation, nil
}

// SetConversationSLAState 只更新会话的 sla_state 标记，供 SLA 巡检做幂等记录。
func (s *MemoryStore) SetConversationSLAState(conversationID, state string) (domain.Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	conversation, ok := s.conversations[strings.TrimSpace(conversationID)]
	if !ok {
		return domain.Conversation{}, ErrNotFound
	}
	conversation.SLAState = strings.TrimSpace(state)
	conversation.UpdatedAt = time.Now().UTC()
	return *conversation, nil
}

// ReturnConversationToAI 把会话交回 AI 接待：状态置 ai_serving，保留负责客服便于追溯。
// 刻意不清空 sla_state：它是"本会话本状态已告警"的幂等标记，客服真实回复/接管时才会清空。
func (s *MemoryStore) ReturnConversationToAI(conversationID string) (domain.Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	conversation, ok := s.conversations[strings.TrimSpace(conversationID)]
	if !ok {
		return domain.Conversation{}, ErrNotFound
	}
	if conversation.Status == domain.ConversationClosed {
		return domain.Conversation{}, ErrForbidden
	}
	conversation.Status = domain.ConversationAIServing
	conversation.UpdatedAt = time.Now().UTC()
	return *conversation, nil
}

// ReassignConversation 是 sla_idle_action=reassign 的存储能力：
// targetAgentID 非空时转给该客服（不改变会话状态）；为空时释放当前客服并把会话置为
// 未分配的 human_requested，交由应用层重新入队。
func (s *MemoryStore) ReassignConversation(conversationID, targetAgentID string) (domain.Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	conversation, ok := s.conversations[strings.TrimSpace(conversationID)]
	if !ok {
		return domain.Conversation{}, ErrNotFound
	}
	if conversation.Status == domain.ConversationClosed {
		return domain.Conversation{}, ErrConflict
	}
	targetAgentID = strings.TrimSpace(targetAgentID)
	now := time.Now().UTC()
	if targetAgentID == "" {
		// 释放：把并发还给原客服，会话回到"等待人工"，由应用层入队。
		if old := s.agentsByID[conversation.AssignedAgentID]; old != nil && old.CurrentConversations > 0 {
			old.CurrentConversations--
			old.Updated = now
		}
		if conversation.Status == domain.ConversationAssigned {
			conversation.Status = domain.ConversationHumanRequested
		}
		conversation.AssignedAgentID = ""
		s.fillAssignedAgentLocked(conversation)
		conversation.UpdatedAt = now
		return *conversation, nil
	}
	if conversation.AssignedAgentID == targetAgentID {
		// 幂等：已经在目标客服名下。
		return *conversation, nil
	}
	target, ok := s.agentsByID[targetAgentID]
	if !ok || target.DisabledAt != nil || target.Status != domain.AgentOnline {
		return domain.Conversation{}, ErrConflict
	}
	if target.CurrentConversations >= target.MaxConversations {
		return domain.Conversation{}, ErrConflict
	}
	if old := s.agentsByID[conversation.AssignedAgentID]; old != nil && old.CurrentConversations > 0 {
		old.CurrentConversations--
		old.Updated = now
	}
	target.CurrentConversations++
	target.Updated = now
	conversation.AssignedAgentID = targetAgentID
	s.fillAssignedAgentLocked(conversation)
	conversation.UpdatedAt = now
	return *conversation, nil
}

// AddUnattendedNotice 落库无人值守兜底话术并记录发送时间；已发送过返回 ErrConflict。
// 这里刻意不调整会话已读计数：兜底话术是系统提示，不应替客服"已读"访客消息。
func (s *MemoryStore) AddUnattendedNotice(conversationID, content string, translations map[string]string) (domain.Message, domain.Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	conversation, ok := s.conversations[strings.TrimSpace(conversationID)]
	if !ok {
		return domain.Message{}, domain.Conversation{}, ErrNotFound
	}
	if conversation.Status == domain.ConversationClosed {
		return domain.Message{}, domain.Conversation{}, ErrForbidden
	}
	if conversation.UnattendedNotifiedAt != nil {
		return domain.Message{}, domain.Conversation{}, ErrConflict
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return domain.Message{}, domain.Conversation{}, fmt.Errorf("%w: 无人值守兜底话术不能为空", ErrInvalidInput)
	}
	msg := s.createMessageLocked(conversation.ID, "", domain.SenderSystem, "system", domain.MessageSystem, content, translations)
	now := msg.CreatedAt
	conversation.UnattendedNotifiedAt = &now
	conversation.LastMessage = content
	conversation.LastMessageAt = &now
	conversation.UpdatedAt = now
	return msg, *conversation, nil
}

// AgentAlertChannels 返回某客服的提醒渠道（按创建时间升序）；ConfigMasked 为接口展示用掩码。
func (s *MemoryStore) AgentAlertChannels(agentID string) []domain.AgentAlertChannel {
	s.mu.RLock()
	defer s.mu.RUnlock()

	agentID = strings.TrimSpace(agentID)
	out := make([]domain.AgentAlertChannel, 0, len(s.alertChannels))
	for _, channel := range s.alertChannels {
		if channel.AgentID != agentID {
			continue
		}
		out = append(out, cloneAgentAlertChannel(*channel))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// AgentAlertChannel 按 ID 读取单个提醒渠道（Config 为明文）。
func (s *MemoryStore) AgentAlertChannel(id string) (domain.AgentAlertChannel, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	channel, ok := s.alertChannels[strings.TrimSpace(id)]
	if !ok {
		return domain.AgentAlertChannel{}, false
	}
	return cloneAgentAlertChannel(*channel), true
}

// CreateAgentAlertChannel 新增提醒渠道；每个客服最多 3 个。
func (s *MemoryStore) CreateAgentAlertChannel(agentID, kind string, config map[string]string) (domain.AgentAlertChannel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	agentID, kind, normalizedConfig, err := normalizeAlertChannelCreate(agentID, kind, config)
	if err != nil {
		return domain.AgentAlertChannel{}, err
	}
	if _, ok := s.agentsByID[agentID]; !ok {
		return domain.AgentAlertChannel{}, ErrNotFound
	}
	count := 0
	for _, channel := range s.alertChannels {
		if channel.AgentID == agentID {
			count++
		}
	}
	if count >= domain.AlertChannelMaxPerAgent {
		return domain.AgentAlertChannel{}, alertChannelLimitError()
	}
	now := time.Now().UTC()
	channel := &domain.AgentAlertChannel{
		ID:        "alc_" + randomID(8),
		AgentID:   agentID,
		Kind:      kind,
		Config:    normalizedConfig,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	// 内存存储与既有 ai_settings 一致，明文只保存在进程内存；PostgreSQL 实现负责加密入库。
	s.alertChannels[channel.ID] = channel
	return cloneAgentAlertChannel(*channel), nil
}

// UpdateAgentAlertChannel 局部更新渠道；config==nil 表示配置不变，
// 否则按合并语义处理：未出现的键保留原值、显式空串删除、其余覆盖。
func (s *MemoryStore) UpdateAgentAlertChannel(id string, patch domain.AgentAlertChannelPatch, config *map[string]string) (domain.AgentAlertChannel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	channel, ok := s.alertChannels[strings.TrimSpace(id)]
	if !ok {
		return domain.AgentAlertChannel{}, ErrNotFound
	}
	if patch.Enabled != nil {
		channel.Enabled = *patch.Enabled
	}
	if config != nil {
		merged, err := mergeAlertChannelConfig(channel.Kind, channel.Config, *config)
		if err != nil {
			return domain.AgentAlertChannel{}, fmt.Errorf("%w: %s", ErrInvalidInput, err)
		}
		channel.Config = merged
	}
	channel.UpdatedAt = time.Now().UTC()
	return cloneAgentAlertChannel(*channel), nil
}

// DeleteAgentAlertChannel 删除渠道。
func (s *MemoryStore) DeleteAgentAlertChannel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	id = strings.TrimSpace(id)
	if _, ok := s.alertChannels[id]; !ok {
		return ErrNotFound
	}
	delete(s.alertChannels, id)
	return nil
}

// AlertSMTPSettings 返回全局 SMTP 设置；Password 为明文（json:"-"），PasswordMasked 供接口返回。
func (s *MemoryStore) AlertSMTPSettings() domain.AlertSMTPSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.alertSMTP.WithMask()
}

// UpdateAlertSMTPSettings 覆盖保存 SMTP 设置；password==nil 不改，"" 清除，其它覆盖。
func (s *MemoryStore) UpdateAlertSMTPSettings(next domain.AlertSMTPSettings, password *string) (domain.AlertSMTPSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized, err := next.Normalize()
	if err != nil {
		return domain.AlertSMTPSettings{}, fmt.Errorf("%w: %s", ErrInvalidInput, err)
	}
	// next.Password 不由调用方控制：密码只能通过 password 参数修改。
	normalized.Password = s.alertSMTP.Password
	if password != nil {
		normalized.Password = *password
	}
	now := time.Now().UTC()
	normalized.UpdatedAt = &now
	s.alertSMTP = normalized
	return s.alertSMTP.WithMask(), nil
}

// cloneAgentAlertChannel 复制渠道并重算掩码，避免调用方修改内部 map。
func cloneAgentAlertChannel(channel domain.AgentAlertChannel) domain.AgentAlertChannel {
	out := channel
	out.Config = copyStringMap(channel.Config)
	out.ConfigMasked = domain.MaskAlertChannelConfig(channel.Kind, out.Config)
	out.Masked = domain.MaskAlertChannelPrimary(channel.Kind, out.Config)
	return out
}
