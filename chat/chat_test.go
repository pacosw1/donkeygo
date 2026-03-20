package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pacosw1/donkeygo/middleware"
	"github.com/pacosw1/donkeygo/push"
)

// ── Mock DB ─────────────────────────────────────────────────────────────────

type mockChatDB struct {
	messages     []*ChatMessage
	threads      []*ChatThread
	unread       int
	deviceTokens []string
	err          error
}

func (m *mockChatDB) GetChatMessages(userID string, limit, offset int) ([]*ChatMessage, error) {
	end := offset + limit
	if end > len(m.messages) {
		end = len(m.messages)
	}
	if offset >= len(m.messages) {
		return nil, m.err
	}
	return m.messages[offset:end], m.err
}
func (m *mockChatDB) GetChatMessagesSince(userID string, sinceID int) ([]*ChatMessage, error) {
	var result []*ChatMessage
	for _, msg := range m.messages {
		if msg.ID > sinceID {
			result = append(result, msg)
		}
	}
	return result, m.err
}
func (m *mockChatDB) SendChatMessage(userID, sender, message, messageType string) (*ChatMessage, error) {
	msg := &ChatMessage{
		ID: len(m.messages) + 1, UserID: userID, Sender: sender,
		Message: message, MessageType: messageType, CreatedAt: time.Now(),
	}
	m.messages = append(m.messages, msg)
	return msg, m.err
}
func (m *mockChatDB) MarkChatRead(userID, reader string) error { return m.err }
func (m *mockChatDB) GetUnreadCount(userID string) (int, error) { return m.unread, m.err }
func (m *mockChatDB) AdminListChatThreads(limit int) ([]*ChatThread, error) {
	if limit > len(m.threads) {
		return m.threads, m.err
	}
	return m.threads[:limit], m.err
}
func (m *mockChatDB) EnabledDeviceTokens(userID string) ([]string, error) {
	return m.deviceTokens, m.err
}

func authReq(method, path, body string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	ctx := context.WithValue(r.Context(), middleware.CtxUserID, "user-1")
	return r.WithContext(ctx)
}

// ── HandleGetChat ───────────────────────────────────────────────────────────

