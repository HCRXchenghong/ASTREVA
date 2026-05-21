package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"customer-service/backend/internal/domain"
)

type WebhookNotifier struct {
	url    string
	bearer string
	client *http.Client
}

type WebhookPayload struct {
	Notification domain.AgentNotification `json:"notification"`
	Devices      []domain.PushDevice      `json:"devices"`
}

func NewWebhookNotifier(url string, bearer string, timeout time.Duration) *WebhookNotifier {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &WebhookNotifier{
		url:    url,
		bearer: strings.TrimSpace(bearer),
		client: &http.Client{Timeout: timeout},
	}
}

func (n *WebhookNotifier) NotifyAgent(ctx context.Context, notification domain.AgentNotification, devices []domain.PushDevice) error {
	if n == nil || n.url == "" || len(devices) == 0 {
		return nil
	}
	payload, err := json.Marshal(WebhookPayload{Notification: notification, Devices: devices})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if n.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+n.bearer)
	}
	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return errors.New("push webhook returned " + resp.Status)
	}
	return nil
}
