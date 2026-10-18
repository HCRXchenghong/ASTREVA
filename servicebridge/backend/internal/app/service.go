package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"customer-service/backend/internal/ai"
	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/knowledge"
	"customer-service/backend/internal/notify"
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

type Service struct {
	store    store.Store
	hub      *realtime.Hub
	ai       *ai.Client
	logger   *slog.Logger
	notifier notify.Notifier

	websocketAllowedOrigins map[string]bool

	// limitTracker 是进程内的滑动窗口计数器（访客消息频率 + AI 调用次数）。
	// 限额计数不落库，重启后窗口重建；审计事件独立落库。
	limitTracker *limitTracker
	// limitTexts 缓存超限提示的译文，避免刷屏式请求反复调用翻译上游。
	limitTexts *limitTextTranslator

	// watching 是"只读围观"的进程内登记表（见 service_policy_flow.go），重启后失效。
	watching *watchRegistry
	// alertLinkHint 是提醒渠道正文里的会话链接前缀（PUBLIC_ADMIN_URL），为空则不下发链接。
	alertLinkHint string
	// policyLoop 是 SLA 巡检 ticker 的运行状态（见 service_policy_sla.go），可优雅停止。
	policyLoop *policyLoopRunner

	// profile 是访客画像的进程内运行时（地理解析器与告警去重，见 visitor_profile.go）。
	profile *visitorProfileRuntime
}

func NewService(store store.Store, hub *realtime.Hub, logger *slog.Logger) *Service {
	return &Service{
		store:                   store,
		hub:                     hub,
		ai:                      ai.NewClient(),
		logger:                  logger,
		notifier:                notify.DisabledNotifier{},
		websocketAllowedOrigins: map[string]bool{"*": true},
		limitTracker:            newLimitTracker(),
		limitTexts:              newLimitTextTranslator(),
		watching:                newWatchRegistry(),
		policyLoop:              &policyLoopRunner{},
		profile:                 newVisitorProfileRuntime(),
	}
}

func (s *Service) SetNotifier(notifier notify.Notifier) {
	if notifier != nil {
		s.notifier = notifier
	}
}

func (s *Service) SetWebSocketAllowedOrigins(origins map[string]bool) {
	s.websocketAllowedOrigins = map[string]bool{}
	for origin, allowed := range origins {
		s.websocketAllowedOrigins[origin] = allowed
	}
}

func (s *Service) Store() store.Store {
	return s.store
}

// Notifier 返回当前通知通道，供 HTTP 层发送提醒渠道测试消息。
func (s *Service) Notifier() notify.Notifier {
	if s.notifier == nil {
		return notify.DisabledNotifier{}
	}
	return s.notifier
}

func (s *Service) Hub() *realtime.Hub {
	return s.hub
}

func (s *Service) AuthFromRequest(r *http.Request) (domain.AuthSession, error) {
	token := bearerToken(r)
	if token == "" {
		return domain.AuthSession{}, store.ErrUnauthorized
	}
	session, ok := s.store.Auth(token)
	if !ok {
		return domain.AuthSession{}, store.ErrUnauthorized
	}
	return session, nil
}

func (s *Service) AuthVisitorFromRequest(r *http.Request) (domain.AuthSession, error) {
	token := bearerToken(r)
	if token == "" {
		return domain.AuthSession{}, store.ErrUnauthorized
	}
	session, ok := s.store.AuthVisitor(token)
	if !ok {
		return domain.AuthSession{}, store.ErrUnauthorized
	}
	return session, nil
}

func (s *Service) LoginAdmin(account, password string) (domain.AuthSession, domain.AdminUser, error) {
	return s.store.LoginAdmin(account, password)
}

func (s *Service) LoginAgent(account, password string) (domain.AuthSession, domain.Agent, error) {
	return s.store.LoginAgent(account, password)
}

func (s *Service) TestAIReply(ctx context.Context, userText string) (string, error) {
	userText = strings.TrimSpace(userText)
	if userText == "" {
		userText = "你好，请简单介绍一下你能提供什么帮助。"
	}
	settings := s.settingsWithCacheMode()
	return s.ai.Reply(ctx, settings, s.store.ContactSettings(), nil, userText, ai.LangZh, "")
}

func (s *Service) AIModels(ctx context.Context, preview domain.AISettings) ([]string, error) {
	settings := s.store.AISettings()
	if strings.TrimSpace(preview.BaseURL) != "" {
		settings.BaseURL = strings.TrimSpace(preview.BaseURL)
	}
	if strings.TrimSpace(preview.APIKey) != "" {
		settings.APIKey = strings.TrimSpace(preview.APIKey)
	}
	if strings.TrimSpace(preview.APIType) != "" {
		settings.APIType = strings.TrimSpace(preview.APIType)
	}
	if preview.TimeoutSeconds > 0 {
		settings.TimeoutSeconds = preview.TimeoutSeconds
	}
	return s.ai.Models(ctx, settings)
}

// settingsWithCacheMode 读取 AI 配置，并按探测结果解析当前模型的缓存模式。
// 探测结果为 auto（自动缓存）或 explicit（显式标记）时启用对应的最高效请求方式，
// 其余情况（none/未探测）保持原有请求格式，行为与改造前完全一致。
func (s *Service) settingsWithCacheMode() domain.AISettings {
	settings := s.store.AISettings()
	probe := s.store.AICacheProbe()
	mode := strings.TrimSpace(probe.ModelResults[settings.Model])
	if mode == ai.CacheModeAuto || mode == ai.CacheModeExplicit {
		settings.CacheMode = mode
	}
	return settings
}

// ProbeModelCaches 逐个实测模型的提示词缓存支持并持久化结果（最多 20 个模型）。
// 当前配置的模型永远第一个探测；同一时刻只允许一个探测任务，避免重复消耗。
func (s *Service) ProbeModelCaches(ctx context.Context, models []string) {
	current := s.store.AICacheProbe()
	if current.Status == "running" {
		return
	}
	settings := s.store.AISettings()
	seen := map[string]bool{}
	ordered := make([]string, 0, len(models)+1)
	if strings.TrimSpace(settings.Model) != "" {
		configured := strings.TrimSpace(settings.Model)
		ordered = append(ordered, configured)
		seen[configured] = true
	}
	const maxProbeModels = 20
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		ordered = append(ordered, model)
		if len(ordered) >= maxProbeModels {
			break
		}
	}
	probe := domain.AICacheProbe{
		ModelResults: map[string]string{},
		Status:       "running",
	}
	for model, mode := range current.ModelResults {
		probe.ModelResults[model] = mode
	}
	s.store.SaveAICacheProbe(probe)
	for _, model := range ordered {
		mode, err := s.ai.ProbeCacheSupport(ctx, settings, model)
		if err != nil {
			s.logger.Warn("cache probe failed", "model", model, "error", err)
			mode = ai.CacheModeNone
		}
		probe.ModelResults[model] = mode
		s.store.SaveAICacheProbe(probe)
	}
	now := time.Now().UTC()
	probe.Status = "done"
	probe.ProbedAt = &now
	s.store.SaveAICacheProbe(probe)
}

