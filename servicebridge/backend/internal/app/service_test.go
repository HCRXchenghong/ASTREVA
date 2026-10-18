package app

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

func TestAIReplyTimeoutTracksConfiguredTimeout(t *testing.T) {
	// 未配置或配置小于默认值时，至少给默认 20s，再加翻译余量。
	if got := aiReplyTimeout(domain.AISettings{}); got != defaultAITimeout+translationWindow {
		t.Fatalf("unexpected default ai reply timeout: %s", got)
	}
	if got := aiReplyTimeout(domain.AISettings{TimeoutSeconds: 5}); got != defaultAITimeout+translationWindow {
		t.Fatalf("small configured timeout must fall back to default: %s", got)
	}
	// 配置值更大时，外层超时随之增大，保证 AI 请求不会被提前掐断。
	if got := aiReplyTimeout(domain.AISettings{TimeoutSeconds: 30}); got != 30*time.Second+translationWindow {
		t.Fatalf("configured timeout not honored: %s", got)
	}
	// 上限 150s，避免外层的翻译余量把超时推得过大。
	if got := aiReplyTimeout(domain.AISettings{TimeoutSeconds: 120}); got != maxAIReplyTimeout {
		t.Fatalf("configured timeout must be capped: %s", got)
	}
}

func TestVisitorWebSocketCannotSendToAnotherConversation(t *testing.T) {
	st := store.NewMemoryStore(store.Options{})
	service := NewService(st, realtime.NewHub(), slog.Default())
	visitorA, err := st.CreateVisitorConversation("192.0.2.40", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor A: %v", err)
	}
	visitorB, err := st.CreateVisitorConversation("192.0.2.41", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor B: %v", err)
	}

	client := realtime.NewClient(realtime.RoleVisitor, visitorA.Conversation.ID, visitorA.Conversation.ID, nil, service.hub, nil, slog.Default())
	beforeA := len(st.Messages(visitorA.Conversation.ID))
	beforeB := len(st.Messages(visitorB.Conversation.ID))
	payload, err := json.Marshal(map[string]any{
		"conversation_id": visitorB.Conversation.ID,
		"client_msg_id":   "cross-conversation-1",
		"message_type":    domain.MessageText,
		"content":         "越权写入测试",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	service.handleSocketEvent(client, realtime.IncomingEvent{Event: "message.send", Data: payload})

	if got := len(st.Messages(visitorB.Conversation.ID)); got != beforeB {
		t.Fatalf("cross-conversation message was written: before=%d after=%d", beforeB, got)
	}
	if got := len(st.Messages(visitorA.Conversation.ID)); got != beforeA {
		t.Fatalf("rejected message should not be written to own conversation: before=%d after=%d", beforeA, got)
	}
}

func TestVisitorWebSocketRejectsMismatchedConversationID(t *testing.T) {
	st := store.NewMemoryStore(store.Options{})
	service := NewService(st, realtime.NewHub(), slog.Default())
	visitorA, err := st.CreateVisitorConversation("192.0.2.43", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor A: %v", err)
	}
	visitorB, err := st.CreateVisitorConversation("192.0.2.44", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor B: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(service.ServeWebSocket))
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") +
		"?role=visitor&token=" + url.QueryEscape(visitorA.Token) +
		"&conversation_id=" + url.QueryEscape(visitorB.Conversation.ID)
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected mismatched visitor conversation to be rejected")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("expected 403 for mismatched conversation, got status=%d err=%v", status, err)
	}
}

func TestVisitorWebSocketCanSendToOwnConversation(t *testing.T) {
	st := store.NewMemoryStore(store.Options{})
	service := NewService(st, realtime.NewHub(), slog.Default())
	visitor, err := st.CreateVisitorConversation("192.0.2.42", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor: %v", err)
	}

	client := realtime.NewClient(realtime.RoleVisitor, visitor.Conversation.ID, visitor.Conversation.ID, nil, service.hub, nil, slog.Default())
	before := len(st.Messages(visitor.Conversation.ID))
	payload, err := json.Marshal(map[string]any{
		"client_msg_id": "own-conversation-1",
		"message_type":  domain.MessageText,
		"content":       "自己的会话消息",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	service.handleSocketEvent(client, realtime.IncomingEvent{Event: "message.send", Data: payload})

	if got := len(st.Messages(visitor.Conversation.ID)); got != before+1 {
		t.Fatalf("own conversation message was not written: before=%d after=%d", before, got)
	}
}

func TestVisitorWebSocketCannotCloseAnotherConversation(t *testing.T) {
	st := store.NewMemoryStore(store.Options{})
	service := NewService(st, realtime.NewHub(), slog.Default())
	visitorA, err := st.CreateVisitorConversation("192.0.2.45", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor A: %v", err)
	}
	visitorB, err := st.CreateVisitorConversation("192.0.2.46", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor B: %v", err)
	}

	client := realtime.NewClient(realtime.RoleVisitor, visitorA.Conversation.ID, visitorA.Conversation.ID, nil, service.hub, nil, slog.Default())
	payload, err := json.Marshal(map[string]any{
		"conversation_id": visitorB.Conversation.ID,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	service.handleSocketEvent(client, realtime.IncomingEvent{Event: "conversation.close", Data: payload})

	conversation, ok := st.Conversation(visitorB.Conversation.ID)
	if !ok {
		t.Fatal("conversation B missing")
	}
	if conversation.Status == domain.ConversationClosed {
		t.Fatal("visitor closed another visitor's conversation")
	}
}

func TestVisitorWebSocketCanCloseOwnConversation(t *testing.T) {
	st := store.NewMemoryStore(store.Options{})
	service := NewService(st, realtime.NewHub(), slog.Default())
	visitor, err := st.CreateVisitorConversation("192.0.2.47", "web", "zh", "direct", "")
	if err != nil {
		t.Fatalf("create visitor: %v", err)
	}

	client := realtime.NewClient(realtime.RoleVisitor, visitor.Conversation.ID, visitor.Conversation.ID, nil, service.hub, nil, slog.Default())
	service.handleSocketEvent(client, realtime.IncomingEvent{Event: "conversation.close", Data: []byte(`{}`)})

	conversation, ok := st.Conversation(visitor.Conversation.ID)
	if !ok {
		t.Fatal("conversation missing")
	}
	if conversation.Status != domain.ConversationClosed {
		t.Fatalf("own conversation was not closed: %s", conversation.Status)
	}
}
