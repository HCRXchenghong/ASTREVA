package store

import (
	"database/sql"

	"customer-service/backend/internal/domain"
)

// aiLimitsColumns 是 ai_limits 表的固定列序，扫描函数与查询必须严格对应。
const aiLimitsColumns = `enabled, visitor_messages_per_minute, visitor_messages_per_day,
	ai_calls_per_conversation_day, ai_reply_max_chars, exceed_message, updated_at`

// scanAILimits 按 aiLimitsColumns 的顺序扫描限额配置。
func scanAILimits(row scanTarget) (domain.AILimits, error) {
	var out domain.AILimits
	var updatedAt sql.NullTime
	err := row.Scan(
		&out.Enabled,
		&out.VisitorMessagesPerMinute,
		&out.VisitorMessagesPerDay,
		&out.AICallsPerConversationDay,
		&out.AIReplyMaxChars,
		&out.ExceedMessage,
		&updatedAt,
	)
	if err != nil {
		return domain.AILimits{}, err
	}
	// 历史库可能写入了越界值：读取时同样归一化，保证运行期行为可控。
	out = domain.NormalizeAILimits(out)
	if updatedAt.Valid {
		value := updatedAt.Time
		out.UpdatedAt = &value
	}
	return out, nil
}

// AILimits 读取单例限额配置；未写入或查询失败时返回默认值，避免影响消息投递主链路。
func (s *PostgresStore) AILimits() domain.AILimits {
	ctx, cancel := dbContext()
	defer cancel()

	row := s.pool.QueryRow(ctx, `SELECT `+aiLimitsColumns+` FROM ai_limits WHERE id=1`)
	limits, err := scanAILimits(row)
	if err != nil {
		return domain.DefaultAILimits()
	}
	return limits
}

// UpdateAILimits 覆盖保存单例限额配置（不存在时插入），并返回归一化后的最终值。
func (s *PostgresStore) UpdateAILimits(next domain.AILimits) (domain.AILimits, error) {
	ctx, cancel := dbContext()
	defer cancel()

	normalized, err := normalizeAILimits(next)
	if err != nil {
		return domain.AILimits{}, err
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO ai_limits (id, enabled, visitor_messages_per_minute, visitor_messages_per_day,
			ai_calls_per_conversation_day, ai_reply_max_chars, exceed_message, updated_at)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE
		SET enabled=EXCLUDED.enabled,
		    visitor_messages_per_minute=EXCLUDED.visitor_messages_per_minute,
		    visitor_messages_per_day=EXCLUDED.visitor_messages_per_day,
		    ai_calls_per_conversation_day=EXCLUDED.ai_calls_per_conversation_day,
		    ai_reply_max_chars=EXCLUDED.ai_reply_max_chars,
		    exceed_message=EXCLUDED.exceed_message,
		    updated_at=EXCLUDED.updated_at
		RETURNING `+aiLimitsColumns,
		normalized.Enabled,
		normalized.VisitorMessagesPerMinute,
		normalized.VisitorMessagesPerDay,
		normalized.AICallsPerConversationDay,
		normalized.AIReplyMaxChars,
		normalized.ExceedMessage,
		dbNow(),
	)
	updated, err := scanAILimits(row)
	if err != nil {
		return domain.AILimits{}, err
	}
	return updated, nil
}
