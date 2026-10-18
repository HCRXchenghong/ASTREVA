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

	// ===== AI 知识库 =====
	// KnowledgeBases 返回全部知识库（按 sort_order、创建时间排序）。
	KnowledgeBases() []domain.KnowledgeBase
	KnowledgeBase(id string) (domain.KnowledgeBase, bool)
	CreateKnowledgeBase(next domain.KnowledgeBase) (domain.KnowledgeBase, error)
	// UpdateKnowledgeBase 覆盖更新知识库（ID/创建时间不变）。
	UpdateKnowledgeBase(id string, next domain.KnowledgeBase) (domain.KnowledgeBase, error)
	// DeleteKnowledgeBase 删除知识库并级联删除其条目。
	DeleteKnowledgeBase(id string) (domain.KnowledgeBase, error)
	// KnowledgeEntries 按库与关键词分页列出条目（后台管理用）。
	KnowledgeEntries(baseID, query string, limit, offset int) []domain.KnowledgeEntry
	KnowledgeEntry(id string) (domain.KnowledgeEntry, bool)
	CreateKnowledgeEntry(next domain.KnowledgeEntry) (domain.KnowledgeEntry, error)
	UpdateKnowledgeEntry(id string, next domain.KnowledgeEntry) (domain.KnowledgeEntry, error)
	DeleteKnowledgeEntry(id string) (domain.KnowledgeEntry, error)
	// KnowledgeEntriesForBases 返回启用中的条目：仅限启用中的知识库；
	// baseIDs 为空表示全部启用库。Postgres 用 WHERE enabled AND base_id = ANY($1) 取候选，
	// 避免全表扫描；后续可再加倒排表（本期不做）。
	KnowledgeEntriesForBases(baseIDs []string) []domain.KnowledgeEntry
	KnowledgeSettings() domain.KnowledgeSettings
	UpdateKnowledgeSettings(next domain.KnowledgeSettings) (domain.KnowledgeSettings, error)

	// ===== AI 限额 =====
	// AILimits 读取 AI 频率/次数/单次字数限额（单例表 ai_limits，未写入时返回默认值）。
	AILimits() domain.AILimits
	// UpdateAILimits 覆盖保存限额配置；负数归零、超上限收敛，updated_at 由存储层写入。
	UpdateAILimits(next domain.AILimits) (domain.AILimits, error)

	// ===== 客服策略（批次 4：本期只做存储与接口，不接线计时器）=====
	// ServicePolicy 读取客服策略单例（未写入时返回 DefaultServicePolicy()）。
	ServicePolicy() domain.ServicePolicy
	// UpdateServicePolicy 覆盖保存客服策略；非法枚举回落、负数归零、超上限收敛。
	UpdateServicePolicy(next domain.ServicePolicy) (domain.ServicePolicy, error)

	// QueueList 返回排队列表（priority DESC, enqueued_at ASC）。
	QueueList() []domain.QueuedConversation
	// EnqueueConversation 把会话写入排队队列（已存在时更新分组/优先级/原因，保留原入队时间）。
	EnqueueConversation(entry domain.QueuedConversation) (domain.QueuedConversation, error)
	// DequeueConversation 把会话移出排队队列；不存在时返回 ErrNotFound。
	DequeueConversation(conversationID string) error

	// BlacklistList 返回黑名单（kind 为空表示全部；已过期项仍会返回，便于后台清理）。
	BlacklistList(kind string) []domain.BlacklistEntry
	// AddBlacklist 新增黑名单项；(kind,value) 重复时返回 ErrConflict。
	AddBlacklist(entry domain.BlacklistEntry) (domain.BlacklistEntry, error)
	// DeleteBlacklist 删除黑名单项；不存在时返回 ErrNotFound。
	DeleteBlacklist(id string) error
	// MatchBlacklist 判断 (kind,value) 是否命中有效黑名单；过期项视为未命中并顺手清理。
	MatchBlacklist(kind, value string) (domain.BlacklistEntry, bool)

	// TakeoverConversation 客服正式接管会话：分配给该客服、状态置 assigned、清空 SLA 状态，
	// 并把 first_response_at 重置为空（接管即开启人工首响计时），同时移出排队队列。
	// 会话已被他人负责或已结束时返回 ErrForbidden；重复接管同一会话是幂等的。
	TakeoverConversation(agentID, conversationID string) (domain.Conversation, error)
	// AssignConversationToAgent 把一个未分配的会话静默分配给指定客服（排队出队分配使用）：
	// 递增客服并发并把会话移出排队队列，不改变会话状态。
	// 会话已被他人负责、已结束、客服非在线或已达并发上限时返回 ErrConflict。
	AssignConversationToAgent(conversationID, agentID string) (domain.Conversation, error)
	// SetConversationSLAState 只更新会话的 sla_state 标记（含 updated_at），
	// 供 SLA 巡检记录 first_response / idle，保证同一超时只告警一次。
	SetConversationSLAState(conversationID, state string) (domain.Conversation, error)
	// ReturnConversationToAI 把会话交回 AI 接待（状态置 ai_serving，保留负责客服便于追溯）。
	// 用于策略 sla_idle_action=ai；不改变 assigned_agent_id 与 sla_state（后者是巡检幂等标记）。
	ReturnConversationToAI(conversationID string) (domain.Conversation, error)
	// ReassignConversation 用于策略 sla_idle_action=reassign 的存储能力：
	//   - targetAgentID 非空：把会话从当前客服转给目标客服（递减原客服并发、递增目标并发），
	//     不改变会话状态（与既有转接语义一致）；目标客服不可用时返回 ErrConflict；
	//   - targetAgentID 为空：释放当前客服并把会话置为未分配（assigned → human_requested），
	//     交由应用层重新入队。
	ReassignConversation(conversationID, targetAgentID string) (domain.Conversation, error)
	// AddUnattendedNotice 落库一条无人值守兜底系统消息并记录 unattended_notified_at。
	// 同一会话已发送过时返回 ErrConflict，保证"每个会话只发一次"在重启后依然成立。
	AddUnattendedNotice(conversationID, content string, translations map[string]string) (domain.Message, domain.Conversation, error)

	// AgentAlertChannels 返回某客服的全部提醒渠道（Config 为明文，仅服务端内部使用；
	// ConfigMasked 为掩码，供接口返回）。
	AgentAlertChannels(agentID string) []domain.AgentAlertChannel
	// AgentAlertChannel 按 ID 读取单个提醒渠道（Config 为明文），供渠道测试发送复用已保存配置。
	AgentAlertChannel(id string) (domain.AgentAlertChannel, bool)
	// CreateAgentAlertChannel 新增提醒渠道；每个客服最多 3 个，超出返回 ErrInvalidInput。
	CreateAgentAlertChannel(agentID, kind string, config map[string]string) (domain.AgentAlertChannel, error)
	// UpdateAgentAlertChannel 局部更新渠道；config==nil 表示不改配置。
	// config 为合并语义：未出现的键保留原值，显式空串删除该键，其余覆盖。
	UpdateAgentAlertChannel(id string, patch domain.AgentAlertChannelPatch, config *map[string]string) (domain.AgentAlertChannel, error)
	// DeleteAgentAlertChannel 删除渠道；不存在时返回 ErrNotFound。
	DeleteAgentAlertChannel(id string) error

	// AlertSMTPSettings 读取全局 SMTP 设置；Password 为明文（json:"-"），PasswordMasked 供接口返回。
	AlertSMTPSettings() domain.AlertSMTPSettings
	// UpdateAlertSMTPSettings 覆盖保存 SMTP 设置；password==nil 不改，"" 清除，其它覆盖。
	UpdateAlertSMTPSettings(next domain.AlertSMTPSettings, password *string) (domain.AlertSMTPSettings, error)

	// RequestHumanHandoff 静默转人工：会话状态置为 human_requested 并分配客服，
	// 不产生任何消息，供 AI 知识库未命中（unknown_action=handoff）时复用既有转人工逻辑。
	RequestHumanHandoff(conversationID string) (domain.Conversation, error)
	// AddAIMessageWithKnowledge 与 AddAIMessage 相同，并记录本次回答依据的知识条目 ID。
	AddAIMessageWithKnowledge(conversationID, content string, translations map[string]string, knowledgeEntryIDs []string) (domain.Message, domain.Conversation, error)

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
