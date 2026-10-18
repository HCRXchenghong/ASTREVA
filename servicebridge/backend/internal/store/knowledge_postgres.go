package store

import (
	"context"
	"database/sql"
	"strings"

	"github.com/jackc/pgx/v5"

	"customer-service/backend/internal/domain"
)

const (
	knowledgeBaseColumns  = `id, name, description, enabled, sort_order, created_at, updated_at`
	knowledgeEntryColumns = `id, base_id, title, content, COALESCE(keywords::text, '[]'), category, priority, enabled, created_at, updated_at`
	// knowledgeEntryColumnsAliased 是带 e. 前缀的同序列表，供 JOIN 查询使用。
	knowledgeEntryColumnsAliased = `e.id, e.base_id, e.title, e.content, COALESCE(e.keywords::text, '[]'), e.category, e.priority, e.enabled, e.created_at, e.updated_at`
)

func scanKnowledgeBase(row scanTarget) (domain.KnowledgeBase, error) {
	var out domain.KnowledgeBase
	err := row.Scan(&out.ID, &out.Name, &out.Description, &out.Enabled, &out.SortOrder, &out.CreatedAt, &out.UpdatedAt)
	return out, err
}

func scanKnowledgeEntry(row scanTarget) (domain.KnowledgeEntry, error) {
	var out domain.KnowledgeEntry
	var keywordsJSON string
	err := row.Scan(&out.ID, &out.BaseID, &out.Title, &out.Content, &keywordsJSON, &out.Category, &out.Priority, &out.Enabled, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return out, err
	}
	out.Keywords = parseStringListJSON(keywordsJSON)
	return out, nil
}

func scanKnowledgeEntries(rows pgx.Rows) ([]domain.KnowledgeEntry, error) {
	result := []domain.KnowledgeEntry{}
	for rows.Next() {
		entry, err := scanKnowledgeEntry(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	return result, rows.Err()
}

func scanKnowledgeSettings(row scanTarget) (domain.KnowledgeSettings, error) {
	var out domain.KnowledgeSettings
	var updatedAt sql.NullTime
	err := row.Scan(&out.UnknownAction, &out.RefuseReply, &out.ScoreThreshold, &out.MaxEntries, &out.Enabled, &updatedAt)
	if err != nil {
		return domain.KnowledgeSettings{}, err
	}
	out = domain.NormalizeKnowledgeSettings(out)
	if updatedAt.Valid {
		value := updatedAt.Time
		out.UpdatedAt = &value
	}
	return out, nil
}

// KnowledgeBases 返回全部知识库，按 sort_order、创建时间、ID 稳定排序。
func (s *PostgresStore) KnowledgeBases() []domain.KnowledgeBase {
	ctx, cancel := dbContext()
	defer cancel()

	rows, err := s.pool.Query(ctx, `
		SELECT `+knowledgeBaseColumns+`
		FROM ai_knowledge_bases
		ORDER BY sort_order ASC, created_at ASC, id ASC
	`)
	if err != nil {
		return []domain.KnowledgeBase{}
	}
	defer rows.Close()
	result := []domain.KnowledgeBase{}
	for rows.Next() {
		base, err := scanKnowledgeBase(rows)
		if err != nil {
			return []domain.KnowledgeBase{}
		}
		result = append(result, base)
	}
	return result
}

func (s *PostgresStore) KnowledgeBase(id string) (domain.KnowledgeBase, bool) {
	ctx, cancel := dbContext()
	defer cancel()

	row := s.pool.QueryRow(ctx, `SELECT `+knowledgeBaseColumns+` FROM ai_knowledge_bases WHERE id=$1`, strings.TrimSpace(id))
	base, err := scanKnowledgeBase(row)
	if err != nil {
		return domain.KnowledgeBase{}, false
	}
	return base, true
}

func (s *PostgresStore) CreateKnowledgeBase(next domain.KnowledgeBase) (domain.KnowledgeBase, error) {
	ctx, cancel := dbContext()
	defer cancel()

	normalized, err := normalizeKnowledgeBase(next)
	if err != nil {
		return domain.KnowledgeBase{}, err
	}
	if normalized.ID == "" {
		normalized.ID = "kb_" + randomID(8)
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO ai_knowledge_bases (id, name, description, enabled, sort_order)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+knowledgeBaseColumns,
		normalized.ID, normalized.Name, normalized.Description, normalized.Enabled, normalized.SortOrder)
	created, err := scanKnowledgeBase(row)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.KnowledgeBase{}, ErrConflict
		}
		return domain.KnowledgeBase{}, err
	}
	return created, nil
}

