package openapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"customer-service/backend/internal/ai"
	"customer-service/backend/internal/app"
	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/store"
)

const maxMessageContentRunes = 4000

var allowedOpenMessageTypes = map[domain.MessageType]bool{
	domain.MessageText:  true,
	domain.MessageEmoji: true,
	domain.MessageImage: true,
}

// HandleOpenCORS 处理开放接口的预检请求。
func (s *Service) HandleOpenCORS(w http.ResponseWriter, r *http.Request) {
	applyOpenCORS(w)
	w.WriteHeader(http.StatusNoContent)
}

// HandleMe 返回当前授权应用信息，供接入方做连通性测试。
// GET /open/v1/me
func (s *Service) HandleMe(w http.ResponseWriter, r *http.Request) {
	applyOpenCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeOpenError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	apiApp, ok := s.requireApp(w, r)
	if !ok {
		return
	}
	writeOpenJSON(w, http.StatusOK, map[string]any{
		"app": map[string]any{
			"id":         apiApp.ID,
			"name":       apiApp.Name,
			"key_prefix": apiApp.KeyPrefix,
			"rate_limit": apiApp.RateLimit,
		},
		"server_time": time.Now().UTC(),
	})
}

// HandleConversations 创建/列出本应用名下的会话。
// POST /open/v1/conversations 创建会话（可带外部用户标识与语言）。
// GET  /open/v1/conversations 列出本应用会话（最多 200 条，按更新时间倒序）。
func (s *Service) HandleConversations(w http.ResponseWriter, r *http.Request) {
	applyOpenCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	apiApp, ok := s.requireApp(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodPost:
		s.createConversation(w, r, apiApp)
	case http.MethodGet:
		s.listConversations(w, apiApp)
	default:
		writeOpenError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) createConversation(w http.ResponseWriter, r *http.Request, apiApp domain.APIApp) {
	var req struct {
		ExternalUserID string `json:"external_user_id"`
		Lang           string `json:"lang"`
	}
	if r.Body != nil {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		if err := decoder.Decode(&req); err != nil && !errors.Is(err, http.ErrBodyNotAllowed) && err.Error() != "EOF" {
			writeOpenError(w, http.StatusBadRequest, "invalid json body")
			return
		}
	}
	req.ExternalUserID = strings.TrimSpace(req.ExternalUserID)
	if len([]rune(req.ExternalUserID)) > 128 {
		writeOpenError(w, http.StatusBadRequest, "external_user_id too long (max 128 chars)")
		return
	}
	ip, ipSource := s.resolveIP(r)
	session, err := s.app.CreateVisitorConversation(ip, appSource(apiApp.ID), req.Lang, ipSource, req.ExternalUserID)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	conversation := session.Conversation
	writeOpenJSON(w, http.StatusCreated, map[string]any{
		"conversation":     conversation,
		"initial_messages": session.InitialMessages,
	})
}

func (s *Service) listConversations(w http.ResponseWriter, apiApp domain.APIApp) {
	all := s.store.AllConversations()
	owned := make([]domain.Conversation, 0, 64)
	for _, conversation := range all {
		if conversationOwnedBy(conversation, apiApp) {
			owned = append(owned, conversation)
		}
	}
	sort.Slice(owned, func(i, j int) bool { return owned[i].UpdatedAt.After(owned[j].UpdatedAt) })
	if len(owned) > maxListConversations {
		owned = owned[:maxListConversations]
	}
	writeOpenJSON(w, http.StatusOK, map[string]any{"conversations": owned})
}

// HandleConversation 查询本应用名下的单个会话。
// GET /open/v1/conversations/{id}
func (s *Service) HandleConversation(w http.ResponseWriter, r *http.Request) {
	applyOpenCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		writeOpenError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	apiApp, ok := s.requireApp(w, r)
	if !ok {
		return
	}
	conversation, ok := s.store.Conversation(r.PathValue("id"))
	if !ok || !conversationOwnedBy(conversation, apiApp) {
		writeOpenError(w, http.StatusNotFound, store.ErrNotFound.Error())
		return
	}
	writeOpenJSON(w, http.StatusOK, map[string]any{"conversation": conversation})
}

// HandleConversationMessages 拉取历史消息 / 代外部用户发送消息。
// GET  /open/v1/conversations/{id}/messages?limit=&before=
// POST /open/v1/conversations/{id}/messages
func (s *Service) HandleConversationMessages(w http.ResponseWriter, r *http.Request) {
	applyOpenCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	apiApp, ok := s.requireApp(w, r)
	if !ok {
		return
	}
	conversation, ok := s.store.Conversation(r.PathValue("id"))
	if !ok || !conversationOwnedBy(conversation, apiApp) {
		writeOpenError(w, http.StatusNotFound, store.ErrNotFound.Error())
		return
	}
	switch r.Method {
	case http.MethodGet:
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		page := s.store.PagedMessages(conversation.ID, limit, r.URL.Query().Get("before"))
		lang := ai.NormalizeLang(conversation.VisitorLang, ai.LangZh)
		page.Messages = app.VisitorFacingMessagesForLang(page.Messages, lang)
		writeOpenJSON(w, http.StatusOK, page)
	case http.MethodPost:
		s.sendVisitorMessage(w, r, conversation)
	default:
		writeOpenError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Service) sendVisitorMessage(w http.ResponseWriter, r *http.Request, conversation domain.Conversation) {
	var req struct {
		ClientMsgID string             `json:"client_msg_id"`
		Content     string             `json:"content"`
		MessageType domain.MessageType `json:"message_type"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	if err := decoder.Decode(&req); err != nil {
		writeOpenError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.MessageType == "" {
		req.MessageType = domain.MessageText
	}
	if !allowedOpenMessageTypes[req.MessageType] {
		writeOpenError(w, http.StatusBadRequest, "message_type must be text, emoji or image")
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeOpenError(w, http.StatusBadRequest, "content is required")
		return
	}
	if len([]rune(content)) > maxMessageContentRunes {
		writeOpenError(w, http.StatusBadRequest, "content too long (max 4000 chars)")
		return
	}
	result, err := s.app.AddVisitorMessage(conversation.ID, strings.TrimSpace(req.ClientMsgID), content, req.MessageType)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	lang := ai.NormalizeLang(result.Conversation.VisitorLang, ai.LangZh)
	generated := make([]domain.Message, 0, len(result.Generated))
	for _, msg := range result.Generated {
		generated = append(generated, app.VisitorFacingMessageForLang(msg, lang))
	}
	writeOpenJSON(w, http.StatusCreated, map[string]any{
		"message":      app.VisitorFacingMessageForLang(result.Input, lang),
		"generated":    generated,
		"conversation": result.Conversation,
	})
}

// HandleConversationClose 关闭本应用名下的会话。
// POST /open/v1/conversations/{id}/close
func (s *Service) HandleConversationClose(w http.ResponseWriter, r *http.Request) {
	applyOpenCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeOpenError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	apiApp, ok := s.requireApp(w, r)
	if !ok {
		return
	}
	conversationID := r.PathValue("id")
	conversation, ok := s.store.Conversation(conversationID)
	if !ok || !conversationOwnedBy(conversation, apiApp) {
		writeOpenError(w, http.StatusNotFound, store.ErrNotFound.Error())
		return
	}
	actor := domain.AuthSession{Kind: domain.AccountVisitor, AccountID: conversationID}
	updated, err := s.app.CloseConversation(actor, conversationID)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	writeOpenJSON(w, http.StatusOK, map[string]any{"conversation": updated})
}
