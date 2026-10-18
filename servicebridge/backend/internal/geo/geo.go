// Package geo 实现访客 IP 的三级地理来源解析（零第三方依赖）：
//
//	CDN 头 → 本地地理库 → 后台配置的第三方 IP 库
//
// 三级都未命中时返回 false，调用方保持地理字段为空（只按 Accept-Language 推断语言）。
// 本地库支持两种形态：GeoLite2 官方 CSV 目录（Blocks + Locations，二分查找），
// 或 .mmdb 文件（内置最小 MMDB 读取器，见 mmdb.go）；两者都是惰性加载、进程内
// 缓存，产出的 GeoRecord 形状一致（Basis 统一为 geoip）。
// 第三方库带超时、按 IP 缓存（含负缓存），失败静默降级。
package geo

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"customer-service/backend/internal/domain"
)

// 解析错误：本地库不可用时只记录一次警告，绝不影响会话。
var (
	// ErrLocalDBUnavailable 表示本地地理库（CSV 目录或 .mmdb 文件）缺失或格式错误。
	ErrLocalDBUnavailable = errors.New("本地地理库不可用")
)

// cdnCountryHeaders 是按优先级排列的 CDN 国家码头（大小写不敏感）。
var cdnCountryHeaders = []string{"CF-IPCountry", "X-Vercel-IP-Country", "X-Country-Code"}

// CountryFromCDNHeaders 从 CDN 注入的请求头解析 2 位国家码。
// 依次检查 CF-IPCountry / X-Vercel-IP-Country / X-Country-Code，
// 非 2 位字母或占位值（XX/T1/ZZ）视为未命中。
func CountryFromCDNHeaders(header http.Header) (string, bool) {
	if header == nil {
		return "", false
	}
	for _, name := range cdnCountryHeaders {
		// http.Header.Get 已是大小写不敏感；这里再兜底一次非规范写法（如测试直接塞 map）。
		value := header.Get(name)
		if strings.TrimSpace(value) == "" {
			for key, values := range header {
				if strings.EqualFold(key, name) && len(values) > 0 {
					value = values[0]
					break
				}
			}
		}
		if code := domain.NormalizeCountryCode(value); code != "" {
			return code, true
		}
	}
	return "", false
}

// RecordFromCDN 把 CDN 头解析结果包装成地理记录（只有国家信息）。
func RecordFromCDN(header http.Header) (domain.GeoRecord, bool) {
	code, ok := CountryFromCDNHeaders(header)
	if !ok {
		return domain.GeoRecord{}, false
	}
	return domain.GeoRecord{
		Country:     domain.CountryName(code),
		CountryCode: code,
		Basis:       domain.GeoBasisCDN,
	}, true
}

// Config 是一次解析所需的运行时配置（来自 visitor_profile_settings 单例）。
type Config struct {
	// GeoIPDir 是本地地理库路径：既可以是 GeoLite2 CSV 目录，也可以是 .mmdb 文件。
	GeoIPDir string
	// ThirdPartyURL 必须包含 {ip} 占位（可选 {key}）；为空表示未配置第三方库。
	ThirdPartyURL string
	// ThirdPartyKey 为解密后的明文密钥，只用于请求头/占位替换，绝不入日志。
	ThirdPartyKey string
	// ThirdPartyTimeoutMS 为单次第三方请求超时；<=0 时用默认值。
	ThirdPartyTimeoutMS int
	// CacheTTL 为 IP 结果缓存时长（含负缓存）；<=0 时用默认值。
	CacheTTL time.Duration
}

// DefaultCacheTTL 是未配置时的 IP 结果缓存时长。
const DefaultCacheTTL = 24 * time.Hour

// DefaultTimeout 是未配置时的第三方请求超时。
const DefaultTimeout = 2 * time.Second

type cacheEntry struct {
	record  domain.GeoRecord
	found   bool
	expires time.Time
}

// Resolver 组合三级来源并缓存结果，可被多个请求并发复用。
// 配置变化（目录/URL/密钥）会自动重建对应的子解析器并切换缓存命名空间。
type Resolver struct {
	mu       sync.Mutex
	local    *LocalStore
	localDir string
	localErr error
	third    *ThirdPartyClient
	thirdSig string
	cache    map[string]cacheEntry
	// now 允许测试注入时间；默认 time.Now。
	now func() time.Time
}

// NewResolver 创建一个三级解析器。
func NewResolver() *Resolver {
	return &Resolver{cache: map[string]cacheEntry{}, now: time.Now}
}

