package realtime

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type ClientRole string

const (
	RoleVisitor ClientRole = "visitor"
	RoleAgent   ClientRole = "agent"
	RoleAdmin   ClientRole = "admin"
)

type Event struct {
	Event string `json:"event"`
	Data  any    `json:"data,omitempty"`
}

type IncomingEvent struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data,omitempty"`
}

type Handler func(client *Client, event IncomingEvent)

type DeliveryTarget string

const (
	TargetVisitor DeliveryTarget = "visitor"
	TargetAgent   DeliveryTarget = "agent"
	TargetAdmin   DeliveryTarget = "admin"
	TargetAdmins  DeliveryTarget = "admins"
)

type WireEvent struct {
	NodeID string         `json:"node_id"`
	Target DeliveryTarget `json:"target"`
	Key    string         `json:"key,omitempty"`
	Event  Event          `json:"event"`
}

type Publisher func(WireEvent)

type Client struct {
	Role           ClientRole
	AccountID      string
	ConversationID string
	// APIAppID 用于开放 API 的 WebSocket，管理员重置/停用应用时可定向断开。
	APIAppID string
	// ExpiresAt 是握手时解析出的会话/令牌过期时间；0 表示不按过期时间断开。
	ExpiresAt time.Time

	conn    *websocket.Conn
	hub     *Hub
	send    chan []byte
	sendMu  sync.RWMutex
	closed  bool
	handler Handler
	logger  *slog.Logger
}

type Hub struct {
	mu sync.RWMutex

	visitors map[string]map[*Client]struct{}
	agents   map[string]map[*Client]struct{}
	admins   map[*Client]struct{}

	nodeID        string
	publisher     Publisher
	maxClients    int
	maxPerAccount int
	totalClients  int
}

type HubStats struct {
	Visitors int
	Agents   int
	Admins   int
}

func NewHub() *Hub {
	return NewHubWithNodeID("")
}

func NewHubWithNodeID(nodeID string) *Hub {
	return &Hub{
		visitors:      map[string]map[*Client]struct{}{},
		agents:        map[string]map[*Client]struct{}{},
		admins:        map[*Client]struct{}{},
		nodeID:        nodeID,
		maxClients:    5000,
		maxPerAccount: 10,
	}
}

// SetLimits 设置全局连接上限与单账号（访客为单会话）连接上限。
func (h *Hub) SetLimits(maxClients, maxPerAccount int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if maxClients > 0 {
		h.maxClients = maxClients
	}
	if maxPerAccount > 0 {
		h.maxPerAccount = maxPerAccount
	}
}

func (h *Hub) accountCountLocked(client *Client) int {
	switch client.Role {
	case RoleVisitor:
		return len(h.visitors[client.ConversationID])
	case RoleAgent:
		return len(h.agents[client.AccountID])
	case RoleAdmin:
		count := 0
		for existing := range h.admins {
			if existing.AccountID == client.AccountID {
				count++
			}
		}
		return count
	default:
		return 0
	}
}

func (h *Hub) SetPublisher(publisher Publisher) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.publisher = publisher
}

func (h *Hub) Stats() HubStats {
	h.mu.RLock()
	defer h.mu.RUnlock()

	stats := HubStats{Admins: len(h.admins)}
	for _, clients := range h.visitors {
		stats.Visitors += len(clients)
	}
	for _, clients := range h.agents {
		stats.Agents += len(clients)
	}
	return stats
}

func NewClient(role ClientRole, accountID, conversationID string, conn *websocket.Conn, hub *Hub, handler Handler, logger *slog.Logger) *Client {
	return &Client{
		Role:           role,
		AccountID:      accountID,
		ConversationID: conversationID,
		conn:           conn,
		hub:            hub,
		send:           make(chan []byte, 64),
		handler:        handler,
		logger:         logger,
	}
}

