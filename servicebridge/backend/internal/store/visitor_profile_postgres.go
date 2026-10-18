package store

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"customer-service/backend/internal/domain"
)

// ===== 访客画像（P5）：PostgreSQL 实现 =====

const visitorProfileColumns = `visitor_id, country, country_code, region, city, timezone,
	nationality_guess, nationality_confidence, COALESCE(nationality_basis::text, '[]'), ip_first, ip_last,
	COALESCE(languages::text, '{}'), dialect, code_switching, summary,
	COALESCE(tags::text, '[]'), COALESCE(interests::text, '[]'), COALESCE(evidence::text, '[]'),
	visits, last_path, last_seen_at, ai_updated_at, created_at, updated_at`

func scanVisitorProfile(row scanTarget) (domain.VisitorProfile, error) {
	var out domain.VisitorProfile
	var basisJSON, languagesJSON, tagsJSON, interestsJSON, evidenceJSON string
	var confidence float64
	err := row.Scan(
		&out.VisitorID, &out.Country, &out.CountryCode, &out.Region, &out.City, &out.Timezone,
		&out.NationalityGuess, &confidence, &basisJSON, &out.IPFirst, &out.IPLast,
		&languagesJSON, &out.Dialect, &out.CodeSwitching, &out.Summary,
		&tagsJSON, &interestsJSON, &evidenceJSON,
		&out.Visits, &out.LastPath, &out.LastSeenAt, &out.AIUpdatedAt, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		return domain.VisitorProfile{}, err
	}
	out.NationalityConfidence = confidence
	out.NationalityBasis = parseProfileListJSON(basisJSON)
	out.Languages = parseLanguageCountsJSON(languagesJSON)
	out.Tags = parseProfileListJSON(tagsJSON)
	out.Interests = parseProfileListJSON(interestsJSON)
	out.Evidence = parseEvidenceJSON(evidenceJSON)
	return out, nil
}

// VisitorProfileSettings 读取画像单例配置；未写入时返回默认值。
func (s *PostgresStore) VisitorProfileSettings() domain.VisitorProfileSettings {
	ctx, cancel := dbContext()
	defer cancel()
	settings, err := s.visitorProfileSettingsTx(ctx, s.pool)
	if err != nil {
		return domain.NormalizeVisitorProfileSettings(domain.DefaultVisitorProfileSettings())
	}
	return settings
}

// visitorProfileSettingsTx 在给定执行器上读取配置（不存在时返回默认值）。
func (s *PostgresStore) visitorProfileSettingsTx(ctx context.Context, exec dbTx) (domain.VisitorProfileSettings, error) {
	var out domain.VisitorProfileSettings
	var storedKey string
	var updatedAt time.Time
	err := exec.QueryRow(ctx, `
		SELECT enabled, geoip_dir, third_party_url, third_party_key_ciphertext,
		       third_party_timeout_ms, cache_ttl_hours, ai_summary_enabled,
		       ai_summary_every_messages, show_full_ip, updated_at
		FROM visitor_profile_settings WHERE id=1
	`).Scan(
		&out.Enabled, &out.GeoIPDir, &out.ThirdPartyURL, &storedKey,
		&out.ThirdPartyTimeoutMS, &out.CacheTTLHours, &out.AISummaryEnabled,
		&out.AISummaryEveryMessages, &out.ShowFullIP, &updatedAt,
	)
	if err != nil {
		return domain.NormalizeVisitorProfileSettings(domain.DefaultVisitorProfileSettings()), nil
	}
	key, revealErr := revealSecret(s.options.DataEncryptionKey, storedKey)
	if revealErr != nil {
		// 密钥无法解密（如加密密钥轮换）：配置仍可用，只是第三方库不可用。
		key = ""
	}
	out.ThirdPartyKey = key
	out.UpdatedAt = &updatedAt
	return domain.NormalizeVisitorProfileSettings(out), nil
}

