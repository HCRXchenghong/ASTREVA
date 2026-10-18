package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"customer-service/backend/internal/domain"
)

// ===== 客服策略 / 排队 / 黑名单 / 提醒渠道 / SMTP 的 PostgreSQL 实现 =====
// 建表语句见 postgres.go 的 ensureCoreTables 与 migrations/006_service_policy.sql。
// 本批次只提供存储与接口能力，不接线 SLA 计时器、自动关闭、优先级切换等状态机。

const (
	servicePolicyColumns = `queue_enabled, max_wait_seconds, sla_first_response_seconds, sla_idle_minutes,
		sla_idle_action, auto_close_idle_minutes, ai_keyword_priority, unattended_enabled, unattended_reply,
		takeover_popup_enabled, updated_at`
	queuedConversationColumns = `conversation_id, group_name, priority, enqueued_at, reason`
	blacklistColumns          = `id, kind, value, reason, created_by, expires_at, created_at`
	alertChannelColumns       = `id, agent_id, kind, config_ciphertext, enabled, created_at, updated_at`
	alertSMTPColumns          = `host, port, username, password_ciphertext, from_address, use_tls, updated_at`
)

// scanServicePolicy 按 servicePolicyColumns 的顺序扫描策略单例。
func scanServicePolicy(row scanTarget) (domain.ServicePolicy, error) {
	var out domain.ServicePolicy
	var updatedAt sql.NullTime
	if err := row.Scan(
		&out.QueueEnabled,
		&out.MaxWaitSeconds,
		&out.SLAFirstResponseSeconds,
		&out.SLAIdleMinutes,
		&out.SLAIdleAction,
		&out.AutoCloseIdleMinutes,
		&out.AIKeywordPriority,
		&out.UnattendedEnabled,
		&out.UnattendedReply,
		&out.TakeoverPopupEnabled,
		&updatedAt,
	); err != nil {
		return domain.ServicePolicy{}, err
	}
	// 历史库可能写入越界值：读取时同样归一化，保证运行期行为可控。
	out = out.Normalize()
	if updatedAt.Valid {
		value := updatedAt.Time
		out.UpdatedAt = &value
	}
	return out, nil
}

// ServicePolicy 读取策略单例；未写入或查询失败时返回默认值（不影响主链路）。
func (s *PostgresStore) ServicePolicy() domain.ServicePolicy {
	ctx, cancel := dbContext()
	defer cancel()

	row := s.pool.QueryRow(ctx, `SELECT `+servicePolicyColumns+` FROM service_policy WHERE id=1`)
	policy, err := scanServicePolicy(row)
	if err != nil {
		return domain.DefaultServicePolicy()
	}
	return policy
}

// UpdateServicePolicy 覆盖保存策略单例（不存在时插入），返回归一化后的最终值。
func (s *PostgresStore) UpdateServicePolicy(next domain.ServicePolicy) (domain.ServicePolicy, error) {
	ctx, cancel := dbContext()
	defer cancel()

	normalized := next.Normalize()
	row := s.pool.QueryRow(ctx, `
		INSERT INTO service_policy (id, queue_enabled, max_wait_seconds, sla_first_response_seconds,
			sla_idle_minutes, sla_idle_action, auto_close_idle_minutes, ai_keyword_priority,
			unattended_enabled, unattended_reply, takeover_popup_enabled, updated_at)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (id) DO UPDATE
		SET queue_enabled=EXCLUDED.queue_enabled,
		    max_wait_seconds=EXCLUDED.max_wait_seconds,
		    sla_first_response_seconds=EXCLUDED.sla_first_response_seconds,
		    sla_idle_minutes=EXCLUDED.sla_idle_minutes,
		    sla_idle_action=EXCLUDED.sla_idle_action,
		    auto_close_idle_minutes=EXCLUDED.auto_close_idle_minutes,
		    ai_keyword_priority=EXCLUDED.ai_keyword_priority,
		    unattended_enabled=EXCLUDED.unattended_enabled,
		    unattended_reply=EXCLUDED.unattended_reply,
		    takeover_popup_enabled=EXCLUDED.takeover_popup_enabled,
		    updated_at=EXCLUDED.updated_at
		RETURNING `+servicePolicyColumns,
		normalized.QueueEnabled,
		normalized.MaxWaitSeconds,
		normalized.SLAFirstResponseSeconds,
		normalized.SLAIdleMinutes,
		normalized.SLAIdleAction,
		normalized.AutoCloseIdleMinutes,
		normalized.AIKeywordPriority,
		normalized.UnattendedEnabled,
		normalized.UnattendedReply,
		normalized.TakeoverPopupEnabled,
		dbNow(),
	)
	return scanServicePolicy(row)
}

