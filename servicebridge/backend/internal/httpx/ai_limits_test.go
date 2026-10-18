package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"customer-service/backend/internal/domain"
)

// TestAdminAILimitsHTTP 覆盖限额配置接口：鉴权（非管理员 403）、默认值、
// 指针语义的局部更新、越界 400 与审计事件。
func TestAdminAILimitsHTTP(t *testing.T) {
	router, st := newKnowledgeRouter(t)

	// 未登录与非管理员均不可访问。
	doJSON(t, router, http.MethodGet, "/api/admin/ai-limits", "", nil, http.StatusForbidden)
	agentToken := loginAgentPreferencesFixture(t, router, "kf_ai_limits")
	doJSON(t, router, http.MethodGet, "/api/admin/ai-limits", agentToken, nil, http.StatusForbidden)
	doJSON(t, router, http.MethodPatch, "/api/admin/ai-limits", agentToken, map[string]any{
		"visitor_messages_per_minute": 5,
	}, http.StatusForbidden)

	adminToken := loginForToken(t, router, "/api/admin/login", "superadmin", "123456")

	// 默认值必须与 domain.DefaultAILimits() 一致。
	initial := doJSON(t, router, http.MethodGet, "/api/admin/ai-limits", adminToken, nil, http.StatusOK)
	limits, ok := initial["limits"].(map[string]any)
	if !ok {
		t.Fatalf("响应缺少 limits 字段：%#v", initial)
	}
	defaults := domain.DefaultAILimits()
	if limits["enabled"] != true ||
		limits["visitor_messages_per_minute"] != float64(defaults.VisitorMessagesPerMinute) ||
		limits["visitor_messages_per_day"] != float64(defaults.VisitorMessagesPerDay) ||
		limits["ai_calls_per_conversation_day"] != float64(defaults.AICallsPerConversationDay) ||
		limits["ai_reply_max_chars"] != float64(defaults.AIReplyMaxChars) ||
		limits["exceed_message"] != "" {
		t.Fatalf("默认限额不符：%#v", limits)
	}

	// 局部更新：只改指定字段，其余字段保持原值。
	patched := doJSON(t, router, http.MethodPatch, "/api/admin/ai-limits", adminToken, map[string]any{
		"visitor_messages_per_minute": 5,
		"exceed_message":              "请慢一点，稍后再试。",
	}, http.StatusOK)
	updated, ok := patched["limits"].(map[string]any)
	if !ok {
		t.Fatalf("PATCH 响应缺少 limits 字段：%#v", patched)
	}
	if updated["visitor_messages_per_minute"] != float64(5) || updated["exceed_message"] != "请慢一点，稍后再试。" {
		t.Fatalf("局部更新未生效：%#v", updated)
	}
	if updated["visitor_messages_per_day"] != float64(defaults.VisitorMessagesPerDay) ||
		updated["ai_calls_per_conversation_day"] != float64(defaults.AICallsPerConversationDay) ||
		updated["ai_reply_max_chars"] != float64(defaults.AIReplyMaxChars) {
		t.Fatalf("未传字段不应被改写：%#v", updated)
	}
	stored := st.AILimits()
	if stored.VisitorMessagesPerMinute != 5 || stored.ExceedMessage != "请慢一点，稍后再试。" {
		t.Fatalf("限额未持久化：%+v", stored)
	}

	// 越界值选择返回 400（而非静默收敛），管理员可明确看到问题。
	for name, payload := range map[string]map[string]any{
		"negative_per_minute":     {"visitor_messages_per_minute": -1},
		"per_minute_over_cap":     {"visitor_messages_per_minute": domain.AILimitMaxPerMinute + 1},
		"per_day_over_cap":        {"visitor_messages_per_day": domain.AILimitMaxPerDay + 1},
		"ai_calls_over_cap":       {"ai_calls_per_conversation_day": domain.AILimitMaxAICalls + 1},
		"max_chars_over_cap":      {"ai_reply_max_chars": domain.AILimitMaxReplyChars + 1},
		"exceed_message_too_long": {"exceed_message": strings.Repeat("长", 1001)},
		"unknown_field":           {"unknown_field": 1},
	} {
		t.Run(name, func(t *testing.T) {
			doJSON(t, router, http.MethodPatch, "/api/admin/ai-limits", adminToken, payload, http.StatusBadRequest)
		})
	}

	// 允许关闭总开关或把某一项配置为 0（不限制）。
	final := doJSON(t, router, http.MethodPatch, "/api/admin/ai-limits", adminToken, map[string]any{
		"enabled":                     false,
		"visitor_messages_per_minute": 0,
		"ai_reply_max_chars":          0,
	}, http.StatusOK)
	finalLimits := final["limits"].(map[string]any)
	if finalLimits["enabled"] != false || finalLimits["visitor_messages_per_minute"] != float64(0) ||
		finalLimits["ai_reply_max_chars"] != float64(0) {
		t.Fatalf("关闭开关/归零未生效：%#v", finalLimits)
	}

	// 更新动作必须留痕。
	audit := doJSON(t, router, http.MethodGet, "/api/admin/audit-events?limit=50", adminToken, nil, http.StatusOK)
	events, _ := audit["events"].([]any)
	found := false
	for _, item := range events {
		event, _ := item.(map[string]any)
		if event["action"] == "ai_limits.update" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("缺少 ai_limits.update 审计事件：%#v", events)
	}
}

