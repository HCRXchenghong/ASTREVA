// 客服提醒渠道（批次 4）：把分配/转接/新消息/SLA 提醒投递到客服自配的
// telegram / bark / email 渠道。本文件只负责"发出去"，不参与分配与状态机。
package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	"customer-service/backend/internal/domain"
)

// AlertPayload 是一条客服提醒的内容。
type AlertPayload struct {
	Title          string
	Body           string
	ConversationID string
	LinkHint       string
}

// mailSender 抽象 smtp.SendMail，便于单测注入假实现（不发真实邮件）。
type mailSender func(addr string, a smtp.Auth, from string, to []string, msg []byte) error

// smtpSendMail 是生产实现：587 等提交口由标准库按服务器能力自动升级 STARTTLS。
func smtpSendMail(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
	return smtp.SendMail(addr, a, from, to, msg)
}

// SetSMTPProvider 注入 SMTP 设置读取函数（来自 alert_smtp_settings 单例）。
// email 渠道每次发送前读取最新配置，管理员改完立即生效。
func (p *Provider) SetSMTPProvider(settings func() domain.AlertSMTPSettings) {
	p.smtpSettings = settings
}

// SendAlertChannels 逐条投递启用的提醒渠道；单渠道失败重试 2 次（200ms/600ms 指数退避），
// 最终失败用 errors.Join 汇总返回。未启用的渠道直接跳过。
func (p *Provider) SendAlertChannels(ctx context.Context, channels []domain.AgentAlertChannel, payload AlertPayload) error {
	var failures []error
	for _, channel := range channels {
		if !channel.Enabled {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(channel.Kind))
		var err error
		switch kind {
		case domain.AlertChannelKindTelegram:
			err = p.sendAlertWithRetry(ctx, kind, func(ctx context.Context) error {
				return p.sendTelegramAlert(ctx, channel.Config, payload)
			})
		case domain.AlertChannelKindBark:
			err = p.sendAlertWithRetry(ctx, kind, func(ctx context.Context) error {
				return p.sendBarkAlert(ctx, channel.Config, payload)
			})
		case domain.AlertChannelKindEmail:
			err = p.sendAlertWithRetry(ctx, kind, func(ctx context.Context) error {
				return p.sendEmailAlert(ctx, channel.Config, payload)
			})
		default:
			err = fmt.Errorf("不支持的提醒渠道 %q", kind)
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("%s 渠道发送失败: %w", kind, err))
		}
	}
	return errors.Join(failures...)
}

// sendAlertWithRetry 执行一次发送并在失败后重试 len(retryDelays) 次。
// 日志只记渠道种类/第几次/错误文本，错误文本已由各渠道做密钥脱敏。
func (p *Provider) sendAlertWithRetry(ctx context.Context, kind string, attempt func(context.Context) error) error {
	var lastErr error
	for index := 0; index <= len(p.retryDelays); index++ {
		if index > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(p.retryDelays[index-1]):
			}
		}
		if err := attempt(ctx); err != nil {
			lastErr = err
			p.logger.Warn("alert channel send failed",
				"kind", kind,
				"attempt", index+1,
				"error", err,
			)
			continue
		}
		return nil
	}
	return lastErr
}

