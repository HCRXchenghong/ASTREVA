// Package openapi 提供面向第三方平台的开放接口（API 授权、会话接入与实时消息订阅），
// 以及统一后台用于管理 API 授权的服务端接口。
package openapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"customer-service/backend/internal/app"
	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/store"
)

const (
	// KeyPrefix API Key 固定前缀，便于识别与后台展示。
	KeyPrefix = "ast_live_"
	// keyRandomBytes 随机部分字节数（hex 编码后长度翻倍）。
	keyRandomBytes = 24
	// keyPrefixDisplayLen 后台展示用的 Key 前缀长度（含 ast_live_）。
	keyPrefixDisplayLen = len(KeyPrefix) + 4
	// DefaultRateLimit 未单独配置限速时，每个应用每分钟的请求上限。
	DefaultRateLimit = 300
	// maxListConversations 列表接口单次返回的会话数上限。
	maxListConversations = 200
)

var (
	ErrKeyInvalid = errors.New("api key invalid or disabled")
	ErrRateLimit  = errors.New("api rate limit exceeded")
	ErrDisabled   = errors.New("open api disabled")
)

// IPResolver 从请求解析真实客户端 IP 与来源标记。
type IPResolver func(r *http.Request) (ip, source string)

// Options 构造 Service 的依赖。
type Options struct {
	Store            store.Store
	App              *app.Service
	Logger           *slog.Logger
	Enabled          bool
	DefaultRateLimit int
	ResolveIP        IPResolver
}

// Service 承载开放接口与 API 授权管理逻辑。
type Service struct {
	store       store.Store
	app         *app.Service
	logger      *slog.Logger
	enabled     bool
	defaultRate int
	resolveIP   IPResolver

	mu      sync.Mutex
	buckets map[string]*bucket
}

func NewService(options Options) *Service {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	resolveIP := options.ResolveIP
	if resolveIP == nil {
		resolveIP = func(r *http.Request) (string, string) { return r.RemoteAddr, "direct" }
	}
	defaultRate := options.DefaultRateLimit
	if defaultRate <= 0 {
		defaultRate = DefaultRateLimit
	}
	return &Service{
		store:       options.Store,
		app:         options.App,
		logger:      logger,
		enabled:     options.Enabled,
		defaultRate: defaultRate,
		resolveIP:   resolveIP,
		buckets:     map[string]*bucket{},
	}
}

// GenerateAPIKey 生成新的 API Key：明文仅在创建/重置时返回一次，
// 系统内只保存哈希与前缀。
func GenerateAPIKey() (plaintext, prefix, hash string, err error) {
	buf := make([]byte, keyRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", "", err
	}
	plaintext = KeyPrefix + hex.EncodeToString(buf)
	return plaintext, plaintext[:keyPrefixDisplayLen], HashAPIKey(plaintext), nil
}

// HashAPIKey 计算 API Key 的 SHA-256 哈希（小写 hex）。
func HashAPIKey(plaintext string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(plaintext)))
	return hex.EncodeToString(sum[:])
}

// apiKeyFromRequest 提取请求携带的 API Key：
// 支持 Authorization: Bearer <key>、X-API-Key 头，以及 query 参数 key（WebSocket 用）。
func apiKeyFromRequest(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("X-API-Key")); value != "" {
		return value
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth != "" {
		for _, prefix := range []string{"Bearer ", "bearer ", "token "} {
			if strings.HasPrefix(auth, prefix) {
				return strings.TrimSpace(strings.TrimPrefix(auth, prefix))
			}
		}
	}
	return strings.TrimSpace(r.URL.Query().Get("key"))
}

// authenticate 校验 API Key 并返回对应授权应用；同时累计调用量。
func (s *Service) authenticate(r *http.Request) (domain.APIApp, error) {
	if !s.enabled {
		return domain.APIApp{}, ErrDisabled
	}
	key := apiKeyFromRequest(r)
	if key == "" || !strings.HasPrefix(key, KeyPrefix) {
		return domain.APIApp{}, ErrKeyInvalid
	}
	apiApp, ok := s.store.APIAppByKeyHash(HashAPIKey(key))
	if !ok || !apiApp.Enabled {
		return domain.APIApp{}, ErrKeyInvalid
	}
	s.store.TouchAPIApp(apiApp.ID)
	return apiApp, nil
}

// requireApp 完成鉴权与限速；失败时已写好错误响应。
func (s *Service) requireApp(w http.ResponseWriter, r *http.Request) (domain.APIApp, bool) {
	apiApp, err := s.authenticate(r)
	if err != nil {
		status := http.StatusUnauthorized
		switch {
		case errors.Is(err, ErrRateLimit):
			status = http.StatusTooManyRequests
		case errors.Is(err, ErrDisabled):
			status = http.StatusServiceUnavailable
		}
		writeOpenError(w, status, err.Error())
		return domain.APIApp{}, false
	}
	if !s.allowRequest(apiApp) {
		writeOpenError(w, http.StatusTooManyRequests, ErrRateLimit.Error())
		return domain.APIApp{}, false
	}
	return apiApp, true
}

type bucket struct {
	tokens       float64
	capacity     float64
	refillPerSec float64
	updatedAt    time.Time
}

// allowRequest 按应用维度做令牌桶限速（每分钟 rate_limit 次，桶容量等于限速值）。
func (s *Service) allowRequest(apiApp domain.APIApp) bool {
	limit := apiApp.RateLimit
	if limit <= 0 {
		limit = s.defaultRate
	}
	if limit <= 0 {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	capacity := float64(limit)
	refill := capacity / 60.0
	current, ok := s.buckets[apiApp.ID]
	if !ok {
		current = &bucket{tokens: capacity, capacity: capacity, refillPerSec: refill, updatedAt: now}
		s.buckets[apiApp.ID] = current
	}
	if current.capacity != capacity {
		current.capacity = capacity
		current.refillPerSec = refill
		if current.tokens > capacity {
			current.tokens = capacity
		}
	}
	current.tokens += now.Sub(current.updatedAt).Seconds() * current.refillPerSec
	current.updatedAt = now
	if current.tokens > current.capacity {
		current.tokens = current.capacity
	}
	if current.tokens < 1 {
		return false
	}
	current.tokens--
	return true
}

// conversationOwnedBy 判断会话是否属于该授权应用（创建时 source 写入 api:<app_id>）。
func conversationOwnedBy(conversation domain.Conversation, apiApp domain.APIApp) bool {
	return conversation.Source == appSource(apiApp.ID)
}

func appSource(appID string) string {
	return "api:" + appID
}

func writeOpenJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeOpenError(w http.ResponseWriter, status int, message string) {
	writeOpenJSON(w, status, map[string]any{"error": message})
}

func mapStoreError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, store.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, store.ErrForbidden), errors.Is(err, store.ErrUnauthorized):
		status = http.StatusForbidden
	case errors.Is(err, store.ErrConflict):
		status = http.StatusConflict
	case errors.Is(err, store.ErrInvalidInput):
		status = http.StatusBadRequest
	}
	writeOpenError(w, status, err.Error())
}

// applyOpenCORS 允许第三方平台浏览器侧直接调用开放接口（Key 鉴权，不依赖 Cookie）。
func applyOpenCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-API-Key")
	w.Header().Set("Access-Control-Max-Age", "600")
}
