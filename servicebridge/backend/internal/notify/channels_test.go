package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"customer-service/backend/internal/domain"
)

// newAlertTestProvider 返回重试间隔极短的 Provider，便于单测快速跑完。
// 默认把日志丢弃，避免失败用例刷屏；需要断言日志的用例自行传入 logger。
func newAlertTestProvider(logger *slog.Logger) *Provider {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	provider := NewProvider(logger)
	provider.retryDelays = []time.Duration{time.Millisecond, time.Millisecond}
	return provider
}

// TestSendTelegramAlertSuccess 校验 Telegram 的 URL、请求头与请求体。
func TestSendTelegramAlertSuccess(t *testing.T) {
	token := "123456:AAbbCCddEEff"
	var gotPath, gotContentType string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider := newAlertTestProvider(nil)
	provider.telegramEndpointTemplate = server.URL + "/bot%s/sendMessage"

	err := provider.SendAlertChannels(context.Background(), []domain.AgentAlertChannel{{
		Kind:    domain.AlertChannelKindTelegram,
		Enabled: true,
		Config:  map[string]string{"token": token, "chat_id": "10086"},
	}}, AlertPayload{Title: "新会话", Body: "访客发来消息", ConversationID: "conv_1", LinkHint: "https://x.test/c/1"})
	if err != nil {
		t.Fatalf("telegram 发送失败：%v", err)
	}
	if gotPath != "/bot"+token+"/sendMessage" {
		t.Fatalf("URL 不符：%s", gotPath)
	}
	if gotContentType != "application/json" {
		t.Fatalf("Content-Type 不符：%q", gotContentType)
	}
	if gotBody["chat_id"] != "10086" {
		t.Fatalf("chat_id 不符：%v", gotBody["chat_id"])
	}
	if gotBody["disable_web_page_preview"] != true {
		t.Fatalf("disable_web_page_preview 应为 true：%v", gotBody["disable_web_page_preview"])
	}
	text, _ := gotBody["text"].(string)
	for _, want := range []string{"新会话", "访客发来消息", "conv_1", "https://x.test/c/1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("消息正文缺少 %q：%q", want, text)
		}
	}
}

