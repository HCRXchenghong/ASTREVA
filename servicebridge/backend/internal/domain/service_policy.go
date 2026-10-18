package domain

import (
	"strings"
	"time"
)

// ===== 客服策略（批次 4）：排队 / SLA / 超时关闭 / AI 关键词优先级 / 无人值守 =====

// AI 与关键词的优先级模式。
const (
	AIKeywordPriorityKeywordFirst = "keyword_first" // 现行为：先命中关键词，未命中再请求 AI
	AIKeywordPriorityAIFirst      = "ai_first"      // 先请求 AI，AI 不可用时才命中关键词
	AIKeywordPriorityKeywordOnly  = "keyword_only"  // 只走关键词，不请求 AI
	AIKeywordPriorityAIOnly       = "ai_only"       // 只走 AI，忽略关键词
)

// SLA 空闲超时后的处置动作。
const (
	SLAIdleActionAI       = "ai"       // 交回 AI 接待（会话仍属该客服）
	SLAIdleActionReassign = "reassign" // 转其他在线客服
)

// 会话进入排队队列的原因。
const (
	QueueReasonCapacity = "capacity" // 并发已满
	QueueReasonOffline  = "offline"  // 无人在线
	QueueReasonManual   = "manual"   // 管理员手动入队
)

// 黑名单项的种类。
const (
	BlacklistKindVisitor = "visitor"
	BlacklistKindIP      = "ip"
	BlacklistKindAccount = "account"
)

// 策略数值字段的上限：后台误配置时按上限收敛，避免"配置写了 10 年"之类的值。
const (
	ServicePolicyMaxWaitSecondsLimit             = 86400 // 排队最长等待上限：24 小时
	ServicePolicyMaxSLAFirstResponseSecondsLimit = 3600  // 首响超时上限：1 小时
	ServicePolicyMaxSLAIdleMinutesLimit          = 1440  // 空闲回退上限：24 小时
	ServicePolicyMaxAutoCloseIdleMinutesLimit    = 20160 // 自动关闭上限：14 天
	ServicePolicyMaxUnattendedReplyRunes         = 2000  // 无人值守兜底话术上限
)

// DefaultServicePolicy 返回客服策略的默认值。
// 默认值与表结构默认值保持一致，且本批次不接线任何计时器，因此默认值不会改变现有会话流转。
func DefaultServicePolicy() ServicePolicy {
	return ServicePolicy{
		QueueEnabled:            true,
		MaxWaitSeconds:          600,
		SLAFirstResponseSeconds: 60,
		SLAIdleMinutes:          5,
		SLAIdleAction:           SLAIdleActionAI,
		AutoCloseIdleMinutes:    30,
		AIKeywordPriority:       AIKeywordPriorityKeywordFirst,
		UnattendedEnabled:       true,
		UnattendedReply:         "",
		TakeoverPopupEnabled:    true,
	}
}

