package domain

import (
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

type Agent struct {
	ID                     string      `json:"id"`
	Account                string      `json:"account"`
	Name                   string      `json:"name"`
	Group                  string      `json:"group"`
	Password               string      `json:"-"`
	Status                 AgentStatus `json:"status"`
	DeviceToken            string      `json:"device_token,omitempty"`
	DevicePlatform         string      `json:"device_platform,omitempty"`
	MaxConversations       int         `json:"max_conversations"`
	CurrentConversations   int         `json:"current_conversations"`
	AvatarURL              string      `json:"avatar_url,omitempty"`
	ManualSatisfactionRate *float64    `json:"manual_satisfaction_rate,omitempty"`
	DisabledAt             *time.Time  `json:"disabled_at,omitempty"`
	Created                time.Time   `json:"created_at"`
	Updated                time.Time   `json:"updated_at"`
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
	ID               string             `json:"id"`
	VisitorID        string             `json:"visitor_id"`
	VisitorIP        string             `json:"visitor_ip"`
	VisitorIPSource  string             `json:"visitor_ip_source,omitempty"`
	VisitorRemark    string             `json:"visitor_remark"`
	RemarkUpdatedBy  string             `json:"remark_updated_by,omitempty"`
	RemarkUpdatedAt  *time.Time         `json:"remark_updated_at,omitempty"`
	Status           ConversationStatus `json:"status"`
	AssignedAgentID  string             `json:"assigned_agent_id,omitempty"`
	// AssignedAgentName/AssignedAgentAvatar 是只读派生字段：按 AssignedAgentID 补齐，
	// 供访客端展示真实客服，不落库、不参与写入。
	AssignedAgentName   string `json:"assigned_agent_name,omitempty"`
	AssignedAgentAvatar string `json:"assigned_agent_avatar,omitempty"`
	Source              string `json:"source"`
	VisitorLang      string             `json:"visitor_lang,omitempty"`
	LastMessage      string             `json:"last_message"`
	LastMessageAt    *time.Time         `json:"last_message_at,omitempty"`
	UnreadForAgent   int                `json:"unread_for_agent"`
	UnreadForVisitor int                `json:"unread_for_visitor"`
	CreatedAt        time.Time          `json:"created_at"`
	UpdatedAt        time.Time          `json:"updated_at"`
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
