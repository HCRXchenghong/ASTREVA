package domain

import (
	"net"
	"sort"
	"strings"
	"time"
)

// ===== 访客画像（P5）=====
//
// 本文件定义画像的数据模型、默认值、归一化与"推测"计算。
// 关键约束：
//   - 国籍只是按证据加权的**推测**（nationality_guess），绝不覆盖地理字段本身；
//   - 完整 IP 属于个人信息，show_full_ip=false 时对外只返回脱敏值。

// 地理/国籍依据标识（写入 nationality_basis 与地理来源 basis）。
//
// 契约冻结：nationality_basis 数组元素只允许这 5 个英文键，
// 控制台会把它们映射成中文（IP 国别 / 本地地理库 / 第三方 IP 库 / 语言 / 时区），
// 未知键会原样回显英文。因此本地 CSV 库统一记作 "geoip"。
const (
	// GeoBasisCDN 表示国家码来自 CDN 注入的请求头（CF-IPCountry 等）。
	GeoBasisCDN = "ip_cdn"
	// GeoBasisLocal 表示来自本地 GeoLite2 CSV 库。
	GeoBasisLocal = "geoip"
	// GeoBasisThirdParty 表示来自后台配置的第三方 IP 库。
	GeoBasisThirdParty = "third_party"
	// GeoBasisLanguage 表示依据语言画像推断。
	GeoBasisLanguage = "language"
	// GeoBasisTimezone 表示依据时区推断。
	GeoBasisTimezone = "timezone"
)

// MaxNationalityConfidence 是国籍推测置信度上限（只作推测，不追求确定性）。
const MaxNationalityConfidence = 0.95

// 画像设置默认值：与 visitor_profile_settings 表结构保持一致。
const (
	DefaultThirdPartyTimeoutMS    = 2000
	DefaultThirdPartyCacheTTLHour = 24
	DefaultAISummaryEveryMessages = 10
	// MaxThirdPartyTimeoutMS 是第三方 IP 库超时上限（30s），避免配置成无限等待。
	MaxThirdPartyTimeoutMS = 30000
	// MinThirdPartyTimeoutMS 是第三方 IP 库超时下限，避免配置成必然超时。
	MinThirdPartyTimeoutMS = 200
	// MaxCacheTTLHours 是 IP 解析结果缓存上限（30 天）。
	MaxCacheTTLHours = 720
	// MaxAISummaryEveryMessages 是摘要触发间隔上限，避免配置成几乎不触发。
	MaxAISummaryEveryMessages = 200
)

// GeoRecord 是一次 IP 地理解析的结果；Basis 标明来源层级。
// Country 是可直接展示的名称（GeoLite2 Locations 的 country_name，缺失时回落 ISO 码），
// CountryCode 是 ISO alpha-2 大写码。
type GeoRecord struct {
	Country     string `json:"country"`
	CountryCode string `json:"country_code"`
	Region      string `json:"region"`
	City        string `json:"city"`
	Timezone    string `json:"timezone"`
	Basis       string `json:"basis"`
}

// IsEmpty 判断解析结果是否没有任何可用字段。
func (g GeoRecord) IsEmpty() bool {
	return strings.TrimSpace(g.Country) == "" && strings.TrimSpace(g.CountryCode) == "" &&
		strings.TrimSpace(g.Region) == "" && strings.TrimSpace(g.City) == "" &&
		strings.TrimSpace(g.Timezone) == ""
}

