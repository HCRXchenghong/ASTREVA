package app

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

// newAILimitsService 构造关闭自动 AI 回复（manual_only）的服务实例：
// AddVisitorMessage 不会启动 AI goroutine，涉及上游调用的用例直接同步调用 generateAIReply。
func newAILimitsService(t *testing.T) (*Service, store.Store) {
	t.Helper()
	st := store.NewMemoryStore(store.Options{BootstrapAdminPassword: "123456", BootstrapAgentPassword: "123456"})
	if _, err := st.UpdateAISettings(domain.AISettings{
		Enabled: true,
		Mode:    domain.AIModeManualOnly,
	}); err != nil {
		t.Fatalf("update ai settings: %v", err)
	}
	return NewService(st, realtime.NewHub(), slog.Default()), st
}

// updateLimits 写入测试限额并断言成功。
func updateLimits(t *testing.T, st store.Store, limits domain.AILimits) {
	t.Helper()
	if _, err := st.UpdateAILimits(limits); err != nil {
		t.Fatalf("update ai limits: %v", err)
	}
}

// findLimitAudit 返回指定会话最近一次超限审计事件。
func findLimitAudit(st store.Store, conversationID string) (domain.AuditEvent, bool) {
	for _, event := range st.RecentAuditEvents(50) {
		if event.Action == limitActionExceeded && event.ResourceID == conversationID {
			return event, true
		}
	}
	return domain.AuditEvent{}, false
}

// TestVisitorMessageSlidingWindowLimits 覆盖滑动窗口核心语义：
// 第 N+1 条被拒且不落库、不同会话/访客互不影响、窗口过期后恢复、超限记审计。
func TestVisitorMessageSlidingWindowLimits(t *testing.T) {
	service, st := newAILimitsService(t)
	fakeNow := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	service.limitTracker.visitor.now = func() time.Time { return fakeNow }

	limits := domain.DefaultAILimits()
	limits.VisitorMessagesPerMinute = 2
	limits.VisitorMessagesPerDay = 0
	limits.AICallsPerConversationDay = 0
	limits.AIReplyMaxChars = 0
	limits.ExceedMessage = "发送太频繁了，请稍后再试。"
	updateLimits(t, st, limits)

	visitor, err := st.CreateVisitorConversation("192.0.2.11", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor conversation: %v", err)
	}
	conversationID := visitor.Conversation.ID

	for index := 0; index < 2; index++ {
		if _, err := service.AddVisitorMessage(conversationID, fmt.Sprintf("m%d", index), "你好", domain.MessageText); err != nil {
			t.Fatalf("第 %d 条消息应被接受：%v", index+1, err)
		}
	}
	before := len(st.Messages(conversationID))

	_, err = service.AddVisitorMessage(conversationID, "m-rejected", "你好", domain.MessageText)
	if !errors.Is(err, store.ErrRateLimited) {
		t.Fatalf("第 3 条消息应触发限额，实际 %v", err)
	}
	if got := store.RateLimitMessage(err); got != limits.ExceedMessage {
		t.Fatalf("超限提示应为配置文案 %q，实际 %q", limits.ExceedMessage, got)
	}
	if got := len(st.Messages(conversationID)); got != before {
		t.Fatalf("被拒消息不得落库：before=%d after=%d", before, got)
	}
	if event, ok := findLimitAudit(st, conversationID); !ok {
		t.Fatal("超限必须记录 ai.limit_exceeded 审计事件")
	} else if !strings.Contains(event.Description, domain.AILimitDimensionVisitorMinute) ||
		!strings.Contains(event.Description, "count=3") ||
		!strings.Contains(event.Description, "limit=2") {
		t.Fatalf("审计事件缺少维度与计数：%q", event.Description)
	}

	// 不同会话 / 不同访客使用独立窗口，互不影响。
	other, err := st.CreateVisitorConversation("192.0.2.12", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create other visitor conversation: %v", err)
	}
	if _, err := service.AddVisitorMessage(other.Conversation.ID, "o1", "你好", domain.MessageText); err != nil {
		t.Fatalf("不同会话不应受影响：%v", err)
	}

	// 窗口过期后恢复：推进 61 秒后，旧的分钟级事件全部退出窗口。
	fakeNow = fakeNow.Add(61 * time.Second)
	if _, err := service.AddVisitorMessage(conversationID, "m-recovered", "你好", domain.MessageText); err != nil {
		t.Fatalf("窗口过期后应恢复：%v", err)
	}
}

