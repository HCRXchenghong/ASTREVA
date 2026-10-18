package ai

import (
	"context"
	"errors"
	"strings"

	"customer-service/backend/internal/domain"
)

// CompleteJSON 执行一次"必须输出 JSON"的内部任务（当前用于访客画像摘要）。
// 与 Reply 的区别：不注入客服人设与联系方式提示词，由调用方给出完整系统提示词；
// 上游返回的文本原样返回，由调用方做严格 JSON 解析（解析失败不得覆盖旧值）。
func (c *Client) CompleteJSON(ctx context.Context, settings domain.AISettings, systemPrompt, userText string) (string, error) {
	userText = strings.TrimSpace(userText)
	if userText == "" {
		return "", errors.New("empty user text")
	}
	if strings.TrimSpace(settings.APIKey) == "" {
		return "", errors.New("AI API key is required for JSON completion")
	}
	return c.complete(ctx, settings, systemPrompt, nil, userText, "")
}
