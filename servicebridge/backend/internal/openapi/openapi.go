// Package openapi 提供面向第三方平台的开放接口（API 授权、会话接入与实时消息订阅），
// 以及统一后台用于管理 API 授权的服务端接口。
package openapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	Store   store.Store
	App     *app.Service
	Logger  *slog.Logger
	Enabled bool
	// PolicyControlled=true 时，除 Enabled 外还读取后台服务策略的 open_api_enabled 总开关。
	PolicyControlled bool
	DefaultRateLimit int
	ResolveIP        IPResolver
}

// Service 承载开放接口与 API 授权管理逻辑。
type Service struct {
	store            store.Store
	app              *app.Service
	logger           *slog.Logger
	enabled          bool
	policyControlled bool
	defaultRate      int
	resolveIP        IPResolver

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
		store:            options.Store,
		app:              options.App,
		logger:           logger,
		enabled:          options.Enabled,
		policyControlled: options.PolicyControlled,
		defaultRate:      defaultRate,
		resolveIP:        resolveIP,
		buckets:          map[string]*bucket{},
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
	if s.policyControlled && !s.store.ServicePolicy().OpenAPIEnabled {
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

// scopeDeniedCode 缺权限范围时的错误码：沿用统一后台「error 机器码 + message 中文说明」的错误体。
const scopeDeniedCode = "forbidden"

// requireScope 校验授权应用是否具备指定权限范围（附录 B4）。
// 缺失时返回 403 {"error":"forbidden","message":"缺少权限范围 <scope>"}，
// 并写审计（openapi.scope_denied，只含 app_id/scope/path，不含请求体）。
func (s *Service) requireScope(w http.ResponseWriter, r *http.Request, apiApp domain.APIApp, scope string) bool {
	if domain.HasAPIScope(apiApp.Scopes, scope) {
		return true
	}
	s.recordScopeDenied(r, apiApp, scope)
	writeOpenErrorCode(w, http.StatusForbidden, scopeDeniedCode, "缺少权限范围 "+scope)
	return false
}

// recordScopeDenied 记录因缺权限范围被拒的审计与系统日志。
// 审计只写入应用标识、缺失范围与请求路径；不记录请求体、Key 或任何请求参数。
func (s *Service) recordScopeDenied(r *http.Request, apiApp domain.APIApp, scope string) {
	path := ""
	if r != nil && r.URL != nil {
		path = r.URL.Path
	}
	ip, _ := s.resolveIP(r)
	s.store.RecordAuditEvent(domain.AuditEvent{
		// audit_events 的 actor_kind 仅允许 admin/agent/visitor；
		// 开放接口以访客通道身份运行，应用标识放在 actor_id / resource_id。
		ActorKind:  domain.AccountVisitor,
		ActorID:    appSource(apiApp.ID),
		Action:     "openapi.scope_denied",
		Resource:   "api_app",
		ResourceID: apiApp.ID,
		IPAddress:  ip,
		UserAgent:  r.UserAgent(),
		Description: fmt.Sprintf("开放接口缺少权限范围：app_id=%s scope=%s path=%s",
			apiApp.ID, scope, path),
		CreatedAt: time.Now().UTC(),
	})
	if s.logger != nil {
		s.logger.Warn("open api scope denied", "app_id", apiApp.ID, "scope", scope, "path", path)
	}
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

// writeOpenErrorCode 输出「机器码 + 中文说明」的错误体，与统一后台其它 Go 接口一致。
func writeOpenErrorCode(w http.ResponseWriter, status int, code, message string) {
	writeOpenJSON(w, status, map[string]any{"error": code, "message": message})
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
	case errors.Is(err, store.ErrRateLimited):
		status = http.StatusTooManyRequests
	case errors.Is(err, store.ErrBlacklisted):
		// 黑名单命中：策略拒绝，返回 403 + 统一文案（绝不泄露被拉黑的值）。
		status = http.StatusForbidden
	}
	message := err.Error()
	// 限额错误对外只下发已按访客语言翻译的提示，不暴露内部维度与计数。
	if limitMessage := store.RateLimitMessage(err); limitMessage != "" {
		message = limitMessage
	}
	if errors.Is(err, store.ErrBlacklisted) {
		message = domain.DefaultBlacklistMessage
		if blacklistMessage := store.BlacklistMessage(err); blacklistMessage != "" {
			message = blacklistMessage
		}
	}
	writeOpenError(w, status, message)
}

// applyOpenCORS 允许第三方平台浏览器侧直接调用开放接口（Key 鉴权，不依赖 Cookie）。
func applyOpenCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-API-Key")
	w.Header().Set("Access-Control-Max-Age", "600")
}
