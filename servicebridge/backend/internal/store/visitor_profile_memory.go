package store

import (
	"strings"
	"time"

	"customer-service/backend/internal/domain"
)

// ===== 访客画像（P5）：内存实现 =====

// VisitorProfileSettings 读取画像单例配置；未写入时返回默认值。
func (s *MemoryStore) VisitorProfileSettings() domain.VisitorProfileSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := s.visitorProfileSettings
	out.ThirdPartyKeyMasked = domain.MaskSecret(out.ThirdPartyKey)
	return out
}

// UpdateVisitorProfileSettings 局部更新画像配置（nil=不改，ThirdPartyKey 空串=清除）。
func (s *MemoryStore) UpdateVisitorProfileSettings(patch domain.VisitorProfileSettingsPatch) (domain.VisitorProfileSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	next, err := normalizeVisitorProfileSettings(applyVisitorProfileSettingsPatch(s.visitorProfileSettings, patch))
	if err != nil {
		return s.visitorProfileSettings, err
	}
	now := time.Now().UTC()
	next.UpdatedAt = &now
	s.visitorProfileSettings = next
	out := next
	out.ThirdPartyKeyMasked = domain.MaskSecret(out.ThirdPartyKey)
	return out, nil
}

// VisitorProfile 按访客 ID 读取画像（返回深拷贝，调用方可安全修改）。
func (s *MemoryStore) VisitorProfile(visitorID string) (domain.VisitorProfile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	profile, ok := s.visitorProfiles[strings.TrimSpace(visitorID)]
	if !ok {
		return domain.VisitorProfile{}, false
	}
	return cloneVisitorProfile(*profile), true
}

// UpdateVisitorProfile 在写锁内执行 mutate，保证同访客的并发更新不丢计数。
// mutate 必须是纯内存操作（网络调用请在调用前完成），否则会阻塞整个存储。
func (s *MemoryStore) UpdateVisitorProfile(visitorID string, mutate func(*domain.VisitorProfile) error) (domain.VisitorProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	visitorID = strings.TrimSpace(visitorID)
	if visitorID == "" {
		return domain.VisitorProfile{}, ErrInvalidInput
	}
	current, ok := s.visitorProfiles[visitorID]
	if !ok {
		now := time.Now().UTC()
		current = &domain.VisitorProfile{
			VisitorID:        visitorID,
			NationalityBasis: []string{},
			Languages:        map[string]int{},
			Tags:             []string{},
			Interests:        []string{},
			Evidence:         []domain.VisitorProfileEvidence{},
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		s.visitorProfiles[visitorID] = current
	}
	working := cloneVisitorProfile(*current)
	if mutate != nil {
		if err := mutate(&working); err != nil {
			return cloneVisitorProfile(*current), err
		}
	}
	normalized, err := normalizeVisitorProfileInput(working)
	if err != nil {
		return cloneVisitorProfile(*current), err
	}
	normalized.VisitorID = visitorID
	normalized.CreatedAt = current.CreatedAt
	normalized.UpdatedAt = time.Now().UTC()
	s.visitorProfiles[visitorID] = &normalized
	return cloneVisitorProfile(normalized), nil
}

// VisitorProfiles 按条件筛选并分页返回画像（按最近更新倒序）。
func (s *MemoryStore) VisitorProfiles(query domain.VisitorProfileQuery) ([]domain.VisitorProfile, int) {
	query = normalizeVisitorProfileQuery(query)
	s.mu.RLock()
	defer s.mu.RUnlock()

	matched := make([]domain.VisitorProfile, 0, len(s.visitorProfiles))
	for _, profile := range s.visitorProfiles {
		if !visitorProfileMatchesQuery(*profile, query) {
			continue
		}
		matched = append(matched, cloneVisitorProfile(*profile))
	}
	sortProfilesByUpdated(matched)
	total := len(matched)
	if query.Offset >= total {
		return []domain.VisitorProfile{}, total
	}
	end := query.Offset + query.Limit
	if end > total {
		end = total
	}
	return matched[query.Offset:end], total
}

// ConversationsForVisitor 返回某访客画像键下的全部会话（创建时间倒序）。
// 匹配 conversation.VisitorID（引擎 ID）或 ProfileVisitorID（官网 visitor_ref）：
// 画像键可能是官网 ID，因此两者都要命中。
func (s *MemoryStore) ConversationsForVisitor(visitorID string) []domain.Conversation {
	visitorID = strings.TrimSpace(visitorID)
	if visitorID == "" {
		return []domain.Conversation{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]domain.Conversation, 0, 2)
	for _, conversation := range s.conversations {
		if conversation.VisitorID == visitorID || conversation.ProfileVisitorID == visitorID {
			out = append(out, *conversation)
		}
	}
	sortConversations(out)
	return out
}

// DeleteVisitorProfile 删除画像；不存在时返回 ErrNotFound。
func (s *MemoryStore) DeleteVisitorProfile(visitorID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	visitorID = strings.TrimSpace(visitorID)
	if _, ok := s.visitorProfiles[visitorID]; !ok {
		return ErrNotFound
	}
	delete(s.visitorProfiles, visitorID)
	return nil
}

// pruneVisitorProfileLocked 在会话被删除后清理已无任何会话的访客画像，
// 与"画像随访客数据保留策略清理"的要求对齐（调用方需持有写锁）。
// visitorKey 是画像键：引擎 visitor_id 或官网 visitor_ref，两者都参与存在性判断。
func (s *MemoryStore) pruneVisitorProfileLocked(visitorKey string) {
	visitorKey = strings.TrimSpace(visitorKey)
	if visitorKey == "" {
		return
	}
	for _, conversation := range s.conversations {
		if conversation.VisitorID == visitorKey || conversation.ProfileVisitorID == visitorKey {
			return
		}
	}
	delete(s.visitorProfiles, visitorKey)
}