func (s *Service) SubmitRating(conversationID string, score int, tags []string, comment string) (domain.ServiceRating, error) {
	if score < 5 && strings.TrimSpace(comment) == "" {
		return domain.ServiceRating{}, store.ErrInvalidInput
	}
	rating, err := s.store.SubmitRating(conversationID, score, tags, comment)
	if err != nil {
		return domain.ServiceRating{}, err
	}
	s.hub.BroadcastAdmins(realtime.Event{Event: "rating.created", Data: rating})
	return rating, nil
}

func (s *Service) CreateVisitorConversation(ip, source, lang, ipSource, remark string) (domain.VisitorSession, error) {
	// 黑名单：建会话前按 ip 与"账号"（开放 API 的外部用户 ID 落在 remark 上）拦截。
	if err := s.checkBlacklist("", ip, remark); err != nil {
		return domain.VisitorSession{}, err
	}
	session, err := s.store.CreateVisitorConversation(ip, source, lang, ipSource, remark)
	if err != nil {
		return domain.VisitorSession{}, err
	}
	visitorLang := ai.NormalizeLang(session.Conversation.VisitorLang, ai.LangZh)
	for i := range session.InitialMessages {
		s.translateInitialMessageForVisitor(session.Conversation.ID, visitorLang, &session.InitialMessages[i])
	}
	s.hub.BroadcastAdmins(realtime.Event{Event: "conversation.created", Data: session.Conversation})
	if session.Conversation.AssignedAgentID != "" {
		s.hub.SendToAgent(session.Conversation.AssignedAgentID, realtime.Event{Event: "conversation.assigned", Data: session.Conversation})
		// 新会话被静默分配给在线客服时也要推送，否则 App 在后台收不到任何提醒。
		s.notifyAgent(session.Conversation)
		s.dispatchAgentAlert(session.Conversation.AssignedAgentID, "新会话分配",
			"访客「"+visitorDisplayName(session.Conversation)+"」发起了新会话。", session.Conversation.ID)
	}
	// 新会话立即向在线客服弹「接管」提示（未点接管前 AI 继续接待）。
	s.notifyTakeoverRequest(session.Conversation, session.Conversation.CreatedAt)
	if session.Conversation.AssignedAgentID == "" {
		// 分配失败（无在线客服/并发已满）：按策略入队，并在需要时补发无人值守兜底。
		s.handleUnassignedConversation(session.Conversation, false)
	}
	return session, nil
}

// UpdateVisitorLang 访客切换网站语言后同步会话语言；语言未变时直接返回，
// 变更后广播会话状态，让客服/后台侧同步看到新的访客语言。
func (s *Service) UpdateVisitorLang(conversationID, lang string) (domain.Conversation, error) {
	normalized := ai.NormalizeLang(lang, ai.LangZh)
	current, ok := s.store.Conversation(conversationID)
	if !ok {
		return domain.Conversation{}, store.ErrNotFound
	}
	if current.VisitorLang == normalized {
		return current, nil
	}
	updated, err := s.store.UpdateVisitorLang(conversationID, normalized)
	if err != nil {
		return domain.Conversation{}, err
	}
	if updated.AssignedAgentID != "" {
		s.hub.SendToAgent(updated.AssignedAgentID, realtime.Event{Event: "conversation.status_changed", Data: updated})
	}
	s.hub.BroadcastAdmins(realtime.Event{Event: "conversation.status_changed", Data: updated})
	return updated, nil
}

func (s *Service) SetAgentStatus(agentID string, status domain.AgentStatus) (domain.Agent, error) {
	agent, assigned, err := s.store.SetAgentStatus(agentID, status)
	if err != nil {
		return domain.Agent{}, err
	}
	s.hub.BroadcastAdmins(realtime.Event{Event: "agent.status_changed", Data: agent})
	for _, conversation := range assigned {
		if conversation.AssignedAgentID != "" {
			s.hub.SendToAgent(conversation.AssignedAgentID, realtime.Event{Event: "conversation.assigned", Data: conversation})
			s.dispatchAgentAlert(conversation.AssignedAgentID, "新会话分配",
				"访客「"+visitorDisplayName(conversation)+"」已分配给您。", conversation.ID)
		} else if status == domain.AgentOffline && conversation.FirstResponseAt == nil {
			// 离线释放：会话尚未被人工真正回复过，重新入队而不是丢弃。
			s.handleUnassignedConversation(conversation, true)
		}
		s.hub.SendToVisitor(conversation.ID, realtime.Event{Event: "conversation.status_changed", Data: conversation})
		s.hub.BroadcastAdmins(realtime.Event{Event: "conversation.status_changed", Data: conversation})
	}
	if status == domain.AgentOnline {
		// 客服上线后按 优先级/入队时间 出队分配排队中的会话。
		s.drainQueue()
	}
	return agent, nil
}

func (s *Service) CreateAgent(agent domain.Agent, password string) (domain.Agent, error) {
	created, err := s.store.CreateAgent(agent, password)
	if err != nil {
		return domain.Agent{}, err
	}
	s.hub.BroadcastAdmins(realtime.Event{Event: "agent.created", Data: created})
	return created, nil
}

func (s *Service) UpdateAgent(id string, agent domain.Agent) (domain.Agent, error) {
	updated, err := s.store.UpdateAgent(id, agent)
	if err != nil {
		return domain.Agent{}, err
	}
	s.hub.BroadcastAdmins(realtime.Event{Event: "agent.updated", Data: updated})
	return updated, nil
}

func (s *Service) ResetAgentPassword(id, password string) (domain.Agent, error) {
	updated, err := s.store.ResetAgentPassword(id, password)
	if err != nil {
		return domain.Agent{}, err
	}
	s.hub.DisconnectAgent(updated.ID, "password_reset")
	s.hub.BroadcastAdmins(realtime.Event{Event: "agent.password_reset", Data: updated})
	return updated, nil
}

