package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequireAuth_NoToken(t *testing.T) {
	auth := RequireAuth(AuthConfig{
		ParseToken: func(token string) (string, error) {
			return "user-123", nil
		},
	})

	handler := auth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", w.Code)
	}
}

func TestRequireAuth_ValidToken(t *testing.T) {
	auth := RequireAuth(AuthConfig{
		ParseToken: func(token string) (string, error) {
			if token == "valid-token" {
				return "user-123", nil
			}
			return "", fmt.Errorf("invalid")
		},
	})

	handler := auth(func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(CtxUserID).(string)
		w.Write([]byte(userID))
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "user-123" {
		t.Fatalf("expected user-123, got %s", w.Body.String())
	}
}

func TestRequireAuth_Cookie(t *testing.T) {
	auth := RequireAuth(AuthConfig{
		ParseToken: func(token string) (string, error) {
			if token == "cookie-token" {
				return "user-456", nil
			}
			return "", fmt.Errorf("invalid")
		},
	})

	handler := auth(func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(CtxUserID).(string)
		w.Write([]byte(userID))
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "cookie-token"})
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "user-456" {
		t.Fatalf("expected user-456, got %s", w.Body.String())
	}
}

func TestRequireAdmin_APIKey(t *testing.T) {
	admin := RequireAdmin(AdminConfig{
		AdminKey: "secret-key",
	})

	handler := admin(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// With key
	req := httptest.NewRequest("GET", "/admin", nil)
	req.Header.Set("X-Admin-Key", "secret-key")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with admin key, got %d", w.Code)
	}

	// Without key
	req = httptest.NewRequest("GET", "/admin", nil)
	w = httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without admin key, got %d", w.Code)
	}
}

func TestCORS_AllOrigins(t *testing.T) {
	handler := CORS("*")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("expected *, got %s", got)
	}
}

func TestCORS_Preflight(t *testing.T) {
	handler := CORS("*")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("OPTIONS", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for preflight, got %d", w.Code)
	}
}

func TestRateLimiter(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)

	for i := 0; i < 3; i++ {
		if !rl.Allow("1.2.3.4") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	if rl.Allow("1.2.3.4") {
		t.Fatal("4th request should be blocked")
	}

	if !rl.Allow("5.6.7.8") {
		t.Fatal("different IP should be allowed")
	}
}

func TestVersion(t *testing.T) {
	handler := Version("2", "1")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("X-API-Version"); got != "2" {
		t.Fatalf("expected X-API-Version=2, got %s", got)
	}
	if got := w.Header().Get("X-Minimum-Version"); got != "1" {
		t.Fatalf("expected X-Minimum-Version=1, got %s", got)
	}
}

func TestRequestLog(t *testing.T) {
	handler := RequestLog("/api/health")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)
	handler := RateLimit(rl)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", w.Code)
	}

	// Second request should be blocked
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: expected 429, got %d", w.Code)
	}

	var body map[string]string
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["error"] != "rate limit exceeded" {
		t.Fatalf("expected rate limit error, got %s", body["error"])
	}
}
