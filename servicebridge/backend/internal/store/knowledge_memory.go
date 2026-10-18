package store

import (
	"sort"
	"strings"
	"time"

	"customer-service/backend/internal/domain"
)

// KnowledgeBases 返回全部知识库，按 sort_order、创建时间、ID 稳定排序。
func (s *MemoryStore) KnowledgeBases() []domain.KnowledgeBase {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]domain.KnowledgeBase, 0, len(s.knowledgeBases))
	for _, base := range s.knowledgeBases {
		out = append(out, *base)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (s *MemoryStore) KnowledgeBase(id string) (domain.KnowledgeBase, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	base, ok := s.knowledgeBases[strings.TrimSpace(id)]
	if !ok {
		return domain.KnowledgeBase{}, false
	}
	return *base, true
}

func (s *MemoryStore) CreateKnowledgeBase(next domain.KnowledgeBase) (domain.KnowledgeBase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized, err := normalizeKnowledgeBase(next)
	if err != nil {
		return domain.KnowledgeBase{}, err
	}
	if normalized.ID == "" {
		normalized.ID = "kb_" + randomID(8)
	}
	if _, exists := s.knowledgeBases[normalized.ID]; exists {
		return domain.KnowledgeBase{}, ErrConflict
	}
	now := time.Now().UTC()
	normalized.CreatedAt = now
	normalized.UpdatedAt = now
	stored := normalized
	s.knowledgeBases[stored.ID] = &stored
	return stored, nil
}

func (s *MemoryStore) UpdateKnowledgeBase(id string, next domain.KnowledgeBase) (domain.KnowledgeBase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current, ok := s.knowledgeBases[strings.TrimSpace(id)]
	if !ok {
		return domain.KnowledgeBase{}, ErrNotFound
	}
	normalized, err := normalizeKnowledgeBase(next)
	if err != nil {
		return domain.KnowledgeBase{}, err
	}
	normalized.ID = current.ID
	normalized.CreatedAt = current.CreatedAt
	normalized.UpdatedAt = time.Now().UTC()
	*current = normalized
	return *current, nil
}

// DeleteKnowledgeBase 删除知识库并级联删除其下全部条目（与 Postgres 的 ON DELETE CASCADE 一致）。
func (s *MemoryStore) DeleteKnowledgeBase(id string) (domain.KnowledgeBase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	baseID := strings.TrimSpace(id)
	base, ok := s.knowledgeBases[baseID]
	if !ok {
		return domain.KnowledgeBase{}, ErrNotFound
	}
	deleted := *base
	for entryID, entry := range s.knowledgeEntries {
		if entry.BaseID == baseID {
			delete(s.knowledgeEntries, entryID)
		}
	}
	delete(s.knowledgeBases, baseID)
	return deleted, nil
}

// KnowledgeEntries 按库与关键词过滤条目，支持 limit/offset 分页。
func (s *MemoryStore) KnowledgeEntries(baseID, query string, limit, offset int) []domain.KnowledgeEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	baseID = strings.TrimSpace(baseID)
	query = strings.ToLower(strings.TrimSpace(query))
	out := make([]domain.KnowledgeEntry, 0, len(s.knowledgeEntries))
	for _, entry := range s.knowledgeEntries {
		if baseID != "" && entry.BaseID != baseID {
			continue
		}
		if query != "" && !knowledgeEntryMatchesQuery(*entry, query) {
			continue
		}
		out = append(out, copyKnowledgeEntry(*entry))
	}
	sortKnowledgeEntries(out)
	if offset < 0 {
		offset = 0
	}
	if offset >= len(out) {
		return []domain.KnowledgeEntry{}
	}
	out = out[offset:]
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *MemoryStore) KnowledgeEntry(id string) (domain.KnowledgeEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.knowledgeEntries[strings.TrimSpace(id)]
	if !ok {
		return domain.KnowledgeEntry{}, false
	}
	return copyKnowledgeEntry(*entry), true
}

func (s *MemoryStore) CreateKnowledgeEntry(next domain.KnowledgeEntry) (domain.KnowledgeEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized, err := normalizeKnowledgeEntry(next)
	if err != nil {
		return domain.KnowledgeEntry{}, err
	}
	if _, ok := s.knowledgeBases[normalized.BaseID]; !ok {
		return domain.KnowledgeEntry{}, ErrNotFound
	}
	if normalized.ID == "" {
		normalized.ID = "ke_" + randomID(8)
	}
	if _, exists := s.knowledgeEntries[normalized.ID]; exists {
		return domain.KnowledgeEntry{}, ErrConflict
	}
	now := time.Now().UTC()
	normalized.CreatedAt = now
	normalized.UpdatedAt = now
	stored := copyKnowledgeEntry(normalized)
	s.knowledgeEntries[stored.ID] = &stored
	return copyKnowledgeEntry(stored), nil
}

