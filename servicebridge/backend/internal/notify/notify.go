// Package notify 负责把客服通知投递到客服设备。
//
// 推送参数（Firebase 服务账号、APNs .p8、客户端 FirebaseOptions）全部由统一后台
// 写入数据库，Provider 每次发送前读取最新配置，因此管理员改完立即生效，不需要重启。
package notify

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"customer-service/backend/internal/domain"
)

// Message 是一条待投递的推送。
type Message struct {
	DeviceToken    string
	Platform       string
	AgentID        string
	ConversationID string
	Title          string
	Body           string
	Data           map[string]string
}

// Notifier 由调用方提供当前推送配置，便于后台改配置后立即生效。
type Notifier interface {
	Notify(ctx context.Context, settings domain.PushSettings, message Message) error
	// SendAlertChannels 把客服提醒投递到该客服配置的提醒渠道（telegram / bark / email）。
	// 单个渠道失败会重试 2 次，最终失败通过 errors.Join 汇总返回；密钥不写入日志。
	SendAlertChannels(ctx context.Context, channels []domain.AgentAlertChannel, payload AlertPayload) error
}

// DisabledNotifier 不做任何事情，用于未接入推送的测试与最小部署。
type DisabledNotifier struct{}

func (DisabledNotifier) Notify(context.Context, domain.PushSettings, Message) error { return nil }

func (DisabledNotifier) SendAlertChannels(context.Context, []domain.AgentAlertChannel, AlertPayload) error {
	return nil
}

type cachedToken struct {
	value     string
	expiresAt time.Time
}

// Provider 是有状态的推送通道实现，内部缓存 OAuth / APNs 令牌。
type Provider struct {
	logger *slog.Logger
	client *http.Client

	// 允许测试替换端点。
	oauthEndpoint       string
	fcmEndpointTemplate string
	apnsEndpoints       map[string]string

	// allowPrivate 仅用于本地/测试；生产环境禁止 Bark 等自定义地址访问私网。
	allowPrivate bool

	// 提醒渠道（批次 4）：端点与重试间隔允许测试替换；sendMail 抽象便于用假 SMTP 交互测试。
	telegramEndpointTemplate string
	barkEndpointTemplate     string
	retryDelays              []time.Duration
	smtpSettings             func() domain.AlertSMTPSettings
	sendMail                 mailSender

	mu     sync.Mutex
	tokens map[string]cachedToken
}

const (
	fcmScope          = "https://www.googleapis.com/auth/firebase.messaging"
	defaultOAuthURL   = "https://oauth2.googleapis.com/token"
	apnsProductionURL = "https://api.push.apple.com"
	apnsSandboxURL    = "https://api.sandbox.push.apple.com"
	oauthSafetyWindow = 2 * time.Minute
	apnsTokenTTL      = 45 * time.Minute
)

// NewProvider 构造推送通道。
func NewProvider(logger *slog.Logger) *Provider {
	if logger == nil {
		logger = slog.Default()
	}
	return &Provider{
		logger:              logger,
		client:              &http.Client{Timeout: 10 * time.Second},
		fcmEndpointTemplate: "https://fcm.googleapis.com/v1/projects/%s/messages:send",
		apnsEndpoints: map[string]string{
			"sandbox":    apnsSandboxURL,
			"production": apnsProductionURL,
		},
		telegramEndpointTemplate: "https://api.telegram.org/bot%s/sendMessage",
		barkEndpointTemplate:     "%s/%s/%s/%s",
		allowPrivate:             true,
		retryDelays:              []time.Duration{200 * time.Millisecond, 600 * time.Millisecond},
		sendMail:                 smtpSendMail,
		tokens:                   map[string]cachedToken{},
	}
}

// SetAllowPrivate 控制是否允许提醒渠道访问 loopback/私网地址。
// 生产环境必须为 false，避免客服配置的 Bark server 变成内网 SSRF。
func (p *Provider) SetAllowPrivate(allow bool) {
	p.allowPrivate = allow
}

// Notify 按设备平台选择通道；未配置任何通道时只记录日志，不影响聊天主流程。
func (p *Provider) Notify(ctx context.Context, settings domain.PushSettings, message Message) error {
	token := strings.TrimSpace(message.DeviceToken)
	if token == "" {
		return nil
	}
	platform := strings.ToLower(strings.TrimSpace(message.Platform))
	if platform == string(domain.PushPlatformIOS) && settings.APNsConfigured {
		err := p.sendAPNs(ctx, settings, message, token)
		if err == nil {
			return nil
		}
		if !settings.FCMConfigured {
			return err
		}
		p.logger.Warn("apns push failed, falling back to fcm", "error", err)
	}
	if settings.FCMConfigured {
		return p.sendFCM(ctx, settings, message, token)
	}
	p.logger.Info("push skipped: no channel configured",
		"agent_id", message.AgentID,
		"conversation_id", message.ConversationID,
		"platform", platform,
	)
	return nil
}