// scanQueuedConversation 扫描一条排队记录。
func scanQueuedConversation(row scanTarget) (domain.QueuedConversation, error) {
	var out domain.QueuedConversation
	err := row.Scan(&out.ConversationID, &out.GroupName, &out.Priority, &out.EnqueuedAt, &out.Reason)
	return out, err
}

// QueueList 返回排队列表：priority DESC, enqueued_at ASC（越大越优先、同优先级先到先服务）。
func (s *PostgresStore) QueueList() []domain.QueuedConversation {
	ctx, cancel := dbContext()
	defer cancel()

	rows, err := s.pool.Query(ctx, `
		SELECT `+queuedConversationColumns+`
		FROM conversation_queue
		ORDER BY priority DESC, enqueued_at ASC, conversation_id ASC
	`)
	if err != nil {
		return []domain.QueuedConversation{}
	}
	defer rows.Close()

	out := []domain.QueuedConversation{}
	for rows.Next() {
		entry, err := scanQueuedConversation(rows)
		if err != nil {
			return out
		}
		out = append(out, entry)
	}
	return out
}

// EnqueueConversation 写入排队队列；重复入队更新分组/优先级/原因，保留原入队时间。
func (s *PostgresStore) EnqueueConversation(entry domain.QueuedConversation) (domain.QueuedConversation, error) {
	ctx, cancel := dbContext()
	defer cancel()

	conversationID := strings.TrimSpace(entry.ConversationID)
	if conversationID == "" {
		return domain.QueuedConversation{}, fmt.Errorf("%w: conversation_id 不能为空", ErrInvalidInput)
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE id=$1)`, conversationID).Scan(&exists); err != nil {
		return domain.QueuedConversation{}, err
	}
	if !exists {
		return domain.QueuedConversation{}, ErrNotFound
	}
	groupName := strings.TrimSpace(entry.GroupName)
	reason := domain.NormalizeQueueReason(entry.Reason)
	row := s.pool.QueryRow(ctx, `
		INSERT INTO conversation_queue (conversation_id, group_name, priority, enqueued_at, reason)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (conversation_id) DO UPDATE
		SET group_name=EXCLUDED.group_name,
		    priority=EXCLUDED.priority,
		    reason=EXCLUDED.reason
		RETURNING `+queuedConversationColumns,
		conversationID, groupName, entry.Priority, dbNow(), reason)
	return scanQueuedConversation(row)
}

// DequeueConversation 把会话移出排队队列。
func (s *PostgresStore) DequeueConversation(conversationID string) error {
	ctx, cancel := dbContext()
	defer cancel()

	tag, err := s.pool.Exec(ctx, `DELETE FROM conversation_queue WHERE conversation_id=$1`, strings.TrimSpace(conversationID))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// scanBlacklistEntry 扫描一条黑名单记录。
func scanBlacklistEntry(row scanTarget) (domain.BlacklistEntry, error) {
	var out domain.BlacklistEntry
	var expiresAt sql.NullTime
	if err := row.Scan(&out.ID, &out.Kind, &out.Value, &out.Reason, &out.CreatedBy, &expiresAt, &out.CreatedAt); err != nil {
		return domain.BlacklistEntry{}, err
	}
	if expiresAt.Valid {
		value := expiresAt.Time
		out.ExpiresAt = &value
	}
	return out, nil
}

// BlacklistList 返回黑名单：kind 为空表示全部；按创建时间倒序。
func (s *PostgresStore) BlacklistList(kind string) []domain.BlacklistEntry {
	kind = strings.ToLower(strings.TrimSpace(kind))
	query := `SELECT ` + blacklistColumns + ` FROM service_blacklist`
	args := []any{}
	if kind != "" {
		if !domain.ValidBlacklistKind(kind) {
			return []domain.BlacklistEntry{}
		}
		query += ` WHERE kind=$1`
		args = append(args, kind)
	}
	query += ` ORDER BY created_at DESC, id DESC`

	ctx, cancel := dbContext()
	defer cancel()
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return []domain.BlacklistEntry{}
	}
	defer rows.Close()

	out := []domain.BlacklistEntry{}
	for rows.Next() {
		entry, err := scanBlacklistEntry(rows)
		if err != nil {
			return out
		}
		out = append(out, entry)
	}
	return out
}

// AddBlacklist 新增黑名单项；(kind,value) 重复时返回 ErrConflict。
func (s *PostgresStore) AddBlacklist(entry domain.BlacklistEntry) (domain.BlacklistEntry, error) {
	ctx, cancel := dbContext()
	defer cancel()

	normalized, err := normalizeBlacklistEntry(entry)
	if err != nil {
		return domain.BlacklistEntry{}, err
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO service_blacklist (id, kind, value, reason, created_by, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+blacklistColumns,
		"bl_"+randomID(8), normalized.Kind, normalized.Value, normalized.Reason, normalized.CreatedBy, normalized.ExpiresAt, dbNow())
	out, err := scanBlacklistEntry(row)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.BlacklistEntry{}, fmt.Errorf("%w: 该黑名单项已存在", ErrConflict)
		}
		return domain.BlacklistEntry{}, err
	}
	return out, nil
}

// DeleteBlacklist 删除黑名单项。
func (s *PostgresStore) DeleteBlacklist(id string) error {
	ctx, cancel := dbContext()
	defer cancel()

	tag, err := s.pool.Exec(ctx, `DELETE FROM service_blacklist WHERE id=$1`, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MatchBlacklist 判断 (kind,value) 是否命中有效黑名单；过期项视为未命中并顺手删除。
func (s *PostgresStore) MatchBlacklist(kind, value string) (domain.BlacklistEntry, bool) {
	normalized, err := normalizeBlacklistEntry(domain.BlacklistEntry{Kind: kind, Value: value})
	if err != nil {
		return domain.BlacklistEntry{}, false
	}
	ctx, cancel := dbContext()
	defer cancel()

	row := s.pool.QueryRow(ctx, `
		SELECT `+blacklistColumns+`
		FROM service_blacklist
		WHERE kind=$1 AND value=$2
		ORDER BY created_at DESC
		LIMIT 1
	`, normalized.Kind, normalized.Value)
	entry, err := scanBlacklistEntry(row)
	if err != nil {
		return domain.BlacklistEntry{}, false
	}
	if entry.Expired(time.Now().UTC()) {
		_, _ = s.pool.Exec(ctx, `DELETE FROM service_blacklist WHERE id=$1`, entry.ID)
		return domain.BlacklistEntry{}, false
	}
	return entry, true
}

// TakeoverConversation 客服正式接管会话：分配给该客服、状态置 assigned、清空 sla_state，
// 并把 first_response_at 重置为空（接管即开启人工首响计时），同时移出排队队列。
func (s *PostgresStore) TakeoverConversation(agentID, conversationID string) (domain.Conversation, error) {
	ctx, cancel := dbContext()
	defer cancel()

	agentID = strings.TrimSpace(agentID)
	var out domain.Conversation
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		conversation, err := s.conversationByIDForUpdateTx(ctx, tx, strings.TrimSpace(conversationID))
		if err != nil {
			return mapNoRows(err)
		}
		if conversation.Status == domain.ConversationClosed {
			return ErrForbidden
		}
		if conversation.AssignedAgentID != "" && conversation.AssignedAgentID != agentID {
			// 已被他人负责：不允许抢占，交由管理员转接。
			return ErrForbidden
		}
		agent, err := s.agentByIDForUpdateTx(ctx, tx, agentID)
		if err != nil || agent.DisabledAt != nil {
			return ErrForbidden
		}
		now := dbNow()
		if conversation.AssignedAgentID != agentID {
			if _, err := tx.Exec(ctx, `
				UPDATE agents SET current_conversations=current_conversations+1, updated_at=$2 WHERE id=$1
			`, agent.ID, now); err != nil {
				return err
			}
			conversation.AssignedAgentID = agentID
		}
		if _, err := tx.Exec(ctx, `
			UPDATE conversations
			SET assigned_agent_id=$2, status=$3, sla_state='', first_response_at=NULL, updated_at=$4
			WHERE id=$1
		`, conversation.ID, agentID, domain.ConversationAssigned, now); err != nil {
			return err
		}
		// 接管后不必再排队等待分配。
		if _, err := tx.Exec(ctx, `DELETE FROM conversation_queue WHERE conversation_id=$1`, conversation.ID); err != nil {
			return err
		}
		conversation.Status = domain.ConversationAssigned
		conversation.SLAState = ""
		conversation.FirstResponseAt = nil
		conversation.UpdatedAt = now
		conversation.AssignedAgentName = agent.Name
		conversation.AssignedAgentAvatar = agent.AvatarURL
		out = conversation
		return nil
	})
	return out, err
}

// AssignConversationToAgent 静默分配（排队出队时使用）：只写 assigned_agent_id，不改会话状态，
// 同时递增客服并发并把会话移出排队队列；并发竞态下返回 ErrConflict。
func (s *PostgresStore) AssignConversationToAgent(conversationID, agentID string) (domain.Conversation, error) {
	ctx, cancel := dbContext()
	defer cancel()

	agentID = strings.TrimSpace(agentID)
	var out domain.Conversation
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		conversation, err := s.conversationByIDForUpdateTx(ctx, tx, strings.TrimSpace(conversationID))
		if err != nil {
			return mapNoRows(err)
		}
		if conversation.Status == domain.ConversationClosed {
			return ErrConflict
		}
		if conversation.AssignedAgentID == agentID {
			if _, err := tx.Exec(ctx, `DELETE FROM conversation_queue WHERE conversation_id=$1`, conversation.ID); err != nil {
				return err
			}
			out = conversation
			return nil
		}
		if conversation.AssignedAgentID != "" {
			return ErrConflict
		}
		agent, err := s.agentByIDForUpdateTx(ctx, tx, agentID)
		if err != nil || agent.DisabledAt != nil || agent.Status != domain.AgentOnline {
			return ErrConflict
		}
		now := dbNow()
		// 原子占位：只有仍有并发额度时才允许分配；失败即回滚整个事务（含计数）。
		tag, err := tx.Exec(ctx, `
			UPDATE agents SET current_conversations=current_conversations+1, updated_at=$2
			WHERE id=$1 AND current_conversations < max_conversations
		`, agent.ID, now)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrConflict
		}
		tag, err = tx.Exec(ctx, `
			UPDATE conversations SET assigned_agent_id=$2, updated_at=$3
			WHERE id=$1 AND assigned_agent_id IS NULL
		`, conversation.ID, agentID, now)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrConflict
		}
		if _, err := tx.Exec(ctx, `DELETE FROM conversation_queue WHERE conversation_id=$1`, conversation.ID); err != nil {
			return err
		}
		conversation.AssignedAgentID = agentID
		conversation.AssignedAgentName = agent.Name
		conversation.AssignedAgentAvatar = agent.AvatarURL
		conversation.UpdatedAt = now
		out = conversation
		return nil
	})
	return out, err
}

// AddUnattendedNotice 落库无人值守兜底系统消息并记录 unattended_notified_at；
// 已发送过返回 ErrConflict。刻意不调整已读计数：系统提示不应替客服"已读"访客消息。
func (s *PostgresStore) AddUnattendedNotice(conversationID, content string, translations map[string]string) (domain.Message, domain.Conversation, error) {
	ctx, cancel := dbContext()
	defer cancel()

	content = strings.TrimSpace(content)
	if content == "" {
		return domain.Message{}, domain.Conversation{}, fmt.Errorf("%w: 无人值守兜底话术不能为空", ErrInvalidInput)
	}
	var outMessage domain.Message
	var outConversation domain.Conversation
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		conversation, err := s.conversationByIDForUpdateTx(ctx, tx, strings.TrimSpace(conversationID))
		if err != nil {
			return mapNoRows(err)
		}
		if conversation.Status == domain.ConversationClosed {
			return ErrForbidden
		}
		if conversation.UnattendedNotifiedAt != nil {
			return ErrConflict
		}
		msg, err := s.createMessageTx(ctx, tx, conversation.ID, "", domain.SenderSystem, "system", domain.MessageSystem, content, translations)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE conversations
			SET unattended_notified_at=$2, last_message=$3, last_message_at=$2, updated_at=$2
			WHERE id=$1
		`, conversation.ID, msg.CreatedAt, content); err != nil {
			return err
		}
		notifiedAt := msg.CreatedAt
		conversation.UnattendedNotifiedAt = &notifiedAt
		conversation.LastMessage = content
		conversation.LastMessageAt = &notifiedAt
		conversation.UpdatedAt = notifiedAt
		outMessage, outConversation = msg, conversation
		return nil
	})
	return outMessage, outConversation, err
}