func (s *MemoryStore) UpdateKnowledgeEntry(id string, next domain.KnowledgeEntry) (domain.KnowledgeEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current, ok := s.knowledgeEntries[strings.TrimSpace(id)]
	if !ok {
		return domain.KnowledgeEntry{}, ErrNotFound
	}
	normalized, err := normalizeKnowledgeEntry(next)
	if err != nil {
		return domain.KnowledgeEntry{}, err
	}
	if _, ok := s.knowledgeBases[normalized.BaseID]; !ok {
		return domain.KnowledgeEntry{}, ErrNotFound
	}
	normalized.ID = current.ID
	normalized.CreatedAt = current.CreatedAt
	normalized.UpdatedAt = time.Now().UTC()
	*current = copyKnowledgeEntry(normalized)
	return copyKnowledgeEntry(*current), nil
}

func (s *MemoryStore) DeleteKnowledgeEntry(id string) (domain.KnowledgeEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entryID := strings.TrimSpace(id)
	entry, ok := s.knowledgeEntries[entryID]
	if !ok {
		return domain.KnowledgeEntry{}, ErrNotFound
	}
	deleted := copyKnowledgeEntry(*entry)
	delete(s.knowledgeEntries, entryID)
	return deleted, nil
}

// KnowledgeEntriesForBases 返回启用中的知识库下的启用条目；baseIDs 为空表示全部启用库。
func (s *MemoryStore) KnowledgeEntriesForBases(baseIDs []string) []domain.KnowledgeEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	requested := cleanKnowledgeBaseIDs(baseIDs)
	allowed := map[string]bool{}
	for _, baseID := range requested {
		if base, ok := s.knowledgeBases[baseID]; ok && base.Enabled {
			allowed[baseID] = true
		}
	}
	// 只要显式指定了库（即使全部无效），也必须限定在指定集合内，不能回退到全部库。
	restricted := len(requested) > 0
	out := make([]domain.KnowledgeEntry, 0, len(s.knowledgeEntries))
	for _, entry := range s.knowledgeEntries {
		if !entry.Enabled {
			continue
		}
		base, ok := s.knowledgeBases[entry.BaseID]
		if !ok || !base.Enabled {
			continue
		}
		if restricted && !allowed[entry.BaseID] {
			continue
		}
		out = append(out, copyKnowledgeEntry(*entry))
	}
	sortKnowledgeEntries(out)
	return out
}

func (s *MemoryStore) KnowledgeSettings() domain.KnowledgeSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.knowledgeSettings
}

func (s *MemoryStore) UpdateKnowledgeSettings(next domain.KnowledgeSettings) (domain.KnowledgeSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized, err := normalizeKnowledgeSettings(next)
	if err != nil {
		return s.knowledgeSettings, err
	}
	now := time.Now().UTC()
	normalized.UpdatedAt = &now
	s.knowledgeSettings = normalized
	return s.knowledgeSettings, nil
}

// RequestHumanHandoff 静默转人工：状态置为 human_requested 并分配客服，不产生任何消息。
func (s *MemoryStore) RequestHumanHandoff(conversationID string) (domain.Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	conversation, ok := s.conversations[strings.TrimSpace(conversationID)]
	if !ok {
		return domain.Conversation{}, ErrNotFound
	}
	if conversation.Status == domain.ConversationClosed {
		return domain.Conversation{}, ErrForbidden
	}
	conversation.Status = domain.ConversationHumanRequested
	conversation.UpdatedAt = time.Now().UTC()
	s.assignHumanConversationLocked(conversation)
	return *conversation, nil
}

// knowledgeEntryMatchesQuery 后台列表的关键词过滤：标题/正文/分类/关键词包含即命中。
func knowledgeEntryMatchesQuery(entry domain.KnowledgeEntry, lowerQuery string) bool {
	haystacks := []string{entry.Title, entry.Content, entry.Category, strings.Join(entry.Keywords, " ")}
	for _, haystack := range haystacks {
		if strings.Contains(strings.ToLower(haystack), lowerQuery) {
			return true
		}
	}
	return false
}

// sortKnowledgeEntries 统一后台列表顺序：优先级降序 → 更新时间降序 → ID 升序。
func sortKnowledgeEntries(entries []domain.KnowledgeEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Priority != entries[j].Priority {
			return entries[i].Priority > entries[j].Priority
		}
		if !entries[i].UpdatedAt.Equal(entries[j].UpdatedAt) {
			return entries[i].UpdatedAt.After(entries[j].UpdatedAt)
		}
		return entries[i].ID < entries[j].ID
	})
}

// copyKnowledgeEntry 复制条目并复制关键词切片，避免调用方改到内部状态。
func copyKnowledgeEntry(entry domain.KnowledgeEntry) domain.KnowledgeEntry {
	entry.Keywords = append([]string(nil), entry.Keywords...)
	return entry
}