// TestVisitorMessageMinuteAndDayLimits 分别验证每分钟与每天两个维度独立生效。
func TestVisitorMessageMinuteAndDayLimits(t *testing.T) {
	t.Run("per_minute", func(t *testing.T) {
		service, st := newAILimitsService(t)
		fakeNow := time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
		service.limitTracker.visitor.now = func() time.Time { return fakeNow }

		limits := domain.DefaultAILimits()
		limits.VisitorMessagesPerMinute = 1
		limits.VisitorMessagesPerDay = 0
		limits.AICallsPerConversationDay = 0
		limits.AIReplyMaxChars = 0
		limits.ExceedMessage = "请稍后再试。"
		updateLimits(t, st, limits)

		visitor, err := st.CreateVisitorConversation("192.0.2.21", "web", "zh", "direct", "")
		if err != nil {
			t.Fatalf("create visitor: %v", err)
		}
		conversationID := visitor.Conversation.ID

		if _, err := service.AddVisitorMessage(conversationID, "a1", "你好", domain.MessageText); err != nil {
			t.Fatalf("第 1 条应被接受：%v", err)
		}
		if _, err := service.AddVisitorMessage(conversationID, "a2", "你好", domain.MessageText); !errors.Is(err, store.ErrRateLimited) {
			t.Fatalf("每分钟限额应拒绝第 2 条，实际 %v", err)
		}

		// 超过 1 分钟后同一访客恢复。
		fakeNow = fakeNow.Add(61 * time.Second)
		if _, err := service.AddVisitorMessage(conversationID, "a3", "你好", domain.MessageText); err != nil {
			t.Fatalf("分钟窗口过期后应恢复：%v", err)
		}
	})

	t.Run("per_day", func(t *testing.T) {
		service, st := newAILimitsService(t)
		fakeNow := time.Date(2026, 2, 2, 9, 0, 0, 0, time.UTC)
		service.limitTracker.visitor.now = func() time.Time { return fakeNow }

		limits := domain.DefaultAILimits()
		limits.VisitorMessagesPerMinute = 0
		limits.VisitorMessagesPerDay = 2
		limits.AICallsPerConversationDay = 0
		limits.AIReplyMaxChars = 0
		limits.ExceedMessage = "今日消息已达上限，请明天再试。"
		updateLimits(t, st, limits)

		visitor, err := st.CreateVisitorConversation("192.0.2.22", "web", "zh", "direct", "")
		if err != nil {
			t.Fatalf("create visitor: %v", err)
		}
		conversationID := visitor.Conversation.ID

		for index := 0; index < 2; index++ {
			if _, err := service.AddVisitorMessage(conversationID, fmt.Sprintf("d%d", index), "你好", domain.MessageText); err != nil {
				t.Fatalf("第 %d 条应被接受：%v", index+1, err)
			}
		}
		if _, err := service.AddVisitorMessage(conversationID, "d3", "你好", domain.MessageText); !errors.Is(err, store.ErrRateLimited) {
			t.Fatalf("每天限额应拒绝第 3 条，实际 %v", err)
		}
		// 仅过去两分钟：分钟维度未限制，但每天维度仍然生效。
		fakeNow = fakeNow.Add(2 * time.Minute)
		if _, err := service.AddVisitorMessage(conversationID, "d4", "你好", domain.MessageText); !errors.Is(err, store.ErrRateLimited) {
			t.Fatalf("每天限额在 24 小时内应持续生效，实际 %v", err)
		}
		// 超过 24 小时后恢复。
		fakeNow = fakeNow.Add(25 * time.Hour)
		if _, err := service.AddVisitorMessage(conversationID, "d5", "你好", domain.MessageText); err != nil {
			t.Fatalf("每天窗口过期后应恢复：%v", err)
		}
	})
}

// seedAIServingConversation 创建会话并通过存储层写入一条访客消息，
// 使会话进入 AI 接待状态（generateAIReply 只在 ai_serving/human_requested 下落库 AI 消息）。
// 直接调用存储层不会启动 AI goroutine，避免干扰上游调用计数。
func seedAIServingConversation(t *testing.T, st store.Store, ip string) string {
	t.Helper()
	if _, err := st.UpdateBusinessHours(domain.BusinessHours{Timezone: "Asia/Shanghai", Enabled: false}); err != nil {
		t.Fatalf("update business hours: %v", err)
	}
	visitor, err := st.CreateVisitorConversation(ip, "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor: %v", err)
	}
	conversationID := visitor.Conversation.ID
	if _, err := st.AddVisitorMessage(conversationID, "seed", "你好", domain.MessageText, nil); err != nil {
		t.Fatalf("seed visitor message: %v", err)
	}
	return conversationID
}

