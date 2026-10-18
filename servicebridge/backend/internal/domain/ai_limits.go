package domain

import (
	"strings"
	"time"
)

// AI 限额的维度标识：用于审计事件与错误信息，避免各处散落字符串。
const (
	AILimitDimensionVisitorMinute = "visitor_messages_per_minute"
	AILimitDimensionVisitorDay    = "visitor_messages_per_day"
	AILimitDimensionAICalls       = "ai_calls_per_conversation_day"
)

// AI 限额的上限收敛值：后台配置超过这些值时按上限收敛，防止误配置拖垮服务。
const (
	AILimitMaxPerMinute  = 600
	AILimitMaxPerDay     = 20000
	AILimitMaxAICalls    = 2000
	AILimitMaxReplyChars = 20000
)

// DefaultAILimitExceedMessage 是未配置 exceed_message 时给访客的默认中文提示。
// 其它语言由现有翻译链路按访客语言翻译，失败时回退中文原文。
const DefaultAILimitExceedMessage = "当前咨询请求过于频繁，请稍后再试。"

// DefaultAILimits 返回 AI 限额的默认值。
// 默认值与表结构默认值保持一致：开启限额，但阈值足够宽松，保证普通会话不受影响；
// 任意一项为 0 表示该项不限制。
func DefaultAILimits() AILimits {
	return AILimits{
		Enabled:                   true,
		VisitorMessagesPerMinute:  20,
		VisitorMessagesPerDay:     300,
		AICallsPerConversationDay: 60,
		AIReplyMaxChars:           1200,
		ExceedMessage:             "",
	}
}

// AILimits 是 AI 频率/次数/单次字数的全局限额配置（单例表 ai_limits）。
type AILimits struct {
	// Enabled 为总开关；关闭后所有限额均不生效。
	Enabled bool `json:"enabled"`
	// VisitorMessagesPerMinute 是同一会话+访客+IP 每分钟最多可发送的访客消息数，0 表示不限制。
	VisitorMessagesPerMinute int `json:"visitor_messages_per_minute"`
	// VisitorMessagesPerDay 是同一会话+访客+IP 每天（最近 24 小时）最多可发送的访客消息数，0 表示不限制。
	VisitorMessagesPerDay int `json:"visitor_messages_per_day"`
	// AICallsPerConversationDay 是同一会话每天（最近 24 小时）最多调用 AI 上游的次数，0 表示不限制。
	AICallsPerConversationDay int `json:"ai_calls_per_conversation_day"`
	// AIReplyMaxChars 是单条 AI 回复的最大字符数（按 rune 计），0 表示不限制。
	AIReplyMaxChars int `json:"ai_reply_max_chars"`
	// ExceedMessage 是超限时给访客的提示（默认中文，其它语言走翻译链路）。
	// 空字符串表示使用 DefaultAILimitExceedMessage；数值字段的 0 有业务含义（不限制），
	// 因此这里不使用 omitempty。
	ExceedMessage string `json:"exceed_message"`
	// UpdatedAt 是最近一次更新时间；从未写入时为 nil，因此使用 omitempty。
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// NormalizeAILimits 归一化限额配置：
// 负数归零（零表示不限制），超过上限的值收敛到上限，提示文案去首尾空白。
func NormalizeAILimits(next AILimits) AILimits {
	out := next
	out.VisitorMessagesPerMinute = clampAILimit(out.VisitorMessagesPerMinute, AILimitMaxPerMinute)
	out.VisitorMessagesPerDay = clampAILimit(out.VisitorMessagesPerDay, AILimitMaxPerDay)
	out.AICallsPerConversationDay = clampAILimit(out.AICallsPerConversationDay, AILimitMaxAICalls)
	out.AIReplyMaxChars = clampAILimit(out.AIReplyMaxChars, AILimitMaxReplyChars)
	out.ExceedMessage = strings.TrimSpace(out.ExceedMessage)
	return out
}

// LimitExceedMessageText 返回实际下发给访客的超限提示原文：优先使用配置值，
// 未配置时回退内置中文默认文案。
func LimitExceedMessageText(configured string) string {
	text := strings.TrimSpace(configured)
	if text == "" {
		return DefaultAILimitExceedMessage
	}
	return text
}

// clampAILimit 把限额收敛到 [0, max] 区间。
func clampAILimit(value, max int) int {
	if value < 0 {
		return 0
	}
	if value > max {
		return max
	}
	return value
}
