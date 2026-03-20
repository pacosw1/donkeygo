// Package chat provides WebSocket-based real-time user↔developer chat.
package chat

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"nhooyr.io/websocket"

	"github.com/pacosw1/donkeygo/httputil"
	"github.com/pacosw1/donkeygo/middleware"
	"github.com/pacosw1/donkeygo/push"
)

// ChatDB is the database interface required by the chat package.
type ChatDB interface {
	GetChatMessages(userID string, limit, offset int) ([]*ChatMessage, error)
	GetChatMessagesSince(userID string, sinceID int) ([]*ChatMessage, error)
	SendChatMessage(userID, sender, message, messageType string) (*ChatMessage, error)
	MarkChatRead(userID, reader string) error
	GetUnreadCount(userID string) (int, error)
	AdminListChatThreads(limit int) ([]*ChatThread, error)
	// For push: get enabled device tokens
	EnabledDeviceTokens(userID string) ([]string, error)
}

// ChatMessage represents a chat message.
type ChatMessage struct {
	ID          int       `json:"id"`
	UserID      string    `json:"user_id"`
	Sender      string    `json:"sender"`
	Message     string    `json:"message"`
	MessageType string    `json:"message_type"`
	ReadAt      *string   `json:"read_at,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// ChatThread represents a chat thread summary for admin view.
type ChatThread struct {
	UserID        string `json:"user_id"`
	UserName      string `json:"user_name"`
	UserEmail     string `json:"user_email"`
	LastMessage   string `json:"last_message"`
	LastSender    string `json:"last_sender"`
	UnreadCount   int    `json:"unread_count"`
	LastMessageAt string `json:"last_message_at"`
}

// Config configures the chat service.
type Config struct {
	// ParseToken validates a session token and returns user ID.
	ParseToken func(token string) (string, error)
	// AdminAuth validates admin access. Returns true if authenticated.
	AdminAuth func(r *http.Request) bool
}

// Service provides chat handlers and WebSocket hub.
type Service struct {
	db   ChatDB
	push push.Provider
	hub  *Hub
	cfg  Config
}

// New creates a chat service.
func New(db ChatDB, pushProvider push.Provider, cfg Config) *Service {
	return &Service{
		db:   db,
		push: pushProvider,
		hub:  newHub(),
		cfg:  cfg,
	}
}

// Migrations returns the SQL migrations needed by the chat package.
func Migrations() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS chat_messages (
			id           SERIAL PRIMARY KEY,
			user_id      TEXT NOT NULL,
			sender       TEXT NOT NULL DEFAULT 'user',
			message      TEXT NOT NULL,
			message_type TEXT NOT NULL DEFAULT 'text',
			read_at      TIMESTAMPTZ,
			created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_messages_user ON chat_messages(user_id, created_at)`,
	}
}

// ── WebSocket Types ─────────────────────────────────────────────────────────

// WSEvent is a WebSocket message envelope.
type WSEvent struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type wsNewMessage struct {
	ID          int    `json:"id"`
	UserID      string `json:"user_id"`
	Sender      string `json:"sender"`
	Message     string `json:"message"`
	MessageType string `json:"message_type"`
	CreatedAt   string `json:"created_at"`
}

type wsTyping struct {
	UserID string `json:"user_id"`
	Sender string `json:"sender"`
}

// ── Hub ─────────────────────────────────────────────────────────────────────

type wsConn struct {
	conn   *websocket.Conn
	userID string
	role   string
	send   chan []byte
	done   chan struct{}
}

// Hub manages WebSocket connections.
type Hub struct {
	mu          sync.RWMutex
	connections map[string][]*wsConn
}

func newHub() *Hub {
	return &Hub{connections: make(map[string][]*wsConn)}
}

func (h *Hub) connKey(role, userID string) string {
	return role + ":" + userID
}

func (h *Hub) register(c *wsConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := h.connKey(c.role, c.userID)
	h.connections[key] = append(h.connections[key], c)
}

func (h *Hub) unregister(c *wsConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := h.connKey(c.role, c.userID)
	conns := h.connections[key]
	for i, conn := range conns {
		if conn == c {
			h.connections[key] = append(conns[:i], conns[i+1:]...)
			break
		}
	}
	if len(h.connections[key]) == 0 {
		delete(h.connections, key)
	}
}

