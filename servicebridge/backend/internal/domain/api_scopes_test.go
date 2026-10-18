package domain

import (
	"strings"
	"testing"
)

// TestNormalizeAPIScopes 校验权限范围的定义、去重、排序与失败语义。
func TestNormalizeAPIScopes(t *testing.T) {
	if len(AllAPIScopes()) != 7 {
		t.Fatalf("权限范围应为 7 个，实际 %d", len(AllAPIScopes()))
	}
	all := AllAPIScopes()
	// 调用方修改返回值不得影响包级定义。
	all[0] = "hacked"
	if AllAPIScopes()[0] != APIScopeMeRead {
		t.Fatal("AllAPIScopes 必须返回副本")
	}

	for _, scope := range []string{
		APIScopeMeRead, APIScopeConversationsRead, APIScopeConversationsCreate,
		APIScopeConversationsClose, APIScopeMessagesRead, APIScopeMessagesWrite, APIScopeEventsSubscribe,
	} {
		if !ValidAPIScope(scope) {
			t.Fatalf("%s 应为合法范围", scope)
		}
	}
	if ValidAPIScope("bogus:scope") {
		t.Fatal("未知范围不应通过校验")
	}

	// 空数组 = 不受限。
	empty, err := NormalizeAPIScopes(nil)
	if err != nil || len(empty) != 0 || empty == nil {
		t.Fatalf("空 scopes 应为非 nil 空切片，实际 %v err=%v", empty, err)
	}

	// 去重 + 自动补 me:read + 按标准顺序排列。
	normalized, err := NormalizeAPIScopes([]string{APIScopeMessagesWrite, APIScopeConversationsRead, APIScopeMessagesWrite})
	if err != nil {
		t.Fatalf("归一失败：%v", err)
	}
	want := []string{APIScopeMeRead, APIScopeConversationsRead, APIScopeMessagesWrite}
	if strings.Join(normalized, ",") != strings.Join(want, ",") {
		t.Fatalf("归一结果 = %v，期望 %v", normalized, want)
	}

	// 非法范围返回中文说明。
	if _, err := NormalizeAPIScopes([]string{APIScopeMeRead, "bad"}); err == nil || !strings.Contains(err.Error(), "不支持的权限范围") {
		t.Fatalf("非法范围应返回中文错误，实际 %v", err)
	}
}

// TestEffectiveAndHasAPIScope 校验「空 = 全部」的兼容语义与 me:read 不可取消。
func TestEffectiveAndHasAPIScope(t *testing.T) {
	if got := EffectiveAPIScopes([]string{}); len(got) != len(AllAPIScopes()) {
		t.Fatalf("空 scopes 应生效为全部范围，实际 %v", got)
	}
	effective := EffectiveAPIScopes([]string{APIScopeMessagesRead})
	if len(effective) != 2 || effective[0] != APIScopeMeRead || effective[1] != APIScopeMessagesRead {
		t.Fatalf("生效范围应自动含 me:read，实际 %v", effective)
	}

	if !HasAPIScope(nil, APIScopeEventsSubscribe) {
		t.Fatal("空 scopes（不受限）应放行任意范围")
	}
	if !HasAPIScope([]string{APIScopeConversationsRead}, APIScopeMeRead) {
		t.Fatal("me:read 在任何收窄组合下都应可用")
	}
	if HasAPIScope([]string{APIScopeConversationsRead}, APIScopeMessagesWrite) {
		t.Fatal("未授予的 messages:write 不应放行")
	}
}
