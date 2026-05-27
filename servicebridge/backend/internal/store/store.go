package store

import (
	"context"
	"time"

	"customer-service/backend/internal/domain"
)

type Store interface {
	Ping(ctx context.Context) error
	LoginAdmin(account, password string) (domain.AuthSession, domain.AdminUser, error)
	LoginAgent(account, password string) (domain.AuthSession, domain.Agent, error)
	Auth(token string) (domain.AuthSession, bool)
	AuthVisitor(token string) (domain.AuthSession, bool)

	CreateVisitorConversation(ip, source string) (domain.VisitorSession, error)
	SetAgentStatus(agentID string, status domain.AgentStatus) (domain.Agent, []domain.Conversation, error)
	ConversationsForAgent(agentID string) ([]domain.Conversation, error)
	AllConversations() []domain.Conversation
	AllAgents() []domain.Agent
	Conversation(id string) (domain.Conversation, bool)
	Messages(conversationID string) []domain.Message
	PagedMessages(conversationID string, limit int, beforeServerMsgID string) MessagePage
	MarkConversationRead(actor domain.AuthSession, conversationID string) (domain.Conversation, error)

	UpdateRemark(actor domain.AuthSession, conversationID, remark string) (domain.Conversation, error)
	AddVisitorMessage(conversationID, clientMsgID, content string, messageType domain.MessageType) (MessageResult, error)
	AddAIMessage(conversationID, content string) (domain.Message, domain.Conversation, error)
	AddAgentMessage(agentID, conversationID, clientMsgID, content string, messageType domain.MessageType) (MessageResult, error)
	RevokeMessage(actor domain.AuthSession, conversationID, serverMsgID string) (domain.Message, domain.Conversation, error)
	EscalateNoReplyToAI(conversationID string, visitorMessageSentAt time.Time) (domain.Conversation, bool)
	CloseConversation(actor domain.AuthSession, conversationID string) (domain.Conversation, domain.Message, error)
	DeleteConversation(actor domain.AuthSession, conversationID string) error
	TransferConversation(actor domain.AuthSession, conversationID, agentID, group string) (domain.Conversation, domain.Message, error)
	SubmitRating(conversationID string, score int, tags []string, comment string) (domain.ServiceRating, error)
	RatingSummary() domain.RatingSummary
	RecentRatings(limit int) []domain.ServiceRating
	DashboardStats() domain.DashboardStats
	RecordAuditEvent(event domain.AuditEvent) domain.AuditEvent
	RecentAuditEvents(limit int) []domain.AuditEvent

	ContactSettings() domain.ContactSettings
	UpdateContactSettings(next domain.ContactSettings) (domain.ContactSettings, error)
	KeywordRules() []domain.KeywordRule
	CreateKeywordRule(next domain.KeywordRule) (domain.KeywordRule, error)
	UpdateKeywordRule(id string, next domain.KeywordRule) (domain.KeywordRule, error)
	AISettings() domain.AISettings
	UpdateAISettings(next domain.AISettings) (domain.AISettings, error)
	FeishuSettings() domain.FeishuSettings
	UpdateFeishuSettings(next domain.FeishuSettings) (domain.FeishuSettings, error)
	BusinessHours() domain.BusinessHours
	UpdateBusinessHours(next domain.BusinessHours) (domain.BusinessHours, error)
	ChangePassword(session domain.AuthSession, currentPassword, newPassword string) error

	CreateAgent(next domain.Agent, password string) (domain.Agent, error)
	UpdateAgent(id string, next domain.Agent) (domain.Agent, error)
	ResetAgentPassword(id, password string) (domain.Agent, error)
	DisableAgent(id string) (domain.Agent, error)
	DeleteAgent(id string) (domain.Agent, error)
	EnsureFeishuAgent(id, name string) (domain.Agent, error)
	BindFeishuMessage(binding domain.FeishuMessageBinding) (domain.FeishuMessageBinding, error)
	FeishuConversationByMessage(messageID string) (string, bool)
	RegisterAgentPushDevice(agentID string, device domain.PushDevice) (domain.PushDevice, error)
	PushDevicesForAgent(agentID string) []domain.PushDevice
}

type MessagePage struct {
	Messages   []domain.Message `json:"messages"`
	HasMore    bool             `json:"has_more"`
	NextBefore string           `json:"next_before,omitempty"`
}
