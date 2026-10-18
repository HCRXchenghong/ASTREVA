package domain

import (
	"fmt"
	"strings"
)

// 开放 API（/open/v1/*）权限范围定义，见 docs/AI_PROVIDERS_AND_SERVICE_POLICY.md 附录 B。
//
// 兼容策略：APIApp.Scopes 为空（nil 或长度为 0）表示「不受限」，等同于全部范围，
// 既有的应用因此行为完全不变；一旦收窄为具体范围，me:read 始终自动保留。
const (
	// APIScopeMeRead 读取授权应用自身信息（GET /open/v1/me），默认给所有应用且不可取消。
	APIScopeMeRead = "me:read"
	// APIScopeConversationsRead 查询会话列表与详情。
	APIScopeConversationsRead = "conversations:read"
	// APIScopeConversationsCreate 创建会话。
	APIScopeConversationsCreate = "conversations:create"
	// APIScopeConversationsClose 关闭会话。
	APIScopeConversationsClose = "conversations:close"
	// APIScopeMessagesRead 拉取会话历史消息。
	APIScopeMessagesRead = "messages:read"
	// APIScopeMessagesWrite 代访客发送消息。
	APIScopeMessagesWrite = "messages:write"
	// APIScopeEventsSubscribe 订阅会话实时事件（WebSocket）。
	APIScopeEventsSubscribe = "events:subscribe"
)

// allAPIScopes 是全部权限范围的标准顺序（与附录 B2 表格一致）。
var allAPIScopes = []string{
	APIScopeMeRead,
	APIScopeConversationsRead,
	APIScopeConversationsCreate,
	APIScopeConversationsClose,
	APIScopeMessagesRead,
	APIScopeMessagesWrite,
	APIScopeEventsSubscribe,
}

// AllAPIScopes 返回全部权限范围（标准顺序）的副本，避免调用方修改包级切片。
func AllAPIScopes() []string {
	out := make([]string, len(allAPIScopes))
	copy(out, allAPIScopes)
	return out
}

// ValidAPIScope 判断是否为受支持的权限范围。
func ValidAPIScope(scope string) bool {
	for _, item := range allAPIScopes {
		if item == scope {
			return true
		}
	}
	return false
}

// APIScopeListText 返回用于错误提示的可选范围清单（顿号分隔）。
func APIScopeListText() string {
	return strings.Join(allAPIScopes, "、")
}

// NormalizeAPIScopes 清洗并校验权限范围：
//   - 空数组 = 不受限（等同全部范围），原样返回空切片；
//   - 去重、去空白，并按标准顺序排列；
//   - 非法范围返回中文错误；
//   - 非空范围自动补齐 me:read（该范围不可取消）。
func NormalizeAPIScopes(scopes []string) ([]string, error) {
	// 统一返回非 nil 空切片，保证 JSON 序列化为 []（而非 null），便于控制台展示「未限制」。
	if len(scopes) == 0 {
		return []string{}, nil
	}
	seen := make(map[string]bool, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if !ValidAPIScope(scope) {
			return nil, fmt.Errorf("不支持的权限范围 %q，可选值：%s", scope, APIScopeListText())
		}
		seen[scope] = true
	}
	if len(seen) == 0 {
		// 只有空白项时视同未设置：不受限。
		return []string{}, nil
	}
	seen[APIScopeMeRead] = true
	out := make([]string, 0, len(seen))
	for _, scope := range allAPIScopes {
		if seen[scope] {
			out = append(out, scope)
		}
	}
	return out, nil
}

// EffectiveAPIScopes 返回应用实际生效的权限范围：
// 空表示「不受限」，展开为全部范围；非空则保证含 me:read 且按标准顺序返回。
func EffectiveAPIScopes(scopes []string) []string {
	normalized, err := NormalizeAPIScopes(scopes)
	if err != nil || len(normalized) == 0 {
		return AllAPIScopes()
	}
	return normalized
}

// HasAPIScope 判断应用是否具备某权限范围；空 scopes（不受限）恒为 true。
func HasAPIScope(scopes []string, scope string) bool {
	if len(scopes) == 0 {
		return true
	}
	for _, item := range scopes {
		if strings.TrimSpace(item) == scope {
			return true
		}
	}
	// me:read 在非空范围中始终保留，即使历史数据缺失也按生效处理。
	return scope == APIScopeMeRead
}
