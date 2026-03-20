package chat

import (
	"testing"
)

func TestMigrations(t *testing.T) {
	m := Migrations()
	if len(m) == 0 {
		t.Fatal("expected at least one migration")
	}
}

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
