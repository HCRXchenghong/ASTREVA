package app

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"customer-service/backend/internal/ai"
	"customer-service/backend/internal/domain"
)

// 熔断器参数：
//   - 连续失败达到 aiCircuitFailureThreshold 次即打开；
//   - 普通故障（网络/超时/5xx）冷却默认 30s，可用 AI_CIRCUIT_COOLDOWN_MS 覆盖（最小 5s）；
//   - 鉴权类故障（401/403）冷却默认 5 分钟，可用 AI_CIRCUIT_AUTH_COOLDOWN_MS 覆盖（最小 5s），
//     避免拿着无效 Key 反复打上游；
//   - 冷却结束后进入 half_open，只放行一次探测：成功关闭并清零，失败立即重新打开。
const (
	aiCircuitFailureThreshold    = 3
	defaultAICircuitCooldown     = 30 * time.Second
	defaultAICircuitAuthCooldown = 5 * time.Minute
	minAICircuitCooldown         = 5 * time.Second
	// maxAILastErrorRunes 限制健康接口/日志里保留的上游错误长度，避免刷屏。
	maxAILastErrorRunes = 240
)

// aiCircuitEntry 是单个供应商的熔断状态。
type aiCircuitEntry struct {
	failures    int
	state       string
	openedUntil time.Time
	// probing 表示 half_open 下已放行一次探测；probedAt 用于探测超时后重新放行，
	// 避免探测请求异常中断（进程重启/上下文取消）导致该供应商永久卡在 half_open。
	probing   bool
	probedAt  time.Time
	lastError string
}

// aiCircuitBreaker 是进程内的按供应商维度熔断器（并发安全）。
// 计数只统计真实上游调用：绑定解析失败（供应商被禁用等）不会进入这里。
type aiCircuitBreaker struct {
	mu           sync.Mutex
	now          func() time.Time
	threshold    int
	cooldown     time.Duration
	authCooldown time.Duration
	entries      map[string]*aiCircuitEntry
}

// newAICircuitBreaker 构造熔断器；now 为 nil 时使用真实时钟（测试可注入假时钟）。
func newAICircuitBreaker(cooldown, authCooldown time.Duration, now func() time.Time) *aiCircuitBreaker {
	if now == nil {
		now = time.Now
	}
	if cooldown <= 0 {
		cooldown = defaultAICircuitCooldown
	}
	if authCooldown <= 0 {
		authCooldown = defaultAICircuitAuthCooldown
	}
	return &aiCircuitBreaker{
		now:          now,
		threshold:    aiCircuitFailureThreshold,
		cooldown:     cooldown,
		authCooldown: authCooldown,
		entries:      map[string]*aiCircuitEntry{},
	}
}

// newServiceAICircuitBreaker 按环境变量构造熔断器：
// AI_CIRCUIT_COOLDOWN_MS / AI_CIRCUIT_AUTH_COOLDOWN_MS（毫秒，最小 5 秒，非法值回落默认）。
func newServiceAICircuitBreaker() *aiCircuitBreaker {
	return newAICircuitBreaker(envAICircuitCooldown("AI_CIRCUIT_COOLDOWN_MS", defaultAICircuitCooldown),
		envAICircuitCooldown("AI_CIRCUIT_AUTH_COOLDOWN_MS", defaultAICircuitAuthCooldown), time.Now)
}

// envAICircuitCooldown 解析毫秒级冷却配置；未配置/非法时用 fallback，小于最小冷却时收敛到最小值。
func envAICircuitCooldown(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		return fallback
	}
	value := time.Duration(ms) * time.Millisecond
	if value < minAICircuitCooldown {
		return minAICircuitCooldown
	}
	return value
}

// allow 判断当前是否可以向上游发起调用：
// closed 直接放行；open 在冷却期内拒绝、冷却结束后转 half_open 并放行一次；half_open 只放行一次探测。
func (b *aiCircuitBreaker) allow(providerID string) bool {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	entry := b.entries[providerID]
	if entry == nil {
		return true
	}
	now := b.now()
	switch entry.state {
	case domain.AICircuitStateOpen:
		if now.Before(entry.openedUntil) {
			return false
		}
		entry.state = domain.AICircuitStateHalfOpen
		entry.probing = true
		entry.probedAt = now
		return true
	case domain.AICircuitStateHalfOpen:
		// 上一次探测仍在有效期内：不放行第二次，避免冷却后瞬间涌入。
		if entry.probing && now.Sub(entry.probedAt) < b.cooldown {
			return false
		}
		entry.probing = true
		entry.probedAt = now
		return true
	default:
		return true
	}
}

