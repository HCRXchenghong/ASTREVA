package geo

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"customer-service/backend/internal/domain"
)

// ThirdPartyConfig 是第三方 IP 库的配置。
type ThirdPartyConfig struct {
	// URLTemplate 必须包含 {ip} 占位；可选 {key} 占位（不写则用请求头发送密钥）。
	URLTemplate string
	// APIKey 为明文密钥，只用于占位替换或请求头，绝不写日志。
	APIKey string
	// TimeoutMS 为单次请求超时（毫秒），<=0 用默认值。
	TimeoutMS int
}

// ThirdPartyClient 是零依赖的第三方 IP 库客户端：带超时、按 IP 缓存（含负缓存）。
// 任何失败（网络、超时、非 2xx、JSON 异常）都静默降级为"未命中"，不影响会话。
type ThirdPartyClient struct {
	template string
	apiKey   string
	timeout  time.Duration
	client   *http.Client

	mu    sync.Mutex
	cache map[string]thirdPartyEntry
	ttl   time.Duration
	now   func() time.Time
}

type thirdPartyEntry struct {
	record  domain.GeoRecord
	found   bool
	expires time.Time
}

// NewThirdPartyClient 创建客户端；URLTemplate 为空时 Lookup 直接返回未命中。
func NewThirdPartyClient(cfg ThirdPartyConfig) *ThirdPartyClient {
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &ThirdPartyClient{
		template: strings.TrimSpace(cfg.URLTemplate),
		apiKey:   strings.TrimSpace(cfg.APIKey),
		timeout:  timeout,
		client:   &http.Client{Timeout: timeout},
		cache:    map[string]thirdPartyEntry{},
		ttl:      DefaultCacheTTL,
		now:      time.Now,
	}
}

// Configured 表示是否配置了可用的 URL 模板。
func (c *ThirdPartyClient) Configured() bool {
	return c != nil && c.template != "" && strings.Contains(c.template, "{ip}")
}

// Lookup 查询第三方 IP 库；命中返回地理记录，未命中/失败返回 false。
// 结果（含未命中）按 IP 缓存，缓存时长默认 24h。
func (c *ThirdPartyClient) Lookup(ctx context.Context, ip string) (domain.GeoRecord, bool) {
	if !c.Configured() {
		return domain.GeoRecord{}, false
	}
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return domain.GeoRecord{}, false
	}
	if record, ok, cached := c.cached(ip); cached {
		return record, ok
	}
	record, ok := c.fetch(ctx, ip)
	c.store(ip, record, ok)
	return record, ok
}

func (c *ThirdPartyClient) cached(ip string) (domain.GeoRecord, bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, exists := c.cache[ip]
	if !exists {
		return domain.GeoRecord{}, false, false
	}
	if c.now().After(entry.expires) {
		delete(c.cache, ip)
		return domain.GeoRecord{}, false, false
	}
	return entry.record, entry.found, true
}

func (c *ThirdPartyClient) store(ip string, record domain.GeoRecord, found bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[ip] = thirdPartyEntry{record: record, found: found, expires: c.now().Add(c.ttl)}
}

// fetch 执行一次带超时的 HTTP 查询；失败静默返回未命中。
func (c *ThirdPartyClient) fetch(ctx context.Context, ip string) (domain.GeoRecord, bool) {
	endpoint := strings.ReplaceAll(c.template, "{ip}", url.PathEscape(ip))
	if strings.Contains(endpoint, "{key}") {
		endpoint = strings.ReplaceAll(endpoint, "{key}", url.QueryEscape(c.apiKey))
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return domain.GeoRecord{}, false
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(timeoutCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return domain.GeoRecord{}, false
	}
	request.Header.Set("Accept", "application/json")
	if c.apiKey != "" && !strings.Contains(c.template, "{key}") {
		// 同时给出两种最常见的鉴权头，兼容不同第三方服务；密钥绝不进日志。
		request.Header.Set("X-API-Key", c.apiKey)
		request.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return domain.GeoRecord{}, false
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return domain.GeoRecord{}, false
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return domain.GeoRecord{}, false
	}
	return parseThirdPartyBody(body)
}

// parseThirdPartyBody 解析第三方响应，兼容多种常见字段命名与嵌套结构。
func parseThirdPartyBody(body []byte) (domain.GeoRecord, bool) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return domain.GeoRecord{}, false
	}
	flat := map[string]any{}
	// 常见包装：{"data":{...}} / {"location":{...}} / {"result":{...}}
	for _, key := range []string{"data", "location", "result"} {
		if nested, ok := payload[key].(map[string]any); ok {
			for nestedKey, value := range nested {
				flat[strings.ToLower(nestedKey)] = value
			}
		}
	}
	for key, value := range payload {
		if _, isMap := value.(map[string]any); isMap {
			continue
		}
		flat[strings.ToLower(key)] = value
	}
	code := domain.NormalizeCountryCode(firstString(flat,
		"country_code", "countrycode", "country_iso_code", "countryiso", "country_code_iso"))
	name := firstString(flat, "country_name", "countryname")
	// 部分服务把可读名放在 country，把码放在 country_code；也有服务只给码。
	if rawCountry := firstString(flat, "country"); rawCountry != "" {
		if code == "" {
			if normalized := domain.NormalizeCountryCode(rawCountry); normalized != "" {
				code = normalized
			} else {
				name = rawCountry
			}
		} else if name == "" {
			name = rawCountry
		}
	}
	region := firstString(flat, "region", "regionname", "region_name", "subdivision_1_name", "subdivision", "state")
	city := firstString(flat, "city", "city_name", "cityname", "town")
	timezone := firstString(flat, "timezone", "time_zone", "timezone_name", "zone")
	if code == "" && name == "" && region == "" && city == "" && timezone == "" {
		return domain.GeoRecord{}, false
	}
	if name == "" {
		name = domain.CountryName(code)
	}
	return domain.GeoRecord{
		Country:     strings.TrimSpace(name),
		CountryCode: code,
		Region:      strings.TrimSpace(region),
		City:        strings.TrimSpace(city),
		Timezone:    strings.TrimSpace(timezone),
		Basis:       domain.GeoBasisThirdParty,
	}, true
}

// firstString 返回第一个非空字符串值（键名已转小写）。
func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := values[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return strings.TrimSpace(typed)
			}
		case json.Number:
			if strings.TrimSpace(typed.String()) != "" {
				return typed.String()
			}
		}
	}
	return ""
}