// UpdateVisitorProfileSettings 局部更新画像配置：nil=不改，ThirdPartyKey 空串=清除。
func (s *PostgresStore) UpdateVisitorProfileSettings(patch domain.VisitorProfileSettingsPatch) (domain.VisitorProfileSettings, error) {
	ctx, cancel := dbContext()
	defer cancel()

	current, _ := s.visitorProfileSettingsTx(ctx, s.pool)
	next, err := normalizeVisitorProfileSettings(applyVisitorProfileSettingsPatch(current, patch))
	if err != nil {
		return current, err
	}
	protectedKey, err := protectSecret(s.options.DataEncryptionKey, next.ThirdPartyKey)
	if err != nil {
		return current, err
	}
	now := dbNow()
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO visitor_profile_settings (
			id, enabled, geoip_dir, third_party_url, third_party_key_ciphertext,
			third_party_timeout_ms, cache_ttl_hours, ai_summary_enabled,
			ai_summary_every_messages, show_full_ip, updated_at
		)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO UPDATE SET
			enabled=EXCLUDED.enabled,
			geoip_dir=EXCLUDED.geoip_dir,
			third_party_url=EXCLUDED.third_party_url,
			third_party_key_ciphertext=EXCLUDED.third_party_key_ciphertext,
			third_party_timeout_ms=EXCLUDED.third_party_timeout_ms,
			cache_ttl_hours=EXCLUDED.cache_ttl_hours,
			ai_summary_enabled=EXCLUDED.ai_summary_enabled,
			ai_summary_every_messages=EXCLUDED.ai_summary_every_messages,
			show_full_ip=EXCLUDED.show_full_ip,
			updated_at=EXCLUDED.updated_at
	`, next.Enabled, next.GeoIPDir, next.ThirdPartyURL, protectedKey,
		next.ThirdPartyTimeoutMS, next.CacheTTLHours, next.AISummaryEnabled,
		next.AISummaryEveryMessages, next.ShowFullIP, now); err != nil {
		return current, err
	}
	next.UpdatedAt = &now
	next.ThirdPartyKeyMasked = domain.MaskSecret(next.ThirdPartyKey)
	return next, nil
}

// VisitorProfile 按访客 ID 读取画像。
func (s *PostgresStore) VisitorProfile(visitorID string) (domain.VisitorProfile, bool) {
	ctx, cancel := dbContext()
	defer cancel()

	profile, err := scanVisitorProfile(s.pool.QueryRow(ctx,
		`SELECT `+visitorProfileColumns+` FROM visitor_profiles WHERE visitor_id=$1`,
		strings.TrimSpace(visitorID)))
	if err != nil {
		return domain.VisitorProfile{}, false
	}
	return profile, true
}

// UpdateVisitorProfile 在事务内 SELECT ... FOR UPDATE 后执行 mutate 并整行写回。
// mutate 必须是纯内存操作（网络调用请在调用前完成），否则会长时间占用连接。
func (s *PostgresStore) UpdateVisitorProfile(visitorID string, mutate func(*domain.VisitorProfile) error) (domain.VisitorProfile, error) {
	ctx, cancel := dbContext()
	defer cancel()

	visitorID = strings.TrimSpace(visitorID)
	if visitorID == "" {
		return domain.VisitorProfile{}, ErrInvalidInput
	}
	var out domain.VisitorProfile
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		profile, err := scanVisitorProfile(tx.QueryRow(ctx,
			`SELECT `+visitorProfileColumns+` FROM visitor_profiles WHERE visitor_id=$1 FOR UPDATE`, visitorID))
		switch {
		case err == nil:
		case errors.Is(err, pgx.ErrNoRows):
			now := dbNow()
			profile = domain.VisitorProfile{
				VisitorID:        visitorID,
				NationalityBasis: []string{},
				Languages:        map[string]int{},
				Tags:             []string{},
				Interests:        []string{},
				Evidence:         []domain.VisitorProfileEvidence{},
				CreatedAt:        now,
				UpdatedAt:        now,
			}
		default:
			return err
		}
		if mutate != nil {
			if err := mutate(&profile); err != nil {
				return err
			}
		}
		normalized, err := normalizeVisitorProfileInput(profile)
		if err != nil {
			return err
		}
		normalized.VisitorID = visitorID
		normalized.CreatedAt = profile.CreatedAt
		normalized.UpdatedAt = dbNow()
		basisJSON := marshalProfileListJSON(normalized.NationalityBasis)
		languagesJSON := marshalLanguageCountsJSON(normalized.Languages)
		tagsJSON := marshalProfileListJSON(normalized.Tags)
		interestsJSON := marshalProfileListJSON(normalized.Interests)
		evidenceJSON := marshalEvidenceJSON(normalized.Evidence)
		written, err := scanVisitorProfile(tx.QueryRow(ctx, `
			INSERT INTO visitor_profiles (
				visitor_id, country, country_code, region, city, timezone,
				nationality_guess, nationality_confidence, nationality_basis, ip_first, ip_last,
				languages, dialect, code_switching, summary, tags, interests, evidence,
				visits, last_path, last_seen_at, ai_updated_at, created_at, updated_at
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$11,$12::jsonb,$13,$14,$15,$16::jsonb,$17::jsonb,$18::jsonb,$19,$20,$21,$22,$23,$24)
			ON CONFLICT (visitor_id) DO UPDATE SET
				country=EXCLUDED.country,
				country_code=EXCLUDED.country_code,
				region=EXCLUDED.region,
				city=EXCLUDED.city,
				timezone=EXCLUDED.timezone,
				nationality_guess=EXCLUDED.nationality_guess,
				nationality_confidence=EXCLUDED.nationality_confidence,
				nationality_basis=EXCLUDED.nationality_basis,
				ip_first=EXCLUDED.ip_first,
				ip_last=EXCLUDED.ip_last,
				languages=EXCLUDED.languages,
				dialect=EXCLUDED.dialect,
				code_switching=EXCLUDED.code_switching,
				summary=EXCLUDED.summary,
				tags=EXCLUDED.tags,
				interests=EXCLUDED.interests,
				evidence=EXCLUDED.evidence,
				visits=EXCLUDED.visits,
				last_path=EXCLUDED.last_path,
				last_seen_at=EXCLUDED.last_seen_at,
				ai_updated_at=EXCLUDED.ai_updated_at,
				updated_at=EXCLUDED.updated_at
			RETURNING `+visitorProfileColumns,
			normalized.VisitorID, normalized.Country, normalized.CountryCode, normalized.Region, normalized.City, normalized.Timezone,
			normalized.NationalityGuess, normalized.NationalityConfidence, basisJSON, normalized.IPFirst, normalized.IPLast,
			languagesJSON, normalized.Dialect, normalized.CodeSwitching, normalized.Summary,
			tagsJSON, interestsJSON, evidenceJSON,
			normalized.Visits, normalized.LastPath, normalized.LastSeenAt, normalized.AIUpdatedAt, normalized.CreatedAt, normalized.UpdatedAt,
		))
		if err != nil {
			return err
		}
		out = written
		return nil
	})
	if err != nil {
		return domain.VisitorProfile{}, err
	}
	return out, nil
}

// VisitorProfiles 按关键词/国家码/标签筛选并分页返回画像。
func (s *PostgresStore) VisitorProfiles(query domain.VisitorProfileQuery) ([]domain.VisitorProfile, int) {
	ctx, cancel := dbContext()
	defer cancel()

	query = normalizeVisitorProfileQuery(query)
	conditions := make([]string, 0, 3)
	args := make([]any, 0, 12)
	if query.Country != "" {
		if code := domain.NormalizeCountryCode(query.Country); code != "" {
			args = append(args, code)
			conditions = append(conditions, "country_code = $"+strconv.Itoa(len(args)))
		} else {
			args = append(args, "%"+strings.ToUpper(query.Country)+"%")
			conditions = append(conditions, "UPPER(country) LIKE $"+strconv.Itoa(len(args)))
		}
	}
	if query.Tag != "" {
		raw, err := json.Marshal([]string{query.Tag})
		if err != nil {
			raw = []byte("[]")
		}
		args = append(args, string(raw))
		conditions = append(conditions, "tags @> $"+strconv.Itoa(len(args))+"::jsonb")
	}
	if query.Query != "" {
		pattern := "%" + query.Query + "%"
		start := len(args) + 1
		args = append(args, pattern, pattern, pattern, pattern, pattern, pattern)
		conditions = append(conditions, "(visitor_id ILIKE $"+strconv.Itoa(start)+
			" OR ip_last ILIKE $"+strconv.Itoa(start+1)+
			" OR ip_first ILIKE $"+strconv.Itoa(start+2)+
			" OR summary ILIKE $"+strconv.Itoa(start+3)+
			" OR country ILIKE $"+strconv.Itoa(start+4)+
			" OR city ILIKE $"+strconv.Itoa(start+5)+")")
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM visitor_profiles`+where, args...).Scan(&total); err != nil {
		return []domain.VisitorProfile{}, 0
	}
	limitIndex := len(args) + 1
	offsetIndex := len(args) + 2
	args = append(args, query.Limit, query.Offset)
	rows, err := s.pool.Query(ctx, `SELECT `+visitorProfileColumns+` FROM visitor_profiles`+where+
		` ORDER BY updated_at DESC, visitor_id ASC LIMIT $`+strconv.Itoa(limitIndex)+` OFFSET $`+strconv.Itoa(offsetIndex), args...)
	if err != nil {
		return []domain.VisitorProfile{}, total
	}
	defer rows.Close()
	out := make([]domain.VisitorProfile, 0)
	for rows.Next() {
		profile, err := scanVisitorProfile(rows)
		if err != nil {
			return []domain.VisitorProfile{}, total
		}
		out = append(out, profile)
	}
	if err := rows.Err(); err != nil {
		return []domain.VisitorProfile{}, total
	}
	return out, total
}

