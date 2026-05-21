package app

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

func TestVisitorWebSocketCannotSendToAnotherConversation(t *testing.T) {
	st := store.NewMemoryStore(store.Options{})
	service := NewService(st, realtime.NewHub(), slog.Default())
	visitorA, err := st.CreateVisitorConversation("192.0.2.40", "web")
	if err != nil {
		t.Fatalf("create visitor A: %v", err)
	}
	visitorB, err := st.CreateVisitorConversation("192.0.2.41", "web")
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
	visitorA, err := st.CreateVisitorConversation("192.0.2.43", "web")
	if err != nil {
		t.Fatalf("create visitor A: %v", err)
	}
	visitorB, err := st.CreateVisitorConversation("192.0.2.44", "web")
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
	visitor, err := st.CreateVisitorConversation("192.0.2.42", "web")
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
	visitorA, err := st.CreateVisitorConversation("192.0.2.45", "web")
	if err != nil {
		t.Fatalf("create visitor A: %v", err)
	}
	visitorB, err := st.CreateVisitorConversation("192.0.2.46", "web")
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
	visitor, err := st.CreateVisitorConversation("192.0.2.47", "web")
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
