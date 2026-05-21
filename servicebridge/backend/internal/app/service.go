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
	"customer-service/backend/internal/realtime"
	"customer-service/backend/internal/store"
)

type Service struct {
	store    store.Store
	hub      *realtime.Hub
	ai       *ai.Client
	notifier Notifier
	logger   *slog.Logger

	websocketAllowedOrigins map[string]bool
}

type Notifier interface {
	NotifyAgent(ctx context.Context, notification domain.AgentNotification, devices []domain.PushDevice) error
}

func NewService(store store.Store, hub *realtime.Hub, logger *slog.Logger) *Service {
	return &Service{
		store:                   store,
		hub:                     hub,
		ai:                      ai.NewClient(),
		logger:                  logger,
		websocketAllowedOrigins: map[string]bool{"*": true},
	}
}

func (s *Service) SetNotifier(notifier Notifier) {
	s.notifier = notifier
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
	return s.ai.Reply(ctx, s.store.AISettings(), s.store.ContactSettings(), userText)
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

func (s *Service) CreateVisitorConversation(ip, source string) (domain.VisitorSession, error) {
	session, err := s.store.CreateVisitorConversation(ip, source)
	if err != nil {
		return domain.VisitorSession{}, err
	}
	s.hub.BroadcastAdmins(realtime.Event{Event: "conversation.created", Data: session.Conversation})
	if session.Conversation.AssignedAgentID != "" {
		s.hub.SendToAgent(session.Conversation.AssignedAgentID, realtime.Event{Event: "conversation.assigned", Data: session.Conversation})
	}
	return session, nil
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

func (s *Service) RegisterAgentPushDevice(agentID string, device domain.PushDevice) (domain.PushDevice, error) {
	registered, err := s.store.RegisterAgentPushDevice(agentID, device)
	if err != nil {
		return domain.PushDevice{}, err
	}
	s.hub.BroadcastAdmins(realtime.Event{Event: "agent.push_device_registered", Data: map[string]any{
		"agent_id":  agentID,
		"platform":  registered.Platform,
		"provider":  registered.Provider,
		"device_id": registered.ID,
	}})
	return registered, nil
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
	result, err := s.store.AddVisitorMessage(conversationID, clientMsgID, content, messageType)
	if err != nil {
		return store.MessageResult{}, err
	}

	s.hub.SendToVisitor(conversationID, realtime.Event{Event: "message.ack", Data: result.Input})
	if result.Conversation.AssignedAgentID != "" {
		s.hub.SendToAgent(result.Conversation.AssignedAgentID, realtime.Event{Event: "message.receive", Data: result.Input})
		s.hub.SendToAgent(result.Conversation.AssignedAgentID, realtime.Event{Event: "conversation.status_changed", Data: result.Conversation})
	}
	for _, msg := range result.Generated {
		s.hub.SendToVisitor(conversationID, realtime.Event{Event: "message.receive", Data: msg})
		if result.Conversation.AssignedAgentID != "" {
			s.hub.SendToAgent(result.Conversation.AssignedAgentID, realtime.Event{Event: "message.receive", Data: msg})
		}
	}
	s.hub.SendToVisitor(conversationID, realtime.Event{Event: "conversation.status_changed", Data: result.Conversation})
	s.hub.BroadcastAdmins(realtime.Event{Event: "conversation.status_changed", Data: result.Conversation})
	if result.Conversation.Status == domain.ConversationHumanRequested || result.Conversation.AssignedAgentID != "" {
		s.notifyAgent(result.Conversation)
	}
	if result.NeedAI {
		go s.generateAIReply(conversationID, result.AIText)
	}
	if result.Conversation.Status == domain.ConversationAssigned && result.Conversation.AssignedAgentID != "" {
		s.scheduleNoReplyAI(conversationID, result.AIText, result.VisitorMessageSentAt)
	}
	return result, nil
}

func (s *Service) scheduleNoReplyAI(conversationID, userText string, visitorMessageSentAt time.Time) {
	timeoutSeconds := s.store.AISettings().NoReplyTimeoutSeconds
	if timeoutSeconds <= 0 {
		return
	}
	go func() {
		timer := time.NewTimer(time.Duration(timeoutSeconds) * time.Second)
		defer timer.Stop()
		<-timer.C
		conversation, ok := s.store.EscalateNoReplyToAI(conversationID, visitorMessageSentAt)
		if !ok {
			return
		}
		s.hub.SendToVisitor(conversationID, realtime.Event{Event: "conversation.status_changed", Data: conversation})
		if conversation.AssignedAgentID != "" {
			s.hub.SendToAgent(conversation.AssignedAgentID, realtime.Event{Event: "conversation.status_changed", Data: conversation})
		}
		s.hub.BroadcastAdmins(realtime.Event{Event: "conversation.status_changed", Data: conversation})
		s.generateAIReply(conversationID, userText)
	}()
}

func (s *Service) generateAIReply(conversationID, userText string) {
	reply, err := s.ai.Reply(context.Background(), s.store.AISettings(), s.store.ContactSettings(), userText)
	if err != nil {
		s.logger.Warn("AI reply failed", "conversation_id", conversationID, "error", err)
		reply = "AI 客服暂时繁忙，已为您记录问题。如需人工处理，请输入“人工客服”。"
	}
	msg, conversation, err := s.store.AddAIMessage(conversationID, reply)
	if err != nil {
		s.logger.Warn("save AI reply failed", "conversation_id", conversationID, "error", err)
		return
	}
	s.hub.SendToVisitor(conversationID, realtime.Event{Event: "message.receive", Data: msg})
	if conversation.AssignedAgentID != "" {
		s.hub.SendToAgent(conversation.AssignedAgentID, realtime.Event{Event: "message.receive", Data: msg})
	}
	s.hub.SendToVisitor(conversationID, realtime.Event{Event: "conversation.status_changed", Data: conversation})
	s.hub.BroadcastAdmins(realtime.Event{Event: "conversation.status_changed", Data: conversation})
}

func (s *Service) AddAgentMessage(agentID, conversationID, clientMsgID, content string, messageType domain.MessageType) (store.MessageResult, error) {
	result, err := s.store.AddAgentMessage(agentID, conversationID, clientMsgID, content, messageType)
	if err != nil {
		return store.MessageResult{}, err
	}
	s.hub.SendToAgent(agentID, realtime.Event{Event: "message.ack", Data: result.Input})
	s.hub.SendToVisitor(conversationID, realtime.Event{Event: "message.receive", Data: result.Input})
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
	s.hub.SendToVisitor(conversationID, realtime.Event{Event: "message.receive", Data: msg})
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
	s.hub.SendToVisitor(conversationID, realtime.Event{Event: "message.receive", Data: msg})
	s.hub.SendToVisitor(conversationID, realtime.Event{Event: "conversation.status_changed", Data: conversation})
	if conversation.AssignedAgentID != "" {
		s.hub.SendToAgent(conversation.AssignedAgentID, realtime.Event{Event: "conversation.assigned", Data: conversation})
		s.hub.SendToAgent(conversation.AssignedAgentID, realtime.Event{Event: "message.receive", Data: msg})
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
	if s.notifier == nil {
		return
	}
	devices := s.store.PushDevicesForAgent(conversation.AssignedAgentID)
	if len(devices) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.notifier.NotifyAgent(ctx, notification, devices); err != nil {
			s.logger.Warn("push notification failed", "agent_id", notification.AgentID, "conversation_id", notification.ConversationID, "error", err)
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
