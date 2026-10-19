package store

import (
	"context"
	"time"

	"customer-service/backend/internal/domain"
)

// ===== 数据看板「询盘」只读聚合 =====
//
// 口径（用户已确认）：询盘 = 当天有访客消息的会话数。
// - 只统计 messages.sender_type = 'visitor' 的消息；
// - 同一会话同一天有多条访客消息只计 1 次（按 conversation_id 去重）；
// - 与 site_leads（官网表单提交）完全无关，不要混用。
//
// 这里单独放一个只读窄接口，避免把统计口径塞进 Store 主接口，也避免
// 为了看板去改动会话/消息的写入路径。httpx 侧通过类型断言使用它。

// InquiryCounts 是看板询盘口径的聚合结果。
type InquiryCounts struct {
	Today int `json:"today"`
	Month int `json:"month"`
}

// InquiryCounter 是「有访客消息的会话数」只读聚合能力。
// todayStart/monthStart/monthEnd 必须是带时区的绝对时间点，区间为左闭右开。
type InquiryCounter interface {
	CountInquiryConversations(ctx context.Context, todayStart, monthStart, monthEnd time.Time) (InquiryCounts, error)
}

// CountInquiryConversations 用一条聚合查询完成今日/本月询盘计数。
//
// 注意参数编号：$1/$2/$3 全部在 SQL 中被引用。这里刻意不套用汇总查询的
// 参数偏移，避免重演 "could not determine data type of parameter $1"。
func (s *PostgresStore) CountInquiryConversations(ctx context.Context, todayStart, monthStart, monthEnd time.Time) (InquiryCounts, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, defaultDBTimeout)
	defer cancel()

	var counts InquiryCounts
	err := s.pool.QueryRow(ctx, `
		SELECT
			count(DISTINCT conversation_id) FILTER (
				WHERE sender_type = 'visitor' AND created_at >= $1
			)::int AS today_inquiries,
			count(DISTINCT conversation_id) FILTER (
				WHERE sender_type = 'visitor' AND created_at >= $2 AND created_at < $3
			)::int AS month_inquiries
		FROM messages
	`, todayStart, monthStart, monthEnd).Scan(&counts.Today, &counts.Month)
	if err != nil {
		return InquiryCounts{}, err
	}
	return counts, nil
}

// CountInquiryConversations 是内存存储分支的同口径实现：
// 直接遍历内存里的消息，按 conversation_id 去重。
func (s *MemoryStore) CountInquiryConversations(_ context.Context, todayStart, monthStart, monthEnd time.Time) (InquiryCounts, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	today := make(map[string]struct{})
	month := make(map[string]struct{})
	for conversationID, messages := range s.messages {
		for _, message := range messages {
			if message.SenderType != domain.SenderVisitor {
				continue
			}
			createdAt := message.CreatedAt
			if !createdAt.Before(todayStart) {
				today[conversationID] = struct{}{}
			}
			if !createdAt.Before(monthStart) && createdAt.Before(monthEnd) {
				month[conversationID] = struct{}{}
			}
		}
	}
	return InquiryCounts{Today: len(today), Month: len(month)}, nil
}
