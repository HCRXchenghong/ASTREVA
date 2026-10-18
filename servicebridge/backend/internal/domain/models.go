package domain

import (
	"net"
	"net/url"
	"strings"
	"time"
)

type AccountKind string

const (
	AccountAdmin   AccountKind = "admin"
	AccountAgent   AccountKind = "agent"
	AccountVisitor AccountKind = "visitor"
)

type AgentStatus string

const (
	AgentOnline  AgentStatus = "online"
	AgentBusy    AgentStatus = "busy"
	AgentOffline AgentStatus = "offline"
)

type ConversationStatus string

const (
	ConversationWaiting        ConversationStatus = "waiting"
	ConversationAssigned       ConversationStatus = "assigned"
	ConversationAIServing      ConversationStatus = "ai_serving"
	ConversationHumanRequested ConversationStatus = "human_requested"
	ConversationClosed         ConversationStatus = "closed"
)

type SenderType string

const (
	SenderVisitor SenderType = "visitor"
	SenderAgent   SenderType = "agent"
	SenderAI      SenderType = "ai"
	SenderSystem  SenderType = "system"
)

type MessageType string

const (
	MessageText             MessageType = "text"
	MessageEmoji            MessageType = "emoji"
	MessageImage            MessageType = "image"
	MessageAudio            MessageType = "audio"
	MessageContactTelephone MessageType = "contact_telephone"
	MessageContactWhatsApp  MessageType = "contact_whatsapp"
	MessageContactTelegram  MessageType = "contact_telegram"
	MessageSystem           MessageType = "system"
	MessageAIText           MessageType = "ai_text"
	MessageHandoff          MessageType = "handoff_request"
	MessageRevoked          MessageType = "revoked"
)

type AICustomerMode string

const (
	AIModeHumanFirst AICustomerMode = "human_first"
	AIModeAlwaysAI   AICustomerMode = "always_ai"
	AIModeManualOnly AICustomerMode = "manual_only"
)

type AdminUser struct {
	ID       string    `json:"id"`
	Account  string    `json:"account"`
	Name     string    `json:"name"`
	Password string    `json:"-"`
	Created  time.Time `json:"created_at"`
}

// 翻译方言模式取值。
const (
	DialectModeStandard = "standard"
	DialectModeDialect  = "dialect"
)

// AgentPreferences 是客服账号级的翻译与辅助偏好。
// 使用指针字段区分"未设置"与显式 false，读取时通过 WithDefaults 填充默认值。
type AgentPreferences struct {
	AutoTranslateIncoming *bool  `json:"auto_translate_incoming,omitempty"`        // 访客消息自动译成中文，默认 true
	AutoTranslateOutgoing *bool  `json:"auto_translate_outgoing,omitempty"`        // 我的消息自动译成访客语言，默认 true
	ReviewTranslation     *bool  `json:"review_translation_before_send,omitempty"` // 发送前确认/编辑译文，默认 true
	DialectMode           string `json:"translation_dialect_mode,omitempty"`       // "standard"（默认）| "dialect"
	AISuggestReplies      *bool  `json:"ai_suggest_replies,omitempty"`             // AI 推荐发送内容，默认 true
}

// NormalizeDialectMode 归一化方言模式：空或非法值一律回落到 standard。
func NormalizeDialectMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case DialectModeDialect:
		return DialectModeDialect
	default:
		return DialectModeStandard
	}
}

// ValidDialectMode 判断方言模式是否为允许的取值（standard / dialect）。
func ValidDialectMode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case DialectModeStandard, DialectModeDialect:
		return true
	default:
		return false
	}
}

// WithDefaults 返回填充默认值后的偏好副本：nil 布尔值按 true 处理，
// DialectMode 为空或非法时填 "standard"。
func (p AgentPreferences) WithDefaults() AgentPreferences {
	out := p
	if out.AutoTranslateIncoming == nil {
		value := true
		out.AutoTranslateIncoming = &value
	}
	if out.AutoTranslateOutgoing == nil {
		value := true
		out.AutoTranslateOutgoing = &value
	}
	if out.ReviewTranslation == nil {
		value := true
		out.ReviewTranslation = &value
	}
	if out.AISuggestReplies == nil {
		value := true
		out.AISuggestReplies = &value
	}
	out.DialectMode = NormalizeDialectMode(out.DialectMode)
	return out
}

