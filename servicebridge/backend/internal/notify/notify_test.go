package notify

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFCMNotifierSendsMessage(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected authorization header: %s", r.Header.Get("Authorization"))
		}
		_ = json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := FCMNotifier{Endpoint: server.URL, AccessToken: "test-token", Logger: slog.Default()}
	err := notifier.Notify(context.Background(), Message{
		AgentID:        "device-token",
		ConversationID: "conv_1",
		Title:          "新消息",
		Body:           "访客发来了新消息",
		Data:           map[string]string{"conversation_id": "conv_1"},
	})
	if err != nil {
		t.Fatalf("notify: %v", err)
	}
	message := received["message"].(map[string]any)
	if message["token"] != "device-token" {
		t.Fatalf("unexpected device token: %v", message["token"])
	}
}

func TestFCMNotifierNoopWithoutToken(t *testing.T) {
	notifier := FCMNotifier{Endpoint: "http://127.0.0.1:1", AccessToken: ""}
	if err := notifier.Notify(context.Background(), Message{}); err != nil {
		t.Fatalf("expected noop without token, got %v", err)
	}
}
