package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"customer-service/backend/internal/domain"
)

type Client struct {
	httpClient *http.Client
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) Reply(ctx context.Context, settings domain.AISettings, contacts domain.ContactSettings, userText string) (string, error) {
	userText = strings.TrimSpace(userText)
	if userText == "" {
		return "", errors.New("empty user text")
	}
	if settings.APIKey == "" {
		return fallbackReply(userText, contacts), nil
	}

	timeout := time.Duration(settings.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	apiType := strings.TrimSpace(settings.APIType)
	if apiType == "" {
		apiType = "chat_completions"
	}
	if apiType == "responses" {
		return c.responses(ctx, settings, contacts, userText)
	}
	if apiType == "anthropic_messages" {
		return c.anthropicMessages(ctx, settings, contacts, userText)
	}
	if apiType == "chat_completions" {
		return c.chatCompletions(ctx, settings, contacts, userText)
	}
	return "", fmt.Errorf("unsupported AI API type: %s", apiType)
}

func (c *Client) Models(ctx context.Context, settings domain.AISettings) ([]string, error) {
	if strings.TrimSpace(settings.APIKey) == "" {
		return nil, errors.New("AI API key is required")
	}
	timeout := time.Duration(settings.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, normalizeBaseURL(settings.BaseURL)+"/models", nil)
	if err != nil {
		return nil, err
	}
	setAPIHeaders(req, settings)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, responseError(resp)
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return nil, err
	}
	models := make([]string, 0, len(out.Data))
	seen := map[string]bool{}
	for _, item := range out.Data {
		id := strings.TrimSpace(item.ID)
		if id != "" && !seen[id] {
			seen[id] = true
			models = append(models, id)
		}
	}
	if len(models) == 0 {
		return nil, errors.New("upstream returned no models")
	}
	return models, nil
}

func (c *Client) chatCompletions(ctx context.Context, settings domain.AISettings, contacts domain.ContactSettings, userText string) (string, error) {
	baseURL := normalizeBaseURL(settings.BaseURL)
	body := map[string]any{
		"model":       nonEmpty(settings.Model, "gpt-4o-mini"),
		"temperature": settings.Temperature,
		"messages": []map[string]string{
			{"role": "system", "content": enrichPrompt(settings.SystemPrompt, contacts)},
			{"role": "user", "content": userText},
		},
	}
	if settings.MaxOutputTokens > 0 {
		body["max_tokens"] = settings.MaxOutputTokens
	}

	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	setAPIHeaders(req, settings)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", responseError(resp)
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", errors.New("empty AI response")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

func (c *Client) responses(ctx context.Context, settings domain.AISettings, contacts domain.ContactSettings, userText string) (string, error) {
	baseURL := normalizeBaseURL(settings.BaseURL)
	body := map[string]any{
		"model":        nonEmpty(settings.Model, "gpt-4o-mini"),
		"instructions": enrichPrompt(settings.SystemPrompt, contacts),
		"input":        userText,
		"temperature":  settings.Temperature,
	}
	if settings.MaxOutputTokens > 0 {
		body["max_output_tokens"] = settings.MaxOutputTokens
	}

	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/responses", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	setAPIHeaders(req, settings)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", responseError(resp)
	}

	var out struct {
		OutputText string `json:"output_text"`
		Output     []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.OutputText) != "" {
		return strings.TrimSpace(out.OutputText), nil
	}
	for _, item := range out.Output {
		for _, content := range item.Content {
			if strings.TrimSpace(content.Text) != "" {
				return strings.TrimSpace(content.Text), nil
			}
		}
	}
	return "", errors.New("empty AI response")
}

func (c *Client) anthropicMessages(ctx context.Context, settings domain.AISettings, contacts domain.ContactSettings, userText string) (string, error) {
	body := map[string]any{
		"model":       nonEmpty(settings.Model, "claude-3-5-haiku-latest"),
		"max_tokens":  maxInt(settings.MaxOutputTokens, 512),
		"temperature": settings.Temperature,
		"system":      enrichPrompt(settings.SystemPrompt, contacts),
		"messages": []map[string]string{
			{"role": "user", "content": userText},
		},
	}
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, normalizeBaseURL(settings.BaseURL)+"/messages", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	setAPIHeaders(req, settings)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", responseError(resp)
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	for _, item := range out.Content {
		if item.Type == "text" && strings.TrimSpace(item.Text) != "" {
			return strings.TrimSpace(item.Text), nil
		}
	}
	return "", errors.New("empty AI response")
}

func setAPIHeaders(req *http.Request, settings domain.AISettings) {
	if strings.TrimSpace(settings.APIType) == "anthropic_messages" {
		req.Header.Set("x-api-key", settings.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+settings.APIKey)
	}
	req.Header.Set("Accept", "application/json")
}

func maxInt(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func normalizeBaseURL(value string) string {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" {
		return "https://api.openai.com/v1"
	}
	return value
}

func nonEmpty(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func enrichPrompt(prompt string, contacts domain.ContactSettings) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = "你是公司的在线客服助手，请用简体中文礼貌、简洁地回答访客问题。"
	}
	return fmt.Sprintf("%s\n\n公司联系方式：电话 %s，WhatsApp %s，Telegram %s。", prompt, contacts.Telephone, contacts.WhatsApp, contacts.Telegram)
}

func responseError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return fmt.Errorf("AI API status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
}

func fallbackReply(text string, contacts domain.ContactSettings) string {
	if strings.Contains(text, "电话") {
		return "客服电话：" + contacts.Telephone + "，您可以点击下方电话联系按钮直接拨打。"
	}
	lowerText := strings.ToLower(text)
	if strings.Contains(lowerText, "whatsapp") {
		return "官方 WhatsApp：" + contacts.WhatsApp + "，您可以长按复制添加。"
	}
	if strings.Contains(lowerText, "telegram") {
		return "官方 Telegram：" + contacts.Telegram + "，您可以长按复制添加。"
	}
	return "我已收到您的问题。您可以继续补充细节；如需真人处理，请输入“人工客服”。"
}