type Agent struct {
	ID                     string           `json:"id"`
	Account                string           `json:"account"`
	Name                   string           `json:"name"`
	Group                  string           `json:"group"`
	Password               string           `json:"-"`
	Status                 AgentStatus      `json:"status"`
	DeviceToken            string           `json:"device_token,omitempty"`
	DevicePlatform         string           `json:"device_platform,omitempty"`
	MaxConversations       int              `json:"max_conversations"`
	CurrentConversations   int              `json:"current_conversations"`
	AvatarURL              string           `json:"avatar_url,omitempty"`
	ManualSatisfactionRate *float64         `json:"manual_satisfaction_rate,omitempty"`
	Preferences            AgentPreferences `json:"preferences"`
	DisabledAt             *time.Time       `json:"disabled_at,omitempty"`
	Created                time.Time        `json:"created_at"`
	Updated                time.Time        `json:"updated_at"`
}

type Visitor struct {
	ID        string    `json:"id"`
	IP        string    `json:"ip"`
	Remark    string    `json:"remark"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
}

type AgentNotification struct {
	AgentID        string             `json:"agent_id"`
	ConversationID string             `json:"conversation_id"`
	Title          string             `json:"title"`
	Body           string             `json:"body"`
	Status         ConversationStatus `json:"status"`
}

type AuditEvent struct {
	ID          string      `json:"id"`
	ActorKind   AccountKind `json:"actor_kind"`
	ActorID     string      `json:"actor_id"`
	Action      string      `json:"action"`
	Resource    string      `json:"resource"`
	ResourceID  string      `json:"resource_id"`
	IPAddress   string      `json:"ip_address"`
	UserAgent   string      `json:"user_agent"`
	Description string      `json:"description"`
	CreatedAt   time.Time   `json:"created_at"`
}

type Conversation struct {
	ID              string             `json:"id"`
	VisitorID       string             `json:"visitor_id"`
	VisitorIP       string             `json:"visitor_ip"`
	VisitorIPSource string             `json:"visitor_ip_source,omitempty"`
	VisitorRemark   string             `json:"visitor_remark"`
	RemarkUpdatedBy string             `json:"remark_updated_by,omitempty"`
	RemarkUpdatedAt *time.Time         `json:"remark_updated_at,omitempty"`
	Status          ConversationStatus `json:"status"`
	AssignedAgentID string             `json:"assigned_agent_id,omitempty"`
	// AssignedAgentName/AssignedAgentAvatar 是只读派生字段：按 AssignedAgentID 补齐，
	// 供访客端展示真实客服，不落库、不参与写入。
	AssignedAgentName   string `json:"assigned_agent_name,omitempty"`
	AssignedAgentAvatar string `json:"assigned_agent_avatar,omitempty"`
	Source              string `json:"source"`
	VisitorLang         string `json:"visitor_lang,omitempty"`
	// DetectedLang 是最近一条访客消息检测出的语种；ReplyLangOverride 来自
	// 客户显式要求（如"请用英语回答我"），整个会话持续生效，客服可清除。
	DetectedLang        string         `json:"detected_lang,omitempty"`
	ReplyLangOverride   string         `json:"reply_lang_override,omitempty"`
	ReplyLangOverrideAt *time.Time     `json:"reply_lang_override_at,omitempty"`
	LanguageStats       map[string]int `json:"language_stats,omitempty"`
	LastMessage         string         `json:"last_message"`
	LastMessageAt       *time.Time     `json:"last_message_at,omitempty"`
	UnreadForAgent      int            `json:"unread_for_agent"`
	UnreadForVisitor    int            `json:"unread_for_visitor"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
}