func (s *PostgresStore) UpdateKnowledgeBase(id string, next domain.KnowledgeBase) (domain.KnowledgeBase, error) {
	ctx, cancel := dbContext()
	defer cancel()

	normalized, err := normalizeKnowledgeBase(next)
	if err != nil {
		return domain.KnowledgeBase{}, err
	}
	row := s.pool.QueryRow(ctx, `
		UPDATE ai_knowledge_bases
		SET name=$2, description=$3, enabled=$4, sort_order=$5, updated_at=$6
		WHERE id=$1
		RETURNING `+knowledgeBaseColumns,
		strings.TrimSpace(id), normalized.Name, normalized.Description, normalized.Enabled, normalized.SortOrder, dbNow())
	updated, err := scanKnowledgeBase(row)
	if err != nil {
		return domain.KnowledgeBase{}, mapNoRows(err)
	}
	return updated, nil
}

// DeleteKnowledgeBase 删除知识库；外键 ON DELETE CASCADE 会一并删除其条目。
func (s *PostgresStore) DeleteKnowledgeBase(id string) (domain.KnowledgeBase, error) {
	ctx, cancel := dbContext()
	defer cancel()

	row := s.pool.QueryRow(ctx, `DELETE FROM ai_knowledge_bases WHERE id=$1 RETURNING `+knowledgeBaseColumns, strings.TrimSpace(id))
	deleted, err := scanKnowledgeBase(row)
	if err != nil {
		return domain.KnowledgeBase{}, mapNoRows(err)
	}
	return deleted, nil
}

// KnowledgeEntries 按库与关键词分页列出条目（后台管理用）。
func (s *PostgresStore) KnowledgeEntries(baseID, query string, limit, offset int) []domain.KnowledgeEntry {
	ctx, cancel := dbContext()
	defer cancel()

	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+knowledgeEntryColumns+`
		FROM ai_knowledge_entries
		WHERE ($1 = '' OR base_id = $1)
		  AND ($2 = '' OR title ILIKE '%' || $2 || '%' OR content ILIKE '%' || $2 || '%'
		       OR category ILIKE '%' || $2 || '%' OR keywords::text ILIKE '%' || $2 || '%')
		ORDER BY priority DESC, updated_at DESC, id ASC
		LIMIT $3 OFFSET $4
	`, strings.TrimSpace(baseID), strings.TrimSpace(query), limit, offset)
	if err != nil {
		return []domain.KnowledgeEntry{}
	}
	defer rows.Close()
	result, err := scanKnowledgeEntries(rows)
	if err != nil {
		return []domain.KnowledgeEntry{}
	}
	return result
}

func (s *PostgresStore) KnowledgeEntry(id string) (domain.KnowledgeEntry, bool) {
	ctx, cancel := dbContext()
	defer cancel()

	row := s.pool.QueryRow(ctx, `SELECT `+knowledgeEntryColumns+` FROM ai_knowledge_entries WHERE id=$1`, strings.TrimSpace(id))
	entry, err := scanKnowledgeEntry(row)
	if err != nil {
		return domain.KnowledgeEntry{}, false
	}
	return entry, true
}

