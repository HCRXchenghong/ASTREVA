package store

import (
	"errors"
	"fmt"
	"strings"
)

// BlacklistError 表示一次访客请求命中了黑名单（visitor | ip | account）。
//
// Error() 只包含命中的种类，绝不包含被拉黑的值：该错误文本会经 HTTP/WS 原样返回给访客，
// 泄露 kind/value 等于告诉对方"你是被哪个 IP/账号规则拦下的"。面向访客的文案单独放在 Message，
// 由 HTTP/WS 层原样下发（可选按访客语言翻译）。
type BlacklistError struct {
	Kind    string
	Message string
}

func (e *BlacklistError) Error() string {
	return fmt.Sprintf("blacklist blocked: kind=%s", strings.TrimSpace(e.Kind))
}

// Is 让 errors.Is(err, ErrBlacklisted) 成立，便于 HTTP/WS 统一映射为 403。
func (e *BlacklistError) Is(target error) bool {
	return target == ErrBlacklisted
}

// BlacklistMessage 返回面向访客的黑名单提示；不是黑名单错误时返回空串。
func BlacklistMessage(err error) string {
	var target *BlacklistError
	if errors.As(err, &target) {
		return strings.TrimSpace(target.Message)
	}
	return ""
}