// HasActiveConnection checks if a key has any active WebSocket connections.
func (h *Hub) HasActiveConnection(key string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.connections[key]) > 0
}

func (h *Hub) broadcastToUser(userID string, event WSEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.connections["user:"+userID] {
		select {
		case c.send <- data:
		default:
		}
	}
}

func (h *Hub) broadcastToAdmins(event WSEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for key, conns := range h.connections {
		if strings.HasPrefix(key, "admin:") {
			for _, c := range conns {
				select {
				case c.send <- data:
				default:
				}
			}
		}
	}
}

// ── User Handlers ───────────────────────────────────────────────────────────

// HandleGetChat handles GET /api/v1/chat.
func (s *Service) HandleGetChat(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)

	if v := r.URL.Query().Get("since_id"); v != "" {
		sinceID, err := strconv.Atoi(v)
		if err != nil {
			httputil.WriteError(w, http.StatusBadRequest, "invalid since_id")
			return
		}
		msgs, err := s.db.GetChatMessagesSince(userID, sinceID)
		if err != nil {
			httputil.WriteError(w, http.StatusInternalServerError, "failed to get chat messages")
			return
		}
		_ = s.db.MarkChatRead(userID, "user")
		httputil.WriteJSON(w, http.StatusOK, map[string]any{"messages": msgs, "has_more": false})
		return
	}

	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	msgs, err := s.db.GetChatMessages(userID, limit+1, offset)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to get chat messages")
		return
	}

	_ = s.db.MarkChatRead(userID, "user")

	hasMore := len(msgs) > limit
	if hasMore {
		msgs = msgs[:limit]
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]any{"messages": msgs, "has_more": hasMore})
}

// HandleSendChat handles POST /api/v1/chat.
func (s *Service) HandleSendChat(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)

	var req struct {
		Message     string `json:"message"`
		MessageType string `json:"message_type"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Message == "" {
		httputil.WriteError(w, http.StatusBadRequest, "message is required")
		return
	}
	if len(req.Message) > 5000 {
		httputil.WriteError(w, http.StatusBadRequest, "message too long (max 5000 chars)")
		return
	}
	if req.MessageType == "" {
		req.MessageType = "text"
	}

	msg, err := s.db.SendChatMessage(userID, "user", req.Message, req.MessageType)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to send message")
		return
	}

	s.broadcastChatMessage(msg)
	httputil.WriteJSON(w, http.StatusCreated, map[string]string{"status": "sent"})
}

// HandleUnreadCount handles GET /api/v1/chat/unread.
func (s *Service) HandleUnreadCount(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)

	count, err := s.db.GetUnreadCount(userID)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to get unread count")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]int{"count": count})
}

// ── Admin Handlers ──────────────────────────────────────────────────────────

// HandleAdminListChats handles GET /admin/api/chat.
func (s *Service) HandleAdminListChats(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	threads, err := s.db.AdminListChatThreads(limit)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to list chat threads")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]any{"threads": threads, "count": len(threads)})
}

// HandleAdminGetChat handles GET /admin/api/chat/{user_id}.
func (s *Service) HandleAdminGetChat(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("user_id")
	if userID == "" {
		httputil.WriteError(w, http.StatusBadRequest, "missing user_id")
		return
	}

	limit := 200
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	msgs, err := s.db.GetChatMessages(userID, limit, offset)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to get chat messages")
		return
	}

	_ = s.db.MarkChatRead(userID, "admin")
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

// HandleAdminReplyChat handles POST /admin/api/chat/{user_id}.
func (s *Service) HandleAdminReplyChat(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("user_id")
	if userID == "" {
		httputil.WriteError(w, http.StatusBadRequest, "missing user_id")
		return
	}

	var req struct {
		Message     string `json:"message"`
		MessageType string `json:"message_type"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Message == "" {
		httputil.WriteError(w, http.StatusBadRequest, "message is required")
		return
	}
	if req.MessageType == "" {
		req.MessageType = "text"
	}

	msg, err := s.db.SendChatMessage(userID, "admin", req.Message, req.MessageType)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to send reply")
		return
	}

	s.broadcastChatMessage(msg)

	// Send push if user has no active WebSocket
	hasWS := s.hub.HasActiveConnection("user:" + userID)
	if !hasWS {
		go s.sendChatPush(userID, req.Message)
	}

	httputil.WriteJSON(w, http.StatusCreated, map[string]string{"status": "sent"})
}