type Message struct {
	ServerMsgID    string      `json:"server_msg_id"`
	ClientMsgID    string      `json:"client_msg_id,omitempty"`
	ConversationID string      `json:"conversation_id"`
	SenderType     SenderType  `json:"sender_type"`
	SenderID       string      `json:"sender_id"`
	MessageType    MessageType `json:"message_type"`
	Content        string      `json:"content"`
	// Translations 保存该消息面向其他语言的译文（语言代码 -> 译文）。原文始终保存在 Content。
	Translations map[string]string `json:"translations,omitempty"`
	// OriginalContent 仅在下发时填充：当 Content 被替换为译文时保留原文，供客服端「查看原文」使用。
	OriginalContent string `json:"original_content,omitempty"`
	// DetectedLang 是这条消息实际使用的语言（访客消息=访客语言，客服消息=客服写的语言）。
	DetectedLang string `json:"detected_lang,omitempty"`
	// AuthorLang 是内容的原始语言，翻译的源语言。
	AuthorLang string `json:"author_lang,omitempty"`
	// TranslationStatus 记录本条消息的翻译状态：pending / done / failed / skipped。
	TranslationStatus string `json:"translation_status,omitempty"`
	// HiddenFromVisitor 仅在生成访客视图时使用：该消息不应展示给访客（如仅客服端可见的转接提示）。
	HiddenFromVisitor bool        `json:"-"`
	CreatedAt         time.Time   `json:"created_at"`
	RevokedAt         *time.Time  `json:"revoked_at,omitempty"`
	RevokedByKind     AccountKind `json:"revoked_by_kind,omitempty"`
	RevokedByID       string      `json:"revoked_by_id,omitempty"`
}

type KeywordRule struct {
	ID                 string `json:"id"`
	Keyword            string `json:"keyword"`
	MatchType          string `json:"match_type"`
	Reply              string `json:"reply"`
	Enabled            bool   `json:"enabled"`
	Priority           int    `json:"priority"`
	Action             string `json:"action"`
	ShowInQuickReplies bool   `json:"show_in_quick_replies"`
	QuickReplyText     string `json:"quick_reply_text"`
}

type ContactSettings struct {
	Telephone  string `json:"telephone"`
	WhatsApp   string `json:"whatsapp"`
	Telegram   string `json:"telegram"`
	EntryReply string `json:"entry_reply"`
}

type QuickReply struct {
	RuleID   string `json:"rule_id"`
	Text     string `json:"text"`
	SendText string `json:"send_text"`
}

type VisitorWidgetSettings struct {
	Telephone    string       `json:"telephone"`
	WhatsApp     string       `json:"whatsapp"`
	Telegram     string       `json:"telegram"`
	EntryReply   string       `json:"entry_reply"`
	QuickReplies []QuickReply `json:"quick_replies"`
}

