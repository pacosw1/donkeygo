package attest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pacosw1/donkeygo/middleware"
)

// ── Mock DB ─────────────────────────────────────────────────────────────────

type mockAttestDB struct {
	keys map[string]string // userID -> keyID
	err  error
}

func newMockDB() *mockAttestDB {
	return &mockAttestDB{keys: make(map[string]string)}
}

func (m *mockAttestDB) StoreAttestKey(userID, keyID string) error {
	m.keys[userID] = keyID
	return m.err
}
func (m *mockAttestDB) GetAttestKey(userID string) (string, error) {
	return m.keys[userID], m.err
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

// ── HandleChallenge ─────────────────────────────────────────────────────────

func TestHandleChallenge(t *testing.T) {
	svc := New(Config{}, newMockDB())

	req := authReq("POST", "/api/v1/attest/challenge", "")
	w := httptest.NewRecorder()
	svc.HandleChallenge(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]string
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["challenge"] == "" {
		t.Fatal("expected non-empty challenge")
	}

	// Verify challenge was stored
	svc.mu.RLock()
	_, exists := svc.challenges["user-1"]
	svc.mu.RUnlock()
	if !exists {
		t.Fatal("expected challenge to be stored")
	}
}

// ── HandleVerify ────────────────────────────────────────────────────────────

func TestHandleVerify_Valid(t *testing.T) {
	db := newMockDB()
	svc := New(Config{}, db)

	// First get a challenge
	req := authReq("POST", "/api/v1/attest/challenge", "")
	w := httptest.NewRecorder()
	svc.HandleChallenge(w, req)

	var challengeResp map[string]string
	json.Unmarshal(w.Body.Bytes(), &challengeResp)

	// Now verify
	body := `{"key_id":"key-abc","attestation":"base64data","challenge":"` + challengeResp["challenge"] + `"}`
	req = authReq("POST", "/api/v1/attest/verify", body)
	w = httptest.NewRecorder()
	svc.HandleVerify(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["verified"] != true {
		t.Fatal("expected verified=true")
	}
	if db.keys["user-1"] != "key-abc" {
		t.Fatalf("expected key stored, got %s", db.keys["user-1"])
	}
}

func TestHandleVerify_MissingKeyID(t *testing.T) {
	svc := New(Config{}, newMockDB())

	body := `{"attestation":"data"}`
	req := authReq("POST", "/api/v1/attest/verify", body)
	w := httptest.NewRecorder()
	svc.HandleVerify(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleVerify_ExpiredChallenge(t *testing.T) {
	svc := New(Config{}, newMockDB())

	// No challenge generated — should fail
	body := `{"key_id":"key-abc","attestation":"data"}`
	req := authReq("POST", "/api/v1/attest/verify", body)
	w := httptest.NewRecorder()
	svc.HandleVerify(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing challenge, got %d", w.Code)
	}
}

// ── RequireAttest ───────────────────────────────────────────────────────────

func TestRequireAttest_DevModeBypasses(t *testing.T) {
	svc := New(Config{Environment: "development"}, newMockDB())

	called := false
	handler := svc.RequireAttest(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := authReq("GET", "/sensitive", "")
	w := httptest.NewRecorder()
	handler(w, req)

	if !called {
		t.Fatal("handler should be called in dev mode")
	}
}

func TestRequireAttest_NoAssertionHeader(t *testing.T) {
	svc := New(Config{Environment: "production"}, newMockDB())

	handler := svc.RequireAttest(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := authReq("GET", "/sensitive", "")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestRequireAttest_NoAttestKey(t *testing.T) {
	svc := New(Config{Environment: "production"}, newMockDB())

	handler := svc.RequireAttest(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := authReq("GET", "/sensitive", "")
	req.Header.Set("X-App-Assertion", "some-assertion")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without attest key, got %d", w.Code)
	}
}

func TestRequireAttest_WithValidKey(t *testing.T) {
	db := newMockDB()
	db.keys["user-1"] = "key-abc"
	svc := New(Config{Environment: "production"}, db)

	called := false
	handler := svc.RequireAttest(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := authReq("GET", "/sensitive", "")
	req.Header.Set("X-App-Assertion", "some-assertion")
	w := httptest.NewRecorder()
	handler(w, req)

	if !called {
		t.Fatal("handler should be called with valid attest key")
	}
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func TestGenerateHexNonce(t *testing.T) {
	nonce, err := GenerateHexNonce(16)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nonce) != 32 {
		t.Fatalf("expected 32 hex chars, got %d", len(nonce))
	}

	// Two nonces should be different
	nonce2, _ := GenerateHexNonce(16)
	if nonce == nonce2 {
		t.Fatal("expected different nonces")
	}
}

func TestMigrations(t *testing.T) {
	m := Migrations()
	if len(m) == 0 {
		t.Fatal("expected at least one migration")
	}
}
