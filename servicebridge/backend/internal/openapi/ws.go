package openapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"

	"customer-service/backend/internal/domain"
	"customer-service/backend/internal/realtime"
)

var openWebSocketUpgrader = websocket.Upgrader{
	// 开放接口使用 API Key 鉴权，不依赖浏览器同源 Cookie，允许任意来源订阅。
	CheckOrigin: func(r *http.Request) bool { return true },
}

// HandleWebSocket 第三方平台实时订阅会话事件。
//
//	GET /open/v1/ws?conversation_id=<会话ID>
//	鉴权：Authorization: Bearer <api_key> / X-API-Key / query key
//
// 连接建立后按「访客」角色挂载到对应会话，客服回复、AI 回复、状态变化等
// 事件会实时推送；该连接为只读订阅，发送消息请调用
// POST /open/v1/conversations/{id}/messages。
func (s *Service) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	apiApp, err := s.authenticate(r)
	if err != nil {
		status := http.StatusUnauthorized
		if err == ErrDisabled {
			status = http.StatusServiceUnavailable
		}
		http.Error(w, err.Error(), status)
		return
	}
	// WebSocket 订阅同样受权限范围约束；缺范围时返回统一的 403 JSON 错误体并写审计。
	if !s.requireScope(w, r, apiApp, domain.APIScopeEventsSubscribe) {
		return
	}
	conversationID := strings.TrimSpace(r.URL.Query().Get("conversation_id"))
	if conversationID == "" {
		http.Error(w, "conversation_id is required", http.StatusBadRequest)
		return
	}
	conversation, ok := s.store.Conversation(conversationID)
	if !ok || !conversationOwnedBy(conversation, apiApp) {
		http.Error(w, "conversation not found", http.StatusNotFound)
		return
	}

	conn, err := openWebSocketUpgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Warn("open api websocket upgrade failed", "error", err, "app_id", apiApp.ID)
		return
	}
	s.logger.Info("open api websocket subscribed", "app_id", apiApp.ID, "conversation_id", conversationID)
	client := realtime.NewClient(realtime.RoleVisitor, conversationID, conversationID, conn, s.app.Hub(), s.rejectIncomingEvents, s.logger)
	client.Run()
}

// rejectIncomingEvents 开放接口 WebSocket 为只读订阅，拒绝所有上行事件。
func (s *Service) rejectIncomingEvents(client *realtime.Client, event realtime.IncomingEvent) {
	message := "open api websocket is subscribe-only; send messages via POST /open/v1/conversations/{id}/messages"
	if event.Event == "ping" {
		client.Send(realtime.Event{Event: "pong"})
		return
	}
	payload, _ := json.Marshal(map[string]string{"message": message})
	client.Send(realtime.Event{Event: "error", Data: json.RawMessage(payload)})
}