// sendTelegramAlert 通过 Telegram Bot API 发送消息。
// 配置键：token（bot token）、chat_id。
func (p *Provider) sendTelegramAlert(ctx context.Context, config map[string]string, payload AlertPayload) error {
	token := strings.TrimSpace(config["token"])
	chatID := strings.TrimSpace(config["chat_id"])
	if token == "" || chatID == "" {
		return errors.New("telegram 渠道缺少 token 或 chat_id")
	}
	body, err := json.Marshal(map[string]any{
		"chat_id":                  chatID,
		"text":                     alertMessageText(payload),
		"disable_web_page_preview": true,
	})
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf(p.telegramEndpointTemplate, token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return sanitizeAlertError(err, token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		// http 错误文本里会带完整 URL（含 bot token），必须脱敏后再返回/记日志。
		return sanitizeAlertError(err, token)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return sanitizeAlertError(fmt.Errorf("telegram 返回 %s: %s", resp.Status, strings.TrimSpace(string(detail))), token)
	}
	return nil
}

// sendBarkAlert 通过 Bark 发送消息：GET <server>/<key>/<title>/<body>。
// 配置键：key（必填）、server（可选，默认 https://api.day.app）。
func (p *Provider) sendBarkAlert(ctx context.Context, config map[string]string, payload AlertPayload) error {
	key := strings.TrimSpace(config["key"])
	if key == "" {
		return errors.New("bark 渠道缺少 key")
	}
	server := strings.TrimSpace(config["server"])
	if server == "" {
		server = domain.DefaultBarkServer
	}
	if err := validateOutboundURL(server, p.allowPrivate); err != nil {
		return err
	}
	title := alertTitle(payload)
	text := alertBodyText(payload)
	endpoint := fmt.Sprintf(p.barkEndpointTemplate,
		strings.TrimRight(server, "/"),
		url.PathEscape(key),
		url.PathEscape(title),
		url.PathEscape(text),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return sanitizeAlertError(err, key)
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{DialContext: safeDialContext(p.allowPrivate)},
	}
	resp, err := client.Do(req)
	if err != nil {
		return sanitizeAlertError(err, key)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		// 不回显上游响应体，避免把内网服务内容带回给客服端。
		return sanitizeAlertError(fmt.Errorf("bark 返回 %s", resp.Status), key)
	}
	return nil
}

// validateOutboundURL 拒绝非 http(s)、带 userinfo、以及（生产环境）私网/回环/链路本地地址。
func validateOutboundURL(raw string, allowPrivate bool) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("bark 的 server 必须是 http(s) 地址")
	}
	if parsed.User != nil {
		return errors.New("bark 的 server 不能包含账号密码")
	}
	host := parsed.Hostname()
	if host == "" {
		return errors.New("bark 的 server 缺少主机名")
	}
	if !allowPrivate {
		if parsed.Scheme != "https" {
			return errors.New("生产环境 bark server 必须使用 HTTPS")
		}
		lookupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ips, err := net.DefaultResolver.LookupIPAddr(lookupCtx, host)
		if err != nil || len(ips) == 0 {
			return errors.New("bark 的 server 无法解析")
		}
		for _, item := range ips {
			if isDisallowedIP(item.IP) {
				return errors.New("bark 的 server 指向不允许的内网地址")
			}
		}
	}
	return nil
}

func isDisallowedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
		// RFC 6598 CGNAT，云上常被用作内部地址段。
		return true
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}