// Resolve 按 CDN 头 → 本地地理库（CSV 目录或 .mmdb） → 第三方 的顺序解析访客 IP。
// 返回 false 表示三级都未命中（调用方保持地理字段为空）。
func (r *Resolver) Resolve(ctx context.Context, cfg Config, ip string, header http.Header) (domain.GeoRecord, bool) {
	if record, ok := RecordFromCDN(header); ok {
		return record, true
	}
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return domain.GeoRecord{}, false
	}
	ttl := cfg.CacheTTL
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	cacheKey := r.configSignature(cfg) + "|" + ip
	if record, ok := r.cached(cacheKey); ok {
		return record, true
	}
	// 本地库：加载失败只降级，不影响后续第三方来源。
	if local, err := r.localStore(cfg.GeoIPDir); err == nil {
		if record, ok := local.Lookup(ip); ok {
			r.store(cacheKey, record, true, ttl)
			return record, true
		}
	}
	if record, ok := r.thirdParty(cfg).Lookup(ctx, ip); ok {
		r.store(cacheKey, record, true, ttl)
		return record, true
	}
	// 负缓存：短时间内不再重复查询同一 IP 的第三方库。
	r.store(cacheKey, domain.GeoRecord{}, false, ttl)
	return domain.GeoRecord{}, false
}

// LocalError 返回本地库（CSV 目录或 .mmdb 文件）的加载错误（未配置时返回 nil）。
// 应用层据此"记一次 warning"，并视为本地库不可用。
func (r *Resolver) LocalError(geoIPDir string) error {
	_, err := r.localStore(geoIPDir)
	return err
}

// LocalLoadedAt 返回本地库最近一次成功加载的时间（未加载时为零值）。
func (r *Resolver) LocalLoadedAt(geoIPDir string) time.Time {
	local, err := r.localStore(geoIPDir)
	if err != nil || local == nil {
		return time.Time{}
	}
	return local.LoadedAt()
}

// localStore 按路径惰性构建（路径变化时重建）；路径可以是 CSV 目录或 .mmdb 文件。
func (r *Resolver) localStore(dir string) (*LocalStore, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, ErrLocalDBUnavailable
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.local != nil && r.localDir == dir {
		if err := r.local.EnsureLoaded(); err != nil {
			r.localErr = err
			return nil, err
		}
		return r.local, nil
	}
	store := NewLocalStore(dir)
	if err := store.EnsureLoaded(); err != nil {
		// 目录错误也要缓存下来，避免每条消息都重新扫盘。
		r.local = store
		r.localDir = dir
		r.localErr = err
		return nil, err
	}
	r.local = store
	r.localDir = dir
	r.localErr = nil
	return store, nil
}

// thirdParty 按配置惰性构建第三方客户端（URL/密钥/超时变化时重建，缓存随之切换命名空间）。
func (r *Resolver) thirdParty(cfg Config) *ThirdPartyClient {
	signature := strings.TrimSpace(cfg.ThirdPartyURL) + "|" + strings.TrimSpace(cfg.ThirdPartyKey) +
		"|" + itoa(cfg.ThirdPartyTimeoutMS)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.third != nil && r.thirdSig == signature {
		return r.third
	}
	r.third = NewThirdPartyClient(ThirdPartyConfig{
		URLTemplate: cfg.ThirdPartyURL,
		APIKey:      cfg.ThirdPartyKey,
		TimeoutMS:   cfg.ThirdPartyTimeoutMS,
	})
	r.thirdSig = signature
	return r.third
}

// configSignature 把影响结果的配置折叠成缓存命名空间，配置变化后旧缓存自然失效。
func (r *Resolver) configSignature(cfg Config) string {
	return strings.TrimSpace(cfg.GeoIPDir) + "\x00" + strings.TrimSpace(cfg.ThirdPartyURL) + "\x00" +
		strings.TrimSpace(cfg.ThirdPartyKey) + "\x00" + itoa(cfg.ThirdPartyTimeoutMS)
}

func (r *Resolver) cached(key string) (domain.GeoRecord, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.cache[key]
	if !ok {
		return domain.GeoRecord{}, false
	}
	if r.now().After(entry.expires) {
		delete(r.cache, key)
		return domain.GeoRecord{}, false
	}
	return entry.record, entry.found
}

func (r *Resolver) store(key string, record domain.GeoRecord, found bool, ttl time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache[key] = cacheEntry{record: record, found: found, expires: r.now().Add(ttl)}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var buf [20]byte
	index := len(buf)
	for value > 0 {
		index--
		buf[index] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		index--
		buf[index] = '-'
	}
	return string(buf[index:])
}