type ServiceRating struct {
	ID              string    `json:"id"`
	ConversationID  string    `json:"conversation_id"`
	VisitorID       string    `json:"visitor_id"`
	AssignedAgentID string    `json:"assigned_agent_id,omitempty"`
	Score           int       `json:"score"`
	Tags            []string  `json:"tags,omitempty"`
	Comment         string    `json:"comment,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type RatingSummary struct {
	Total            int     `json:"total"`
	Average          float64 `json:"average"`
	Satisfied        int     `json:"satisfied"`
	Neutral          int     `json:"neutral"`
	Unsatisfied      int     `json:"unsatisfied"`
	SatisfactionRate float64 `json:"satisfaction_rate"`
}

type DashboardStats struct {
	TotalConversations  int           `json:"total_conversations"`
	ActiveConversations int           `json:"active_conversations"`
	AIServing           int           `json:"ai_serving"`
	Waiting             int           `json:"waiting"`
	HumanRequested      int           `json:"human_requested"`
	Assigned            int           `json:"assigned"`
	Closed              int           `json:"closed"`
	OnlineAgents        int           `json:"online_agents"`
	TotalAgents         int           `json:"total_agents"`
	Rating              RatingSummary `json:"rating"`
}

type AISettings struct {
	Enabled         bool           `json:"enabled"`
	Mode            AICustomerMode `json:"mode"`
	BaseURL         string         `json:"base_url"`
	APIKey          string         `json:"-"`
	APIKeyMasked    string         `json:"api_key_masked"`
	Model           string         `json:"model"`
	APIType         string         `json:"api_type"`
	Temperature     float64        `json:"temperature"`
	MaxOutputTokens int            `json:"max_output_tokens"`
	TimeoutSeconds  int            `json:"timeout_seconds"`
	SystemPrompt    string         `json:"system_prompt"`
	// CacheMode 为当前 Model 解析出的缓存模式（运行时字段，不入库）：
	// auto=上游自动提示词缓存；explicit=需显式标记（Anthropic）；空=不启用缓存优化。
	CacheMode string `json:"-"`
}

// AICacheProbe 记录各模型的提示词缓存探测结果，与 ai_settings 分表存放。
// Mode 取值：auto=自动缓存；explicit=需显式 cache_control；none=不支持。
type AICacheProbe struct {
	ModelResults map[string]string `json:"model_results"`
	Status       string            `json:"status"`
	Error        string            `json:"error,omitempty"`
	ProbedAt     *time.Time        `json:"probed_at,omitempty"`
}

// AI 供应商类型：文本模型与图像模型分开配置，能力绑定时据此校验。
const (
	AIProviderKindText  = "text"
	AIProviderKindImage = "image"
)

// AI 上游协议类型：前三者用于文本对话，images 用于图像生成。
const (
	AIAPITypeChatCompletions   = "chat_completions"
	AIAPITypeResponses         = "responses"
	AIAPITypeAnthropicMessages = "anthropic_messages"
	AIAPITypeImages            = "images"
)

// AI 能力标识：后台可把每项能力绑定到独立的供应商与模型。
const (
	AICapabilityReception   = "reception"
	AICapabilityTranslation = "translation"
	AICapabilityDetection   = "detection"
	AICapabilitySEO         = "seo"
	AICapabilityImage       = "image"
)

// AICapabilities 返回全部合法能力标识（顺序稳定，便于后台渲染与测试断言）。
func AICapabilities() []string {
	return []string{
		AICapabilityReception,
		AICapabilityTranslation,
		AICapabilityDetection,
		AICapabilitySEO,
		AICapabilityImage,
	}
}

// ValidAICapability 判断能力标识是否合法。
func ValidAICapability(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case AICapabilityReception, AICapabilityTranslation, AICapabilityDetection, AICapabilitySEO, AICapabilityImage:
		return true
	default:
		return false
	}
}

// AICapabilityKind 返回能力要求的供应商类型：图像能力需要 image，其余均为 text。
func AICapabilityKind(capability string) string {
	if strings.EqualFold(strings.TrimSpace(capability), AICapabilityImage) {
		return AIProviderKindImage
	}
	return AIProviderKindText
}

// ValidAIProviderKind 判断供应商类型是否合法。
func ValidAIProviderKind(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case AIProviderKindText, AIProviderKindImage:
		return true
	default:
		return false
	}
}

// ValidAIAPIType 判断上游协议类型是否合法。
func ValidAIAPIType(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case AIAPITypeChatCompletions, AIAPITypeResponses, AIAPITypeAnthropicMessages, AIAPITypeImages:
		return true
	default:
		return false
	}
}

// AIProvider 是一个可复用的 AI 上游供应商配置。
// APIKey 明文只存在于服务端内存，json:"-" 保证任何普通响应都不会下发；
// 对外只暴露 APIKeyMasked（末四位掩码），仅服务间的 /secret 接口返回明文。
type AIProvider struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Kind         string    `json:"kind"`
	APIType      string    `json:"api_type"`
	BaseURL      string    `json:"base_url"`
	APIKey       string    `json:"-"`
	APIKeyMasked string    `json:"api_key_masked"`
	Models       []string  `json:"models"`
	DefaultModel string    `json:"default_model,omitempty"`
	TimeoutMS    int       `json:"timeout_ms"`
	AllowedHosts string    `json:"allowed_hosts,omitempty"`
	Enabled      bool      `json:"enabled"`
	SortOrder    int       `json:"sort_order"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AIBinding 把某项能力绑定到某个供应商与模型；ProviderID 为空表示解除绑定。
type AIBinding struct {
	Capability string         `json:"capability"`
	ProviderID string         `json:"provider_id"`
	Model      string         `json:"model,omitempty"`
	Params     map[string]any `json:"params,omitempty"`
	UpdatedAt  time.Time      `json:"updated_at,omitempty"`
}

// NormalizeAllowedHosts 归一化白名单：去空格、转小写、去重、逗号连接。
func NormalizeAllowedHosts(value string) string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		entry := strings.ToLower(strings.TrimSpace(part))
		if entry == "" || seen[entry] {
			continue
		}
		seen[entry] = true
		out = append(out, entry)
	}
	return strings.Join(out, ",")
}

