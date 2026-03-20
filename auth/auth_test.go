package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pacosw1/donkeygo/middleware"
)

// ── Mock DB ─────────────────────────────────────────────────────────────────

type mockAuthDB struct {
	user *User
	err  error
}

func (m *mockAuthDB) UpsertUserByAppleSub(id, appleSub, email, name string) (*User, error) {
	return m.user, m.err
}

func (m *mockAuthDB) UserByID(id string) (*User, error) {
	if m.user != nil && m.user.ID == id {
		return m.user, nil
	}
	return nil, m.err
}

// ── Session Token Tests ─────────────────────────────────────────────────────

func TestCreateAndParseSessionToken(t *testing.T) {
	svc := New(Config{JWTSecret: "test-secret"}, nil)

	token, err := svc.CreateSessionToken("user-123")
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	userID, err := svc.ParseSessionToken(token)
	if err != nil {
		t.Fatalf("failed to parse token: %v", err)
	}
	if userID != "user-123" {
		t.Fatalf("expected user-123, got %s", userID)
	}
}

func TestParseSessionToken_Invalid(t *testing.T) {
	svc := New(Config{JWTSecret: "test-secret"}, nil)

	_, err := svc.ParseSessionToken("invalid-token")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestParseSessionToken_WrongSecret(t *testing.T) {
	svc1 := New(Config{JWTSecret: "secret-1"}, nil)
	svc2 := New(Config{JWTSecret: "secret-2"}, nil)

	token, _ := svc1.CreateSessionToken("user-123")
	_, err := svc2.ParseSessionToken(token)
	if err == nil {
		t.Fatal("expected error with wrong secret")
	}
}

func TestDefaultSessionExpiry(t *testing.T) {
	svc := New(Config{JWTSecret: "test-secret"}, nil)
	if svc.cfg.SessionExpiry != 7*24*time.Hour {
		t.Fatalf("expected 7-day default expiry, got %v", svc.cfg.SessionExpiry)
	}
}

func TestCustomSessionExpiry(t *testing.T) {
	svc := New(Config{JWTSecret: "test-secret", SessionExpiry: 1 * time.Hour}, nil)
	if svc.cfg.SessionExpiry != 1*time.Hour {
		t.Fatalf("expected 1h expiry, got %v", svc.cfg.SessionExpiry)
	}
}

func TestCreateSessionToken_UniqueIDs(t *testing.T) {
	svc := New(Config{JWTSecret: "test-secret"}, nil)

	t1, _ := svc.CreateSessionToken("user-1")
	t2, _ := svc.CreateSessionToken("user-1")
	if t1 == t2 {
		t.Fatal("expected different tokens for same user (unique JTI)")
	}
}

// ── Handler Tests ───────────────────────────────────────────────────────────

func TestHandleMe(t *testing.T) {
	db := &mockAuthDB{user: &User{ID: "user-123", Email: "test@example.com", Name: "Test"}}
	svc := New(Config{JWTSecret: "test-secret"}, db)

	req := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	auth := middleware.RequireAuth(middleware.AuthConfig{ParseToken: svc.ParseSessionToken})
	token, _ := svc.CreateSessionToken("user-123")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	auth(svc.HandleMe)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["email"] != "test@example.com" {
		t.Fatalf("expected test@example.com, got %v", body["email"])
	}
}

func TestHandleLogout(t *testing.T) {
	svc := New(Config{JWTSecret: "test-secret"}, nil)

	req := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
	w := httptest.NewRecorder()
	svc.HandleLogout(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "session" && c.MaxAge == -1 {
			found = true
		}
	}
	if !found {
		t.Fatal("expected session cookie to be cleared")
	}
}

func TestHandleAppleAuth_MissingToken(t *testing.T) {
	svc := New(Config{JWTSecret: "test-secret"}, nil)

	body := `{"identity_token":""}`
	req := httptest.NewRequest("POST", "/api/v1/auth/apple", strings.NewReader(body))
	w := httptest.NewRecorder()
	svc.HandleAppleAuth(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing token, got %d", w.Code)
	}
}

func TestHandleAppleAuth_InvalidJSON(t *testing.T) {
	svc := New(Config{JWTSecret: "test-secret"}, nil)

	req := httptest.NewRequest("POST", "/api/v1/auth/apple", strings.NewReader(`{invalid}`))
	w := httptest.NewRecorder()
	svc.HandleAppleAuth(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestHandleLogout_SecureCookie(t *testing.T) {
	svc := New(Config{JWTSecret: "test-secret", ProductionEnv: true}, nil)

	req := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
	w := httptest.NewRecorder()
	svc.HandleLogout(w, req)

	cookies := w.Result().Cookies()
	for _, c := range cookies {
		if c.Name == "session" && !c.Secure {
			t.Fatal("expected Secure flag in production")
		}
	}
}

func TestMigrations(t *testing.T) {
	m := Migrations()
	if len(m) == 0 {
		t.Fatal("expected at least one migration")
	}
	// Should contain CREATE TABLE users
	if !strings.Contains(m[0], "users") {
		t.Fatal("first migration should create users table")
	}
}