func (h *Hub) Register(client *Client) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.maxClients > 0 && h.totalClients >= h.maxClients {
		return false
	}
	if h.maxPerAccount > 0 && h.accountCountLocked(client) >= h.maxPerAccount {
		return false
	}
	h.totalClients++
	switch client.Role {
	case RoleVisitor:
		if h.visitors[client.ConversationID] == nil {
			h.visitors[client.ConversationID] = map[*Client]struct{}{}
		}
		h.visitors[client.ConversationID][client] = struct{}{}
	case RoleAgent:
		if h.agents[client.AccountID] == nil {
			h.agents[client.AccountID] = map[*Client]struct{}{}
		}
		h.agents[client.AccountID][client] = struct{}{}
	case RoleAdmin:
		h.admins[client] = struct{}{}
	}
	return true
}

func (h *Hub) Unregister(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	switch client.Role {
	case RoleVisitor:
		delete(h.visitors[client.ConversationID], client)
		if len(h.visitors[client.ConversationID]) == 0 {
			delete(h.visitors, client.ConversationID)
		}
	case RoleAgent:
		delete(h.agents[client.AccountID], client)
		if len(h.agents[client.AccountID]) == 0 {
			delete(h.agents, client.AccountID)
		}
	case RoleAdmin:
		delete(h.admins, client)
	}
	if h.totalClients > 0 {
		h.totalClients--
	}
	client.closeSend()
}

// DisconnectAPIApp 在 API Key 被重置/停用/删除后断开该应用的订阅连接。
func (h *Hub) DisconnectAPIApp(appID string) {
	if appID == "" {
		return
	}
	h.mu.Lock()
	var targets []*Client
	for _, clients := range h.visitors {
		for client := range clients {
			if client.APIAppID == appID {
				targets = append(targets, client)
			}
		}
	}
	h.mu.Unlock()
	for _, client := range targets {
		client.Send(Event{Event: "session.revoked", Data: map[string]string{"reason": "api_key_reset"}})
		_ = client.conn.Close()
	}
}

func (h *Hub) DisconnectConversation(conversationID string) {
	h.mu.Lock()
	var targets []*Client
	for client := range h.visitors[conversationID] {
		targets = append(targets, client)
	}
	h.mu.Unlock()
	for _, client := range targets {
		client.Send(Event{Event: "session.revoked", Data: map[string]string{"reason": "conversation_closed"}})
		_ = client.conn.Close()
	}
}

func (h *Hub) SendToVisitor(conversationID string, event Event) {
	h.sendToVisitorLocal(conversationID, event)
	h.publish(WireEvent{NodeID: h.nodeID, Target: TargetVisitor, Key: conversationID, Event: event})
}

func (h *Hub) SendToAgent(agentID string, event Event) {
	h.sendToAgentLocal(agentID, event)
	h.publish(WireEvent{NodeID: h.nodeID, Target: TargetAgent, Key: agentID, Event: event})
}

func (h *Hub) DisconnectAgent(agentID string, reason string) {
	event := sessionRevokedEvent(reason)
	h.disconnectAgentLocal(agentID, event)
	h.publish(WireEvent{NodeID: h.nodeID, Target: TargetAgent, Key: agentID, Event: event})
}

func (h *Hub) DisconnectAdmin(adminID string, reason string) {
	event := sessionRevokedEvent(reason)
	h.disconnectAdminLocal(adminID, event)
	h.publish(WireEvent{NodeID: h.nodeID, Target: TargetAdmin, Key: adminID, Event: event})
}

func (h *Hub) BroadcastAdmins(event Event) {
	h.broadcastAdminsLocal(event)
	h.publish(WireEvent{NodeID: h.nodeID, Target: TargetAdmins, Event: event})
}

func (h *Hub) Deliver(wire WireEvent) {
	if wire.NodeID != "" && wire.NodeID == h.nodeID {
		return
	}
	switch wire.Target {
	case TargetVisitor:
		h.sendToVisitorLocal(wire.Key, wire.Event)
	case TargetAgent:
		if wire.Event.Event == "session.revoked" {
			h.disconnectAgentLocal(wire.Key, wire.Event)
			return
		}
		h.sendToAgentLocal(wire.Key, wire.Event)
	case TargetAdmin:
		if wire.Event.Event == "session.revoked" {
			h.disconnectAdminLocal(wire.Key, wire.Event)
			return
		}
	case TargetAdmins:
		h.broadcastAdminsLocal(wire.Event)
	}
}

