package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pacosw1/donkeygo/middleware"
)

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

func TestSessionExpiry(t *testing.T) {
	svc := New(Config{JWTSecret: "test-secret", SessionExpiry: 7 * 24 * time.Hour}, nil)

	token, err := svc.CreateSessionToken("user-123")
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	// Should be valid now
	_, err = svc.ParseSessionToken(token)
	if err != nil {
		t.Fatalf("token should be valid: %v", err)
	}
}

// Mock DB for handler tests
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

func TestHandleMe(t *testing.T) {
	db := &mockAuthDB{user: &User{ID: "user-123", Email: "test@example.com", Name: "Test"}}
	svc := New(Config{JWTSecret: "test-secret"}, db)

	req := httptest.NewRequest("GET", "/api/v1/auth/me", nil)

	// Add user ID to context (simulating middleware)
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

func TestMigrations(t *testing.T) {
	m := Migrations()
	if len(m) == 0 {
		t.Fatal("expected at least one migration")
	}
}
