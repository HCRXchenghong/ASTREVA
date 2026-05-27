package feishu

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	ErrDisabled          = errors.New("feishu integration disabled")
	ErrInvalidCallback   = errors.New("invalid feishu callback")
	ErrEncryptedCallback = errors.New("invalid encrypted feishu callback")
)

type Config struct {
	Enabled           bool
	BaseURL           string
	AppID             string
	AppSecret         string
	VerificationToken string
	EncryptKey        string
	DefaultChatID     string
	AgentID           string
	Timeout           time.Duration
}

type Client struct {
	cfg    Config
	client *http.Client
	logger *slog.Logger

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
}

type SentMessage struct {
	MessageID string `json:"message_id"`
	ChatID    string `json:"chat_id"`
}

type Callback struct {
	Challenge string
	Message   *MessageEvent
	Ignored   bool
}

type MessageEvent struct {
	EventID     string
	MessageID   string
	RootID      string
	ParentID    string
	ChatID      string
	MessageType string
	Text        string
	SenderID    string
	SenderType  string
}

func NewClient(cfg Config, logger *slog.Logger) *Client {
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://open.feishu.cn"
	}
	cfg.AppID = strings.TrimSpace(cfg.AppID)
	cfg.AppSecret = strings.TrimSpace(cfg.AppSecret)
	cfg.VerificationToken = strings.TrimSpace(cfg.VerificationToken)
	cfg.EncryptKey = strings.TrimSpace(cfg.EncryptKey)
	cfg.DefaultChatID = strings.TrimSpace(cfg.DefaultChatID)
	cfg.AgentID = strings.TrimSpace(cfg.AgentID)
	if cfg.AgentID == "" {
		cfg.AgentID = "agent_feishu"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 8 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.Timeout},
		logger: logger,
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.cfg.Enabled && c.cfg.AppID != "" && c.cfg.AppSecret != "" && c.cfg.VerificationToken != "" && c.cfg.DefaultChatID != ""
}

func (c *Client) DefaultChatID() string {
	if c == nil {
		return ""
	}
	return c.cfg.DefaultChatID
}

func (c *Client) AgentID() string {
	if c == nil || c.cfg.AgentID == "" {
		return "agent_feishu"
	}
	return c.cfg.AgentID
}

func (c *Client) SendText(ctx context.Context, chatID, text string) (SentMessage, error) {
	if !c.Enabled() {
		return SentMessage{}, ErrDisabled
	}
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		chatID = c.cfg.DefaultChatID
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return SentMessage{}, fmt.Errorf("%w: empty message", ErrInvalidCallback)
	}
	content, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return SentMessage{}, err
	}
	body := map[string]string{
		"receive_id": chatID,
		"msg_type":   "text",
		"content":    string(content),
	}
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			MessageID string `json:"message_id"`
			ChatID    string `json:"chat_id"`
		} `json:"data"`
	}
	if err := c.postTenantJSON(ctx, "/open-apis/im/v1/messages?receive_id_type=chat_id", body, &resp); err != nil {
		return SentMessage{}, err
	}
	if resp.Code != 0 {
		return SentMessage{}, fmt.Errorf("feishu send message failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return SentMessage{MessageID: resp.Data.MessageID, ChatID: nonEmpty(resp.Data.ChatID, chatID)}, nil
}

func (c *Client) ParseCallback(body []byte) (Callback, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return Callback{}, ErrInvalidCallback
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return Callback{}, err
	}
	if rawEncrypt, ok := top["encrypt"]; ok {
		var encrypted string
		if err := json.Unmarshal(rawEncrypt, &encrypted); err != nil {
			return Callback{}, err
		}
		decrypted, err := decryptEvent(c.cfg.EncryptKey, encrypted)
		if err != nil {
			return Callback{}, err
		}
		return c.ParseCallback(decrypted)
	}
	if rawChallenge, ok := top["challenge"]; ok {
		var challenge string
		if err := json.Unmarshal(rawChallenge, &challenge); err != nil {
			return Callback{}, err
		}
		if err := c.verifyTopLevelToken(top); err != nil {
			return Callback{}, err
		}
		return Callback{Challenge: challenge}, nil
	}

	var envelope callbackEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return Callback{}, err
	}
	if envelope.Header.EventType == "" {
		if envelope.Type == "url_verification" && envelope.Challenge != "" {
			if err := c.verifyLegacyToken(envelope.Token); err != nil {
				return Callback{}, err
			}
			return Callback{Challenge: envelope.Challenge}, nil
		}
		return Callback{}, ErrInvalidCallback
	}
	if err := c.verifyHeaderToken(envelope.Header.Token); err != nil {
		return Callback{}, err
	}
	if envelope.Header.EventType != "im.message.receive_v1" {
		return Callback{Ignored: true}, nil
	}
	if envelope.Event.Message.MessageType != "text" {
		return Callback{Ignored: true}, nil
	}
	text := parseTextContent(envelope.Event.Message.Content)
	if strings.TrimSpace(text) == "" {
		return Callback{Ignored: true}, nil
	}
	return Callback{Message: &MessageEvent{
		EventID:     envelope.Header.EventID,
		MessageID:   envelope.Event.Message.MessageID,
		RootID:      envelope.Event.Message.RootID,
		ParentID:    envelope.Event.Message.ParentID,
		ChatID:      envelope.Event.Message.ChatID,
		MessageType: envelope.Event.Message.MessageType,
		Text:        cleanFeishuText(text),
		SenderID:    firstNonEmpty(envelope.Event.Sender.SenderID.OpenID, envelope.Event.Sender.SenderID.UserID, envelope.Event.Sender.SenderID.UnionID),
		SenderType:  envelope.Event.Sender.SenderType,
	}}, nil
}

