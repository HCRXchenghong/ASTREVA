package store

import (
	"fmt"
	"strings"
	"time"

	"customer-service/backend/internal/domain"
)

// ===== 客服策略相关存储的共用校验/归一（memory 与 postgres 两个实现共享）=====

// normalizeBlacklistEntry 归一化黑名单项：种类必须合法、值不能为空，
// ip 类值按既有 normalizeIP 规则清洗（去掉端口、取首个地址）。
func normalizeBlacklistEntry(entry domain.BlacklistEntry) (domain.BlacklistEntry, error) {
	kind := strings.ToLower(strings.TrimSpace(entry.Kind))
	if !domain.ValidBlacklistKind(kind) {
		return domain.BlacklistEntry{}, fmt.Errorf("%w: 黑名单种类只允许 visitor、ip、account", ErrInvalidInput)
	}
	value := strings.TrimSpace(entry.Value)
	if value == "" {
		return domain.BlacklistEntry{}, fmt.Errorf("%w: 黑名单值不能为空", ErrInvalidInput)
	}
	if kind == domain.BlacklistKindIP {
		value = normalizeIP(value)
	}
	entry.Kind = kind
	entry.Value = value
	entry.Reason = strings.TrimSpace(entry.Reason)
	entry.CreatedBy = strings.TrimSpace(entry.CreatedBy)
	// ID/创建时间由存储层生成，调用方传入的值被忽略。
	entry.ID = ""
	entry.CreatedAt = time.Time{}
	return entry, nil
}

// normalizeAlertChannelCreate 归一化提醒渠道的入参（kind + config）。
func normalizeAlertChannelCreate(agentID, kind string, config map[string]string) (string, string, map[string]string, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return "", "", nil, fmt.Errorf("%w: 客服 ID 不能为空", ErrInvalidInput)
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	normalized, err := domain.NormalizeAlertChannelConfig(kind, config)
	if err != nil {
		return "", "", nil, fmt.Errorf("%w: %s", ErrInvalidInput, err)
	}
	return agentID, kind, normalized, nil
}

// mergeAlertChannelConfig 合并渠道配置（PATCH 语义）：
//   - 请求里未出现的键保留已保存的值（尤其是密钥类字段，编辑时不必回传）；
//   - 显式传空字符串表示删除该键（清空）；
//   - 其余键覆盖为新值。
//
// 合并结果仍需通过 NormalizeAlertChannelConfig 校验必填项，删掉必填键会返回错误。
func mergeAlertChannelConfig(kind string, current map[string]string, patch map[string]string) (map[string]string, error) {
	merged := copyStringMap(current)
	if merged == nil {
		merged = map[string]string{}
	}
	for key, value := range patch {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" {
			delete(merged, key)
			continue
		}
		merged[key] = value
	}
	return domain.NormalizeAlertChannelConfig(kind, merged)
}

// alertChannelLimitError 返回渠道数量超限的统一错误（HTTP 层映射为 400）。
func alertChannelLimitError() error {
	return fmt.Errorf("%w: 每个客服最多 %d 个提醒渠道", ErrInvalidInput, domain.AlertChannelMaxPerAgent)
}

// copyStringMap 复制字符串 map，避免把内部明文配置的引用泄漏给调用方。
func copyStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	out := make(map[string]string, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

// autoReplyPath 描述一次访客消息在"关键词 vs AI"上的自动回复路径。
type autoReplyPath struct {
	// keywordAllowed 表示允许命中配置的关键词规则并直接回复。
	keywordAllowed bool
	// tryAI 表示允许把消息交给 AI 生成回复（NeedAI=true）。
	tryAI bool
}

// resolveAutoReplyPath 按 ai_keyword_priority 计算关键词与 AI 的先后关系，
// 四种模式的判定集中在这里，便于单测。
//
// aiEnabled 是既有可用性判定（AI 已启用且模式非 manual_only），keyword_first 的现行为依赖它；
// aiConfigured 表示上游地址与密钥都已配置（确实能向上游发请求），
// 用于 ai_first / ai_only 的"AI 不可用则回退关键词"语义。
func resolveAutoReplyPath(priority string, aiEnabled, aiConfigured bool) autoReplyPath {
	switch domain.NormalizeAIKeywordPriority(priority) {
	case domain.AIKeywordPriorityAIFirst:
		// 先 AI：可用时忽略关键词规则；不可用时才走关键词。
		if aiEnabled && aiConfigured {
			return autoReplyPath{keywordAllowed: false, tryAI: true}
		}
		return autoReplyPath{keywordAllowed: true, tryAI: false}
	case domain.AIKeywordPriorityKeywordOnly:
		// 只走关键词：永不请求 AI。
		return autoReplyPath{keywordAllowed: true, tryAI: false}
	case domain.AIKeywordPriorityAIOnly:
		// 只走 AI：忽略关键词规则，AI 不可用时不再回退关键词。
		return autoReplyPath{keywordAllowed: false, tryAI: aiEnabled && aiConfigured}
	default:
		// keyword_first：现行为——命中关键词即回复并短路 AI，未命中才请求 AI。
		return autoReplyPath{keywordAllowed: true, tryAI: aiEnabled}
	}
}

// aiUpstreamConfigured 判断 AI 上游是否具备可调用配置（地址 + 密钥缺一不可）。
func aiUpstreamConfigured(settings domain.AISettings) bool {
	return strings.TrimSpace(settings.BaseURL) != "" && strings.TrimSpace(settings.APIKey) != ""
}