// safeDialContext 在真正建连前再次校验解析结果，防止 DNS rebinding 把公网域名指向内网。
func safeDialContext(allowPrivate bool) func(context.Context, string, string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if ip := net.ParseIP(host); ip != nil {
			if !allowPrivate && isDisallowedIP(ip) {
				return nil, errors.New("目标地址不允许")
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("目标主机无法解析")
		}
		for _, item := range ips {
			if !allowPrivate && isDisallowedIP(item.IP) {
				return nil, errors.New("目标地址不允许")
			}
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}
}

// sendEmailAlert 通过全局 SMTP 设置发信到渠道配置里的 address。
//
// TLS 说明（保持简单）：
//   - use_tls=true 且端口 465：隐式 TLS（tls.Dial + smtp.NewClient）；
//   - 其它情况交给 smtp.SendMail：标准库会在服务器声明 STARTTLS 时自动升级（如 587 提交口）。
//     因此 use_tls=false 也不会真正"明文"发送密码，但也不强制要求 TLS —— 由服务器能力决定。
func (p *Provider) sendEmailAlert(ctx context.Context, config map[string]string, payload AlertPayload) error {
	address := strings.TrimSpace(config["address"])
	if address == "" {
		return errors.New("email 渠道缺少 address")
	}
	if p.smtpSettings == nil {
		return errors.New("未配置 SMTP 服务")
	}
	settings := p.smtpSettings()
	if strings.TrimSpace(settings.Host) == "" || strings.TrimSpace(settings.FromAddress) == "" {
		return errors.New("SMTP 未配置（host 或 from_address 为空）")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var auth smtp.Auth
	if strings.TrimSpace(settings.Username) != "" {
		auth = smtp.PlainAuth("", settings.Username, settings.Password, settings.Host)
	}
	message := buildAlertEmail(settings.FromAddress, address, payload)
	if settings.UseTLS && settings.Port == 465 {
		return p.sendMailWithImplicitTLS(settings, address, message)
	}
	addr := net.JoinHostPort(settings.Host, strconv.Itoa(settings.Port))
	return p.sendMail(addr, auth, settings.FromAddress, []string{address}, message)
}

// sendMailWithImplicitTLS 处理 465 端口的隐式 TLS 连接。
func (p *Provider) sendMailWithImplicitTLS(settings domain.AlertSMTPSettings, address string, message []byte) error {
	host := strings.TrimSpace(settings.Host)
	addr := net.JoinHostPort(host, strconv.Itoa(settings.Port))
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer client.Close()
	if strings.TrimSpace(settings.Username) != "" {
		if err := client.Auth(smtp.PlainAuth("", settings.Username, settings.Password, host)); err != nil {
			return err
		}
	}
	if err := client.Mail(settings.FromAddress); err != nil {
		return err
	}
	if err := client.Rcpt(address); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(message); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// buildAlertEmail 构造纯文本 UTF-8 邮件（主题用 RFC 2047 base64 编码，避免中文乱码）。
func buildAlertEmail(from, to string, payload AlertPayload) []byte {
	subject := alertTitle(payload)
	var builder strings.Builder
	builder.WriteString("From: " + from + "\r\n")
	builder.WriteString("To: " + to + "\r\n")
	builder.WriteString("Subject: =?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(subject)) + "?=\r\n")
	builder.WriteString("MIME-Version: 1.0\r\n")
	builder.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	builder.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	builder.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	builder.WriteString("\r\n")
	builder.WriteString(alertMessageText(payload))
	return []byte(builder.String())
}

// alertTitle 返回提醒标题（空则用默认中文标题）。
func alertTitle(payload AlertPayload) string {
	title := strings.TrimSpace(payload.Title)
	if title == "" {
		title = "Astreva 客服提醒"
	}
	return title
}

// alertBodyText 返回正文（含会话链接提示与会话 ID，便于客服直接定位会话）。
func alertBodyText(payload AlertPayload) string {
	parts := make([]string, 0, 3)
	if body := strings.TrimSpace(payload.Body); body != "" {
		parts = append(parts, body)
	}
	if hint := strings.TrimSpace(payload.LinkHint); hint != "" {
		parts = append(parts, "会话链接："+hint)
	}
	if conversationID := strings.TrimSpace(payload.ConversationID); conversationID != "" {
		parts = append(parts, "会话 ID："+conversationID)
	}
	return strings.Join(parts, "\n")
}

// alertMessageText 把标题与正文合成单条文本（telegram/email 使用）。
func alertMessageText(payload AlertPayload) string {
	body := alertBodyText(payload)
	if body == "" {
		return alertTitle(payload)
	}
	return alertTitle(payload) + "\n" + body
}

// sanitizeAlertError 把错误文本中出现的密钥替换为 ****，避免 http 错误里的完整 URL 泄漏 token/key。
func sanitizeAlertError(err error, secrets ...string) error {
	if err == nil {
		return nil
	}
	text := err.Error()
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if len(secret) < 4 {
			continue
		}
		text = strings.ReplaceAll(text, secret, "****")
	}
	return errors.New(text)
}