// ── WebSocket Handlers ──────────────────────────────────────────────────────

// HandleUserWS handles GET /api/v1/chat/ws?token=...
func (s *Service) HandleUserWS(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	if s.cfg.ParseToken == nil {
		http.Error(w, "auth not configured", http.StatusInternalServerError)
		return
	}

	userID, err := s.cfg.ParseToken(token)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if err != nil {
		log.Printf("[ws] accept error: %v", err)
		return
	}

	wsc := &wsConn{
		conn:   conn,
		userID: userID,
		role:   "user",
		send:   make(chan []byte, 64),
		done:   make(chan struct{}),
	}

	s.hub.register(wsc)
	go s.wsWritePump(wsc)
	s.wsReadPump(wsc)
	s.hub.unregister(wsc)
}

// HandleAdminWS handles GET /admin/api/chat/ws.
func (s *Service) HandleAdminWS(w http.ResponseWriter, r *http.Request) {
	if s.cfg.AdminAuth == nil || !s.cfg.AdminAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if err != nil {
		log.Printf("[ws] admin accept error: %v", err)
		return
	}

	wsc := &wsConn{
		conn:   conn,
		userID: "admin",
		role:   "admin",
		send:   make(chan []byte, 64),
		done:   make(chan struct{}),
	}

	s.hub.register(wsc)
	go s.wsWritePump(wsc)
	s.wsReadPump(wsc)
	s.hub.unregister(wsc)
}

// ── Internal ────────────────────────────────────────────────────────────────

func (s *Service) broadcastChatMessage(msg *ChatMessage) {
	wsMsg := wsNewMessage{
		ID:          msg.ID,
		UserID:      msg.UserID,
		Sender:      msg.Sender,
		Message:     msg.Message,
		MessageType: msg.MessageType,
		CreatedAt:   msg.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
	payload, _ := json.Marshal(wsMsg)
	event := WSEvent{Type: "new_message", Payload: payload}

	if msg.Sender == "user" {
		s.hub.broadcastToAdmins(event)
	} else {
		s.hub.broadcastToUser(msg.UserID, event)
	}
}

func (s *Service) sendChatPush(userID, message string) {
	tokens, err := s.db.EnabledDeviceTokens(userID)
	if err != nil || len(tokens) == 0 {
		return
	}

	title := "New message from Developer"
	body := message
	if len(body) > 100 {
		body = body[:97] + "..."
	}

	data := map[string]string{
		"type":    "chat_message",
		"user_id": userID,
	}

	for _, token := range tokens {
		if err := s.push.SendWithData(token, title, body, data); err != nil {
			log.Printf("[chat-push] failed: %v", err)
		}
	}
}

func (s *Service) wsReadPump(wsc *wsConn) {
	defer close(wsc.done)
	for {
		_, data, err := wsc.conn.Read(context.Background())
		if err != nil {
			return
		}

		var event WSEvent
		if err := json.Unmarshal(data, &event); err != nil {
			continue
		}

		switch event.Type {
		case "typing":
			var typing wsTyping
			if err := json.Unmarshal(event.Payload, &typing); err != nil {
				continue
			}
			if wsc.role == "user" {
				typing.UserID = wsc.userID
				typing.Sender = "user"
				payload, _ := json.Marshal(typing)
				s.hub.broadcastToAdmins(WSEvent{Type: "typing", Payload: payload})
			} else {
				typing.Sender = "admin"
				payload, _ := json.Marshal(typing)
				s.hub.broadcastToUser(typing.UserID, WSEvent{Type: "typing", Payload: payload})
			}
		}
	}
}

func (s *Service) wsWritePump(wsc *wsConn) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-wsc.send:
			if !ok {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := wsc.conn.Write(ctx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				return
			}

		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := wsc.conn.Ping(ctx)
			cancel()
			if err != nil {
				return
			}

		case <-wsc.done:
			return
		}
	}
}