// VisitorProfileEvidence 是 AI 摘要引用的一条证据消息。
type VisitorProfileEvidence struct {
	MessageID string    `json:"message_id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// VisitorProfile 是按访客聚合、跨会话的客户画像。
// Country 是可展示名称（如"沙特阿拉伯"），CountryCode 是 ISO alpha-2 大写码（如 SA）。
type VisitorProfile struct {
	VisitorID             string                   `json:"visitor_id"`
	Country               string                   `json:"country"`
	CountryCode           string                   `json:"country_code"`
	Region                string                   `json:"region"`
	City                  string                   `json:"city"`
	Timezone              string                   `json:"timezone"`
	NationalityGuess      string                   `json:"nationality_guess"`
	NationalityConfidence float64                  `json:"nationality_confidence"`
	NationalityBasis      []string                 `json:"nationality_basis"`
	IPFirst               string                   `json:"ip_first"`
	IPLast                string                   `json:"ip_last"`
	Languages             map[string]int           `json:"languages"`
	Dialect               string                   `json:"dialect"`
	CodeSwitching         bool                     `json:"code_switching"`
	Summary               string                   `json:"summary"`
	Tags                  []string                 `json:"tags"`
	Interests             []string                 `json:"interests"`
	Evidence              []VisitorProfileEvidence `json:"evidence"`
	Visits                int                      `json:"visits"`
	LastPath              string                   `json:"last_path"`
	AIUpdatedAt           *time.Time               `json:"ai_updated_at,omitempty"`
	CreatedAt             time.Time                `json:"created_at"`
	UpdatedAt             time.Time                `json:"updated_at"`
}

// VisitorProfileConversation 是画像详情里的会话摘要。
type VisitorProfileConversation struct {
	ConversationID string     `json:"conversation_id"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	LastMessageAt  *time.Time `json:"last_message_at,omitempty"`
}

// VisitorProfileView 是接口下发的画像视图（控制台与 App 契约）。
// LastSeenAt 直接取画像的 updated_at；IPLast 已按 show_full_ip 处理（关闭时脱敏为 1.2.3.*）。
type VisitorProfileView struct {
	VisitorID             string         `json:"visitor_id"`
	Country               string         `json:"country"`
	CountryCode           string         `json:"country_code"`
	Region                string         `json:"region"`
	City                  string         `json:"city"`
	Timezone              string         `json:"timezone"`
	NationalityGuess      string         `json:"nationality_guess"`
	NationalityConfidence float64        `json:"nationality_confidence"`
	NationalityBasis      []string       `json:"nationality_basis"`
	Languages             map[string]int `json:"languages"`
	Dialect               string         `json:"dialect"`
	CodeSwitching         bool           `json:"code_switching"`
	Summary               string         `json:"summary"`
	Tags                  []string       `json:"tags"`
	Interests             []string       `json:"interests"`
	Visits                int            `json:"visits"`
	LastPath              string         `json:"last_path"`
	LastSeenAt            *time.Time     `json:"last_seen_at,omitempty"`
	IPLast                string         `json:"ip_last"`
	// Evidence/Conversations 只在详情响应里出现（指针区分"不返回"与"返回空数组"）。
	Evidence      *[]VisitorProfileEvidence     `json:"evidence,omitempty"`
	Conversations *[]VisitorProfileConversation `json:"conversations,omitempty"`
}

// VisitorProfileSettings 是访客画像的单例配置。
// ThirdPartyKey 为明文（json:"-"），只允许服务端内部使用；对外只暴露末四位掩码。
// GeoIPDir 落库列名为 geoip_dir，接口契约键名为 geoip_db_path。
type VisitorProfileSettings struct {
	Enabled                bool       `json:"enabled"`
	GeoIPDir               string     `json:"geoip_db_path"`
	ThirdPartyURL          string     `json:"third_party_url"`
	ThirdPartyKey          string     `json:"-"`
	ThirdPartyKeyMasked    string     `json:"third_party_key_masked"`
	ThirdPartyTimeoutMS    int        `json:"third_party_timeout_ms"`
	CacheTTLHours          int        `json:"cache_ttl_hours"`
	AISummaryEnabled       bool       `json:"ai_summary_enabled"`
	AISummaryEveryMessages int        `json:"ai_summary_every_messages"`
	ShowFullIP             bool       `json:"show_full_ip"`
	UpdatedAt              *time.Time `json:"updated_at,omitempty"`
}

