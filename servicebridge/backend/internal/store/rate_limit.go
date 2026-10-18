package store

import (
	"errors"
	"fmt"
	"strings"
)

// RateLimitError 表示一次请求触发了 AI 限额。
// Error() 只包含维度与计数，不含访客消息内容；面向访客的提示单独放在 Message，
// 由 HTTP/WS 层原样下发（已按访客语言翻译）。
type RateLimitError struct {
	Dimension string
	Limit     int
	Count     int
	Message   string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limited: dimension=%s count=%d limit=%d", e.Dimension, e.Count, e.Limit)
}

// Is 让 errors.Is(err, ErrRateLimited) 成立，便于 HTTP/WS/开放接口统一映射为 429。
func (e *RateLimitError) Is(target error) bool {
	return target == ErrRateLimited
}

// RateLimitMessage 返回面向访客的限额提示；不是限额错误时返回空串。
func RateLimitMessage(err error) string {
	var target *RateLimitError
	if errors.As(err, &target) {
		return strings.TrimSpace(target.Message)
	}
	return ""
}