func (s *Service) DisableAgent(id string) (domain.Agent, error) {
	updated, err := s.store.DisableAgent(id)
	if err != nil {
		return domain.Agent{}, err
	}
	s.hub.DisconnectAgent(updated.ID, "account_disabled")
	s.hub.BroadcastAdmins(realtime.Event{Event: "agent.disabled", Data: updated})
	return updated, nil
}

func (s *Service) DeleteAgent(id string) (domain.Agent, error) {
	deleted, err := s.store.DeleteAgent(id)
	if err != nil {
		return domain.Agent{}, err
	}
	s.hub.DisconnectAgent(deleted.ID, "account_deleted")
	s.hub.BroadcastAdmins(realtime.Event{Event: "agent.deleted", Data: deleted})
	return deleted, nil
}

func (s *Service) ChangePassword(session domain.AuthSession, currentPassword, newPassword string) error {
	if err := s.store.ChangePassword(session, currentPassword, newPassword); err != nil {
		return err
	}
	switch session.Kind {
	case domain.AccountAdmin:
		s.hub.DisconnectAdmin(session.AccountID, "password_changed")
	case domain.AccountAgent:
		s.hub.DisconnectAgent(session.AccountID, "password_changed")
	}
	return nil
}

func (s *Service) AddVisitorMessage(conversationID, clientMsgID, content string, messageType domain.MessageType) (store.MessageResult, error) {
	conversation, ok := s.store.Conversation(conversationID)
	if !ok {
		return store.MessageResult{}, store.ErrNotFound
	}
	// 黑名单：发消息前按 visitor | ip | account 三类拦截（命中写审计，不下发被拉黑的值）。
	if err := s.checkBlacklist(conversation.VisitorID, conversation.VisitorIP, conversationBlacklistAccount(conversation)); err != nil {
		return store.MessageResult{}, err
	}
	// 频率限额：必须在任何落库、翻译与 AI/关键词触发之前拦截。
	// 超限只返回带访客文案的业务错误，由 HTTP/WS 层转成访客可见提示。
	if err := s.checkVisitorMessageLimit(conversation); err != nil {
		return store.MessageResult{}, err
	}
	visitorLang := ai.NormalizeLang(conversation.VisitorLang, ai.LangZh)
	language := s.resolveConversationLanguage(conversation, content, messageType)
	visitorLang = language.ReplyLang
	if messageType == domain.MessageHandoff && strings.TrimSpace(content) == "" {
		content = humanRequestText(visitorLang)
	}
	// 翻译源语言用"这条消息实际使用的语言"，而不是会话初始语言。
	// 固定文案走静态映射，其余走异步翻译：访客先收到原文，客服端先看到原文+「翻译中」，
	// 译文就绪后原地更新（避免把访客的发送请求阻塞在翻译上）。
	// 当前负责客服关闭了"访客消息自动译成中文"时，完全跳过翻译（原文仍正常投递）。
	preferences := s.agentPreferences(conversation.AssignedAgentID)
	autoTranslateIncoming := prefBool(preferences.AutoTranslateIncoming)
	translations, needsAI := staticTranslation(language.DetectedLang, ai.LangZh, messageType, content)
	status := messageTranslationState(language.DetectedLang, ai.LangZh, messageType, content)
	if !autoTranslateIncoming {
		translations = nil
		needsAI = false
		status = "skipped"
	} else if needsAI {
		translations = nil
	}
	result, err := s.store.AddVisitorMessage(conversationID, clientMsgID, content, messageType, translations)
	if err != nil {
		return store.MessageResult{}, err
	}
	// 记录语种与翻译状态，供客服端展示语种标签与「查看原文」。
	if updated, metaErr := s.store.SetMessageLanguageMeta(conversationID, result.Input.ServerMsgID,
		language.DetectedLang, language.DetectedLang, status); metaErr == nil {
		result.Input = updated
	}
	if language.Changed {
		if updated, langErr := s.store.UpdateConversationLanguage(conversationID, store.ConversationLanguagePatch{
			VisitorLang:       &language.ReplyLang,
			DetectedLang:      &language.DetectedLang,
			ReplyLangOverride: &language.Override,
			LanguageStats:     language.Stats,
		}); langErr == nil {
			result.Conversation = updated
		}
	}

	s.hub.SendToVisitor(conversationID, realtime.Event{Event: "message.ack", Data: VisitorFacingMessageForLang(result.Input, visitorLang)})
	if result.Conversation.AssignedAgentID != "" {
		// 客服/后台侧默认看到中文译文，并保留查看原文能力。
		s.hub.SendToAgent(result.Conversation.AssignedAgentID, realtime.Event{Event: "message.receive", Data: AgentFacingMessage(result.Input)})
		s.hub.SendToAgent(result.Conversation.AssignedAgentID, realtime.Event{Event: "conversation.status_changed", Data: result.Conversation})
	}
	for _, msg := range result.Generated {
		s.translateGeneratedMessageForVisitor(conversationID, visitorLang, &msg)
		s.hub.SendToVisitor(conversationID, realtime.Event{Event: "message.receive", Data: VisitorFacingMessageForLang(msg, visitorLang)})
		if result.Conversation.AssignedAgentID != "" {
			s.hub.SendToAgent(result.Conversation.AssignedAgentID, realtime.Event{Event: "message.receive", Data: AgentFacingMessage(msg)})
		}
	}
	s.hub.SendToVisitor(conversationID, realtime.Event{Event: "conversation.status_changed", Data: result.Conversation})
	s.hub.BroadcastAdmins(realtime.Event{Event: "conversation.status_changed", Data: result.Conversation})
	if result.Conversation.Status == domain.ConversationHumanRequested || result.Conversation.AssignedAgentID != "" {
		s.notifyAgent(result.Conversation)
	}
	// 新访客消息：异步推送到负责客服自配的提醒渠道（标题/摘要，不含密钥）。
	if result.Conversation.AssignedAgentID != "" {
		s.dispatchAgentAlert(result.Conversation.AssignedAgentID, "新访客消息",
			"访客「"+visitorDisplayName(result.Conversation)+"」："+alertPreview(result.Input.Content),
			result.Conversation.ID)
	}
	// 仍未分配客服：按策略入队并（必要时）补发无人值守兜底话术。
	if result.Conversation.AssignedAgentID == "" && result.Conversation.Status != domain.ConversationClosed {
		s.handleUnassignedConversation(result.Conversation, false)
	}
	if result.NeedAI {
		// 访客端显示"AI 正在回复"输入指示，AI 消息送达或生成结束时关闭。
		s.hub.SendToVisitor(conversationID, realtime.Event{Event: "conversation.typing", Data: map[string]any{"sender_type": "ai", "typing": true}})
		go s.generateAIReply(conversationID, result.Input.ServerMsgID, result.AIText, visitorLang)
	}
	// 访客画像：异步累计语言/方言/code-switching/地理，绝不阻塞消息主流程。
	// 画像关闭时 trackVisitorMessage 直接返回（不采集、不发起第三方请求）。
	s.trackVisitorMessage(result.Conversation, result.Input, language.DetectedLang)
	if needsAI {
		s.queueTranslation(translationJob{
			conversationID: conversationID,
			serverMsgID:    result.Input.ServerMsgID,
			sourceLang:     language.DetectedLang,
			targetLang:     ai.LangZh,
			messageType:    messageType,
			content:        content,
			styleHint:      s.translationStyleHint(preferences, result.Conversation, ai.LangZh),
			agentID:        result.Conversation.AssignedAgentID,
		})
	}
	return result, nil
}