// succeed 记录一次成功：清零连续失败并关闭熔断。
func (b *aiCircuitBreaker) succeed(providerID string) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	entry := b.entries[providerID]
	if entry == nil {
		return
	}
	entry.failures = 0
	entry.state = domain.AICircuitStateClosed
	entry.openedUntil = time.Time{}
	entry.probing = false
	entry.probedAt = time.Time{}
	entry.lastError = ""
}

// fail 记录一次真实上游失败：达到阈值或 half_open 探测失败时打开熔断，返回是否发生了
// closed/half_open → open 的状态跃迁（供调用方记录低频日志）。
// 401/403 使用更长的冷却时间（配置类错误，避免反复使用无效 Key）。
func (b *aiCircuitBreaker) fail(providerID string, err error) bool {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	entry := b.entries[providerID]
	if entry == nil {
		entry = &aiCircuitEntry{state: domain.AICircuitStateClosed}
		b.entries[providerID] = entry
	}
	entry.failures++
	entry.lastError = truncateAIErrorText(err.Error())
	cooldown := b.cooldown
	if isAIAuthFailure(err) {
		cooldown = b.authCooldown
	}
	// half_open 下的探测失败立即重新打开；closed 下达到阈值才打开。
	if entry.state == domain.AICircuitStateHalfOpen || entry.failures >= b.threshold {
		opened := entry.state != domain.AICircuitStateOpen
		entry.state = domain.AICircuitStateOpen
		entry.openedUntil = b.now().Add(cooldown)
		entry.probing = false
		entry.probedAt = time.Time{}
		return opened
	}
	return false
}

// snapshot 返回某供应商的熔断快照（未记录过时视为 closed）。
func (b *aiCircuitBreaker) snapshot(providerID string) (state string, failures int, openedUntil *time.Time, lastError string) {
	providerID = strings.TrimSpace(providerID)
	b.mu.Lock()
	defer b.mu.Unlock()

	entry := b.entries[providerID]
	if entry == nil {
		return domain.AICircuitStateClosed, 0, nil, ""
	}
	state = entry.state
	if state == "" {
		state = domain.AICircuitStateClosed
	}
	failures = entry.failures
	lastError = entry.lastError
	if state == domain.AICircuitStateOpen && !entry.openedUntil.IsZero() {
		until := entry.openedUntil
		openedUntil = &until
	}
	return state, failures, openedUntil, lastError
}

// isAIAuthFailure 判断是否为鉴权类失败（401/403）：这类配置错误需要更长的熔断冷却。
func isAIAuthFailure(err error) bool {
	var upstream *ai.UpstreamError
	if errors.As(err, &upstream) {
		return upstream.StatusCode == 401 || upstream.StatusCode == 403
	}
	return false
}

// redactedAIError 保留原始错误链（errors.Is/As 仍然可用），但对外文案已脱敏。
type redactedAIError struct {
	err  error
	text string
}

func (e redactedAIError) Error() string { return e.text }

func (e redactedAIError) Unwrap() error { return e.err }

// redactAISecrets 把文本中出现的任何已知密钥替换为 ***，保证日志、健康接口与错误信息绝不泄露 Key。
func redactAISecrets(text string, settingsList ...domain.AISettings) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	for _, settings := range settingsList {
		key := strings.TrimSpace(settings.APIKey)
		if key == "" {
			continue
		}
		text = strings.ReplaceAll(text, key, "***")
	}
	return text
}

// truncateAIErrorText 截断过长的上游错误文本，避免日志与健康接口刷屏。
func truncateAIErrorText(text string) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= maxAILastErrorRunes {
		return text
	}
	return string(runes[:maxAILastErrorRunes]) + "…"
}

// aiFailoverLogLimiter 限制故障转移日志频率：同一 (能力, 供应商) 在窗口内只记一条，
// 避免上游持续故障时刷屏。默认窗口 1 分钟。
type aiFailoverLogLimiter struct {
	mu     sync.Mutex
	now    func() time.Time
	window time.Duration
	last   map[string]time.Time
}

func newAIFailoverLogLimiter(window time.Duration) *aiFailoverLogLimiter {
	if window <= 0 {
		window = time.Minute
	}
	return &aiFailoverLogLimiter{now: time.Now, window: window, last: map[string]time.Time{}}
}

// allow 返回该 key 本次是否允许记录日志。
func (l *aiFailoverLogLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if last, ok := l.last[key]; ok && now.Sub(last) < l.window {
		return false
	}
	l.last[key] = now
	return true
}

// isRetryableAIUpstreamError 判断某次上游错误是否值得切换下一个供应商。
// 网络错误、超时、HTTP 5xx/401/403/429 以及响应协议错误都切换；
// 其余 HTTP 4xx（参数/模型不合法）换供应商通常也无效，但为最大化可用性同样切换一次。
// 唯一不值得继续尝试的是本地上下文已被取消（调用方主动放弃）。
func isRetryableAIUpstreamError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	return true
}