// TestSendAlertRetriesAndCounts 覆盖失败重试 2 次（共 3 次尝试）与失败后重试成功。
func TestSendAlertRetriesAndCounts(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	provider := newAlertTestProvider(nil)
	provider.telegramEndpointTemplate = server.URL + "/bot%s/sendMessage"
	channels := []domain.AgentAlertChannel{{
		Kind: domain.AlertChannelKindTelegram, Enabled: true,
		Config: map[string]string{"token": "tok-1234", "chat_id": "1"},
	}}
	if err := provider.SendAlertChannels(context.Background(), channels, AlertPayload{Title: "t"}); err == nil {
		t.Fatal("持续失败应返回错误")
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("失败重试后应尝试 3 次，实际 %d", got)
	}

	// 第二轮：前两次失败、第三次成功。
	attempts = 0
	recovering := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer recovering.Close()
	provider.telegramEndpointTemplate = recovering.URL + "/bot%s/sendMessage"
	if err := provider.SendAlertChannels(context.Background(), channels, AlertPayload{Title: "t"}); err != nil {
		t.Fatalf("重试成功后不应报错：%v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("应在第 3 次成功，实际尝试 %d 次", got)
	}
}

// TestSendAlertDoesNotLogSecrets：失败日志不得出现 telegram token。
func TestSendAlertDoesNotLogSecrets(t *testing.T) {
	token := "987654:SuperSecretToken"
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	provider := newAlertTestProvider(logger)
	provider.telegramEndpointTemplate = server.URL + "/bot%s/sendMessage"
	err := provider.SendAlertChannels(context.Background(), []domain.AgentAlertChannel{{
		Kind: domain.AlertChannelKindTelegram, Enabled: true,
		Config: map[string]string{"token": token, "chat_id": "1"},
	}}, AlertPayload{Title: "t"})
	if err == nil {
		t.Fatal("应返回错误")
	}
	if strings.Contains(logs.String(), token) {
		t.Fatalf("日志泄漏了 token：%s", logs.String())
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("错误信息泄漏了 token：%v", err)
	}
	if !strings.Contains(logs.String(), "alert channel send failed") {
		t.Fatalf("失败应记 Warn 日志：%s", logs.String())
	}
}

// TestSendAlertSkipsDisabledChannels：停用的渠道不发送。
func TestSendAlertSkipsDisabledChannels(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider := newAlertTestProvider(nil)
	provider.telegramEndpointTemplate = server.URL + "/bot%s/sendMessage"
	err := provider.SendAlertChannels(context.Background(), []domain.AgentAlertChannel{{
		Kind: domain.AlertChannelKindTelegram, Enabled: false,
		Config: map[string]string{"token": "tok-1234", "chat_id": "1"},
	}}, AlertPayload{Title: "t"})
	if err != nil {
		t.Fatalf("停用渠道不应报错：%v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 0 {
		t.Fatalf("停用渠道不应发送，实际请求 %d 次", got)
	}

	// 未知渠道种类应返回错误。
	if err := provider.SendAlertChannels(context.Background(), []domain.AgentAlertChannel{{
		Kind: "sms", Enabled: true, Config: map[string]string{"value": "x"},
	}}, AlertPayload{Title: "t"}); err == nil {
		t.Fatal("未知渠道种类应返回错误")
	}
}

// TestSendBarkAlert 校验 Bark 的 GET URL 组成与重试。
func TestSendBarkAlert(t *testing.T) {
	var gotPath, gotMethod string
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider := newAlertTestProvider(nil)
	err := provider.SendAlertChannels(context.Background(), []domain.AgentAlertChannel{{
		Kind: domain.AlertChannelKindBark, Enabled: true,
		Config: map[string]string{"key": "bark-key-1", "server": server.URL},
	}}, AlertPayload{Title: "新会话", Body: "访客发来消息", ConversationID: "conv_2"})
	if err != nil {
		t.Fatalf("bark 发送失败：%v", err)
	}
	if gotMethod != http.MethodGet {
		t.Fatalf("bark 应使用 GET，实际 %s", gotMethod)
	}
	wantPath := "/bark-key-1/新会话/访客发来消息\n会话 ID：conv_2"
	if gotPath != wantPath {
		t.Fatalf("bark URL 不符：got=%q want=%q", gotPath, wantPath)
	}

	// 失败重试：统计尝试次数。
	atomic.StoreInt32(&attempts, 0)
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()
	err = provider.SendAlertChannels(context.Background(), []domain.AgentAlertChannel{{
		Kind: domain.AlertChannelKindBark, Enabled: true,
		Config: map[string]string{"key": "bark-key-1", "server": failing.URL},
	}}, AlertPayload{Title: "t", Body: "b"})
	if err == nil {
		t.Fatal("bark 持续失败应返回错误")
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("bark 失败应尝试 3 次，实际 %d", got)
	}
}

// TestSendEmailAlertUsesInjectedSender：email 渠道通过可注入的 sendMail 发送（不真发邮件）。
func TestSendEmailAlertUsesInjectedSender(t *testing.T) {
	var gotAddr, gotFrom string
	var gotTo []string
	var gotMsg []byte
	var gotAuth smtp.Auth
	provider := newAlertTestProvider(nil)
	provider.sendMail = func(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
		gotAddr, gotAuth, gotFrom, gotTo, gotMsg = addr, a, from, to, msg
		return nil
	}
	provider.SetSMTPProvider(func() domain.AlertSMTPSettings {
		return domain.AlertSMTPSettings{
			Host:        "smtp.example.com",
			Port:        587,
			Username:    "bot@example.com",
			Password:    "smtp-secret",
			FromAddress: "bot@example.com",
		}
	})

	err := provider.SendAlertChannels(context.Background(), []domain.AgentAlertChannel{{
		Kind: domain.AlertChannelKindEmail, Enabled: true,
		Config: map[string]string{"address": "agent@example.com"},
	}}, AlertPayload{Title: "新会话", Body: "访客发来消息", ConversationID: "conv_3"})
	if err != nil {
		t.Fatalf("email 发送失败：%v", err)
	}
	if gotAddr != "smtp.example.com:587" {
		t.Fatalf("SMTP 地址不符：%s", gotAddr)
	}
	if gotFrom != "bot@example.com" || len(gotTo) != 1 || gotTo[0] != "agent@example.com" {
		t.Fatalf("发件人/收件人不符：from=%s to=%v", gotFrom, gotTo)
	}
	if gotAuth == nil {
		t.Fatal("配置了用户名时应带 SMTP 认证")
	}
	text := string(gotMsg)
	if !strings.Contains(text, "From: bot@example.com") || !strings.Contains(text, "To: agent@example.com") {
		t.Fatalf("邮件头不符：%s", text)
	}
	if !strings.Contains(text, "Content-Type: text/plain; charset=UTF-8") {
		t.Fatalf("邮件应为纯文本 UTF-8：%s", text)
	}
	if !strings.Contains(text, "conv_3") || !strings.Contains(text, "访客发来消息") {
		t.Fatalf("邮件正文缺内容：%s", text)
	}
}

// TestSendEmailAlertRequiresSMTP：未配置 SMTP 时 email 渠道报错，且不调用 sendMail。
func TestSendEmailAlertRequiresSMTP(t *testing.T) {
	called := false
	provider := newAlertTestProvider(nil)
	provider.sendMail = func(string, smtp.Auth, string, []string, []byte) error {
		called = true
		return nil
	}
	err := provider.SendAlertChannels(context.Background(), []domain.AgentAlertChannel{{
		Kind: domain.AlertChannelKindEmail, Enabled: true,
		Config: map[string]string{"address": "agent@example.com"},
	}}, AlertPayload{Title: "t"})
	if err == nil {
		t.Fatal("未注入 SMTP 读取函数时应报错")
	}
	if called {
		t.Fatal("未配置 SMTP 时不应调用 sendMail")
	}

	// 配置了 SMTP 但缺少 host/from 时同样报错。
	provider.SetSMTPProvider(func() domain.AlertSMTPSettings { return domain.AlertSMTPSettings{} })
	if err := provider.SendAlertChannels(context.Background(), []domain.AgentAlertChannel{{
		Kind: domain.AlertChannelKindEmail, Enabled: true,
		Config: map[string]string{"address": "agent@example.com"},
	}}, AlertPayload{Title: "t"}); err == nil {
		t.Fatal("SMTP 未配置 host 时应报错")
	}
}

// TestSendAlertChannelsErrorsAreJoined：多渠道只要有失败就返回错误，并包含渠道种类。
func TestSendAlertChannelsErrorsAreJoined(t *testing.T) {
	provider := newAlertTestProvider(nil)
	err := provider.SendAlertChannels(context.Background(), []domain.AgentAlertChannel{
		{Kind: domain.AlertChannelKindTelegram, Enabled: true, Config: map[string]string{"chat_id": "1"}},
		{Kind: domain.AlertChannelKindBark, Enabled: true, Config: map[string]string{}},
	}, AlertPayload{Title: "t"})
	if err == nil {
		t.Fatal("缺少必填配置应返回错误")
	}
	if !strings.Contains(err.Error(), "telegram") || !strings.Contains(err.Error(), "bark") {
		t.Fatalf("错误应包含两种渠道：%v", err)
	}
}

// TestDisabledNotifierImplementsAlertChannels：DisabledNotifier 必须实现新增接口方法。
func TestDisabledNotifierImplementsAlertChannels(t *testing.T) {
	var notifier Notifier = DisabledNotifier{}
	if err := notifier.SendAlertChannels(context.Background(), nil, AlertPayload{}); err != nil {
		t.Fatalf("DisabledNotifier 不应报错：%v", err)
	}
}
