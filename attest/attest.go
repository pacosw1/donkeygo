// Package attest provides App Attest challenge/verify for device verification.
package attest

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/pacosw1/donkeygo/httputil"
	"github.com/pacosw1/donkeygo/middleware"
)

// AttestDB is the database interface required by the attest package.
type AttestDB interface {
	StoreAttestKey(userID, keyID string) error
	GetAttestKey(userID string) (keyID string, err error)
}

// Config configures the attest service.
type Config struct {
	// Environment: "development" skips attestation in requireAttest middleware.
	Environment string
}

// Service provides App Attest handlers.
type Service struct {
	cfg        Config
	db         AttestDB
	challenges map[string]challengeEntry
	mu         sync.RWMutex
}

type challengeEntry struct {
	challenge []byte
	expiresAt time.Time
}

// New creates an attest service.
func New(cfg Config, db AttestDB) *Service {
	s := &Service{
		cfg:        cfg,
		db:         db,
		challenges: make(map[string]challengeEntry),
	}
	// Periodic cleanup
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			s.cleanupExpiredChallenges()
		}
	}()
	return s
}

// Migrations returns the SQL migrations needed by the attest package.
func Migrations() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS user_attest_keys (
			user_id    TEXT PRIMARY KEY,
			key_id     TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
	}
}

// HandleChallenge handles POST /api/v1/attest/challenge.
func (s *Service) HandleChallenge(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)

	challenge := make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to generate challenge")
		return
	}

	s.mu.Lock()
	s.challenges[userID] = challengeEntry{
		challenge: challenge,
		expiresAt: time.Now().Add(5 * time.Minute),
	}
	s.mu.Unlock()

	httputil.WriteJSON(w, http.StatusOK, map[string]string{
		"challenge": base64.StdEncoding.EncodeToString(challenge),
	})
}

// HandleVerify handles POST /api/v1/attest/verify.
func (s *Service) HandleVerify(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)

	var req struct {
		KeyID       string `json:"key_id"`
		Attestation string `json:"attestation"`
		Challenge   string `json:"challenge"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.KeyID == "" || req.Attestation == "" {
		httputil.WriteError(w, http.StatusBadRequest, "key_id and attestation required")
		return
	}

	s.mu.RLock()
	entry, exists := s.challenges[userID]
	s.mu.RUnlock()

	if !exists || time.Now().After(entry.expiresAt) {
		httputil.WriteError(w, http.StatusBadRequest, "challenge expired or not found — request a new one")
		return
	}

	s.mu.Lock()
	delete(s.challenges, userID)
	s.mu.Unlock()

	// TODO: Full CBOR attestation verification
	// For now, store the key ID and mark user as attested.

	if err := s.db.StoreAttestKey(userID, req.KeyID); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to store attest key")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"verified": true,
		"key_id":   req.KeyID,
	})
}

// RequireAttest returns middleware that requires device attestation on sensitive endpoints.
func (s *Service) RequireAttest(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(middleware.CtxUserID).(string)

		assertionHeader := r.Header.Get("X-App-Assertion")
		if assertionHeader == "" {
			if s.cfg.Environment == "development" {
				next(w, r)
				return
			}
			httputil.WriteError(w, http.StatusForbidden, "device attestation required")
			return
		}

		keyID, err := s.db.GetAttestKey(userID)
		if err != nil || keyID == "" {
			httputil.WriteError(w, http.StatusForbidden, "device not attested — complete attestation first")
			return
		}

		// TODO: Validate assertion using stored public key
		next(w, r)
	}
}

func (s *Service) cleanupExpiredChallenges() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.challenges {
		if now.After(v.expiresAt) {
			delete(s.challenges, k)
		}
	}
}

// GenerateHexNonce creates a random hex string.
func GenerateHexNonce(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
