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

	CreateVisitorConversation(ip, source, lang string) (domain.VisitorSession, error)
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
	BusinessHours() domain.BusinessHours
	UpdateBusinessHours(next domain.BusinessHours) (domain.BusinessHours, error)
	ChangePassword(session domain.AuthSession, currentPassword, newPassword string) error
	RegisterAgentDeviceToken(agentID, deviceToken string) (domain.Agent, error)

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
}

type MessagePage struct {
	Messages   []domain.Message `json:"messages"`
	HasMore    bool             `json:"has_more"`
	NextBefore string           `json:"next_before,omitempty"`
}