// Validate 只验证凭证可用性，不发送真实推送：FCM 会尝试换取一次性 OAuth 令牌，
// APNs 会校验 .p8 能否签出 JWT。返回逐项的人类可读结果，供后台展示。
func (p *Provider) Validate(ctx context.Context, settings domain.PushSettings) map[string]string {
	result := map[string]string{}
	switch {
	case strings.TrimSpace(settings.FCMServiceAccountJSON) != "":
		if _, err := p.serviceAccountToken(ctx, settings.FCMServiceAccountJSON); err != nil {
			result["fcm"] = "失败：" + err.Error()
		} else {
			result["fcm"] = "通过：服务账号已成功换取 OAuth2 令牌"
		}
	case strings.TrimSpace(settings.FCMAccessToken) != "":
		result["fcm"] = "已配置静态 access token（无法在服务端验证有效性，且会随过期时间失效）"
	default:
		result["fcm"] = "未配置"
	}
	if settings.APNsConfigured {
		if _, err := p.apnsToken(settings); err != nil {
			result["apns"] = "失败：" + err.Error()
		} else {
			result["apns"] = "通过：.p8 已成功签出 APNs JWT"
		}
	} else {
		result["apns"] = "未配置"
	}
	result["client"] = "Firebase 客户端参数已写入后台配置"
	return result
}