// VisitorProfileSettingsPatch 描述画像设置的局部更新；nil 表示不修改。
// ThirdPartyKey：nil=不改，""=清除，其它=覆盖。
type VisitorProfileSettingsPatch struct {
	Enabled                *bool
	GeoIPDir               *string
	ThirdPartyURL          *string
	ThirdPartyKey          *string
	ThirdPartyTimeoutMS    *int
	CacheTTLHours          *int
	AISummaryEnabled       *bool
	AISummaryEveryMessages *int
	ShowFullIP             *bool
}

// VisitorProfileQuery 是画像列表的筛选与分页条件。
type VisitorProfileQuery struct {
	Query   string
	Country string
	Tag     string
	Limit   int
	Offset  int
}

// DefaultVisitorProfileSettings 返回画像配置的默认值。
func DefaultVisitorProfileSettings() VisitorProfileSettings {
	return VisitorProfileSettings{
		Enabled:                true,
		ThirdPartyTimeoutMS:    DefaultThirdPartyTimeoutMS,
		CacheTTLHours:          DefaultThirdPartyCacheTTLHour,
		AISummaryEnabled:       true,
		AISummaryEveryMessages: DefaultAISummaryEveryMessages,
		ShowFullIP:             true,
	}
}

// NormalizeVisitorProfileSettings 归一化画像配置：
// 空/越界的数值回落默认值并收敛到合法区间，密钥掩码按明文重新计算。
func NormalizeVisitorProfileSettings(next VisitorProfileSettings) VisitorProfileSettings {
	out := next
	out.GeoIPDir = strings.TrimSpace(out.GeoIPDir)
	out.ThirdPartyURL = strings.TrimSpace(out.ThirdPartyURL)
	out.ThirdPartyKey = strings.TrimSpace(out.ThirdPartyKey)
	if out.ThirdPartyTimeoutMS <= 0 {
		out.ThirdPartyTimeoutMS = DefaultThirdPartyTimeoutMS
	}
	if out.ThirdPartyTimeoutMS < MinThirdPartyTimeoutMS {
		out.ThirdPartyTimeoutMS = MinThirdPartyTimeoutMS
	}
	if out.ThirdPartyTimeoutMS > MaxThirdPartyTimeoutMS {
		out.ThirdPartyTimeoutMS = MaxThirdPartyTimeoutMS
	}
	if out.CacheTTLHours <= 0 {
		out.CacheTTLHours = DefaultThirdPartyCacheTTLHour
	}
	if out.CacheTTLHours > MaxCacheTTLHours {
		out.CacheTTLHours = MaxCacheTTLHours
	}
	if out.AISummaryEveryMessages <= 0 {
		out.AISummaryEveryMessages = DefaultAISummaryEveryMessages
	}
	if out.AISummaryEveryMessages > MaxAISummaryEveryMessages {
		out.AISummaryEveryMessages = MaxAISummaryEveryMessages
	}
	out.ThirdPartyKeyMasked = MaskSecret(out.ThirdPartyKey)
	return out
}

// MaskSecret 返回密钥掩码（保留末四位）；空值返回空串。
func MaskSecret(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 4 {
		return "****"
	}
	return "****" + value[len(value)-4:]
}

// MaskIP 按 show_full_ip=false 的要求脱敏 IP：IPv4 保留前三段（1.2.3.*），
// IPv6 保留前 3 组（2001:db8:85a3:*）；无法解析时原样返回空串。
func MaskIP(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed := net.ParseIP(value)
	if parsed == nil {
		return value
	}
	if v4 := parsed.To4(); v4 != nil {
		return strings.Join([]string{
			itoa(v4[0]), itoa(v4[1]), itoa(v4[2]), "*",
		}, ".")
	}
	// IPv6：保留前三组十六进制，其余用 * 代替。
	parts := strings.Split(parsed.String(), ":")
	if len(parts) <= 3 {
		return parsed.String()
	}
	return strings.Join(parts[:3], ":") + ":*"
}