// ConversationsForVisitor 返回某访客的全部会话（创建时间倒序）。
func (s *PostgresStore) ConversationsForVisitor(visitorID string) []domain.Conversation {
	ctx, cancel := dbContext()
	defer cancel()

	rows, err := s.pool.Query(ctx, `SELECT `+conversationColumns+` FROM conversations WHERE visitor_id=$1 ORDER BY created_at DESC`,
		strings.TrimSpace(visitorID))
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := make([]domain.Conversation, 0)
	for rows.Next() {
		conversation, err := scanConversation(rows)
		if err != nil {
			return nil
		}
		out = append(out, conversation)
	}
	return out
}

// DeleteVisitorProfile 删除画像；不存在时返回 ErrNotFound。
func (s *PostgresStore) DeleteVisitorProfile(visitorID string) error {
	ctx, cancel := dbContext()
	defer cancel()
	tag, err := s.pool.Exec(ctx, `DELETE FROM visitor_profiles WHERE visitor_id=$1`, strings.TrimSpace(visitorID))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// pruneVisitorProfileTx 在事务内清理已无任何会话的访客画像。
func (s *PostgresStore) pruneVisitorProfileTx(ctx context.Context, tx pgx.Tx, visitorID string) error {
	visitorID = strings.TrimSpace(visitorID)
	if visitorID == "" {
		return nil
	}
	_, err := tx.Exec(ctx, `
		DELETE FROM visitor_profiles
		WHERE visitor_id=$1 AND NOT EXISTS (SELECT 1 FROM conversations WHERE conversations.visitor_id=$1)
	`, visitorID)
	return err
}