func (s *Service) generateAIReply(conversationID, currentMsgID, userText string, visitorLang string) {
	// 优先使用 reception 能力绑定的供应商；未绑定/被禁用时回退到全局 ai_settings。
	settings, settingsErr := s.aiSettingsForCapability(domain.AICapabilityReception)
	if settingsErr != nil {
		s.logger.Warn("AI 能力绑定不可用，跳过本次 AI 回复",
			"conversation_id", conversationID, "capability", domain.AICapabilityReception, "error", settingsErr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), aiReplyTimeout(settings))
	defer cancel()
	visitorLang = ai.NormalizeLang(visitorLang, ai.LangZh)
	// 仅读取本会话的历史消息作为上下文，按 conversationID 隔离，绝不跨会话共享。
	history := s.conversationAITurns(conversationID, currentMsgID, visitorLang)
	// 知识库检索：只有存在启用中的知识库时才进入"只依据资料回答"模式；
	// 未配置任何知识库时，下面的分支与改造前完全一致（AI 自由回答）。
	search := s.searchKnowledge(userText, nil)
	var reply string
	var knowledgeEntryIDs []string
	handoffRequested := false
	// limitExceeded 表示本次未调用上游、直接回复超限提示；
	// limitExceededChinese 保存提示的中文原文，供非中文访客的译文落库复用。
	limitExceeded := false
	limitExceededChinese := ""
	switch {
	case search.Active && len(search.Hits) == 0:
		// 库外问题：不调用 AI 自由作答，按配置拒答或静默转人工。
		current, ok := s.store.Conversation(conversationID)
		if !ok || current.Status == domain.ConversationAssigned || current.Status == domain.ConversationClosed {
			// 人工已实际接管或会话已结束：AI 静默撤出，不产生任何消息。
			s.stopAITyping(conversationID)
			return
		}
		reply = knowledgeRefuseText(search.Settings)
		if search.Settings.UnknownAction == domain.KnowledgeUnknownHandoff {
			if _, err := s.store.RequestHumanHandoff(conversationID); err != nil {
				s.logger.Warn("知识库未命中转人工失败", "conversation_id", conversationID, "error", err)
			} else {
				handoffRequested = true
			}
		}
	case settingsErr == nil:
		// AI 每日调用次数：在真正调用上游之前检查；达到上限后不调用上游，
		// 直接落库一条超限提示并记审计，保证访客仍能收到明确反馈。
		limits := s.store.AILimits()
		if limits.Enabled && limits.AICallsPerConversationDay > 0 {
			_, aiCallCount := s.limitTracker.aiCalls.record(conversationID)
			if aiCallCount > limits.AICallsPerConversationDay {
				reply = s.limitExceedText(visitorLang, limits.ExceedMessage)
				limitExceededChinese = domain.LimitExceedMessageText(limits.ExceedMessage)
				limitExceeded = true
				if conversation, ok := s.store.Conversation(conversationID); ok {
					s.recordLimitExceeded(conversation, domain.AILimitDimensionAICalls,
						aiCallCount, limits.AICallsPerConversationDay)
				}
				break
			}
		}
		if len(search.Hits) > 0 {
			// 命中知识：把资料注入 system prompt，并要求只依据资料回答。
			settings.SystemPrompt = knowledge.BuildKnowledgePrompt(settings.SystemPrompt, search.Hits, search.ByID)
			knowledgeEntryIDs = knowledgeHitIDs(search.Hits)
		}
		// cacheKey 用会话 ID：同一会话的多轮请求共享同一缓存前缀，跨会话天然隔离。
		generated, err := s.ai.Reply(ctx, settings, s.store.ContactSettings(), history, userText, visitorLang, conversationID)
		if err != nil {
			s.logger.Warn("AI reply failed", "conversation_id", conversationID, "error", err)
			generated = aiBusyText(visitorLang)
		}
		reply = generated
	default:
		reply = aiBusyText(visitorLang)
	}
	// 单次回复字数：在翻译与落库之前截断，保证访客与客服看到同一份内容。
	// 截断优先保留完整句子，找不到句末标点时硬截断并补省略号。
	if maxChars := s.aiReplyCharLimit(); maxChars > 0 {
		reply = truncateAIReply(reply, maxChars)
	}
	translations := map[string]string{}
	switch {
	case limitExceeded && visitorLang != ai.LangZh:
		// 超限提示的中文原文已知，直接作为中文译文落库，避免再调用一次翻译上游。
		translations[ai.LangZh] = limitExceededChinese
	case visitorLang != ai.LangZh && ai.DetectLang(reply) != ai.LangZh:
		// AI 回复的中文译文属于 translation 能力，单独解析其绑定。
		translateSettings, translateSettingsErr := s.aiSettingsForCapability(domain.AICapabilityTranslation)
		if translateSettingsErr != nil {
			s.logger.Warn("翻译能力绑定不可用，跳过 AI 回复译文",
				"conversation_id", conversationID, "error", translateSettingsErr)
		} else if zhText, translateErr := s.ai.Translate(ctx, translateSettings, reply, visitorLang, ai.LangZh); translateErr == nil && strings.TrimSpace(zhText) != "" {
			translations[ai.LangZh] = zhText
		}
	}
	msg, conversation, err := s.store.AddAIMessageWithKnowledge(conversationID, reply, translations, knowledgeEntryIDs)
	if err != nil {
		// 无论静默撤出还是保存失败，都要关掉访客端的"AI 正在回复"指示。
		s.hub.SendToVisitor(conversationID, realtime.Event{Event: "conversation.typing", Data: map[string]any{"sender_type": "ai", "typing": false}})
		if errors.Is(err, store.ErrConflict) {
			// 人工客服已经实际回复，AI 静默撤出，实现无痕转人工。
			return
		}
		s.logger.Warn("save AI reply failed", "conversation_id", conversationID, "error", err)
		return
	}
	s.hub.SendToVisitor(conversationID, realtime.Event{Event: "message.receive", Data: VisitorFacingMessageForLang(msg, visitorLang)})
	if conversation.AssignedAgentID != "" {
		s.hub.SendToAgent(conversation.AssignedAgentID, realtime.Event{Event: "message.receive", Data: AgentFacingMessage(msg)})
	}
	s.hub.SendToVisitor(conversationID, realtime.Event{Event: "conversation.status_changed", Data: conversation})
	s.hub.BroadcastAdmins(realtime.Event{Event: "conversation.status_changed", Data: conversation})
	if handoffRequested {
		// 知识库未命中转人工：复用与关键词转人工一致的推送/通知行为。
		s.notifyAgent(conversation)
	}
}