// ValidateAllowedHosts 校验白名单条目格式：每项只能是主机名、主机:端口或 IP，
// 允许 "*." / "." 前缀表示子域名通配；空字符串表示不限制。
func ValidateAllowedHosts(value string) bool {
	for _, part := range strings.Split(value, ",") {
		entry := strings.TrimSpace(part)
		if entry == "" {
			continue
		}
		host, _, ok := parseAllowedHostEntry(entry)
		if !ok || strings.TrimSpace(host) == "" {
			return false
		}
	}
	return true
}

// HostAllowed 判断目标 URL 的主机是否命中白名单；allowedHosts 为空时表示不限制。
// 这是防 SSRF 的第一道校验：绑定供应商的 base_url 必须与 allowed_hosts 匹配。
func HostAllowed(rawURL, allowedHosts string) bool {
	if strings.TrimSpace(allowedHosts) == "" {
		return true
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	port := parsed.Port()
	for _, part := range strings.Split(allowedHosts, ",") {
		entry := strings.TrimSpace(part)
		if entry == "" {
			continue
		}
		wildcard := strings.HasPrefix(entry, "*.") || strings.HasPrefix(entry, ".")
		entryHost, entryPort, ok := parseAllowedHostEntry(entry)
		if !ok {
			continue
		}
		entryHost = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(entryHost), "."))
		if entryPort != "" && entryPort != port {
			continue
		}
		if wildcard {
			if host == entryHost || strings.HasSuffix(host, "."+entryHost) {
				return true
			}
			continue
		}
		if host == entryHost {
			return true
		}
	}
	return false
}

// parseAllowedHostEntry 解析单个白名单条目，返回主机与可选端口。
func parseAllowedHostEntry(entry string) (host, port string, ok bool) {
	raw := strings.TrimSpace(entry)
	raw = strings.TrimPrefix(raw, "*.")
	raw = strings.TrimPrefix(raw, ".")
	if raw == "" {
		return "", "", false
	}
	// 白名单只允许主机与端口，出现协议、路径、查询串一律视为非法。
	if strings.Contains(raw, "://") || strings.ContainsAny(raw, " \t\r\n/?#@") {
		return "", "", false
	}
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		return strings.TrimSuffix(strings.TrimPrefix(raw, "["), "]"), "", true
	}
	if hostPart, portPart, err := net.SplitHostPort(raw); err == nil {
		return hostPart, portPart, true
	}
	// 纯 IPv6（含多个冒号）没有端口，整体作为主机。
	return raw, "", true
}

type BusinessHours struct {
	Timezone string `json:"timezone"`
	Start    string `json:"start"`
	End      string `json:"end"`
	Enabled  bool   `json:"enabled"`
	// OfflineReply 为非营业时间给访客的留言提示；为空时使用内置文案。
	OfflineReply string `json:"offline_reply"`
}

// PushPlatform 是客服设备的推送平台，决定使用哪条推送通道。
type PushPlatform string

const (
	PushPlatformAndroid PushPlatform = "android"
	PushPlatformIOS     PushPlatform = "ios"
)

// PushSettings 是可在统一后台配置的推送参数。
// 客户端可见的 Firebase 参数与仅服务端保存的密钥分开：
// 带 json:"-" 的字段永远不会下发到浏览器或 App。
type PushSettings struct {
	FirebaseAPIKey            string `json:"firebase_api_key"`
	FirebaseAppID             string `json:"firebase_app_id"`
	FirebaseMessagingSenderID string `json:"firebase_messaging_sender_id"`
	FirebaseProjectID         string `json:"firebase_project_id"`
	FirebaseStorageBucket     string `json:"firebase_storage_bucket"`
	FirebaseVapidKey          string `json:"firebase_vapid_key"`

	// 服务端密钥：Firebase 服务账号 JSON（用于自动换取 OAuth2 令牌）
	// 或直接提供的短期 access token；以及 APNs 的 .p8 私钥。
	FCMServiceAccountJSON string `json:"-"`
	FCMAccessToken        string `json:"-"`
	APNsKeyP8             string `json:"-"`

	APNsKeyID       string `json:"apns_key_id"`
	APNsTeamID      string `json:"apns_team_id"`
	APNsTopic       string `json:"apns_topic"`
	APNsEnvironment string `json:"apns_environment"`

	// 只读状态字段，供后台展示“是否已配置”和掩码。
	FCMConfigured    bool       `json:"fcm_configured"`
	APNsConfigured   bool       `json:"apns_configured"`
	FCMSecretMasked  string     `json:"fcm_secret_masked"`
	APNsSecretMasked string     `json:"apns_secret_masked"`
	UpdatedAt        *time.Time `json:"updated_at,omitempty"`
}

