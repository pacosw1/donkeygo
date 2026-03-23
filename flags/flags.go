// Package flags provides simple feature flags with user targeting and percentage rollouts.
//
// Usage:
//
//	flagsSvc := flags.New(flags.Config{}, store)
//	enabled, _ := flagsSvc.IsEnabled("dark_mode", userID)
//
//	// HTTP endpoints:
//	mux.HandleFunc("GET /api/v1/flags/{key}", requireAuth(flagsSvc.HandleCheck))
//	mux.HandleFunc("POST /api/v1/flags/check", requireAuth(flagsSvc.HandleBatchCheck))
//	mux.HandleFunc("GET /admin/api/flags", requireAdmin(flagsSvc.HandleAdminList))
//	mux.HandleFunc("POST /admin/api/flags", requireAdmin(flagsSvc.HandleAdminCreate))
//	mux.HandleFunc("PUT /admin/api/flags/{key}", requireAdmin(flagsSvc.HandleAdminUpdate))
//	mux.HandleFunc("DELETE /admin/api/flags/{key}", requireAdmin(flagsSvc.HandleAdminDelete))
package flags

import (
	"hash/crc32"
	"net/http"
	"time"

	"github.com/pacosw1/donkeygo/httputil"
	"github.com/pacosw1/donkeygo/middleware"
)

// FlagsDB is the database interface required by the flags package.
type FlagsDB interface {
	UpsertFlag(f *Flag) error
	GetFlag(key string) (*Flag, error)
	ListFlags() ([]*Flag, error)
	DeleteFlag(key string) error
	GetUserOverride(key, userID string) (*bool, error)
	SetUserOverride(key, userID string, enabled bool) error
	DeleteUserOverride(key, userID string) error
}

// Flag represents a feature flag.
type Flag struct {
	Key         string    `json:"key"`
	Enabled     bool      `json:"enabled"`
	RolloutPct  int       `json:"rollout_pct"` // 0-100
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Config holds flags configuration.
type Config struct{}

// Service provides feature flag evaluation and management.
type Service struct {
	cfg Config
	db  FlagsDB
}

// New creates a flags service.
func New(cfg Config, db FlagsDB) *Service {
	return &Service{cfg: cfg, db: db}
}

// Migrations returns the SQL migrations needed by the flags package.
func Migrations() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS feature_flags (
			key         TEXT PRIMARY KEY,
			enabled     BOOLEAN NOT NULL DEFAULT TRUE,
			rollout_pct INTEGER NOT NULL DEFAULT 100,
			description TEXT NOT NULL DEFAULT '',
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS feature_flag_overrides (
			flag_key TEXT NOT NULL,
			user_id  TEXT NOT NULL,
			enabled  BOOLEAN NOT NULL,
			PRIMARY KEY (flag_key, user_id)
		)`,
	}
}

// ── Core Logic ──────────────────────────────────────────────────────────────

// IsEnabled checks if a feature flag is enabled for a user.
// Priority: user override > rollout percentage > flag default.
func (s *Service) IsEnabled(key, userID string) (bool, error) {
	// 1. Check user-specific override
	override, err := s.db.GetUserOverride(key, userID)
	if err == nil && override != nil {
		return *override, nil
	}

	// 2. Get the flag
	flag, err := s.db.GetFlag(key)
	if err != nil {
		return false, nil // flag not found = disabled
	}
	if !flag.Enabled {
		return false, nil
	}

	// 3. Rollout percentage
	if flag.RolloutPct >= 100 {
		return true, nil
	}
	if flag.RolloutPct <= 0 {
		return false, nil
	}

	// Deterministic hash: same user always gets same result for same flag
	hash := crc32.ChecksumIEEE([]byte(key + ":" + userID))
	return int(hash%100) < flag.RolloutPct, nil
}

// ── User-facing Handlers ────────────────────────────────────────────────────

// HandleCheck handles GET /api/v1/flags/{key} — returns whether a flag is enabled for the user.
func (s *Service) HandleCheck(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)
	key := r.PathValue("key")
	if key == "" {
		httputil.WriteError(w, http.StatusBadRequest, "flag key is required")
		return
	}

	enabled, err := s.IsEnabled(key, userID)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to check flag")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]any{"key": key, "enabled": enabled})
}

// HandleBatchCheck handles POST /api/v1/flags/check — checks multiple flags at once.
func (s *Service) HandleBatchCheck(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)

	var req struct {
		Keys []string `json:"keys"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Keys) == 0 {
		httputil.WriteError(w, http.StatusBadRequest, "keys array is required")
		return
	}

	result := make(map[string]bool, len(req.Keys))
	for _, key := range req.Keys {
		enabled, _ := s.IsEnabled(key, userID)
		result[key] = enabled
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]any{"flags": result})
}

// ── Admin Handlers ──────────────────────────────────────────────────────────

// HandleAdminList handles GET /admin/api/flags.
func (s *Service) HandleAdminList(w http.ResponseWriter, r *http.Request) {
	flags, err := s.db.ListFlags()
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to list flags")
		return
	}
	if flags == nil {
		flags = []*Flag{}
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"flags": flags})
}

// HandleAdminCreate handles POST /admin/api/flags.
func (s *Service) HandleAdminCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key         string `json:"key"`
		Enabled     *bool  `json:"enabled"`
		RolloutPct  *int   `json:"rollout_pct"`
		Description string `json:"description"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Key == "" {
		httputil.WriteError(w, http.StatusBadRequest, "key is required")
		return
	}

	flag := &Flag{
		Key:         req.Key,
		Enabled:     true,
		RolloutPct:  100,
		Description: req.Description,
	}
	if req.Enabled != nil {
		flag.Enabled = *req.Enabled
	}
	if req.RolloutPct != nil {
		flag.RolloutPct = *req.RolloutPct
	}

	if err := s.db.UpsertFlag(flag); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to create flag")
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, flag)
}

// HandleAdminUpdate handles PUT /admin/api/flags/{key}.
func (s *Service) HandleAdminUpdate(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		httputil.WriteError(w, http.StatusBadRequest, "flag key is required")
		return
	}

	existing, err := s.db.GetFlag(key)
	if err != nil {
		httputil.WriteError(w, http.StatusNotFound, "flag not found")
		return
	}

	var req struct {
		Enabled     *bool  `json:"enabled"`
		RolloutPct  *int   `json:"rollout_pct"`
		Description *string `json:"description"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	if req.RolloutPct != nil {
		existing.RolloutPct = *req.RolloutPct
	}
	if req.Description != nil {
		existing.Description = *req.Description
	}

	if err := s.db.UpsertFlag(existing); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to update flag")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, existing)
}

// HandleAdminDelete handles DELETE /admin/api/flags/{key}.
func (s *Service) HandleAdminDelete(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		httputil.WriteError(w, http.StatusBadRequest, "flag key is required")
		return
	}

	if err := s.db.DeleteFlag(key); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to delete flag")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