// lastAIMessage 返回会话内最后一条 AI 文本消息。
func lastAIMessage(st store.Store, conversationID string) (domain.Message, bool) {
	messages := st.Messages(conversationID)
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].SenderType == domain.SenderAI && messages[index].MessageType == domain.MessageAIText {
			return messages[index], true
		}
	}
	return domain.Message{}, false
}

// TestAIDailyCallLimitStopsUpstream 验证达到每日调用上限后不再请求上游，
// 但仍会落库并下发配置的超限文案，同时记录审计。
func TestAIDailyCallLimitStopsUpstream(t *testing.T) {
	stub, calls, _ := newKnowledgeStub(t, "这是正常的 AI 回复。")
	service, st := newKnowledgeService(t, domain.AIModeAlwaysAI, stub.URL)

	limits := domain.DefaultAILimits()
	limits.VisitorMessagesPerMinute = 0
	limits.VisitorMessagesPerDay = 0
	limits.AICallsPerConversationDay = 1
	limits.AIReplyMaxChars = 0
	limits.ExceedMessage = "AI 今日回复次数已达上限，请稍后再试。"
	updateLimits(t, st, limits)

	conversationID := seedAIServingConversation(t, st, "192.0.2.31")

	service.generateAIReply(conversationID, "", "你好，请介绍一下产品", "zh")
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("首次回复应调用上游 1 次，实际 %d", got)
	}
	reply, ok := lastAIMessage(st, conversationID)
	if !ok || reply.Content != "这是正常的 AI 回复。" {
		t.Fatalf("首次 AI 回复未落库：%#v", reply)
	}

	service.generateAIReply(conversationID, "", "再问一个问题", "zh")
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("达到上限后不得调用上游，累计调用 %d 次", got)
	}
	last, ok := lastAIMessage(st, conversationID)
	if !ok {
		t.Fatal("超限后应落库一条 AI 消息")
	}
	if last.Content != limits.ExceedMessage {
		t.Fatalf("超限文案应为 %q，实际 %q", limits.ExceedMessage, last.Content)
	}
	if event, ok := findLimitAudit(st, conversationID); !ok {
		t.Fatal("AI 调用超限必须记录 ai.limit_exceeded 审计事件")
	} else if !strings.Contains(event.Description, domain.AILimitDimensionAICalls) {
		t.Fatalf("审计事件应记录 AI 调用维度：%q", event.Description)
	}
}