func (p *Provider) sendFCM(ctx context.Context, settings domain.PushSettings, message Message, deviceToken string) error {
	accessToken := strings.TrimSpace(settings.FCMAccessToken)
	if accessToken == "" && strings.TrimSpace(settings.FCMServiceAccountJSON) != "" {
		minted, err := p.serviceAccountToken(ctx, settings.FCMServiceAccountJSON)
		if err != nil {
			return err
		}
		accessToken = minted
	}
	if accessToken == "" {
		return nil
	}
	projectID := strings.TrimSpace(settings.FirebaseProjectID)
	if projectID == "" {
		projectID = projectIDFromServiceAccount(settings.FCMServiceAccountJSON)
	}
	if projectID == "" {
		return errors.New("fcm project id is not configured")
	}
	endpoint := fmt.Sprintf(p.fcmEndpointTemplate, url.PathEscape(projectID))
	payload := map[string]any{
		"message": map[string]any{
			"token": deviceToken,
			"notification": map[string]any{
				"title": message.Title,
				"body":  message.Body,
			},
			"data": message.Data,
			"android": map[string]any{
				"priority": "high",
			},
			"apns": map[string]any{
				"headers": map[string]string{"apns-priority": "10"},
				"payload": map[string]any{
					"aps": map[string]any{
						"alert": map[string]string{"title": message.Title, "body": message.Body},
						"sound": "default",
					},
				},
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
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("fcm send failed: %s %s", resp.Status, strings.TrimSpace(string(detail)))
	}
	return nil
}

func (p *Provider) sendAPNs(ctx context.Context, settings domain.PushSettings, message Message, deviceToken string) error {
	jwt, err := p.apnsToken(settings)
	if err != nil {
		return err
	}
	endpoint := p.apnsEndpoints[strings.ToLower(strings.TrimSpace(settings.APNsEnvironment))]
	if endpoint == "" {
		endpoint = apnsProductionURL
	}
	payload := map[string]any{
		"aps": map[string]any{
			"alert": map[string]string{"title": message.Title, "body": message.Body},
			"sound": "default",
		},
	}
	for key, value := range message.Data {
		payload[key] = value
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/3/device/"+url.PathEscape(deviceToken), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "bearer "+jwt)
	req.Header.Set("apns-topic", strings.TrimSpace(settings.APNsTopic))
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", "10")
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("apns send failed: %s %s", resp.Status, strings.TrimSpace(string(detail)))
	}
	return nil
}

// serviceAccountToken 用 Firebase 服务账号换取 OAuth2 access token，并按过期时间缓存。
func (p *Provider) serviceAccountToken(ctx context.Context, serviceAccountJSON string) (string, error) {
	var account struct {
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
		TokenURI    string `json:"token_uri"`
	}
	if err := json.Unmarshal([]byte(serviceAccountJSON), &account); err != nil {
		return "", fmt.Errorf("invalid firebase service account json: %w", err)
	}
	if strings.TrimSpace(account.ClientEmail) == "" || strings.TrimSpace(account.PrivateKey) == "" {
		return "", errors.New("firebase service account requires client_email and private_key")
	}
	cacheKey := "fcm:" + account.ClientEmail
	if cached, ok := p.lookupToken(cacheKey); ok {
		return cached, nil
	}
	tokenURL := strings.TrimSpace(account.TokenURI)
	if p.oauthEndpoint != "" {
		tokenURL = p.oauthEndpoint
	}
	if tokenURL == "" {
		tokenURL = defaultOAuthURL
	}
	now := time.Now()
	assertion, err := signJWT(account.PrivateKey, map[string]any{
		"iss":   account.ClientEmail,
		"scope": fcmScope,
		"aud":   tokenURL,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}, "RS256", "")
	if err != nil {
		return "", err
	}
	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("firebase token exchange failed: %s %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var parsed struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if strings.TrimSpace(parsed.AccessToken) == "" {
		return "", errors.New("firebase token exchange returned empty access_token")
	}
	ttl := time.Duration(parsed.ExpiresIn) * time.Second
	if ttl <= oauthSafetyWindow {
		ttl = time.Hour
	}
	p.storeToken(cacheKey, parsed.AccessToken, now.Add(ttl-oauthSafetyWindow))
	return parsed.AccessToken, nil
}

func (p *Provider) apnsToken(settings domain.PushSettings) (string, error) {
	cacheKey := "apns:" + settings.APNsKeyID + ":" + settings.APNsTeamID
	if cached, ok := p.lookupToken(cacheKey); ok {
		return cached, nil
	}
	now := time.Now()
	token, err := signJWT(settings.APNsKeyP8, map[string]any{
		"iss": strings.TrimSpace(settings.APNsTeamID),
		"iat": now.Unix(),
	}, "ES256", strings.TrimSpace(settings.APNsKeyID))
	if err != nil {
		return "", err
	}
	p.storeToken(cacheKey, token, now.Add(apnsTokenTTL))
	return token, nil
}

func (p *Provider) lookupToken(key string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	cached, ok := p.tokens[key]
	if !ok || time.Now().After(cached.expiresAt) {
		return "", false
	}
	return cached.value, true
}

func (p *Provider) storeToken(key, value string, expiresAt time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tokens[key] = cachedToken{value: value, expiresAt: expiresAt}
}

// signJWT 生成 RS256（Firebase 服务账号）或 ES256（APNs .p8）签名的 JWT。
func signJWT(privateKeyPEM string, claims map[string]any, algorithm, keyID string) (string, error) {
	header := map[string]string{"alg": algorithm, "typ": "JWT"}
	if keyID != "" {
		header["kid"] = keyID
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := base64URL(headerJSON) + "." + base64URL(claimsJSON)
	digest := sha256.Sum256([]byte(signingInput))

	block, _ := pem.Decode([]byte(strings.TrimSpace(privateKeyPEM)))
	if block == nil {
		return "", errors.New("private key is not valid PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		if rsaKey, rsaErr := x509.ParsePKCS1PrivateKey(block.Bytes); rsaErr == nil {
			parsed = rsaKey
		} else {
			return "", fmt.Errorf("parse private key: %w", err)
		}
	}

	var signature []byte
	switch key := parsed.(type) {
	case *rsa.PrivateKey:
		if algorithm != "RS256" {
			return "", errors.New("rsa key requires RS256")
		}
		signature, err = rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		if err != nil {
			return "", err
		}
	case *ecdsa.PrivateKey:
		if algorithm != "ES256" {
			return "", errors.New("ecdsa key requires ES256")
		}
		r, s, signErr := ecdsa.Sign(rand.Reader, key, digest[:])
		if signErr != nil {
			return "", signErr
		}
		signature = make([]byte, 64)
		r.FillBytes(signature[:32])
		s.FillBytes(signature[32:])
	default:
		return "", errors.New("unsupported private key type")
	}
	return signingInput + "." + base64URL(signature), nil
}

func base64URL(value []byte) string {
	return base64.RawURLEncoding.EncodeToString(value)
}

func projectIDFromServiceAccount(serviceAccountJSON string) string {
	if strings.TrimSpace(serviceAccountJSON) == "" {
		return ""
	}
	var account struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal([]byte(serviceAccountJSON), &account); err != nil {
		return ""
	}
	return strings.TrimSpace(account.ProjectID)
}
