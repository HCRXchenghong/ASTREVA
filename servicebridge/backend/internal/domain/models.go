package domain

import "time"

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
	Source           string             `json:"source"`
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