// TestVisitorRateLimitMessageTranslation 验证超限文案按访客语言翻译，失败时回退中文原文。
func TestVisitorRateLimitMessageTranslation(t *testing.T) {
	t.Run("translated", func(t *testing.T) {
		stub, _, _ := newKnowledgeStub(t, "Por favor, espere un momento.")
		service, st := newKnowledgeService(t, domain.AIModeManualOnly, stub.URL)
		limits := domain.DefaultAILimits()
		limits.VisitorMessagesPerMinute = 1
		limits.VisitorMessagesPerDay = 0
		limits.AICallsPerConversationDay = 0
		limits.AIReplyMaxChars = 0
		limits.ExceedMessage = "请稍后再试。"
		updateLimits(t, st, limits)

		visitor, err := st.CreateVisitorConversation("192.0.2.51", "web", "es", "direct", "")
		if err != nil {
			t.Fatalf("create visitor: %v", err)
		}
		conversationID := visitor.Conversation.ID
		if _, err := service.AddVisitorMessage(conversationID, "t1", "Hola, ¿cuál es el precio?", domain.MessageText); err != nil {
			t.Fatalf("第 1 条应被接受：%v", err)
		}
		_, err = service.AddVisitorMessage(conversationID, "t2", "Hola de nuevo", domain.MessageText)
		if !errors.Is(err, store.ErrRateLimited) {
			t.Fatalf("第 2 条应触发限额，实际 %v", err)
		}
		if got := store.RateLimitMessage(err); got != "Por favor, espere un momento." {
			t.Fatalf("超限提示应为译文，实际 %q", got)
		}
	})

	t.Run("fallback_to_chinese", func(t *testing.T) {
		// 空译文视为翻译失败：必须回退中文原文，不能让访客拿不到提示。
		stub, _, _ := newKnowledgeStub(t, "")
		service, st := newKnowledgeService(t, domain.AIModeManualOnly, stub.URL)
		limits := domain.DefaultAILimits()
		limits.VisitorMessagesPerMinute = 1
		limits.VisitorMessagesPerDay = 0
		limits.AICallsPerConversationDay = 0
		limits.AIReplyMaxChars = 0
		limits.ExceedMessage = "请稍后再试。"
		updateLimits(t, st, limits)

		visitor, err := st.CreateVisitorConversation("192.0.2.52", "web", "es", "direct", "")
		if err != nil {
			t.Fatalf("create visitor: %v", err)
		}
		conversationID := visitor.Conversation.ID
		if _, err := service.AddVisitorMessage(conversationID, "f1", "Hola, ¿cuál es el precio?", domain.MessageText); err != nil {
			t.Fatalf("第 1 条应被接受：%v", err)
		}
		_, err = service.AddVisitorMessage(conversationID, "f2", "Hola de nuevo", domain.MessageText)
		if !errors.Is(err, store.ErrRateLimited) {
			t.Fatalf("第 2 条应触发限额，实际 %v", err)
		}
		if got := store.RateLimitMessage(err); got != limits.ExceedMessage {
			t.Fatalf("翻译失败应回退中文原文 %q，实际 %q", limits.ExceedMessage, got)
		}
	})
}

// TestTruncateAIReply 覆盖按句末标点截断、硬截断补省略号与不限制三种情况。
func TestTruncateAIReply(t *testing.T) {
	long := "没有标点的长文本内容"
	cases := []struct {
		name string
		text string
		max  int
		want string
	}{
		{name: "not_limited", text: "短回复", max: 0, want: "短回复"},
		{name: "under_limit", text: "短回复", max: 10, want: "短回复"},
		{name: "sentence_boundary", text: "第一句。第二句。第三句。", max: 8, want: "第一句。第二句。"},
		{name: "english_sentence", text: "Hello world. This is long.", max: 13, want: "Hello world."},
		{name: "newline_boundary", text: "line1\nline2", max: 6, want: "line1"},
		{name: "hard_truncate", text: long, max: 5, want: "没有标点…"},
		{name: "single_char_limit", text: "abcdef", max: 1, want: "a"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := truncateAIReply(testCase.text, testCase.max)
			if got != testCase.want {
				t.Fatalf("truncateAIReply(%q, %d) = %q, want %q", testCase.text, testCase.max, got, testCase.want)
			}
			if testCase.max > 0 && len([]rune(got)) > testCase.max {
				t.Fatalf("截断结果超过上限：got=%d max=%d", len([]rune(got)), testCase.max)
			}
		})
	}
}

// TestGenerateAIReplyTruncatesLongReply 验证 AI 回复在落库前被截断到上限以内。
func TestGenerateAIReplyTruncatesLongReply(t *testing.T) {
	longReply := "这是一段很长的回复。第二句也很长。第三句。"
	stub, _, _ := newKnowledgeStub(t, longReply)
	service, st := newKnowledgeService(t, domain.AIModeAlwaysAI, stub.URL)

	limits := domain.DefaultAILimits()
	limits.VisitorMessagesPerMinute = 0
	limits.VisitorMessagesPerDay = 0
	limits.AICallsPerConversationDay = 0
	limits.AIReplyMaxChars = 10
	updateLimits(t, st, limits)

	conversationID := seedAIServingConversation(t, st, "192.0.2.41")

	service.generateAIReply(conversationID, "", "请详细介绍一下", "zh")

	reply, ok := lastAIMessage(st, conversationID)
	if !ok {
		t.Fatal("AI 回复未落库")
	}
	content := reply.Content
	if got := len([]rune(content)); got > limits.AIReplyMaxChars {
		t.Fatalf("AI 回复未按上限截断：got=%d max=%d content=%q", got, limits.AIReplyMaxChars, content)
	}
	if !strings.HasSuffix(content, "。") {
		t.Fatalf("应优先在句末标点处截断：%q", content)
	}
	if content != "这是一段很长的回复。" {
		t.Fatalf("截断结果不符：%q", content)
	}
}
