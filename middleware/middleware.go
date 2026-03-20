// Package middleware provides reusable HTTP middleware for Go servers.
// All middleware returns http.Handler or http.HandlerFunc for stdlib mux compatibility.
package middleware

import (
	"context"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pacosw1/donkeygo/httputil"
)

// ContextKey is the type for context keys used by middleware.
type ContextKey string

// CtxUserID is the context key for the authenticated user's ID.
const CtxUserID ContextKey = "user_id"

// ── Auth Middleware ──────────────────────────────────────────────────────────

// AuthConfig configures the RequireAuth middleware.
type AuthConfig struct {
	// ParseToken validates a session token and returns the user ID.
	ParseToken func(token string) (userID string, err error)
}

// RequireAuth returns middleware that extracts user ID from Bearer token or session cookie.
func RequireAuth(cfg AuthConfig) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var tokenStr string

			// 1. Authorization: Bearer <token>
			if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
				tokenStr = strings.TrimPrefix(auth, "Bearer ")
			}

			// 2. Fallback to cookie
			if tokenStr == "" {
				if cookie, err := r.Cookie("session"); err == nil {
					tokenStr = cookie.Value
				}
			}

			if tokenStr == "" {
				httputil.WriteError(w, http.StatusUnauthorized, "missing session token")
				return
			}

			userID, err := cfg.ParseToken(tokenStr)
			if err != nil {
				httputil.WriteError(w, http.StatusUnauthorized, "invalid or expired session")
				return
			}

			ctx := context.WithValue(r.Context(), CtxUserID, userID)
			next(w, r.WithContext(ctx))
		}
	}
}

// ── Admin Middleware ─────────────────────────────────────────────────────────

// AdminConfig configures the RequireAdmin middleware.
type AdminConfig struct {
	AdminKey   string
	AdminEmail string
	// ParseToken validates a session token and returns the user ID.
	ParseToken func(token string) (userID string, err error)
	// GetUserEmail returns the email for a user ID.
	GetUserEmail func(userID string) (email string, err error)
}

// RequireAdmin returns middleware that checks admin API key or admin email JWT.
func RequireAdmin(cfg AdminConfig) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			authenticated := false

			// Check admin API key
			if cfg.AdminKey != "" {
				for _, source := range []string{
					r.Header.Get("X-Admin-Key"),
					r.URL.Query().Get("key"),
					r.URL.Query().Get("admin_key"),
				} {
					if source == cfg.AdminKey {
						authenticated = true
						break
					}
				}
				if !authenticated {
					if c, err := r.Cookie("admin_key"); err == nil && c.Value == cfg.AdminKey {
						authenticated = true
					}
				}
			}

			// Check admin session cookie
			if !authenticated && cfg.ParseToken != nil && cfg.GetUserEmail != nil {
				if cookie, err := r.Cookie("admin_session"); err == nil {
					userID, err := cfg.ParseToken(cookie.Value)
					if err == nil {
						email, err := cfg.GetUserEmail(userID)
						if err == nil && email == cfg.AdminEmail {
							authenticated = true
						}
					}
				}
			}

			if !authenticated {
				httputil.WriteError(w, http.StatusUnauthorized, "admin authentication required")
				return
			}

			next(w, r)
		}
	}
}

// ── CORS Middleware ──────────────────────────────────────────────────────────

// CORS returns middleware that adds CORS headers.
// Pass "*" to allow all origins, or a comma-separated list of allowed origins.
func CORS(allowedOrigins string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if allowedOrigins == "*" {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				for _, o := range strings.Split(allowedOrigins, ",") {
					if strings.TrimSpace(o) == origin {
						w.Header().Set("Access-Control-Allow-Origin", origin)
						break
					}
				}
			}

			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Admin-Key, X-Device-Token, X-Timezone")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Max-Age", "86400")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ── Rate Limit Middleware ────────────────────────────────────────────────────

// RateLimiter is an in-memory per-IP rate limiter.
type RateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     int
	window   time.Duration
}

type visitor struct {
	count   int
	resetAt time.Time
}

// NewRateLimiter creates a rate limiter with the given rate and window.
func NewRateLimiter(rate int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		rate:     rate,
		window:   window,
	}
	go func() {
		for {
			time.Sleep(time.Minute)
			rl.mu.Lock()
			now := time.Now()
			for ip, v := range rl.visitors {
				if now.After(v.resetAt) {
					delete(rl.visitors, ip)
				}
			}
			rl.mu.Unlock()
		}
	}()
	return rl
}

// Allow checks if a request from the given IP is allowed.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	v, exists := rl.visitors[ip]

	if !exists || now.After(v.resetAt) {
		rl.visitors[ip] = &visitor{count: 1, resetAt: now.Add(rl.window)}
		return true
	}

	v.count++
	return v.count <= rl.rate
}

// RateLimit returns middleware that applies the given rate limiter.
func RateLimit(rl *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := httputil.GetClientIP(r)
			if !rl.Allow(ip) {
				log.Printf("[ratelimit] blocked %s", ip)
				httputil.WriteError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimitFunc returns middleware for individual HandlerFuncs.
func RateLimitFunc(rl *RateLimiter) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			ip := httputil.GetClientIP(r)
			if !rl.Allow(ip) {
				log.Printf("[ratelimit] blocked %s", ip)
				httputil.WriteError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next(w, r)
		}
	}
}

// ── Request Log Middleware ────────────────────────────────────────────────────

// RequestLog returns middleware that logs HTTP requests with duration.
// skipPaths are paths to exclude from logging (e.g. "/api/health").
func RequestLog(skipPaths ...string) func(http.Handler) http.Handler {
	skip := make(map[string]bool, len(skipPaths))
	for _, p := range skipPaths {
		skip[p] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wrapped := &statusWriter{ResponseWriter: w, status: 200}
			next.ServeHTTP(wrapped, r)

			if skip[r.URL.Path] {
				return
			}

			log.Printf("[http] %s %s %d %v %s",
				r.Method, r.URL.Path, wrapped.status, time.Since(start).Round(time.Millisecond), httputil.GetClientIP(r))
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// ── Version Middleware ───────────────────────────────────────────────────────

// Version returns middleware that adds X-API-Version and X-Minimum-Version headers.
func Version(current, minimum string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-API-Version", current)
			w.Header().Set("X-Minimum-Version", minimum)
			next.ServeHTTP(w, r)
		})
	}
}