// sealAlertChannelConfig 把渠道配置序列化为 JSON 后用 AES-256-GCM 加密。
func (s *PostgresStore) sealAlertChannelConfig(config map[string]string) (string, error) {
	raw, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	return protectSecret(s.options.DataEncryptionKey, string(raw))
}

// revealAlertChannelConfig 解密渠道配置；加密串为空时返回空 map。
func (s *PostgresStore) revealAlertChannelConfig(ciphertext string) (map[string]string, error) {
	plain, err := revealSecret(s.options.DataEncryptionKey, ciphertext)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(plain) == "" {
		return map[string]string{}, nil
	}
	out := map[string]string{}
	if err := json.Unmarshal([]byte(plain), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// scanAgentAlertChannel 扫描渠道并解密配置；解密失败时保留渠道但配置置空，
// 避免单条坏数据导致整个列表不可用（发送时该渠道会因缺配置而失败并记日志）。
func (s *PostgresStore) scanAgentAlertChannel(row scanTarget) (domain.AgentAlertChannel, error) {
	var out domain.AgentAlertChannel
	var ciphertext string
	if err := row.Scan(&out.ID, &out.AgentID, &out.Kind, &ciphertext, &out.Enabled, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return domain.AgentAlertChannel{}, err
	}
	if config, err := s.revealAlertChannelConfig(ciphertext); err == nil {
		out.Config = config
	}
	out.ConfigMasked = domain.MaskAlertChannelConfig(out.Kind, out.Config)
	out.Masked = domain.MaskAlertChannelPrimary(out.Kind, out.Config)
	return out, nil
}

// agentAlertChannelByID 读取单个渠道（含解密配置）。
func (s *PostgresStore) agentAlertChannelByID(id string) (domain.AgentAlertChannel, error) {
	ctx, cancel := dbContext()
	defer cancel()

	row := s.pool.QueryRow(ctx, `SELECT `+alertChannelColumns+` FROM agent_alert_channels WHERE id=$1`, strings.TrimSpace(id))
	return s.scanAgentAlertChannel(row)
}

// AgentAlertChannels 返回某客服的提醒渠道（按创建时间升序）。
func (s *PostgresStore) AgentAlertChannels(agentID string) []domain.AgentAlertChannel {
	ctx, cancel := dbContext()
	defer cancel()

	rows, err := s.pool.Query(ctx, `
		SELECT `+alertChannelColumns+`
		FROM agent_alert_channels
		WHERE agent_id=$1
		ORDER BY created_at ASC, id ASC
	`, strings.TrimSpace(agentID))
	if err != nil {
		return []domain.AgentAlertChannel{}
	}
	defer rows.Close()

	out := []domain.AgentAlertChannel{}
	for rows.Next() {
		channel, err := s.scanAgentAlertChannel(rows)
		if err != nil {
			return out
		}
		out = append(out, channel)
	}
	return out
}

// AgentAlertChannel 按 ID 读取单个提醒渠道（Config 为解密后的明文）。
func (s *PostgresStore) AgentAlertChannel(id string) (domain.AgentAlertChannel, bool) {
	channel, err := s.agentAlertChannelByID(id)
	if err != nil {
		return domain.AgentAlertChannel{}, false
	}
	return channel, true
}

// CreateAgentAlertChannel 新增提醒渠道；每个客服最多 3 个。
func (s *PostgresStore) CreateAgentAlertChannel(agentID, kind string, config map[string]string) (domain.AgentAlertChannel, error) {
	ctx, cancel := dbContext()
	defer cancel()

	agentID, kind, normalizedConfig, err := normalizeAlertChannelCreate(agentID, kind, config)
	if err != nil {
		return domain.AgentAlertChannel{}, err
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agents WHERE id=$1)`, agentID).Scan(&exists); err != nil {
		return domain.AgentAlertChannel{}, err
	}
	if !exists {
		return domain.AgentAlertChannel{}, ErrNotFound
	}
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM agent_alert_channels WHERE agent_id=$1`, agentID).Scan(&count); err != nil {
		return domain.AgentAlertChannel{}, err
	}
	if count >= domain.AlertChannelMaxPerAgent {
		return domain.AgentAlertChannel{}, alertChannelLimitError()
	}
	ciphertext, err := s.sealAlertChannelConfig(normalizedConfig)
	if err != nil {
		return domain.AgentAlertChannel{}, err
	}
	now := dbNow()
	row := s.pool.QueryRow(ctx, `
		INSERT INTO agent_alert_channels (id, agent_id, kind, config_ciphertext, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, true, $5, $5)
		RETURNING `+alertChannelColumns,
		"alc_"+randomID(8), agentID, kind, ciphertext, now)
	return s.scanAgentAlertChannel(row)
}

// UpdateAgentAlertChannel 局部更新渠道；config==nil 表示配置不变，
// 否则按合并语义处理：未出现的键保留原值、显式空串删除、其余覆盖。
func (s *PostgresStore) UpdateAgentAlertChannel(id string, patch domain.AgentAlertChannelPatch, config *map[string]string) (domain.AgentAlertChannel, error) {
	ctx, cancel := dbContext()
	defer cancel()

	current, err := s.agentAlertChannelByID(id)
	if err != nil {
		return domain.AgentAlertChannel{}, mapNoRows(err)
	}
	enabled := current.Enabled
	if patch.Enabled != nil {
		enabled = *patch.Enabled
	}
	nextConfig := current.Config
	if config != nil {
		merged, err := mergeAlertChannelConfig(current.Kind, current.Config, *config)
		if err != nil {
			return domain.AgentAlertChannel{}, fmt.Errorf("%w: %s", ErrInvalidInput, err)
		}
		nextConfig = merged
	}
	ciphertext, err := s.sealAlertChannelConfig(nextConfig)
	if err != nil {
		return domain.AgentAlertChannel{}, err
	}
	row := s.pool.QueryRow(ctx, `
		UPDATE agent_alert_channels
		SET enabled=$2, config_ciphertext=$3, updated_at=$4
		WHERE id=$1
		RETURNING `+alertChannelColumns,
		current.ID, enabled, ciphertext, dbNow())
	out, err := s.scanAgentAlertChannel(row)
	return out, mapNoRows(err)
}

// DeleteAgentAlertChannel 删除渠道。
func (s *PostgresStore) DeleteAgentAlertChannel(id string) error {
	ctx, cancel := dbContext()
	defer cancel()

	tag, err := s.pool.Exec(ctx, `DELETE FROM agent_alert_channels WHERE id=$1`, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// scanAlertSMTP 扫描 SMTP 单例并解密密码。
func (s *PostgresStore) scanAlertSMTP(row scanTarget) (domain.AlertSMTPSettings, error) {
	var out domain.AlertSMTPSettings
	var ciphertext string
	var updatedAt sql.NullTime
	if err := row.Scan(&out.Host, &out.Port, &out.Username, &ciphertext, &out.FromAddress, &out.UseTLS, &updatedAt); err != nil {
		return domain.AlertSMTPSettings{}, err
	}
	if password, err := revealSecret(s.options.DataEncryptionKey, ciphertext); err == nil {
		out.Password = password
	}
	if updatedAt.Valid {
		value := updatedAt.Time
		out.UpdatedAt = &value
	}
	return out.WithMask(), nil
}

// AlertSMTPSettings 读取 SMTP 单例；未写入时返回默认值。
func (s *PostgresStore) AlertSMTPSettings() domain.AlertSMTPSettings {
	ctx, cancel := dbContext()
	defer cancel()

	row := s.pool.QueryRow(ctx, `SELECT `+alertSMTPColumns+` FROM alert_smtp_settings WHERE id=1`)
	settings, err := s.scanAlertSMTP(row)
	if err != nil {
		return domain.DefaultAlertSMTPSettings()
	}
	return settings
}

// UpdateAlertSMTPSettings 覆盖保存 SMTP 设置；password==nil 不改，"" 清除，其它覆盖。
func (s *PostgresStore) UpdateAlertSMTPSettings(next domain.AlertSMTPSettings, password *string) (domain.AlertSMTPSettings, error) {
	ctx, cancel := dbContext()
	defer cancel()

	normalized, err := next.Normalize()
	if err != nil {
		return domain.AlertSMTPSettings{}, fmt.Errorf("%w: %s", ErrInvalidInput, err)
	}
	// 先取当前密文：password==nil 时保持原密码不变。
	var currentCiphertext string
	err = s.pool.QueryRow(ctx, `SELECT password_ciphertext FROM alert_smtp_settings WHERE id=1`).Scan(&currentCiphertext)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return domain.AlertSMTPSettings{}, err
	}
	effectiveCiphertext := currentCiphertext
	if password != nil {
		effectiveCiphertext, err = protectSecret(s.options.DataEncryptionKey, *password)
		if err != nil {
			return domain.AlertSMTPSettings{}, err
		}
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO alert_smtp_settings (id, host, port, username, password_ciphertext, from_address, use_tls, updated_at)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE
		SET host=EXCLUDED.host,
		    port=EXCLUDED.port,
		    username=EXCLUDED.username,
		    password_ciphertext=EXCLUDED.password_ciphertext,
		    from_address=EXCLUDED.from_address,
		    use_tls=EXCLUDED.use_tls,
		    updated_at=EXCLUDED.updated_at
		RETURNING `+alertSMTPColumns,
		normalized.Host, normalized.Port, normalized.Username, effectiveCiphertext, normalized.FromAddress, normalized.UseTLS, dbNow())
	return s.scanAlertSMTP(row)
}