// conversationAITurns 构造传给 AI 的当前会话历史上下文。
// 隔离保证：只按 conversationID 从存储读取本会话消息，AI 客户端无状态、
// 不缓存任何会话内容，因此不同会话之间不可能互相看到对方的对话。
// 上限控制：最多取最近 30 条消息，且总长度不超过 6000 字符，
// 超出时丢弃最旧的轮次，避免提示词无限膨胀。
func (s *Service) conversationAITurns(conversationID, currentMsgID, visitorLang string) []ai.HistoryTurn {
	messages := s.store.Messages(conversationID)
	if len(messages) == 0 {
		return nil
	}
	const maxMessages = 30
	const maxTotalChars = 6000
	start := 0
	if len(messages) > maxMessages {
		start = len(messages) - maxMessages
	}
	turns := make([]ai.HistoryTurn, 0, maxMessages)
	for _, msg := range messages[start:] {
		if msg.ServerMsgID == currentMsgID {
			continue
		}
		var role, content string
		switch {
		case msg.SenderType == domain.SenderVisitor && msg.MessageType == domain.MessageText:
			role, content = "user", msg.Content
		case msg.SenderType == domain.SenderAI && msg.MessageType == domain.MessageAIText:
			role, content = "assistant", msg.Content
		case msg.SenderType == domain.SenderAgent && msg.MessageType == domain.MessageText:
			// 客服消息对访客展示的是译文，AI 上下文同样使用访客语言的译文，
			// 避免把中文原文混进外语对话导致 AI 串语言。
			role = "assistant"
			if translated := strings.TrimSpace(msg.Translations[visitorLang]); translated != "" {
				content = translated
			} else {
				content = msg.Content
			}
		default:
			continue
		}
		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}
		turns = append(turns, ai.HistoryTurn{Role: role, Content: content})
	}
	total := 0
	for _, turn := range turns {
		total += len([]rune(turn.Content))
	}
	for len(turns) > 0 && total > maxTotalChars {
		total -= len([]rune(turns[0].Content))
		turns = turns[1:]
	}
	return turns
}

func (s *Service) AddAgentMessage(agentID, conversationID, clientMsgID, content string, messageType domain.MessageType) (store.MessageResult, error) {
	conversation, ok := s.store.Conversation(conversationID)
	if !ok {
		return store.MessageResult{}, store.ErrNotFound
	}
	visitorLang := conversationReplyLang(conversation)
	authorLang := visitorLang
	if isTranslatableMessageType(messageType) && strings.TrimSpace(content) != "" {
		authorLang = ai.DetectLanguage(content).Code
	}
	// 客服可以写任何语言：源语言=检测结果，目标语言=访客当前语言。
	// 客服关闭了"我的消息自动译成访客语言"时，直接按访客视图投递原文（这是客服自己的选择）。
	preferences := s.agentPreferences(agentID)
	autoTranslateOutgoing := prefBool(preferences.AutoTranslateOutgoing)
	translations, needsAI := staticTranslation(authorLang, visitorLang, messageType, content)
	status := messageTranslationState(authorLang, visitorLang, messageType, content)
	if !autoTranslateOutgoing {
		translations = nil
		needsAI = false
		status = "skipped"
	} else if needsAI {
		translations = nil
	}
	result, err := s.store.AddAgentMessage(agentID, conversationID, clientMsgID, content, messageType, translations)
	if err != nil {
		return store.MessageResult{}, err
	}
	if updated, metaErr := s.store.SetMessageLanguageMeta(conversationID, result.Input.ServerMsgID,
		authorLang, authorLang, status); metaErr == nil {
		result.Input = updated
	}
	s.hub.SendToAgent(agentID, realtime.Event{Event: "message.ack", Data: result.Input})
	if needsAI {
		// 译文未就绪：访客端只看到"客服正在输入"，绝不先收到客服原文语言。
		s.hub.SendToVisitor(conversationID, realtime.Event{Event: "conversation.typing", Data: map[string]any{"sender_type": "agent", "typing": true}})
		s.queueTranslation(translationJob{
			conversationID: conversationID,
			serverMsgID:    result.Input.ServerMsgID,
			sourceLang:     authorLang,
			targetLang:     visitorLang,
			messageType:    messageType,
			content:        content,
			styleHint:      s.translationStyleHint(preferences, conversation, visitorLang),
			agentID:        agentID,
			visitorFacing:  true,
		})
	} else {
		// 无需翻译（或命中静态文案）：直接按访客语言投递。
		s.hub.SendToVisitor(conversationID, realtime.Event{Event: "message.receive", Data: VisitorFacingMessageForLang(result.Input, visitorLang)})
	}
	s.hub.SendToVisitor(conversationID, realtime.Event{Event: "conversation.status_changed", Data: result.Conversation})
	s.hub.BroadcastAdmins(realtime.Event{Event: "conversation.status_changed", Data: result.Conversation})
	return result, nil
}

func (s *Service) RevokeMessage(actor domain.AuthSession, conversationID, serverMsgID string) (domain.Message, domain.Conversation, error) {
	msg, conversation, err := s.store.RevokeMessage(actor, conversationID, serverMsgID)
	if err != nil {
		return domain.Message{}, domain.Conversation{}, err
	}
	visitorMessage := VisitorFacingMessage(msg)
	s.hub.SendToVisitor(conversationID, realtime.Event{Event: "message.revoked", Data: visitorMessage})
	if conversation.AssignedAgentID != "" {
		s.hub.SendToAgent(conversation.AssignedAgentID, realtime.Event{Event: "message.revoked", Data: msg})
		s.hub.SendToAgent(conversation.AssignedAgentID, realtime.Event{Event: "conversation.status_changed", Data: conversation})
	}
	s.hub.BroadcastAdmins(realtime.Event{Event: "message.revoked", Data: msg})
	s.hub.BroadcastAdmins(realtime.Event{Event: "conversation.status_changed", Data: conversation})
	return msg, conversation, nil
}