// ServicePolicy 是客服策略单例（表 service_policy，id=1）。
// 本期只做存储与管理接口，计时器/状态机由下一波接线。
type ServicePolicy struct {
	QueueEnabled bool `json:"queue_enabled"`
	// MaxWaitSeconds 是排队最长等待秒数，超过后转无人值守兜底；0 表示不限制。
	MaxWaitSeconds int `json:"max_wait_seconds"`
	// SLAFirstResponseSeconds 是首次响应的超时秒数；0 表示不提醒。
	SLAFirstResponseSeconds int    `json:"sla_first_response_seconds"`
	SLAIdleMinutes          int    `json:"sla_idle_minutes"`
	SLAIdleAction           string `json:"sla_idle_action"`
	// AutoCloseIdleMinutes 是双方静默多久自动结束会话，0 表示不自动关闭。
	AutoCloseIdleMinutes int    `json:"auto_close_idle_minutes"`
	AIKeywordPriority    string `json:"ai_keyword_priority"`
	UnattendedEnabled    bool   `json:"unattended_enabled"`
	UnattendedReply      string `json:"unattended_reply"`
	TakeoverPopupEnabled bool   `json:"takeover_popup_enabled"`
	// UpdatedAt 是最近一次更新时间；从未写入时为 nil（因此 omitempty）。
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// Normalize 归一化客服策略：非法枚举回落到默认值，负数归零，超上限收敛，话术去首尾空白。
// 存储层在读写两侧都会调用它，保证历史库中的越界值不会影响运行期行为。
func (p ServicePolicy) Normalize() ServicePolicy {
	out := p
	out.MaxWaitSeconds = clampServicePolicyValue(out.MaxWaitSeconds, ServicePolicyMaxWaitSecondsLimit)
	out.SLAFirstResponseSeconds = clampServicePolicyValue(out.SLAFirstResponseSeconds, ServicePolicyMaxSLAFirstResponseSecondsLimit)
	out.SLAIdleMinutes = clampServicePolicyValue(out.SLAIdleMinutes, ServicePolicyMaxSLAIdleMinutesLimit)
	out.AutoCloseIdleMinutes = clampServicePolicyValue(out.AutoCloseIdleMinutes, ServicePolicyMaxAutoCloseIdleMinutesLimit)
	out.SLAIdleAction = NormalizeSLAIdleAction(out.SLAIdleAction)
	out.AIKeywordPriority = NormalizeAIKeywordPriority(out.AIKeywordPriority)
	out.UnattendedReply = truncateRunes(strings.TrimSpace(out.UnattendedReply), ServicePolicyMaxUnattendedReplyRunes)
	// 这里刻意不修改布尔字段：false 是有效配置（例如关闭排队），不能与"未配置"混淆。
	return out
}

// NormalizeAIKeywordPriority 归一化 AI/关键词优先级：空或非法值一律回落到 keyword_first。
func NormalizeAIKeywordPriority(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case AIKeywordPriorityAIFirst:
		return AIKeywordPriorityAIFirst
	case AIKeywordPriorityKeywordOnly:
		return AIKeywordPriorityKeywordOnly
	case AIKeywordPriorityAIOnly:
		return AIKeywordPriorityAIOnly
	default:
		return AIKeywordPriorityKeywordFirst
	}
}

// ValidAIKeywordPriority 判断优先级模式是否为允许的取值。
func ValidAIKeywordPriority(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case AIKeywordPriorityKeywordFirst, AIKeywordPriorityAIFirst, AIKeywordPriorityKeywordOnly, AIKeywordPriorityAIOnly:
		return true
	default:
		return false
	}
}

// NormalizeSLAIdleAction 归一化空闲处置动作：空或非法值一律回落到 ai。
func NormalizeSLAIdleAction(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case SLAIdleActionReassign:
		return SLAIdleActionReassign
	default:
		return SLAIdleActionAI
	}
}

// ValidSLAIdleAction 判断空闲处置动作是否为允许的取值。
func ValidSLAIdleAction(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case SLAIdleActionAI, SLAIdleActionReassign:
		return true
	default:
		return false
	}
}

// NormalizeQueueReason 归一化入队原因：未知值回落到 manual。
func NormalizeQueueReason(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case QueueReasonCapacity:
		return QueueReasonCapacity
	case QueueReasonOffline:
		return QueueReasonOffline
	default:
		return QueueReasonManual
	}
}

// ValidBlacklistKind 判断黑名单种类是否合法（visitor | ip | account）。
func ValidBlacklistKind(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case BlacklistKindVisitor, BlacklistKindIP, BlacklistKindAccount:
		return true
	default:
		return false
	}
}

// QueuedConversation 是排队队列中的一条会话。
type QueuedConversation struct {
	ConversationID string    `json:"conversation_id"`
	GroupName      string    `json:"group_name"`
	Priority       int       `json:"priority"` // 越大越优先
	EnqueuedAt     time.Time `json:"enqueued_at"`
	Reason         string    `json:"reason"`
}

// BlacklistEntry 是一条黑名单项。
type BlacklistEntry struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"` // visitor | ip | account
	Value     string     `json:"value"`
	Reason    string     `json:"reason,omitempty"`
	CreatedBy string     `json:"created_by,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// Expired 判断黑名单项在给定时刻是否已过期；ExpiresAt 为空表示永不过期。
func (b BlacklistEntry) Expired(now time.Time) bool {
	return b.ExpiresAt != nil && !b.ExpiresAt.After(now)
}

func clampServicePolicyValue(value, max int) int {
	if value < 0 {
		return 0
	}
	if value > max {
		return max
	}
	return value
}

// truncateRunes 按字符（rune）截断，避免截断多字节字符。
func truncateRunes(value string, max int) string {
	if max <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}