func itoa(value byte) string {
	if value == 0 {
		return "0"
	}
	var buf [3]byte
	index := len(buf)
	for value > 0 {
		index--
		buf[index] = '0' + value%10
		value /= 10
	}
	return string(buf[index:])
}

// NormalizeVisitorProfile 归一化画像字段：去空格、JSONB 字段补空集合、
// 置信度收敛到 [0, 0.95]、语言计数去掉非正数。
func NormalizeVisitorProfile(profile VisitorProfile) VisitorProfile {
	out := profile
	out.VisitorID = strings.TrimSpace(out.VisitorID)
	out.Country = strings.TrimSpace(out.Country)
	out.CountryCode = NormalizeCountryCode(out.CountryCode)
	// 只有 ISO 码、没有可读名时用内置中文名补齐，保证 country 可直接展示。
	if out.Country == "" && out.CountryCode != "" {
		out.Country = CountryName(out.CountryCode)
	}
	out.Region = strings.TrimSpace(out.Region)
	out.City = strings.TrimSpace(out.City)
	out.Timezone = strings.TrimSpace(out.Timezone)
	out.NationalityGuess = NormalizeCountryCode(out.NationalityGuess)
	if out.NationalityConfidence < 0 {
		out.NationalityConfidence = 0
	}
	if out.NationalityConfidence > MaxNationalityConfidence {
		out.NationalityConfidence = MaxNationalityConfidence
	}
	out.NationalityBasis = cleanStringList(out.NationalityBasis)
	out.IPFirst = strings.TrimSpace(out.IPFirst)
	out.IPLast = strings.TrimSpace(out.IPLast)
	out.Languages = cleanLanguageCounts(out.Languages)
	out.Dialect = strings.TrimSpace(out.Dialect)
	out.Summary = strings.TrimSpace(out.Summary)
	out.Tags = cleanStringList(out.Tags)
	out.Interests = cleanStringList(out.Interests)
	out.Evidence = cleanEvidence(out.Evidence)
	out.LastPath = strings.TrimSpace(out.LastPath)
	if out.Visits < 0 {
		out.Visits = 0
	}
	return out
}

func cleanStringList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func cleanLanguageCounts(counts map[string]int) map[string]int {
	out := map[string]int{}
	for lang, count := range counts {
		lang = strings.TrimSpace(lang)
		if lang == "" || count <= 0 {
			continue
		}
		out[lang] = count
	}
	return out
}

func cleanEvidence(entries []VisitorProfileEvidence) []VisitorProfileEvidence {
	out := make([]VisitorProfileEvidence, 0, len(entries))
	seen := map[string]bool{}
	for _, entry := range entries {
		entry.MessageID = strings.TrimSpace(entry.MessageID)
		if entry.MessageID == "" || seen[entry.MessageID] {
			continue
		}
		seen[entry.MessageID] = true
		entry.Text = strings.TrimSpace(entry.Text)
		out = append(out, entry)
	}
	return out
}

// LanguagePercentages 返回各语种的百分比（四舍五入到整数，按语种码排序），
// 供控制台展示"语种占比"。没有任何计数时返回空 map。
func LanguagePercentages(counts map[string]int) map[string]int {
	total := 0
	for _, count := range counts {
		if count > 0 {
			total += count
		}
	}
	out := map[string]int{}
	if total == 0 {
		return out
	}
	for lang, count := range counts {
		if count <= 0 {
			continue
		}
		out[lang] = int(float64(count)/float64(total)*100 + 0.5)
	}
	return out
}

// SortedLanguages 按计数从高到低返回语种码（同数按字母序），便于稳定展示与测试。
func SortedLanguages(counts map[string]int) []string {
	type item struct {
		lang  string
		count int
	}
	items := make([]item, 0, len(counts))
	for lang, count := range counts {
		if count > 0 {
			items = append(items, item{lang: lang, count: count})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count != items[j].count {
			return items[i].count > items[j].count
		}
		return items[i].lang < items[j].lang
	})
	out := make([]string, 0, len(items))
	for _, entry := range items {
		out = append(out, entry.lang)
	}
	return out
}