func (s *Service) CloseConversation(actor domain.AuthSession, conversationID string) (domain.Conversation, error) {
	conversation, msg, err := s.store.CloseConversation(actor, conversationID)
	if err != nil {
		return domain.Conversation{}, err
	}
	visitorLang := ai.NormalizeLang(conversation.VisitorLang, ai.LangZh)
	s.translateAndStoreSystemMessage(conversationID, visitorLang, &msg)
	s.hub.SendToVisitor(conversationID, realtime.Event{Event: "message.receive", Data: VisitorFacingMessageForLang(msg, visitorLang)})
	s.hub.SendToVisitor(conversationID, realtime.Event{Event: "conversation.status_changed", Data: conversation})
	if conversation.AssignedAgentID != "" {
		s.hub.SendToAgent(conversation.AssignedAgentID, realtime.Event{Event: "conversation.status_changed", Data: conversation})
	}
	s.hub.BroadcastAdmins(realtime.Event{Event: "conversation.status_changed", Data: conversation})
	// 结束的会话不应继续占着排队位置；清理失败（不在队列中）无需关心。
	if err := s.store.DequeueConversation(conversationID); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.logger.Warn("清理排队记录失败", "conversation_id", conversationID, "error", err)
	}
	// 会话结束释放了客服并发：尝试把排队中的会话分配给空闲客服。
	s.drainQueue()
	return conversation, nil
}

func (s *Service) UpdateRemark(actor domain.AuthSession, conversationID, remark string) (domain.Conversation, error) {
	conversation, err := s.store.UpdateRemark(actor, conversationID, remark)
	if err != nil {
		return domain.Conversation{}, err
	}
	if conversation.AssignedAgentID != "" {
		s.hub.SendToAgent(conversation.AssignedAgentID, realtime.Event{Event: "conversation.status_changed", Data: conversation})
	}
	s.hub.BroadcastAdmins(realtime.Event{Event: "conversation.status_changed", Data: conversation})
	return conversation, nil
}

func (s *Service) TransferConversation(actor domain.AuthSession, conversationID, agentID, group string) (domain.Conversation, error) {
	conversation, msg, err := s.store.TransferConversation(actor, conversationID, agentID, group)
	if err != nil {
		return domain.Conversation{}, err
	}
	// 无痕转人工：转接提示只在客服/后台侧可见，不下发给访客。
	s.hub.SendToVisitor(conversationID, realtime.Event{Event: "conversation.status_changed", Data: conversation})
	if conversation.AssignedAgentID != "" {
		s.hub.SendToAgent(conversation.AssignedAgentID, realtime.Event{Event: "conversation.assigned", Data: conversation})
		s.hub.SendToAgent(conversation.AssignedAgentID, realtime.Event{Event: "message.receive", Data: AgentFacingMessage(msg)})
		// 转接后通知新接手的客服（含后台推送与提醒渠道）。
		s.notifyAgent(conversation)
		s.dispatchAgentAlert(conversation.AssignedAgentID, "会话转接",
			"访客「"+visitorDisplayName(conversation)+"」的会话已转接到您名下。", conversation.ID)
	}
	s.hub.BroadcastAdmins(realtime.Event{Event: "conversation.status_changed", Data: conversation})
	// 原负责客服的并发被释放：尝试把排队中的会话分配出去。
	s.drainQueue()
	return conversation, nil
}

