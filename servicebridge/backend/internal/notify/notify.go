package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type Message struct {
	AgentID        string
	ConversationID string
	Title          string
	Body           string
	Data           map[string]string
}

type Notifier interface {
	Notify(ctx context.Context, message Message) error
}

type DisabledNotifier struct{}

func (DisabledNotifier) Notify(context.Context, Message) error { return nil }

type LoggingNotifier struct {
	Logger *slog.Logger
}

func (n LoggingNotifier) Notify(_ context.Context, message Message) error {
	if n.Logger == nil {
		n.Logger = slog.Default()
	}
	n.Logger.Info("push notification queued (no provider configured)",
		"agent_id", message.AgentID,
		"conversation_id", message.ConversationID,
		"title", message.Title,
	)
	return nil
}

type FCMNotifier struct {
	Endpoint    string
	AccessToken string
	Logger      *slog.Logger
	Client      *http.Client
}

func (n FCMNotifier) Notify(ctx context.Context, message Message) error {
	if strings.TrimSpace(n.AccessToken) == "" {
		return nil
	}
	endpoint := strings.TrimSpace(n.Endpoint)
	if endpoint == "" {
		endpoint = "https://fcm.googleapis.com/v1/projects/-/messages:send"
	}
	payload := map[string]any{
		"message": map[string]any{
			"token": message.AgentID,
			"notification": map[string]any{
				"title": message.Title,
				"body":  message.Body,
			},
			"data": message.Data,
			"android": map[string]any{
				"priority": "high",
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+n.AccessToken)
	client := n.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("fcm send failed: %s", resp.Status)
	}
	return nil
}
