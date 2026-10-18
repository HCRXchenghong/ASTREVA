package notify

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"customer-service/backend/internal/domain"
)

func newTestProvider() *Provider {
	provider := NewProvider(slog.Default())
	provider.client = http.DefaultClient
	return provider
}

func rsaKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal rsa key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func ecKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ec key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal ec key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func TestProviderSendsFCMWithStaticToken(t *testing.T) {
	var received map[string]any
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		if !strings.Contains(r.URL.Path, "/projects/demo-project/messages:send") {
			t.Errorf("unexpected fcm path: %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider := newTestProvider()
	provider.fcmEndpointTemplate = server.URL + "/v1/projects/%s/messages:send"
	err := provider.Notify(context.Background(), domain.PushSettings{
		FCMAccessToken:    "test-token",
		FCMConfigured:     true,
		FirebaseProjectID: "demo-project",
	}, Message{
		DeviceToken:    "device-token",
		Platform:       string(domain.PushPlatformAndroid),
		AgentID:        "agent_1",
		ConversationID: "conv_1",
		Title:          "新消息",
		Body:           "访客发来了新消息",
		Data:           map[string]string{"conversation_id": "conv_1"},
	})
	if err != nil {
		t.Fatalf("notify: %v", err)
	}
	if authHeader != "Bearer test-token" {
		t.Fatalf("unexpected authorization header: %s", authHeader)
	}
	message := received["message"].(map[string]any)
	if message["token"] != "device-token" {
		t.Fatalf("unexpected device token: %v", message["token"])
	}
}

func TestProviderMintsServiceAccountToken(t *testing.T) {
	keyPEM := rsaKeyPEM(t)
	var fcmToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse form: %v", err)
			}
			if r.FormValue("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
				t.Errorf("unexpected grant_type: %s", r.FormValue("grant_type"))
			}
			if strings.Count(r.FormValue("assertion"), ".") != 2 {
				t.Errorf("assertion is not a JWT: %s", r.FormValue("assertion"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "minted-token", "expires_in": 3600})
		case "/v1/projects/demo-project/messages:send":
			fcmToken = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	serviceAccount, err := json.Marshal(map[string]string{
		"client_email": "push@demo.iam.gserviceaccount.com",
		"private_key":  keyPEM,
		"token_uri":    server.URL + "/token",
		"project_id":   "demo-project",
	})
	if err != nil {
		t.Fatalf("marshal service account: %v", err)
	}

	provider := newTestProvider()
	provider.oauthEndpoint = server.URL + "/token"
	provider.fcmEndpointTemplate = server.URL + "/v1/projects/%s/messages:send"
	settings := domain.PushSettings{
		FCMServiceAccountJSON: string(serviceAccount),
		FirebaseProjectID:     "demo-project",
	}
	settings.Normalize()
	if !settings.FCMConfigured {
		t.Fatal("service account should mark fcm as configured")
	}
	if settings.FCMSecretMasked == "" || strings.Contains(settings.FCMSecretMasked, "PRIVATE KEY") {
		t.Fatalf("unexpected mask: %q", settings.FCMSecretMasked)
	}
	if err := provider.Notify(context.Background(), settings, Message{DeviceToken: "device", Platform: "android", Title: "t", Body: "b"}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if fcmToken != "minted-token" {
		t.Fatalf("expected minted token, got %q", fcmToken)
	}
	// 第二次调用应命中缓存，不再请求 /token。
	if err := provider.Notify(context.Background(), settings, Message{DeviceToken: "device", Platform: "android", Title: "t", Body: "b"}); err != nil {
		t.Fatalf("notify cached: %v", err)
	}
}

func TestProviderSendsAPNsForIOS(t *testing.T) {
	var path string
	var topic string
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		topic = r.Header.Get("apns-topic")
		auth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider := newTestProvider()
	provider.apnsEndpoints["production"] = server.URL
	settings := domain.PushSettings{
		APNsKeyP8:       ecKeyPEM(t),
		APNsKeyID:       "KEYID123",
		APNsTeamID:      "TEAM123",
		APNsTopic:       "com.astreva.online_customer_service",
		APNsEnvironment: "production",
	}
	settings.Normalize()
	if !settings.APNsConfigured {
		t.Fatal("apns should be configured")
	}
	if err := provider.Notify(context.Background(), settings, Message{
		DeviceToken: "ios-device", Platform: string(domain.PushPlatformIOS), Title: "t", Body: "b",
	}); err != nil {
		t.Fatalf("apns notify: %v", err)
	}
	if path != "/3/device/ios-device" {
		t.Fatalf("unexpected apns path: %s", path)
	}
	if topic != settings.APNsTopic {
		t.Fatalf("unexpected apns topic: %s", topic)
	}
	if !strings.HasPrefix(auth, "bearer ") || strings.Count(strings.TrimPrefix(auth, "bearer "), ".") != 2 {
		t.Fatalf("unexpected apns authorization: %s", auth)
	}
}

func TestProviderSkipsWithoutChannel(t *testing.T) {
	provider := newTestProvider()
	if err := provider.Notify(context.Background(), domain.PushSettings{}, Message{DeviceToken: "device"}); err != nil {
		t.Fatalf("expected noop without channel, got %v", err)
	}
	if err := provider.Notify(context.Background(), domain.PushSettings{FCMConfigured: true}, Message{}); err != nil {
		t.Fatalf("expected noop without device token, got %v", err)
	}
}

func TestValidateReportsCredentials(t *testing.T) {
	provider := newTestProvider()
	empty := provider.Validate(context.Background(), domain.PushSettings{})
	if empty["fcm"] != "未配置" || empty["apns"] != "未配置" {
		t.Fatalf("unexpected empty diagnostics: %#v", empty)
	}
	static := provider.Validate(context.Background(), domain.PushSettings{FCMAccessToken: "abc", FCMConfigured: true})
	if !strings.Contains(static["fcm"], "静态") {
		t.Fatalf("unexpected static token diagnostics: %#v", static)
	}
}

func TestSignJWTRejectsInvalidKey(t *testing.T) {
	if _, err := signJWT("not-a-pem", map[string]any{"iss": "x"}, "RS256", ""); err == nil {
		t.Fatal("expected error for invalid pem")
	}
}
