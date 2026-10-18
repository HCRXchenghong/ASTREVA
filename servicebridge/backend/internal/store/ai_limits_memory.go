package store

import (
	"time"

	"customer-service/backend/internal/domain"
)

// AILimits 返回内存中的 AI 限额配置（启动时已填充默认值）。
func (s *MemoryStore) AILimits() domain.AILimits {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.aiLimits
}

// UpdateAILimits 覆盖保存限额配置；归一化在存储层统一完成，
// 保证直接调用存储的路径（测试、内部任务）也不会写入越界值。
func (s *MemoryStore) UpdateAILimits(next domain.AILimits) (domain.AILimits, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized, err := normalizeAILimits(next)
	if err != nil {
		return s.aiLimits, err
	}
	now := time.Now().UTC()
	normalized.UpdatedAt = &now
	s.aiLimits = normalized
	return s.aiLimits, nil
}