func TestHandleGetChat_Paginated(t *testing.T) {
	db := &mockChatDB{
		messages: []*ChatMessage{
			{ID: 1, UserID: "user-1", Sender: "user", Message: "hello", MessageType: "text", CreatedAt: time.Now()},
			{ID: 2, UserID: "user-1", Sender: "admin", Message: "hi", MessageType: "text", CreatedAt: time.Now()},
		},
	}
	svc := New(db, &push.NoopProvider{}, Config{})

	req := authReq("GET", "/api/v1/chat?limit=10", "")
	w := httptest.NewRecorder()
	svc.HandleGetChat(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	msgs := body["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
}

func TestHandleGetChat_SinceID(t *testing.T) {
	db := &mockChatDB{
		messages: []*ChatMessage{
			{ID: 1, UserID: "user-1", Sender: "user", Message: "old", MessageType: "text", CreatedAt: time.Now()},
			{ID: 5, UserID: "user-1", Sender: "admin", Message: "new", MessageType: "text", CreatedAt: time.Now()},
		},
	}
	svc := New(db, &push.NoopProvider{}, Config{})

	req := authReq("GET", "/api/v1/chat?since_id=3", "")
	w := httptest.NewRecorder()
	svc.HandleGetChat(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	msgs := body["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message since_id=3, got %d", len(msgs))
	}
}

func TestHandleGetChat_InvalidSinceID(t *testing.T) {
	svc := New(&mockChatDB{}, &push.NoopProvider{}, Config{})

	req := authReq("GET", "/api/v1/chat?since_id=abc", "")
	w := httptest.NewRecorder()
	svc.HandleGetChat(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ── HandleSendChat ──────────────────────────────────────────────────────────

func TestHandleSendChat_Valid(t *testing.T) {
	db := &mockChatDB{}
	svc := New(db, &push.NoopProvider{}, Config{})

	body := `{"message":"Hello developer!"}`
	req := authReq("POST", "/api/v1/chat", body)
	w := httptest.NewRecorder()
	svc.HandleSendChat(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if len(db.messages) != 1 {
		t.Fatalf("expected 1 message stored, got %d", len(db.messages))
	}
	if db.messages[0].Sender != "user" {
		t.Fatalf("expected sender=user, got %s", db.messages[0].Sender)
	}
}

func TestHandleSendChat_EmptyMessage(t *testing.T) {
	svc := New(&mockChatDB{}, &push.NoopProvider{}, Config{})

	body := `{"message":""}`
	req := authReq("POST", "/api/v1/chat", body)
	w := httptest.NewRecorder()
	svc.HandleSendChat(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleSendChat_TooLong(t *testing.T) {
	svc := New(&mockChatDB{}, &push.NoopProvider{}, Config{})

	longMsg := strings.Repeat("a", 5001)
	body := `{"message":"` + longMsg + `"}`
	req := authReq("POST", "/api/v1/chat", body)
	w := httptest.NewRecorder()
	svc.HandleSendChat(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for long message, got %d", w.Code)
	}
}

func TestHandleSendChat_DefaultMessageType(t *testing.T) {
	db := &mockChatDB{}
	svc := New(db, &push.NoopProvider{}, Config{})

	body := `{"message":"test"}`
	req := authReq("POST", "/api/v1/chat", body)
	w := httptest.NewRecorder()
	svc.HandleSendChat(w, req)

	if db.messages[0].MessageType != "text" {
		t.Fatalf("expected default message_type=text, got %s", db.messages[0].MessageType)
	}
}

// ── HandleUnreadCount ───────────────────────────────────────────────────────

func TestHandleUnreadCount(t *testing.T) {
	db := &mockChatDB{unread: 5}
	svc := New(db, &push.NoopProvider{}, Config{})

	req := authReq("GET", "/api/v1/chat/unread", "")
	w := httptest.NewRecorder()
	svc.HandleUnreadCount(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]int
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["count"] != 5 {
		t.Fatalf("expected 5 unread, got %d", body["count"])
	}
}

// ── Admin Handlers ──────────────────────────────────────────────────────────

func TestHandleAdminListChats(t *testing.T) {
	db := &mockChatDB{
		threads: []*ChatThread{
			{UserID: "u1", UserName: "Alice", LastMessage: "hi", UnreadCount: 2},
			{UserID: "u2", UserName: "Bob", LastMessage: "hello", UnreadCount: 0},
		},
	}
	svc := New(db, &push.NoopProvider{}, Config{})

	req := httptest.NewRequest("GET", "/admin/api/chat", nil)
	w := httptest.NewRecorder()
	svc.HandleAdminListChats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if int(body["count"].(float64)) != 2 {
		t.Fatalf("expected 2 threads, got %v", body["count"])
	}
}

func TestHandleAdminReplyChat_Valid(t *testing.T) {
	db := &mockChatDB{}
	svc := New(db, &push.NoopProvider{}, Config{})

	body := `{"message":"Thanks for reaching out!"}`
	req := httptest.NewRequest("POST", "/admin/api/chat/user-1", strings.NewReader(body))
	req.SetPathValue("user_id", "user-1")
	w := httptest.NewRecorder()
	svc.HandleAdminReplyChat(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if len(db.messages) != 1 || db.messages[0].Sender != "admin" {
		t.Fatal("expected admin message stored")
	}
}

func TestHandleAdminReplyChat_MissingUserID(t *testing.T) {
	svc := New(&mockChatDB{}, &push.NoopProvider{}, Config{})

	body := `{"message":"test"}`
	req := httptest.NewRequest("POST", "/admin/api/chat/", strings.NewReader(body))
	req.SetPathValue("user_id", "")
	w := httptest.NewRecorder()
	svc.HandleAdminReplyChat(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleAdminReplyChat_EmptyMessage(t *testing.T) {
	svc := New(&mockChatDB{}, &push.NoopProvider{}, Config{})

	body := `{"message":""}`
	req := httptest.NewRequest("POST", "/admin/api/chat/user-1", strings.NewReader(body))
	req.SetPathValue("user_id", "user-1")
	w := httptest.NewRecorder()
	svc.HandleAdminReplyChat(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ── Hub ─────────────────────────────────────────────────────────────────────

func TestHub_RegisterUnregister(t *testing.T) {
	h := newHub()

	wsc := &wsConn{userID: "user-1", role: "user", send: make(chan []byte, 1), done: make(chan struct{})}
	h.register(wsc)

	if !h.HasActiveConnection("user:user-1") {
		t.Fatal("expected active connection")
	}

	h.unregister(wsc)

	if h.HasActiveConnection("user:user-1") {
		t.Fatal("expected no active connection after unregister")
	}
}

func TestHub_BroadcastToUser(t *testing.T) {
	h := newHub()

	wsc := &wsConn{userID: "user-1", role: "user", send: make(chan []byte, 1), done: make(chan struct{})}
	h.register(wsc)
	defer h.unregister(wsc)

	h.broadcastToUser("user-1", WSEvent{Type: "test"})

	select {
	case msg := <-wsc.send:
		if len(msg) == 0 {
			t.Fatal("expected non-empty message")
		}
	default:
		t.Fatal("expected message in send channel")
	}
}

func TestHub_BroadcastToAdmins(t *testing.T) {
	h := newHub()

	wsc := &wsConn{userID: "admin-1", role: "admin", send: make(chan []byte, 1), done: make(chan struct{})}
	h.register(wsc)
	defer h.unregister(wsc)

	h.broadcastToAdmins(WSEvent{Type: "test"})

	select {
	case msg := <-wsc.send:
		if len(msg) == 0 {
			t.Fatal("expected non-empty message")
		}
	default:
		t.Fatal("expected message in send channel")
	}
}

func TestMigrations(t *testing.T) {
	m := Migrations()
	if len(m) == 0 {
		t.Fatal("expected at least one migration")
	}
}
