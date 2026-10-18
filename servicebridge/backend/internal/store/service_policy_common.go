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