func (c *Client) verifyTopLevelToken(top map[string]json.RawMessage) error {
	var token string
	if raw, ok := top["token"]; ok {
		_ = json.Unmarshal(raw, &token)
	}
	return c.verifyLegacyToken(token)
}

func (c *Client) verifyLegacyToken(token string) error {
	if !c.Enabled() {
		return ErrDisabled
	}
	if strings.TrimSpace(token) != c.cfg.VerificationToken {
		return ErrInvalidCallback
	}
	return nil
}

func (c *Client) verifyHeaderToken(token string) error {
	if !c.Enabled() {
		return ErrDisabled
	}
	if strings.TrimSpace(token) != c.cfg.VerificationToken {
		return ErrInvalidCallback
	}
	return nil
}

func (c *Client) postTenantJSON(ctx context.Context, path string, input any, output any) error {
	token, err := c.tenantAccessToken(ctx)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("feishu api returned %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	return json.NewDecoder(resp.Body).Decode(output)
}

func (c *Client) tenantAccessToken(ctx context.Context) (string, error) {
	if !c.Enabled() {
		return "", ErrDisabled
	}
	c.mu.Lock()
	if c.token != "" && time.Now().Before(c.tokenExpiry) {
		token := c.token
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()

	payload, err := json.Marshal(map[string]string{
		"app_id":     c.cfg.AppID,
		"app_secret": c.cfg.AppSecret,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/open-apis/auth/v3/tenant_access_token/internal", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("feishu token api returned %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	var out struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Code != 0 || out.TenantAccessToken == "" {
		return "", fmt.Errorf("feishu token api failed: code=%d msg=%s", out.Code, out.Msg)
	}
	expire := time.Duration(out.Expire) * time.Second
	if expire <= 0 {
		expire = 90 * time.Minute
	}
	c.mu.Lock()
	c.token = out.TenantAccessToken
	c.tokenExpiry = time.Now().Add(expire - 5*time.Minute)
	c.mu.Unlock()
	return out.TenantAccessToken, nil
}

type callbackEnvelope struct {
	Challenge string `json:"challenge"`
	Token     string `json:"token"`
	Type      string `json:"type"`
	Header    struct {
		EventID   string `json:"event_id"`
		EventType string `json:"event_type"`
		Token     string `json:"token"`
		AppID     string `json:"app_id"`
		TenantKey string `json:"tenant_key"`
	} `json:"header"`
	Event struct {
		Sender struct {
			SenderType string `json:"sender_type"`
			SenderID   struct {
				OpenID  string `json:"open_id"`
				UserID  string `json:"user_id"`
				UnionID string `json:"union_id"`
			} `json:"sender_id"`
		} `json:"sender"`
		Message struct {
			MessageID   string `json:"message_id"`
			RootID      string `json:"root_id"`
			ParentID    string `json:"parent_id"`
			ChatID      string `json:"chat_id"`
			MessageType string `json:"message_type"`
			Content     string `json:"content"`
		} `json:"message"`
	} `json:"event"`
}

func parseTextContent(content string) string {
	var parsed struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err == nil {
		return parsed.Text
	}
	unescaped, err := url.QueryUnescape(content)
	if err == nil {
		content = unescaped
	}
	if err := json.Unmarshal([]byte(content), &parsed); err == nil {
		return parsed.Text
	}
	return content
}

func cleanFeishuText(value string) string {
	value = strings.TrimSpace(value)
	for {
		start := strings.Index(value, "<at ")
		if start < 0 {
			break
		}
		end := strings.Index(value[start:], "</at>")
		if end < 0 {
			break
		}
		value = strings.TrimSpace(value[:start] + value[start+end+5:])
	}
	return strings.TrimSpace(value)
}

func decryptEvent(encryptKey, encrypted string) ([]byte, error) {
	encryptKey = strings.TrimSpace(encryptKey)
	encrypted = strings.TrimSpace(encrypted)
	if encryptKey == "" || encrypted == "" {
		return nil, ErrEncryptedCallback
	}
	raw, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return nil, err
	}
	if len(raw) < aes.BlockSize*2 || len(raw)%aes.BlockSize != 0 {
		return nil, ErrEncryptedCallback
	}
	key := sha256.Sum256([]byte(encryptKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(raw)-aes.BlockSize)
	mode := cipher.NewCBCDecrypter(block, raw[:aes.BlockSize])
	mode.CryptBlocks(plain, raw[aes.BlockSize:])
	if len(plain) == 0 {
		return nil, ErrEncryptedCallback
	}
	padding := int(plain[len(plain)-1])
	if padding <= 0 || padding > aes.BlockSize || padding > len(plain) {
		return nil, ErrEncryptedCallback
	}
	for _, value := range plain[len(plain)-padding:] {
		if int(value) != padding {
			return nil, ErrEncryptedCallback
		}
	}
	return bytes.TrimSpace(plain[:len(plain)-padding]), nil
}

func nonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	return nonEmpty(values...)
}
