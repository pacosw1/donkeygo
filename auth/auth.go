// Package auth provides Apple Sign In verification and JWT session management.
package auth

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/pacosw1/donkeygo/httputil"
	"github.com/pacosw1/donkeygo/middleware"
)

// AuthDB is the database interface required by the auth package.
type AuthDB interface {
	UpsertUserByAppleSub(id, appleSub, email, name string) (user *User, err error)
	UserByID(id string) (user *User, err error)
}

// User represents an authenticated user.
type User struct {
	ID          string    `json:"id"`
	AppleSub    string    `json:"apple_sub"`
	Email       string    `json:"email"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"created_at"`
	LastLoginAt time.Time `json:"last_login_at"`
}

// Config holds auth configuration. Apps pass this — no env reading here.
type Config struct {
	JWTSecret        string
	AppleBundleID    string
	AppleWebClientID string // Services ID for web sign-in (optional)
	SessionExpiry    time.Duration
	ProductionEnv    bool // true for Secure cookies
}

// Service provides auth handlers and token management.
type Service struct {
	cfg Config
	db  AuthDB
}

// New creates an auth service.
func New(cfg Config, db AuthDB) *Service {
	if cfg.SessionExpiry == 0 {
		cfg.SessionExpiry = 7 * 24 * time.Hour
	}
	return &Service{cfg: cfg, db: db}
}

// Migrations returns the SQL migrations needed by the auth package.
func Migrations() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS users (
			id            TEXT PRIMARY KEY,
			apple_sub     TEXT UNIQUE NOT NULL,
			email         TEXT NOT NULL DEFAULT '',
			name          TEXT NOT NULL DEFAULT '',
			created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_login_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
	}
}

// ── Apple ID Token Verification ─────────────────────────────────────────────

var (
	appleKeys    map[string]*rsa.PublicKey
	appleKeysMu  sync.Mutex
	appleKeysTTL time.Time
)

type appleJWKSResponse struct {
	Keys []struct {
		Kty string `json:"kty"`
		Kid string `json:"kid"`
		Use string `json:"use"`
		Alg string `json:"alg"`
		N   string `json:"n"`
		E   string `json:"e"`
	} `json:"keys"`
}

func fetchApplePublicKeys() (map[string]*rsa.PublicKey, error) {
	appleKeysMu.Lock()
	defer appleKeysMu.Unlock()

	if appleKeys != nil && time.Now().Before(appleKeysTTL) {
		return appleKeys, nil
	}

	resp, err := http.Get("https://appleid.apple.com/auth/keys")
	if err != nil {
		return nil, fmt.Errorf("fetch apple keys: %w", err)
	}
	defer resp.Body.Close()

	var jwks appleJWKSResponse
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("decode apple keys: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey)
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" {
			continue
		}
		nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			continue
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			continue
		}
		n := new(big.Int).SetBytes(nBytes)
		e := int(new(big.Int).SetBytes(eBytes).Int64())
		keys[k.Kid] = &rsa.PublicKey{N: n, E: e}
	}

	appleKeys = keys
	appleKeysTTL = time.Now().Add(24 * time.Hour)
	return keys, nil
}

type appleClaims struct {
	gojwt.RegisteredClaims
	Email         string `json:"email"`
	EmailVerified any    `json:"email_verified"`
}

// VerifyAppleIDToken verifies an Apple identity token and returns the subject and email.
func (s *Service) VerifyAppleIDToken(tokenString string) (sub, email string, err error) {
	keys, err := fetchApplePublicKeys()
	if err != nil {
		return "", "", err
	}

	token, err := gojwt.ParseWithClaims(tokenString, &appleClaims{}, func(token *gojwt.Token) (any, error) {
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("missing kid header")
		}
		key, ok := keys[kid]
		if !ok {
			return nil, fmt.Errorf("unknown kid: %s", kid)
		}
		return key, nil
	}, gojwt.WithValidMethods([]string{"RS256"}),
		gojwt.WithIssuer("https://appleid.apple.com"),
	)

	if token != nil && token.Valid {
		aud, _ := token.Claims.(*appleClaims)
		if aud != nil {
			validAud := false
			for _, a := range aud.Audience {
				if a == s.cfg.AppleBundleID || (s.cfg.AppleWebClientID != "" && a == s.cfg.AppleWebClientID) {
					validAud = true
					break
				}
			}
			if !validAud {
				return "", "", fmt.Errorf("invalid audience")
			}
		}
	}
	if err != nil {
		return "", "", fmt.Errorf("invalid apple token: %w", err)
	}

	claims, ok := token.Claims.(*appleClaims)
	if !ok || !token.Valid {
		return "", "", fmt.Errorf("invalid token claims")
	}

	return claims.Subject, claims.Email, nil
}

// ── Session JWT ─────────────────────────────────────────────────────────────

type sessionClaims struct {
	gojwt.RegisteredClaims
	UserID string `json:"uid"`
}

// CreateSessionToken creates a signed JWT session token for the given user ID.
func (s *Service) CreateSessionToken(userID string) (string, error) {
	now := time.Now()
	claims := sessionClaims{
		RegisteredClaims: gojwt.RegisteredClaims{
			IssuedAt:  gojwt.NewNumericDate(now),
			ExpiresAt: gojwt.NewNumericDate(now.Add(s.cfg.SessionExpiry)),
			ID:        uuid.New().String(),
		},
		UserID: userID,
	}
	token := gojwt.NewWithClaims(gojwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.cfg.JWTSecret))
}

// ParseSessionToken validates a session JWT and returns the user ID.
func (s *Service) ParseSessionToken(tokenStr string) (string, error) {
	token, err := gojwt.ParseWithClaims(tokenStr, &sessionClaims{}, func(t *gojwt.Token) (any, error) {
		if _, ok := t.Method.(*gojwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(s.cfg.JWTSecret), nil
	})
	if err != nil {
		return "", err
	}
	claims, ok := token.Claims.(*sessionClaims)
	if !ok || !token.Valid {
		return "", fmt.Errorf("invalid session token")
	}
	return claims.UserID, nil
}

// ── HTTP Handlers ───────────────────────────────────────────────────────────

type appleAuthRequest struct {
	IdentityToken string `json:"identity_token"`
	Name          string `json:"name"`
	Platform      string `json:"platform,omitempty"`
}

// HandleAppleAuth handles POST /api/v1/auth/apple.
func (s *Service) HandleAppleAuth(w http.ResponseWriter, r *http.Request) {
	var req appleAuthRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.IdentityToken == "" {
		httputil.WriteError(w, http.StatusBadRequest, "identity_token is required")
		return
	}

	sub, email, err := s.VerifyAppleIDToken(req.IdentityToken)
	if err != nil {
		httputil.WriteError(w, http.StatusUnauthorized, fmt.Sprintf("token verification failed: %v", err))
		return
	}

	user, err := s.db.UpsertUserByAppleSub(uuid.New().String(), sub, email, req.Name)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	sessionToken, err := s.CreateSessionToken(user.ID)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.ProductionEnv,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(s.cfg.SessionExpiry.Seconds()),
	})

	httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"token": sessionToken,
		"user":  user,
	})
}

// HandleMe handles GET /api/v1/auth/me.
func (s *Service) HandleMe(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)
	user, err := s.db.UserByID(userID)
	if err != nil {
		httputil.WriteError(w, http.StatusNotFound, "user not found")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, user)
}

// HandleLogout handles POST /api/v1/auth/logout.
func (s *Service) HandleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.ProductionEnv,
		MaxAge:   -1,
	})
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}