// readSocketEvent 读取并跳过其它事件，直到拿到指定类型的事件。
func readSocketEvent(t *testing.T, conn *websocket.Conn, eventType string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(deadline)
		var payload struct {
			Event string         `json:"event"`
			Data  map[string]any `json:"data"`
		}
		if err := conn.ReadJSON(&payload); err != nil {
			t.Fatalf("读取 WebSocket 事件失败（等待 %s）：%v", eventType, err)
		}
		if payload.Event == eventType {
			return payload.Data
		}
	}
	t.Fatalf("等待 WebSocket 事件 %s 超时", eventType)
	return nil
}

// TestVisitorRateLimitVisibleOverWebSocket 验证超频错误会以 rate_limited 事件
// 与配置文案下发给访客，而不是静默丢弃；被拒消息不落库。
func TestVisitorRateLimitVisibleOverWebSocket(t *testing.T) {
	router, st := newKnowledgeRouter(t)
	if _, err := st.UpdateAISettings(domain.AISettings{
		Enabled: true,
		Mode:    domain.AIModeManualOnly,
	}); err != nil {
		t.Fatalf("update ai settings: %v", err)
	}
	exceedMessage := "发送太频繁了，请稍后再试。"
	if _, err := st.UpdateAILimits(domain.AILimits{
		Enabled:                   true,
		VisitorMessagesPerMinute:  1,
		VisitorMessagesPerDay:     0,
		AICallsPerConversationDay: 0,
		AIReplyMaxChars:           0,
		ExceedMessage:             exceedMessage,
	}); err != nil {
		t.Fatalf("update ai limits: %v", err)
	}

	server := httptest.NewServer(router)
	defer server.Close()

	visitor := doJSON(t, router, http.MethodPost, "/api/visitor/conversations", "", map[string]any{"source": "rate-limit-test"}, http.StatusCreated)
	visitorToken := visitor["token"].(string)
	conversationID := visitor["conversation"].(map[string]any)["id"].(string)

	conn, _ := visitorSocket(t, server.URL, visitorToken, conversationID)
	defer conn.Close()

	sendSocketEvent(t, conn, "message.send", map[string]any{
		"client_msg_id":   "rl-1",
		"conversation_id": conversationID,
		"message_type":    "text",
		"content":         "你好",
	})
	readSocketEvent(t, conn, "message.ack")

	sendSocketEvent(t, conn, "message.send", map[string]any{
		"client_msg_id":   "rl-2",
		"conversation_id": conversationID,
		"message_type":    "text",
		"content":         "再来一条",
	})
	errorData := readSocketEvent(t, conn, "error")
	if errorData["code"] != "rate_limited" {
		t.Fatalf("超限错误码应为 rate_limited：%#v", errorData)
	}
	if errorData["message"] != exceedMessage {
		t.Fatalf("超限提示应为配置文案 %q，实际 %v", exceedMessage, errorData["message"])
	}

	if messages := st.Messages(conversationID); len(messages) != 1 {
		t.Fatalf("被拒消息不得落库：%d", len(messages))
	}
}