func (s *PostgresStore) CreateKnowledgeEntry(next domain.KnowledgeEntry) (domain.KnowledgeEntry, error) {
	ctx, cancel := dbContext()
	defer cancel()

	normalized, err := normalizeKnowledgeEntry(next)
	if err != nil {
		return domain.KnowledgeEntry{}, err
	}
	if err := s.ensureKnowledgeBaseExists(ctx, normalized.BaseID); err != nil {
		return domain.KnowledgeEntry{}, err
	}
	if normalized.ID == "" {
		normalized.ID = "ke_" + randomID(8)
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO ai_knowledge_entries (id, base_id, title, content, keywords, category, priority, enabled)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8)
		RETURNING `+knowledgeEntryColumns,
		normalized.ID, normalized.BaseID, normalized.Title, normalized.Content,
		marshalStringListJSON(normalized.Keywords), normalized.Category, normalized.Priority, normalized.Enabled)
	created, err := scanKnowledgeEntry(row)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.KnowledgeEntry{}, ErrConflict
		}
		return domain.KnowledgeEntry{}, err
	}
	return created, nil
}

func (s *PostgresStore) UpdateKnowledgeEntry(id string, next domain.KnowledgeEntry) (domain.KnowledgeEntry, error) {
	ctx, cancel := dbContext()
	defer cancel()

	normalized, err := normalizeKnowledgeEntry(next)
	if err != nil {
		return domain.KnowledgeEntry{}, err
	}
	if err := s.ensureKnowledgeBaseExists(ctx, normalized.BaseID); err != nil {
		return domain.KnowledgeEntry{}, err
	}
	row := s.pool.QueryRow(ctx, `
		UPDATE ai_knowledge_entries
		SET base_id=$2, title=$3, content=$4, keywords=$5::jsonb, category=$6, priority=$7, enabled=$8, updated_at=$9
		WHERE id=$1
		RETURNING `+knowledgeEntryColumns,
		strings.TrimSpace(id), normalized.BaseID, normalized.Title, normalized.Content,
		marshalStringListJSON(normalized.Keywords), normalized.Category, normalized.Priority, normalized.Enabled, dbNow())
	updated, err := scanKnowledgeEntry(row)
	if err != nil {
		return domain.KnowledgeEntry{}, mapNoRows(err)
	}
	return updated, nil
}

func (s *PostgresStore) DeleteKnowledgeEntry(id string) (domain.KnowledgeEntry, error) {
	ctx, cancel := dbContext()
	defer cancel()

	row := s.pool.QueryRow(ctx, `DELETE FROM ai_knowledge_entries WHERE id=$1 RETURNING `+knowledgeEntryColumns, strings.TrimSpace(id))
	deleted, err := scanKnowledgeEntry(row)
	if err != nil {
		return domain.KnowledgeEntry{}, mapNoRows(err)
	}
	return deleted, nil
}

// KnowledgeEntriesForBases 先用 WHERE enabled=true AND base_id = ANY($1) 取候选，
// 避免把整张知识表读进内存；baseIDs 为空时取全部启用库。
func (s *PostgresStore) KnowledgeEntriesForBases(baseIDs []string) []domain.KnowledgeEntry {
	ctx, cancel := dbContext()
	defer cancel()

	requested := cleanKnowledgeBaseIDs(baseIDs)
	var (
		rows pgx.Rows
		err  error
	)
	if len(requested) > 0 {
		rows, err = s.pool.Query(ctx, `
			SELECT `+knowledgeEntryColumnsAliased+`
			FROM ai_knowledge_entries e
			JOIN ai_knowledge_bases b ON b.id = e.base_id
			WHERE e.enabled = true AND b.enabled = true AND e.base_id = ANY($1)
			ORDER BY e.priority DESC, e.created_at ASC, e.id ASC
		`, requested)
	} else {
		rows, err = s.pool.Query(ctx, `
			SELECT `+knowledgeEntryColumnsAliased+`
			FROM ai_knowledge_entries e
			JOIN ai_knowledge_bases b ON b.id = e.base_id
			WHERE e.enabled = true AND b.enabled = true
			ORDER BY e.priority DESC, e.created_at ASC, e.id ASC
		`)
	}
	if err != nil {
		return []domain.KnowledgeEntry{}
	}
	defer rows.Close()
	result, err := scanKnowledgeEntries(rows)
	if err != nil {
		return []domain.KnowledgeEntry{}
	}
	return result
}

func (s *PostgresStore) KnowledgeSettings() domain.KnowledgeSettings {
	ctx, cancel := dbContext()
	defer cancel()

	row := s.pool.QueryRow(ctx, `
		SELECT unknown_action, refuse_reply, score_threshold, max_entries, enabled, updated_at
		FROM ai_knowledge_settings WHERE id=1
	`)
	settings, err := scanKnowledgeSettings(row)
	if err != nil {
		return domain.DefaultKnowledgeSettings()
	}
	return settings
}

func (s *PostgresStore) UpdateKnowledgeSettings(next domain.KnowledgeSettings) (domain.KnowledgeSettings, error) {
	ctx, cancel := dbContext()
	defer cancel()

	normalized, err := normalizeKnowledgeSettings(next)
	if err != nil {
		return domain.KnowledgeSettings{}, err
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO ai_knowledge_settings (id, unknown_action, refuse_reply, score_threshold, max_entries, enabled, updated_at)
		VALUES (1, $1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE
		SET unknown_action=EXCLUDED.unknown_action, refuse_reply=EXCLUDED.refuse_reply,
		    score_threshold=EXCLUDED.score_threshold, max_entries=EXCLUDED.max_entries,
		    enabled=EXCLUDED.enabled, updated_at=EXCLUDED.updated_at
		RETURNING unknown_action, refuse_reply, score_threshold, max_entries, enabled, updated_at
	`, normalized.UnknownAction, normalized.RefuseReply, normalized.ScoreThreshold, normalized.MaxEntries, normalized.Enabled, dbNow())
	updated, err := scanKnowledgeSettings(row)
	if err != nil {
		return domain.KnowledgeSettings{}, err
	}
	return updated, nil
}

// RequestHumanHandoff 静默转人工：状态置为 human_requested 并分配客服，不产生任何消息。
func (s *PostgresStore) RequestHumanHandoff(conversationID string) (domain.Conversation, error) {
	ctx, cancel := dbContext()
	defer cancel()

	var out domain.Conversation
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		conversation, err := s.conversationByIDForUpdateTx(ctx, tx, strings.TrimSpace(conversationID))
		if err != nil {
			return mapNoRows(err)
		}
		if conversation.Status == domain.ConversationClosed {
			return ErrForbidden
		}
		if err := s.requestHumanTx(ctx, tx, &conversation); err != nil {
			return err
		}
		out, err = s.conversationByIDForUpdateTx(ctx, tx, conversation.ID)
		return err
	})
	if err != nil {
		return domain.Conversation{}, err
	}
	return out, nil
}

// ensureKnowledgeBaseExists 校验条目所属知识库存在，缺失时返回 ErrNotFound。
func (s *PostgresStore) ensureKnowledgeBaseExists(ctx context.Context, baseID string) error {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ai_knowledge_bases WHERE id=$1)`, baseID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}