// FirebaseClientConfig 是下发给客服 App 的公开配置（不含任何服务端密钥）。
type FirebaseClientConfig struct {
	APIKey            string `json:"api_key"`
	AppID             string `json:"app_id"`
	MessagingSenderID string `json:"messaging_sender_id"`
	ProjectID         string `json:"project_id"`
	StorageBucket     string `json:"storage_bucket"`
	VapidKey          string `json:"vapid_key"`
}

// PushEnabled 表示是否至少配置了一条可用的推送通道。
func (p PushSettings) PushEnabled() bool {
	return p.FCMConfigured || p.APNsConfigured
}

// ClientConfig 返回可安全下发给 App 的 Firebase 参数。
func (p PushSettings) ClientConfig() FirebaseClientConfig {
	return FirebaseClientConfig{
		APIKey:            p.FirebaseAPIKey,
		AppID:             p.FirebaseAppID,
		MessagingSenderID: p.FirebaseMessagingSenderID,
		ProjectID:         p.FirebaseProjectID,
		StorageBucket:     p.FirebaseStorageBucket,
		VapidKey:          p.FirebaseVapidKey,
	}
}

// Normalize 根据密钥是否存在刷新“是否已配置”与掩码字段。
// 两个存储实现都在读写后调用它，保证接口对外表现一致。
func (p *PushSettings) Normalize() {
	p.FCMConfigured = strings.TrimSpace(p.FCMServiceAccountJSON) != "" || strings.TrimSpace(p.FCMAccessToken) != ""
	p.APNsConfigured = strings.TrimSpace(p.APNsKeyP8) != "" &&
		strings.TrimSpace(p.APNsKeyID) != "" &&
		strings.TrimSpace(p.APNsTeamID) != "" &&
		strings.TrimSpace(p.APNsTopic) != ""
	switch strings.ToLower(strings.TrimSpace(p.APNsEnvironment)) {
	case "sandbox", "production":
		p.APNsEnvironment = strings.ToLower(strings.TrimSpace(p.APNsEnvironment))
	default:
		p.APNsEnvironment = "production"
	}
	p.FCMSecretMasked = maskPushSecret(p.FCMServiceAccountJSON, p.FCMAccessToken)
	p.APNsSecretMasked = maskPushSecret(p.APNsKeyP8, "")
}

func maskPushSecret(primary, fallback string) string {
	value := strings.TrimSpace(primary)
	if value == "" {
		value = strings.TrimSpace(fallback)
	}
	if value == "" {
		return ""
	}
	if len(value) <= 4 {
		return "****"
	}
	return "****" + value[len(value)-4:]
}

type AuthSession struct {
	Token     string      `json:"token"`
	Kind      AccountKind `json:"kind"`
	AccountID string      `json:"account_id"`
	CreatedAt time.Time   `json:"created_at"`
	ExpiresAt time.Time   `json:"expires_at"`
}

type VisitorSession struct {
	Token           string       `json:"token"`
	Visitor         Visitor      `json:"visitor"`
	Conversation    Conversation `json:"conversation"`
	InitialMessages []Message    `json:"initial_messages"`
}

type AppRelease struct {
	ID           string    `json:"id"`
	Platform     string    `json:"platform"`
	VersionName  string    `json:"version_name"`
	VersionCode  int64     `json:"version_code"`
	FileName     string    `json:"file_name"`
	FilePath     string    `json:"-"`
	DownloadURL  string    `json:"download_url,omitempty"`
	FileSize     int64     `json:"file_size"`
	Checksum     string    `json:"checksum"`
	ReleaseNotes string    `json:"release_notes"`
	ForceUpdate  bool      `json:"force_update"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
}

// APIApp 第三方平台 API 授权。API Key 只保存 SHA-256 哈希，
// 明文仅在创建/重置时返回一次。
type APIApp struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	KeyPrefix    string     `json:"key_prefix"`
	KeyHash      string     `json:"-"`
	Enabled      bool       `json:"enabled"`
	RateLimit    int        `json:"rate_limit"`
	Note         string     `json:"note"`
	RequestCount int64      `json:"request_count"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}