func (s *Service) ServeWebSocket(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: s.allowWebSocketOrigin,
	}

	role := realtime.ClientRole(r.URL.Query().Get("role"))
	token := r.URL.Query().Get("token")
	conversationID := r.URL.Query().Get("conversation_id")

	var accountID string
	switch role {
	case realtime.RoleVisitor:
		session, ok := s.store.AuthVisitor(token)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		accountID = session.AccountID
		if conversationID == "" {
			conversationID = session.AccountID
		} else if conversationID != session.AccountID {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	case realtime.RoleAgent, realtime.RoleAdmin:
		session, ok := s.store.Auth(token)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if role == realtime.RoleAgent && session.Kind != domain.AccountAgent {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if role == realtime.RoleAdmin && session.Kind != domain.AccountAdmin {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		accountID = session.AccountID
	default:
		http.Error(w, "invalid role", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Warn("websocket upgrade failed", "error", err)
		return
	}

	client := realtime.NewClient(role, accountID, conversationID, conn, s.hub, s.handleSocketEvent, s.logger)
	client.Run()
}

func (s *Service) allowWebSocketOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	if len(s.websocketAllowedOrigins) == 0 || s.websocketAllowedOrigins["*"] {
		return true
	}
	return s.websocketAllowedOrigins[origin]
}

func (s *Service) handleSocketEvent(client *realtime.Client, event realtime.IncomingEvent) {
	switch event.Event {
	case "conversation.typing":
		// 客服端输入中指示：透传给访客，让访客聊天窗口显示"正在回复"。
		if client.Role != realtime.RoleAgent {
			return
		}
		var req struct {
			ConversationID string `json:"conversation_id"`
			Typing         bool   `json:"typing"`
		}
		if err := json.Unmarshal(event.Data, &req); err != nil {
			return
		}
		if strings.TrimSpace(req.ConversationID) == "" {
			return
		}
		s.hub.SendToVisitor(req.ConversationID, realtime.Event{Event: "conversation.typing", Data: map[string]any{"sender_type": "agent", "typing": req.Typing}})
	case "message.send":
		var req struct {
			ClientMsgID    string             `json:"client_msg_id"`
			ConversationID string             `json:"conversation_id"`
			MessageType    domain.MessageType `json:"message_type"`
			Content        string             `json:"content"`
		}
		if err := json.Unmarshal(event.Data, &req); err != nil {
			client.Send(realtime.Event{Event: "error", Data: map[string]string{"message": "invalid message payload"}})
			return
		}
		if req.MessageType == "" {
			req.MessageType = domain.MessageText
		}
		if client.Role == realtime.RoleVisitor {
			conversationID := req.ConversationID
			if conversationID == "" {
				conversationID = client.ConversationID
			}
			if conversationID != client.ConversationID {
				client.Send(errorEvent(store.ErrForbidden))
				return
			}
			if _, err := s.AddVisitorMessage(conversationID, req.ClientMsgID, req.Content, req.MessageType); err != nil {
				client.Send(errorEvent(err))
			}
			return
		}
		if client.Role == realtime.RoleAgent {
			if _, err := s.AddAgentMessage(client.AccountID, req.ConversationID, req.ClientMsgID, req.Content, req.MessageType); err != nil {
				client.Send(errorEvent(err))
			}
			return
		}
		client.Send(errorEvent(store.ErrForbidden))
	case "conversation.close":
		var req struct {
			ConversationID string `json:"conversation_id"`
		}
		if err := json.Unmarshal(event.Data, &req); err != nil {
			client.Send(errorEvent(err))
			return
		}
		if client.Role == realtime.RoleVisitor {
			if strings.TrimSpace(req.ConversationID) == "" {
				req.ConversationID = client.ConversationID
			}
			if req.ConversationID != client.ConversationID {
				client.Send(errorEvent(store.ErrForbidden))
				return
			}
		}
		kind := domain.AccountVisitor
		if client.Role == realtime.RoleAgent {
			kind = domain.AccountAgent
		}
		if client.Role == realtime.RoleAdmin {
			kind = domain.AccountAdmin
		}
		if _, err := s.CloseConversation(domain.AuthSession{Kind: kind, AccountID: client.AccountID}, req.ConversationID); err != nil {
			client.Send(errorEvent(err))
		}
	default:
		client.Send(realtime.Event{Event: "error", Data: map[string]string{"message": "unknown event"}})
	}
}

func (s *Service) notifyAgent(conversation domain.Conversation) {
	if conversation.AssignedAgentID == "" {
		return
	}
	notification := domain.AgentNotification{
		AgentID:        conversation.AssignedAgentID,
		ConversationID: conversation.ID,
		Title:          conversation.VisitorRemark,
		Body:           conversation.LastMessage,
		Status:         conversation.Status,
	}
	s.hub.SendToAgent(conversation.AssignedAgentID, realtime.Event{Event: "agent.notification", Data: notification})
	deviceToken := ""
	devicePlatform := ""
	for _, agent := range s.store.AllAgents() {
		if agent.ID == conversation.AssignedAgentID {
			deviceToken = strings.TrimSpace(agent.DeviceToken)
			devicePlatform = strings.TrimSpace(agent.DevicePlatform)
			break
		}
	}
	pushSettings := s.store.PushSettings()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.notifier.Notify(ctx, pushSettings, notify.Message{
			DeviceToken:    deviceToken,
			Platform:       devicePlatform,
			AgentID:        notification.AgentID,
			ConversationID: notification.ConversationID,
			Title:          notification.Title,
			Body:           notification.Body,
			Data: map[string]string{
				"conversation_id": notification.ConversationID,
				"status":          string(notification.Status),
			},
		}); err != nil {
			s.logger.Warn("push notification failed", "agent_id", notification.AgentID, "error", err)
		}
	}()
}

func bearerToken(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return strings.TrimSpace(header[7:])
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func nonEmpty(values ...string) string {
	return firstNonEmpty(values...)
}

func positiveInt(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func errorEvent(err error) realtime.Event {
	status := "error"
	message := err.Error()
	if errors.Is(err, store.ErrUnauthorized) {
		status = "unauthorized"
	}
	if errors.Is(err, store.ErrForbidden) {
		status = "forbidden"
	}
	if errors.Is(err, store.ErrNotFound) {
		status = "not_found"
	}
	if errors.Is(err, store.ErrRateLimited) {
		// 限额错误必须把面向访客的文案原样下发，不能只给内部维度信息。
		status = "rate_limited"
		if limitMessage := store.RateLimitMessage(err); limitMessage != "" {
			message = limitMessage
		}
	}
	if errors.Is(err, store.ErrBlacklisted) {
		// 黑名单命中：同样只下发统一文案，绝不包含被拉黑的 kind/value。
		status = "blacklisted"
		message = domain.DefaultBlacklistMessage
		if blacklistMessage := store.BlacklistMessage(err); blacklistMessage != "" {
			message = blacklistMessage
		}
	}
	return realtime.Event{Event: "error", Data: map[string]string{"code": status, "message": message}}
}

// translationWindow 是同步翻译路径的最长等待时间，超时后回退为原文下发，避免阻塞消息送达。
const translationWindow = 25 * time.Second

// defaultAITimeout 是未配置（或配置过小）时 AI 请求的默认超时。
const defaultAITimeout = 20 * time.Second

// maxAIReplyTimeout 是 AI 回复整条链路的外层超时上限，AI 配置最多允许 120s，留足翻译余量。
const maxAIReplyTimeout = 150 * time.Second

// aiReplyTimeout 计算 AI 回复链路的外层超时：取配置的超时时间（不低于默认 20s）再加翻译余量，
// 保证 AI 请求本身不会被外层 context 提前掐断，同时不超过 150s 上限。
func aiReplyTimeout(settings domain.AISettings) time.Duration {
	timeout := time.Duration(settings.TimeoutSeconds) * time.Second
	if timeout < defaultAITimeout {
		timeout = defaultAITimeout
	}
	timeout += translationWindow
	if timeout > maxAIReplyTimeout {
		timeout = maxAIReplyTimeout
	}
	return timeout
}

// translateInitialMessageForVisitor 把欢迎语等初始消息翻译成访客语言。
// 能静态映射的直接映射；其余异步调用 AI 翻译，翻译完成后通过 WebSocket 推送更新（前端按消息 ID 替换气泡）。
func (s *Service) translateInitialMessageForVisitor(conversationID, visitorLang string, msg *domain.Message) {
	if visitorLang == ai.LangZh || !isTranslatableMessageType(msg.MessageType) {
		return
	}
	if translated, ok := knownSystemText(msg.Content, visitorLang); ok {
		if updated, err := s.store.UpdateMessageTranslations(conversationID, msg.ServerMsgID, map[string]string{visitorLang: translated}); err == nil {
			msg.Translations = updated.Translations
		}
		return
	}
	serverMsgID := msg.ServerMsgID
	original := msg.Content
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), translationWindow)
		defer cancel()
		translated, err := s.ai.Translate(ctx, s.translationSettingsOrDefault(conversationID), original, ai.LangZh, visitorLang)
		if err != nil || strings.TrimSpace(translated) == "" {
			s.logger.Warn("translate initial message failed", "conversation_id", conversationID, "error", err)
			return
		}
		updated, err := s.store.UpdateMessageTranslations(conversationID, serverMsgID, map[string]string{visitorLang: translated})
		if err != nil {
			return
		}
		s.hub.SendToVisitor(conversationID, realtime.Event{Event: "message.receive", Data: VisitorFacingMessageForLang(updated, visitorLang)})
	}()
}

