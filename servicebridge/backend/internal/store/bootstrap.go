package store

import (
	"log/slog"
	"strings"
)

// effectiveBootstrapPassword 不再内置 123456 之类的默认口令：
// - 生产环境由 config.Validate 强制要求强口令，走不到这里；
// - 本地/测试未配置时生成本进程随机口令，并只在本机日志提示一次。
// 这样即使误把服务暴露到局域网，也不存在“公开默认口令”可登录。
func effectiveBootstrapPassword(configured, role string, logger *slog.Logger) string {
	if value := strings.TrimSpace(configured); value != "" {
		return value
	}
	generated := randomID(18)
	if logger != nil {
		logger.Warn("未配置 bootstrap 口令，已生成本次进程随机口令；请设置环境变量后重启",
			"role", role, "password", generated)
	}
	return generated
}
