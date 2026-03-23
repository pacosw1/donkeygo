package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/pacosw1/donkeygo/httputil"
)

// adminClaims holds JWT claims for admin sessions.
type adminClaims struct {
	gojwt.RegisteredClaims
	Email string `json:"email"`
	Role  string `json:"role"`
}

func createAdminJWT(secret, sub, email string, expiry time.Duration) (string, error) {
	now := time.Now()
	claims := adminClaims{
		RegisteredClaims: gojwt.RegisteredClaims{
			Subject:   sub,
			IssuedAt:  gojwt.NewNumericDate(now),
			ExpiresAt: gojwt.NewNumericDate(now.Add(expiry)),
		},
		Email: email,
		Role:  "admin",
	}
	token := gojwt.NewWithClaims(gojwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func parseAdminJWT(secret, tokenStr string) (*adminClaims, error) {
	token, err := gojwt.ParseWithClaims(tokenStr, &adminClaims{}, func(t *gojwt.Token) (any, error) {
		if _, ok := t.Method.(*gojwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*adminClaims)
	if !ok || !token.Valid || claims.Role != "admin" {
		return nil, fmt.Errorf("invalid admin token")
	}
	return claims, nil
}

// handleAuth accepts an Apple ID token, verifies it, and sets an admin session cookie.
func (p *Panel) handleAuth(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.IDToken == "" {
		httputil.WriteError(w, http.StatusBadRequest, "id_token is required")
		return
	}

	if p.cfg.VerifyToken == nil {
		httputil.WriteError(w, http.StatusInternalServerError, "token verification not configured")
		return
	}

	sub, email, err := p.cfg.VerifyToken(req.IDToken)
	if err != nil {
		httputil.WriteError(w, http.StatusUnauthorized, fmt.Sprintf("token verification failed: %v", err))
		return
	}

	// Check email whitelist
	allowed := false
	for _, e := range p.cfg.AllowedEmails {
		if e == email {
			allowed = true
			break
		}
	}
	if !allowed {
		httputil.WriteError(w, http.StatusForbidden, "access denied")
		return
	}

	token, err := createAdminJWT(p.cfg.JWTSecret, sub, email, p.cfg.SessionExpiry)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "admin_session",
		Value:    token,
		Path:     "/admin",
		MaxAge:   int(p.cfg.SessionExpiry.Seconds()),
		HttpOnly: true,
		Secure:   p.cfg.Production,
		SameSite: http.SameSiteLaxMode,
	})

	httputil.WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// handleLogout clears the admin session cookie.
func (p *Panel) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_session",
		Value:    "",
		Path:     "/admin",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   p.cfg.Production,
	})
	http.Redirect(w, r, "/admin/", http.StatusFound)
}

// isAuthenticated checks if the request has a valid admin session.
func (p *Panel) isAuthenticated(r *http.Request) bool {
	// Check API key
	if p.cfg.AdminKey != "" {
		key := r.Header.Get("X-Admin-Key")
		if key == "" {
			key = r.URL.Query().Get("key")
		}
		if key == "" {
			if c, err := r.Cookie("admin_key"); err == nil {
				key = c.Value
			}
		}
		if key != "" && key == p.cfg.AdminKey {
			return true
		}
	}

	// Check JWT cookie
	cookie, err := r.Cookie("admin_session")
	if err != nil {
		return false
	}
	claims, err := parseAdminJWT(p.cfg.JWTSecret, cookie.Value)
	if err != nil {
		return false
	}
	for _, e := range p.cfg.AllowedEmails {
		if e == claims.Email {
			return true
		}
	}
	return false
}

// requireAdmin wraps an http.Handler with admin authentication.
func (p *Panel) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !p.isAuthenticated(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// Set API key cookie if it came via query param
		if p.cfg.AdminKey != "" && r.URL.Query().Get("key") == p.cfg.AdminKey {
			http.SetCookie(w, &http.Cookie{
				Name:     "admin_key",
				Value:    p.cfg.AdminKey,
				Path:     "/admin/",
				HttpOnly: true,
				Secure:   p.cfg.Production,
				SameSite: http.SameSiteLaxMode,
				MaxAge:   86400 * 7,
			})
		}
		next.ServeHTTP(w, r)
	})
}

// requireAdminFunc wraps an http.HandlerFunc with admin authentication.
func (p *Panel) requireAdminFunc(next http.HandlerFunc) http.HandlerFunc {
	return p.requireAdmin(next).ServeHTTP
}
