package store

import (
	"errors"
	"strings"
	"testing"

	"customer-service/backend/internal/domain"
)

// TestAILimitsMemoryStore 覆盖内存实现的默认值、单例覆盖、越界收敛与非法文案校验。
func TestAILimitsMemoryStore(t *testing.T) {
	st := NewMemoryStore(Options{})

	defaults := st.AILimits()
	want := domain.DefaultAILimits()
	if defaults.Enabled != want.Enabled ||
		defaults.VisitorMessagesPerMinute != want.VisitorMessagesPerMinute ||
		defaults.VisitorMessagesPerDay != want.VisitorMessagesPerDay ||
		defaults.AICallsPerConversationDay != want.AICallsPerConversationDay ||
		defaults.AIReplyMaxChars != want.AIReplyMaxChars ||
		defaults.ExceedMessage != want.ExceedMessage {
		t.Fatalf("默认限额不符：got=%+v want=%+v", defaults, want)
	}
	if defaults.UpdatedAt != nil {
		t.Fatalf("未写入时 UpdatedAt 应为 nil：%v", defaults.UpdatedAt)
	}

	updated, err := st.UpdateAILimits(domain.AILimits{
		Enabled:                   false,
		VisitorMessagesPerMinute:  -3,
		VisitorMessagesPerDay:     999999,
		AICallsPerConversationDay: 999999,
		AIReplyMaxChars:           999999,
		ExceedMessage:             "  请稍后再试。  ",
	})
	if err != nil {
		t.Fatalf("更新限额失败：%v", err)
	}
	if updated.Enabled {
		t.Fatal("Enabled 未持久化")
	}
	if updated.VisitorMessagesPerMinute != 0 {
		t.Fatalf("负数应归零，实际 %d", updated.VisitorMessagesPerMinute)
	}
	if updated.VisitorMessagesPerDay != domain.AILimitMaxPerDay {
		t.Fatalf("每日上限应收敛到 %d，实际 %d", domain.AILimitMaxPerDay, updated.VisitorMessagesPerDay)
	}
	if updated.AICallsPerConversationDay != domain.AILimitMaxAICalls {
		t.Fatalf("AI 调用上限应收敛到 %d，实际 %d", domain.AILimitMaxAICalls, updated.AICallsPerConversationDay)
	}
	if updated.AIReplyMaxChars != domain.AILimitMaxReplyChars {
		t.Fatalf("字数上限应收敛到 %d，实际 %d", domain.AILimitMaxReplyChars, updated.AIReplyMaxChars)
	}
	if updated.ExceedMessage != "请稍后再试。" {
		t.Fatalf("提示文案未去空白：%q", updated.ExceedMessage)
	}
	if updated.UpdatedAt == nil {
		t.Fatal("更新后 UpdatedAt 不应为 nil")
	}

	// 单例覆盖：再次读取必须拿到最近一次写入。
	again := st.AILimits()
	if again.Enabled || again.ExceedMessage != "请稍后再试。" || again.VisitorMessagesPerDay != domain.AILimitMaxPerDay {
		t.Fatalf("限额未持久化：%+v", again)
	}

	// 超长提示文案属于非法输入，应显式拒绝而不是静默截断。
	if _, err := st.UpdateAILimits(domain.AILimits{ExceedMessage: strings.Repeat("a", 1001)}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("超长提示文案应返回 ErrInvalidInput，实际 %v", err)
	}
}
