package store

import (
	"context"

	"customer-service/backend/internal/domain"
)

type Store interface {
	Ping(ctx context.Context) error
	LoginAdmin(account, password string) (domain.AuthSession, domain.AdminUser, error)
	LoginAgent(account, password string) (domain.AuthSession, domain.Agent, error)
	Auth(token string) (domain.AuthSession, bool)
	AuthVisitor(token string) (domain.AuthSession, bool)

	CreateVisitorConversation(ip, source, lang, ipSource, remark string) (domain.VisitorSession, error)
	UpdateVisitorLang(conversationID, lang string) (domain.Conversation, error)
	AICacheProbe() domain.AICacheProbe
	SaveAICacheProbe(probe domain.AICacheProbe)
	SetAgentStatus(agentID string, status domain.AgentStatus) (domain.Agent, []domain.Conversation, error)
	ConversationsForAgent(agentID string) ([]domain.Conversation, error)
	AllConversations() []domain.Conversation
	AllAgents() []domain.Agent
	Conversation(id string) (domain.Conversation, bool)
	Messages(conversationID string) []domain.Message
	PagedMessages(conversationID string, limit int, beforeServerMsgID string) MessagePage
	MarkConversationRead(actor domain.AuthSession, conversationID string) (domain.Conversation, error)

	UpdateRemark(actor domain.AuthSession, conversationID, remark string) (domain.Conversation, error)
	AddVisitorMessage(conversationID, clientMsgID, content string, messageType domain.MessageType, translations map[string]string) (MessageResult, error)
	AddAIMessage(conversationID, content string, translations map[string]string) (domain.Message, domain.Conversation, error)
	AddAgentMessage(agentID, conversationID, clientMsgID, content string, messageType domain.MessageType, translations map[string]string) (MessageResult, error)
	UpdateMessageTranslations(conversationID, serverMsgID string, translations map[string]string) (domain.Message, error)
	// SetMessageLanguageMeta 记录消息的语种与翻译状态（检测在应用层完成）。
	SetMessageLanguageMeta(conversationID, serverMsgID, detectedLang, authorLang, translationStatus string) (domain.Message, error)
	// UpdateConversationLanguage 更新会话的语言状态（访客语言、检测语种、显式指令覆盖、语种统计）。
	UpdateConversationLanguage(conversationID string, patch ConversationLanguagePatch) (domain.Conversation, error)
	RevokeMessage(actor domain.AuthSession, conversationID, serverMsgID string) (domain.Message, domain.Conversation, error)
	CloseConversation(actor domain.AuthSession, conversationID string) (domain.Conversation, domain.Message, error)
	DeleteConversation(actor domain.AuthSession, conversationID string) error
	TransferConversation(actor domain.AuthSession, conversationID, agentID, group string) (domain.Conversation, domain.Message, error)
	SubmitRating(conversationID string, score int, tags []string, comment string) (domain.ServiceRating, error)
	RatingSummary() domain.RatingSummary
	RatingSummaryForAgent(agentID string) domain.RatingSummary
	RatingSummariesByAgent() map[string]domain.RatingSummary
	RatingsForAgent(agentID string) []domain.ServiceRating
	RecentRatings(limit int) []domain.ServiceRating
	DashboardStats() domain.DashboardStats
	RecordAuditEvent(event domain.AuditEvent) domain.AuditEvent
	RecentAuditEvents(limit int) []domain.AuditEvent

	ContactSettings() domain.ContactSettings
	UpdateContactSettings(next domain.ContactSettings) (domain.ContactSettings, error)
	KeywordRules() []domain.KeywordRule
	CreateKeywordRule(next domain.KeywordRule) (domain.KeywordRule, error)
	UpdateKeywordRule(id string, next domain.KeywordRule) (domain.KeywordRule, error)
	DeleteKeywordRule(id string) (domain.KeywordRule, error)
	AISettings() domain.AISettings
	UpdateAISettings(next domain.AISettings) (domain.AISettings, error)
	// ListAIProviders 返回全部 AI 供应商（APIKey 只带末四位掩码，不含明文）。
	ListAIProviders() []domain.AIProvider
	// AIProvider 读取单个供应商；返回的 APIKey 为解密后的明文，仅供服务端内部使用。
	AIProvider(id string) (domain.AIProvider, bool)
	// CreateAIProvider 创建供应商，apiKey 为明文，落库前加密。
	CreateAIProvider(next domain.AIProvider, apiKey string) (domain.AIProvider, error)
	// UpdateAIProvider 覆盖更新供应商；apiKey==nil 表示保留原密钥，传空串表示清除。
	UpdateAIProvider(id string, patch domain.AIProvider, apiKey *string) (domain.AIProvider, error)
	DeleteAIProvider(id string) error
	// AIBindings 返回能力标识到绑定的映射。
	AIBindings() map[string]domain.AIBinding
	UpsertAIBinding(next domain.AIBinding) (domain.AIBinding, error)
	BusinessHours() domain.BusinessHours
	UpdateBusinessHours(next domain.BusinessHours) (domain.BusinessHours, error)
	PushSettings() domain.PushSettings
	UpdatePushSettings(next domain.PushSettings) (domain.PushSettings, error)
	ChangePassword(session domain.AuthSession, currentPassword, newPassword string) error
	RegisterAgentDeviceToken(agentID, deviceToken, platform string) (domain.Agent, error)
	// AgentPreferences 读取客服账号级偏好（已填充默认值）。
	AgentPreferences(agentID string) (domain.AgentPreferences, error)
	// UpdateAgentPreferences 覆盖保存客服偏好；非法方言模式归一为 standard。
	UpdateAgentPreferences(agentID string, next domain.AgentPreferences) (domain.AgentPreferences, error)

	AppReleases(platform string) []domain.AppRelease
	CreateAppRelease(next domain.AppRelease) (domain.AppRelease, error)
	UpdateAppRelease(id string, next domain.AppRelease) (domain.AppRelease, error)
	DeleteAppRelease(id string) (domain.AppRelease, error)
	AppReleaseByID(id string) (domain.AppRelease, bool)

	CreateAgent(next domain.Agent, password string) (domain.Agent, error)
	UpdateAgent(id string, next domain.Agent) (domain.Agent, error)
	UpdateAgentAvatar(agentID, avatarURL string) (domain.Agent, error)
	SetAgentManualRating(agentID string, rate float64) (domain.Agent, error)
	ClearAgentManualRating(agentID string) (domain.Agent, error)
	ResetAgentRatings(agentID string) (domain.Agent, error)
	ResetAgentPassword(id, password string) (domain.Agent, error)
	DisableAgent(id string) (domain.Agent, error)
	DeleteAgent(id string) (domain.Agent, error)

	APIApps() []domain.APIApp
	CreateAPIApp(next domain.APIApp) (domain.APIApp, error)
	UpdateAPIApp(id string, next domain.APIApp) (domain.APIApp, error)
	DeleteAPIApp(id string) error
	APIAppByKeyHash(keyHash string) (domain.APIApp, bool)
	TouchAPIApp(id string)
}

type MessagePage struct {
	Messages   []domain.Message `json:"messages"`
	HasMore    bool             `json:"has_more"`
	NextBefore string           `json:"next_before,omitempty"`
}

// ConversationLanguagePatch 描述会话语言状态的可选更新字段，nil 表示不修改。
type ConversationLanguagePatch struct {
	VisitorLang       *string
	DetectedLang      *string
	ReplyLangOverride *string
	LanguageStats     map[string]int
}