// isTranslatableMessageType 判断消息内容是否为可翻译的自然语言文本。
func isTranslatableMessageType(messageType domain.MessageType) bool {
	switch messageType {
	case domain.MessageText, domain.MessageAIText, domain.MessageSystem, domain.MessageHandoff,
		domain.MessageContactTelephone, domain.MessageContactWhatsApp, domain.MessageContactTelegram:
		return true
	default:
		return false
	}
}

// knownSystemTexts 对固定系统文案提供静态翻译，避免为固定短语额外调用 AI。
var knownSystemTexts = map[string]map[string]string{
	"会话已结束":     {"en": "The conversation has ended.", "ar": "انتهت المحادثة."},
	"对方撤回了一条消息": {"en": "A message was recalled.", "ar": "تم سحب رسالة."},
	// 默认欢迎语：静态三语映射，保证会话创建响应里就带译文，访客端零延迟、不闪中文。
	"您好，欢迎咨询在线客服，请问有什么可以帮您？": {
		"en": "Hello! Welcome to our live support — how can we help you today?",
		"ar": "مرحبًا! أهلاً بك في خدمة الدعم المباشر، كيف يمكننا مساعدتك اليوم؟",
	},
	"我已收到您的问题。您可以继续补充细节，人工客服会尽快为您跟进。": {
		"en": "I've got your question — feel free to add more details and our team will follow up shortly.",
		"ar": "استلمنا سؤالك، يمكنك إضافة المزيد من التفاصيل وسيتابع فريق الدعم معك في أقرب وقت.",
	},
}

func knownSystemText(content, lang string) (string, bool) {
	translations, ok := knownSystemTexts[strings.TrimSpace(content)]
	if !ok {
		return "", false
	}
	translated, ok := translations[lang]
	return translated, ok && translated != ""
}

func humanRequestText(lang string) string {
	switch lang {
	case ai.LangEn:
		return "Please connect me to a human agent."
	case ai.LangAr:
		return "أريد التحدث مع موظف خدمة العملاء."
	default:
		return "人工客服"
	}
}

func aiBusyText(lang string) string {
	switch lang {
	case ai.LangEn:
		return "Our AI assistant is temporarily busy. Your question has been recorded and our support team will follow up shortly."
	case ai.LangAr:
		return "المساعد الذكي مشغول حاليًا، تم تسجيل سؤالك وسيتابع فريق الدعم معك قريبًا."
	default:
		return "AI 客服暂时繁忙，已为您记录问题，人工客服会尽快为您跟进。"
	}
}

// translateToChinese 把访客消息翻译成中文供客服/后台查看。翻译失败或已是中文时返回 nil（下发原文）。
func (s *Service) translateToChinese(sourceLang string, messageType domain.MessageType, content string) map[string]string {
	return s.translateMessage(sourceLang, ai.LangZh, messageType, content)
}

// translateMessage 通用译文生成：源语言与目标语言相同、不可翻译类型或翻译失败时返回 nil。
func (s *Service) translateMessage(sourceLang, targetLang string, messageType domain.MessageType, content string) map[string]string {
	return s.translateMessageWithStyle(sourceLang, targetLang, messageType, content, "")
}

// translateMessageWithStyle 在通用译文生成的基础上附加目标语言风格提示（方言模式）。
func (s *Service) translateMessageWithStyle(sourceLang, targetLang string, messageType domain.MessageType, content, styleHint string) map[string]string {
	sourceLang = ai.NormalizeLang(sourceLang, ai.LangZh)
	targetLang = ai.NormalizeLang(targetLang, ai.LangZh)
	if sourceLang == targetLang || !isTranslatableMessageType(messageType) || strings.TrimSpace(content) == "" {
		return nil
	}
	if translated, ok := knownSystemText(content, targetLang); ok {
		return map[string]string{targetLang: translated}
	}
	// 翻译能力优先使用其绑定的供应商，未绑定/禁用时回退到全局 ai_settings。
	settings, settingsErr := s.aiSettingsForCapability(domain.AICapabilityTranslation)
	if settingsErr != nil {
		s.logger.Warn("翻译能力绑定不可用，跳过本次翻译",
			"source", sourceLang, "target", targetLang, "error", settingsErr)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), translationWindow)
	defer cancel()
	translated, err := s.ai.TranslateWithStyle(ctx, settings, content, sourceLang, targetLang, styleHint)
	if err != nil || strings.TrimSpace(translated) == "" {
		s.logger.Warn("translate message failed", "source", sourceLang, "target", targetLang, "error", err)
		return nil
	}
	return map[string]string{targetLang: translated}
}

// translateFromChineseForVisitor 兼容旧调用点：把中文消息翻译成访客语言。
func (s *Service) translateFromChineseForVisitor(visitorLang string, messageType domain.MessageType, content string) map[string]string {
	return s.translateMessage(ai.LangZh, visitorLang, messageType, content)
}

// translateGeneratedMessageForVisitor 翻译关键词自动回复等系统生成消息，并落库译文。
func (s *Service) translateGeneratedMessageForVisitor(conversationID, visitorLang string, msg *domain.Message) {
	s.translateAndStoreSystemMessage(conversationID, visitorLang, msg)
}

// translateAndStoreSystemMessage 为访客翻译系统消息：优先静态映射，其次 AI 翻译，并把译文保存到消息记录。
func (s *Service) translateAndStoreSystemMessage(conversationID, visitorLang string, msg *domain.Message) {
	if visitorLang == ai.LangZh || !isTranslatableMessageType(msg.MessageType) {
		return
	}
	if translated, ok := knownSystemText(msg.Content, visitorLang); ok {
		msg.Translations = map[string]string{visitorLang: translated}
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), translationWindow)
		defer cancel()
		translated, err := s.ai.Translate(ctx, s.translationSettingsOrDefault(conversationID), msg.Content, ai.LangZh, visitorLang)
		if err != nil || strings.TrimSpace(translated) == "" {
			s.logger.Warn("translate system message failed", "conversation_id", conversationID, "error", err)
			return
		}
		msg.Translations = map[string]string{visitorLang: translated}
	}
	if updated, err := s.store.UpdateMessageTranslations(conversationID, msg.ServerMsgID, msg.Translations); err == nil {
		msg.Translations = updated.Translations
	}
}
