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
	MessageText          MessageType = "text"
	MessageEmoji         MessageType = "emoji"
	MessageImage         MessageType = "image"
	MessageAudio         MessageType = "audio"
	MessageContactPhone  MessageType = "contact_phone"
	MessageContactWechat MessageType = "contact_wechat"
	MessageSystem        MessageType = "system"
	MessageAIText        MessageType = "ai_text"
	MessageHandoff       MessageType = "handoff_request"
	MessageRevoked       MessageType = "revoked"
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
	ID                   string      `json:"id"`
	Account              string      `json:"account"`
	Name                 string      `json:"name"`
	Group                string      `json:"group"`
	Password             string      `json:"-"`
	Status               AgentStatus `json:"status"`
	MaxConversations     int         `json:"max_conversations"`
	CurrentConversations int         `json:"current_conversations"`
	DisabledAt           *time.Time  `json:"disabled_at,omitempty"`
	Created              time.Time   `json:"created_at"`
	Updated              time.Time   `json:"updated_at"`
}

type Visitor struct {
	ID        string    `json:"id"`
	IP        string    `json:"ip"`
	Remark    string    `json:"remark"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
}

type PushDevice struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Platform  string    `json:"platform"`
	Token     string    `json:"token"`
	Provider  string    `json:"provider"`
	Enabled   bool      `json:"enabled"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

type AgentNotification struct {
	AgentID        string             `json:"agent_id"`
	ConversationID string             `json:"conversation_id"`
	Title          string             `json:"title"`
	Body           string             `json:"body"`
	Status         ConversationStatus `json:"status"`
}

type FeishuSettings struct {
	Enabled                 bool      `json:"enabled"`
	BaseURL                 string    `json:"base_url"`
	AppID                   string    `json:"app_id"`
	AppSecret               string    `json:"-"`
	AppSecretMasked         string    `json:"app_secret_masked"`
	VerificationToken       string    `json:"-"`
	VerificationTokenMasked string    `json:"verification_token_masked"`
	EncryptKey              string    `json:"-"`
	EncryptKeyMasked        string    `json:"encrypt_key_masked"`
	DefaultChatID           string    `json:"default_chat_id"`
	AgentID                 string    `json:"agent_id"`
	TimeoutSeconds          int       `json:"timeout_seconds"`
	UpdatedAt               time.Time `json:"updated_at,omitempty"`
}

type FeishuMessageBinding struct {
	MessageID      string    `json:"message_id"`
	ConversationID string    `json:"conversation_id"`
	ChatID         string    `json:"chat_id"`
	RootMessageID  string    `json:"root_message_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
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
	VisitorRemark    string             `json:"visitor_remark"`
	RemarkUpdatedBy  string             `json:"remark_updated_by,omitempty"`
	RemarkUpdatedAt  *time.Time         `json:"remark_updated_at,omitempty"`
	Status           ConversationStatus `json:"status"`
	AssignedAgentID  string             `json:"assigned_agent_id,omitempty"`
	Source           string             `json:"source"`
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
	CreatedAt      time.Time   `json:"created_at"`
	RevokedAt      *time.Time  `json:"revoked_at,omitempty"`
	RevokedByKind  AccountKind `json:"revoked_by_kind,omitempty"`
	RevokedByID    string      `json:"revoked_by_id,omitempty"`
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
	Phone           string `json:"phone"`
	Wechat          string `json:"wechat"`
	WechatReplyType string `json:"wechat_reply_type"`
	WechatImageURL  string `json:"wechat_image_url"`
	QQ              string `json:"qq"`
	QQReplyType     string `json:"qq_reply_type"`
	QQImageURL      string `json:"qq_image_url"`
	EntryReply      string `json:"entry_reply"`
}

type QuickReply struct {
	RuleID   string `json:"rule_id"`
	Text     string `json:"text"`
	SendText string `json:"send_text"`
}

type VisitorWidgetSettings struct {
	Phone           string       `json:"phone"`
	Wechat          string       `json:"wechat"`
	WechatReplyType string       `json:"wechat_reply_type"`
	WechatImageURL  string       `json:"wechat_image_url"`
	QQ              string       `json:"qq"`
	QQReplyType     string       `json:"qq_reply_type"`
	QQImageURL      string       `json:"qq_image_url"`
	EntryReply      string       `json:"entry_reply"`
	QuickReplies    []QuickReply `json:"quick_replies"`
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
	Enabled               bool           `json:"enabled"`
	Mode                  AICustomerMode `json:"mode"`
	BaseURL               string         `json:"base_url"`
	APIKey                string         `json:"-"`
	APIKeyMasked          string         `json:"api_key_masked"`
	Model                 string         `json:"model"`
	APIType               string         `json:"api_type"`
	Temperature           float64        `json:"temperature"`
	MaxOutputTokens       int            `json:"max_output_tokens"`
	TimeoutSeconds        int            `json:"timeout_seconds"`
	SystemPrompt          string         `json:"system_prompt"`
	NoReplyTimeoutSeconds int            `json:"agent_no_reply_timeout_seconds"`
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