func (h *Hub) sendToVisitorLocal(conversationID string, event Event) {
	h.mu.RLock()
	targets := make([]*Client, 0, len(h.visitors[conversationID]))
	for client := range h.visitors[conversationID] {
		targets = append(targets, client)
	}
	h.mu.RUnlock()

	for _, client := range targets {
		client.Send(event)
	}
}

func (h *Hub) sendToAgentLocal(agentID string, event Event) {
	h.mu.RLock()
	targets := make([]*Client, 0, len(h.agents[agentID]))
	for client := range h.agents[agentID] {
		targets = append(targets, client)
	}
	h.mu.RUnlock()

	for _, client := range targets {
		client.Send(event)
	}
}

func (h *Hub) disconnectAgentLocal(agentID string, event Event) {
	h.mu.RLock()
	targets := make([]*Client, 0, len(h.agents[agentID]))
	for client := range h.agents[agentID] {
		targets = append(targets, client)
	}
	h.mu.RUnlock()

	for _, client := range targets {
		client.Disconnect(event)
	}
}

func (h *Hub) broadcastAdminsLocal(event Event) {
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.admins))
	for client := range h.admins {
		clients = append(clients, client)
	}
	h.mu.RUnlock()

	for _, client := range clients {
		client.Send(event)
	}
}

func (h *Hub) disconnectAdminLocal(adminID string, event Event) {
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.admins))
	for client := range h.admins {
		if client.AccountID == adminID {
			clients = append(clients, client)
		}
	}
	h.mu.RUnlock()

	for _, client := range clients {
		client.Disconnect(event)
	}
}

func (h *Hub) publish(event WireEvent) {
	h.mu.RLock()
	publisher := h.publisher
	h.mu.RUnlock()
	if publisher != nil {
		publisher(event)
	}
}

func sessionRevokedEvent(reason string) Event {
	return Event{Event: "session.revoked", Data: map[string]string{"reason": reason}}
}

func (c *Client) Send(event Event) {
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	c.sendMu.RLock()
	defer c.sendMu.RUnlock()
	if c.closed {
		return
	}
	select {
	case c.send <- payload:
	default:
		c.logger.Warn("websocket send buffer full", "role", c.Role, "account_id", c.AccountID, "conversation_id", c.ConversationID)
	}
}

func (c *Client) Disconnect(event Event) {
	c.Send(event)
	c.closeSend()
}

func (c *Client) closeSend() {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	close(c.send)
}

func (c *Client) Run() {
	if !c.hub.Register(c) {
		if c.conn != nil {
			_ = c.conn.WriteJSON(Event{Event: "error", Data: map[string]string{"message": "too many connections"}})
			_ = c.conn.Close()
		}
		return
	}
	defer func() {
		c.hub.Unregister(c)
		if c.conn != nil {
			_ = c.conn.Close()
		}
	}()
	if c.conn == nil {
		return
	}
	// 会话令牌过期或连接超过最大生命周期时主动断开，客户端可用新令牌重连。
	lifetime := 24 * time.Hour
	if !c.ExpiresAt.IsZero() {
		if remaining := time.Until(c.ExpiresAt); remaining > 0 && remaining < lifetime {
			lifetime = remaining
		}
	}
	closeTimer := time.AfterFunc(lifetime, func() {
		c.Send(Event{Event: "session.revoked", Data: map[string]string{"reason": "session_expired"}})
		_ = c.conn.Close()
	})
	defer closeTimer.Stop()

	go c.writeLoop()
	c.Send(Event{Event: "connected", Data: map[string]any{
		"role":            c.Role,
		"account_id":      c.AccountID,
		"conversation_id": c.ConversationID,
	}})

	c.conn.SetReadLimit(64 * 1024)
	_ = c.conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	})

	for {
		var event IncomingEvent
		if err := c.conn.ReadJSON(&event); err != nil {
			c.logger.Info("websocket closed", "role", c.Role, "account_id", c.AccountID, "error", err)
			return
		}
		if event.Event == "ping" {
			c.Send(Event{Event: "pong", Data: map[string]any{"time": time.Now().UTC().Format(time.RFC3339)}})
			continue
		}
		if c.handler != nil {
			c.handler(c, event)
		}
	}
}

func (c *Client) writeLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case payload, ok := <-c.send:
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(5*time.Second)); err != nil {
				return
			}
		}
	}
}
