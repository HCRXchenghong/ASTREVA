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
}

func NewService(store store.Store, hub *realtime.Hub, logger *slog.Logger) *Service {
	return &Service{
		store:                   store,
		hub:                     hub,
		ai:                      ai.NewClient(),
		logger:                  logger,
		notifier:                notify.DisabledNotifier{},
		websocketAllowedOrigins: map[string]bool{"*": true},
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
	return s.ai.Reply(ctx, s.store.AISettings(), s.store.ContactSettings(), userText, ai.LangZh)
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

func (s *Service) CreateVisitorConversation(ip, source, lang, ipSource string) (domain.VisitorSession, error) {
	session, err := s.store.CreateVisitorConversation(ip, source, lang, ipSource)
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
		}
		s.hub.SendToVisitor(conversation.ID, realtime.Event{Event: "conversation.status_changed", Data: conversation})
		s.hub.BroadcastAdmins(realtime.Event{Event: "conversation.status_changed", Data: conversation})
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
	visitorLang := ai.NormalizeLang(conversation.VisitorLang, ai.LangZh)
	if messageType == domain.MessageHandoff && strings.TrimSpace(content) == "" {
		content = humanRequestText(visitorLang)
	}
	translations := s.translateToChinese(visitorLang, messageType, content)
	result, err := s.store.AddVisitorMessage(conversationID, clientMsgID, content, messageType, translations)
	if err != nil {
		return store.MessageResult{}, err
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
	if result.NeedAI {
		// 访客端显示"AI 正在回复"输入指示，AI 消息送达或生成结束时关闭。
		s.hub.SendToVisitor(conversationID, realtime.Event{Event: "conversation.typing", Data: map[string]any{"sender_type": "ai", "typing": true}})
		go s.generateAIReply(conversationID, result.AIText, visitorLang)
	}
	return result, nil
}

func (s *Service) generateAIReply(conversationID, userText string, visitorLang string) {
	ctx, cancel := context.WithTimeout(context.Background(), translationWindow)
	defer cancel()
	visitorLang = ai.NormalizeLang(visitorLang, ai.LangZh)
	reply, err := s.ai.Reply(ctx, s.store.AISettings(), s.store.ContactSettings(), userText, visitorLang)
	if err != nil {
		s.logger.Warn("AI reply failed", "conversation_id", conversationID, "error", err)
		reply = aiBusyText(visitorLang)
	}
	translations := map[string]string{}
	if visitorLang != ai.LangZh && ai.DetectLang(reply) != ai.LangZh {
		zhText, translateErr := s.ai.Translate(ctx, s.store.AISettings(), reply, visitorLang, ai.LangZh)
		if translateErr == nil && strings.TrimSpace(zhText) != "" {
			translations[ai.LangZh] = zhText
		}
	}
	msg, conversation, err := s.store.AddAIMessage(conversationID, reply, translations)
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
}

func (s *Service) AddAgentMessage(agentID, conversationID, clientMsgID, content string, messageType domain.MessageType) (store.MessageResult, error) {
	conversation, ok := s.store.Conversation(conversationID)
	if !ok {
		return store.MessageResult{}, store.ErrNotFound
	}
	visitorLang := ai.NormalizeLang(conversation.VisitorLang, ai.LangZh)
	translations := s.translateFromChineseForVisitor(visitorLang, messageType, content)
	result, err := s.store.AddAgentMessage(agentID, conversationID, clientMsgID, content, messageType, translations)
	if err != nil {
		return store.MessageResult{}, err
	}
	s.hub.SendToAgent(agentID, realtime.Event{Event: "message.ack", Data: result.Input})
	// 访客侧看到的是其语言译文；客服发送的中文原文不下发给访客。
	s.hub.SendToVisitor(conversationID, realtime.Event{Event: "message.receive", Data: VisitorFacingMessageForLang(result.Input, visitorLang)})
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
	}
	s.hub.BroadcastAdmins(realtime.Event{Event: "conversation.status_changed", Data: conversation})
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
	for _, agent := range s.store.AllAgents() {
		if agent.ID == conversation.AssignedAgentID {
			deviceToken = strings.TrimSpace(agent.DeviceToken)
			break
		}
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.notifier.Notify(ctx, notify.Message{
			AgentID:        deviceToken,
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
	return realtime.Event{Event: "error", Data: map[string]string{"code": status, "message": message}}
}

// translationWindow 是同步翻译路径的最长等待时间，超时后回退为原文下发，避免阻塞消息送达。
const translationWindow = 25 * time.Second

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
		translated, err := s.ai.Translate(ctx, s.store.AISettings(), original, ai.LangZh, visitorLang)
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
func (s *Service) translateToChinese(visitorLang string, messageType domain.MessageType, content string) map[string]string {
	if visitorLang == ai.LangZh || !isTranslatableMessageType(messageType) {
		return nil
	}
	if ai.DetectLang(content) == ai.LangZh {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), translationWindow)
	defer cancel()
	translated, err := s.ai.Translate(ctx, s.store.AISettings(), content, visitorLang, ai.LangZh)
	if err != nil || strings.TrimSpace(translated) == "" {
		s.logger.Warn("translate visitor message to Chinese failed", "error", err)
		return nil
	}
	return map[string]string{ai.LangZh: translated}
}

// translateFromChineseForVisitor 把客服/后台的中文消息翻译成访客语言。失败或访客语言为中文时返回 nil。
func (s *Service) translateFromChineseForVisitor(visitorLang string, messageType domain.MessageType, content string) map[string]string {
	if visitorLang == ai.LangZh || !isTranslatableMessageType(messageType) {
		return nil
	}
	if translated, ok := knownSystemText(content, visitorLang); ok {
		return map[string]string{visitorLang: translated}
	}
	ctx, cancel := context.WithTimeout(context.Background(), translationWindow)
	defer cancel()
	translated, err := s.ai.Translate(ctx, s.store.AISettings(), content, ai.LangZh, visitorLang)
	if err != nil || strings.TrimSpace(translated) == "" {
		s.logger.Warn("translate agent message for visitor failed", "error", err)
		return nil
	}
	return map[string]string{visitorLang: translated}
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
		translated, err := s.ai.Translate(ctx, s.store.AISettings(), msg.Content, ai.LangZh, visitorLang)
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
