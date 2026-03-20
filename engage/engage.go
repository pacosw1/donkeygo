// Package engage provides event tracking, subscription management, sessions, and feedback handlers.
package engage

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/pacosw1/donkeygo/httputil"
	"github.com/pacosw1/donkeygo/middleware"
)

// EngageDB is the database interface required by the engage package.
type EngageDB interface {
	TrackEvents(userID string, events []EventInput) error
	UpdateSubscription(userID, productID, status string, expiresAt *time.Time) error
	UpdateSubscriptionDetails(userID, originalTransactionID string, priceCents int, currencyCode string) error
	GetSubscription(userID string) (*UserSubscription, error)
	IsProUser(userID string) (bool, error)
	GetEngagementData(userID string) (*EngagementData, error)
	StartSession(userID, sessionID, appVersion, osVersion, country string) error
	EndSession(userID, sessionID string, durationS int) error
	SaveFeedback(userID, feedbackType, message, appVersion string) error
}

// EventInput represents a single event to track.
type EventInput struct {
	Event     string `json:"event"`
	Metadata  string `json:"metadata"`
	Timestamp string `json:"timestamp"`
}

// UserSubscription represents a user's subscription state.
type UserSubscription struct {
	UserID    string     `json:"user_id"`
	ProductID string     `json:"product_id"`
	Status    string     `json:"status"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// EngagementData holds computed engagement metrics for a user.
type EngagementData struct {
	DaysActive          int    `json:"days_active"`
	TotalLogs           int    `json:"total_logs"`
	CurrentStreak       int    `json:"current_streak"`
	SubscriptionStatus  string `json:"subscription_status"`
	PaywallShownCount   int    `json:"paywall_shown_count"`
	LastPaywallDate     string `json:"last_paywall_date"`
	GoalsCompletedTotal int    `json:"goals_completed_total"`
}

// Config holds engage configuration.
type Config struct{}

// Service provides engagement tracking handlers.
type Service struct {
	cfg             Config
	db              EngageDB
	PaywallTrigger  func(data *EngagementData) string // custom paywall trigger logic
}

// New creates an engage service.
func New(cfg Config, db EngageDB) *Service {
	return &Service{
		cfg:            cfg,
		db:             db,
		PaywallTrigger: DefaultPaywallTrigger,
	}
}

// Migrations returns the SQL migrations needed by the engage package.
func Migrations() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS user_subscriptions (
			user_id    TEXT PRIMARY KEY,
			product_id TEXT NOT NULL DEFAULT '',
			status     TEXT NOT NULL DEFAULT 'free',
			expires_at TIMESTAMPTZ,
			started_at TIMESTAMPTZ,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`DO $$ BEGIN ALTER TABLE user_subscriptions ADD COLUMN original_transaction_id TEXT NOT NULL DEFAULT ''; EXCEPTION WHEN duplicate_column THEN NULL; END $$`,
		`DO $$ BEGIN ALTER TABLE user_subscriptions ADD COLUMN price_cents INTEGER NOT NULL DEFAULT 0; EXCEPTION WHEN duplicate_column THEN NULL; END $$`,
		`DO $$ BEGIN ALTER TABLE user_subscriptions ADD COLUMN currency_code TEXT NOT NULL DEFAULT 'USD'; EXCEPTION WHEN duplicate_column THEN NULL; END $$`,
		`CREATE TABLE IF NOT EXISTS user_activity (
			id         SERIAL PRIMARY KEY,
			user_id    TEXT NOT NULL,
			event      TEXT NOT NULL,
			metadata   JSONB NOT NULL DEFAULT '{}',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_activity_user_event ON user_activity(user_id, event, created_at)`,
		`CREATE TABLE IF NOT EXISTS user_feedback (
			id          SERIAL PRIMARY KEY,
			user_id     TEXT NOT NULL,
			type        TEXT NOT NULL DEFAULT 'general',
			message     TEXT NOT NULL,
			app_version TEXT NOT NULL DEFAULT '',
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS user_sessions (
			id          TEXT PRIMARY KEY,
			user_id     TEXT NOT NULL,
			started_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			ended_at    TIMESTAMPTZ,
			duration_s  INTEGER DEFAULT 0,
			app_version TEXT NOT NULL DEFAULT '',
			os_version  TEXT NOT NULL DEFAULT '',
			country     TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_sessions_user ON user_sessions(user_id, started_at)`,
	}
}

// DefaultPaywallTrigger is the default paywall trigger logic.
func DefaultPaywallTrigger(data *EngagementData) string {
	if data.DaysActive >= 14 && data.TotalLogs >= 50 {
		return "power_user"
	}
	if data.GoalsCompletedTotal >= 10 && data.PaywallShownCount < 3 {
		return "milestone"
	}
	return ""
}

// ── Handlers ────────────────────────────────────────────────────────────────

type trackEventsRequest struct {
	Events []eventInputJSON `json:"events"`
}

type eventInputJSON struct {
	Event     string          `json:"event"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
}

// HandleTrackEvents handles POST /api/v1/events.
func (s *Service) HandleTrackEvents(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)

	var req trackEventsRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Events) == 0 {
		httputil.WriteError(w, http.StatusBadRequest, "events array is required")
		return
	}
	if len(req.Events) > 100 {
		httputil.WriteError(w, http.StatusBadRequest, "maximum 100 events per batch")
		return
	}

	dbEvents := make([]EventInput, len(req.Events))
	for i, e := range req.Events {
		meta := "{}"
		if e.Metadata != nil {
			meta = string(e.Metadata)
		}
		dbEvents[i] = EventInput{
			Event:     e.Event,
			Metadata:  meta,
			Timestamp: e.Timestamp,
		}
	}

	if err := s.db.TrackEvents(userID, dbEvents); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to track events")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]any{"tracked": len(dbEvents)})
}

type updateSubscriptionRequest struct {
	ProductID             string  `json:"product_id"`
	Status                string  `json:"status"`
	ExpiresAt             *string `json:"expires_at,omitempty"`
	OriginalTransactionID string  `json:"original_transaction_id,omitempty"`
	PriceCents            int     `json:"price_cents,omitempty"`
	CurrencyCode          string  `json:"currency_code,omitempty"`
}

// HandleUpdateSubscription handles PUT /api/v1/subscription.
func (s *Service) HandleUpdateSubscription(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)

	var req updateSubscriptionRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Status == "" {
		httputil.WriteError(w, http.StatusBadRequest, "status is required")
		return
	}

	validStatuses := map[string]bool{"active": true, "expired": true, "cancelled": true, "trial": true, "free": true}
	if !validStatuses[req.Status] {
		httputil.WriteError(w, http.StatusBadRequest, "status must be one of: active, expired, cancelled, trial, free")
		return
	}

	var expiresAt *time.Time
	if req.ExpiresAt != nil {
		if t, err := time.Parse(time.RFC3339, *req.ExpiresAt); err == nil {
			utc := t.UTC()
			expiresAt = &utc
		}
	}

	if err := s.db.UpdateSubscription(userID, req.ProductID, req.Status, expiresAt); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to update subscription")
		return
	}

	if req.OriginalTransactionID != "" || req.PriceCents > 0 {
		currency := req.CurrencyCode
		if currency == "" {
			currency = "USD"
		}
		_ = s.db.UpdateSubscriptionDetails(userID, req.OriginalTransactionID, req.PriceCents, currency)
	}

	sub, err := s.db.GetSubscription(userID)
	if err != nil || sub == nil {
		httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "updated"})
		return
	}
	httputil.WriteJSON(w, http.StatusOK, sub)
}

type sessionReportRequest struct {
	SessionID  string `json:"session_id"`
	Action     string `json:"action"`
	AppVersion string `json:"app_version"`
	OSVersion  string `json:"os_version"`
	Country    string `json:"country"`
	DurationS  int    `json:"duration_s,omitempty"`
}

// HandleSessionReport handles POST /api/v1/sessions.
func (s *Service) HandleSessionReport(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)

	var req sessionReportRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.SessionID == "" || req.Action == "" {
		httputil.WriteError(w, http.StatusBadRequest, "session_id and action required")
		return
	}
	if req.Action != "start" && req.Action != "end" {
		httputil.WriteError(w, http.StatusBadRequest, "action must be 'start' or 'end'")
		return
	}

	if req.Action == "start" {
		if err := s.db.StartSession(userID, req.SessionID, req.AppVersion, req.OSVersion, req.Country); err != nil {
			httputil.WriteError(w, http.StatusInternalServerError, "failed to start session")
			return
		}
	} else {
		if err := s.db.EndSession(userID, req.SessionID, req.DurationS); err != nil {
			httputil.WriteError(w, http.StatusInternalServerError, "failed to end session")
			return
		}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleGetEligibility handles GET /api/v1/user/eligibility.
func (s *Service) HandleGetEligibility(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)

	data, err := s.db.GetEngagementData(userID)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to get engagement data")
		return
	}

	isPro, _ := s.db.IsProUser(userID)

	var paywallTrigger *string
	if !isPro && s.PaywallTrigger != nil {
		trigger := s.PaywallTrigger(data)
		if trigger != "" {
			paywallTrigger = &trigger
		}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"paywall_trigger": paywallTrigger,
		"days_active":     data.DaysActive,
		"total_logs":      data.TotalLogs,
		"streak":          data.CurrentStreak,
		"is_pro":          isPro,
	})
}

type submitFeedbackRequest struct {
	Type       string `json:"type"`
	Message    string `json:"message"`
	AppVersion string `json:"app_version"`
}

// HandleSubmitFeedback handles POST /api/v1/feedback.
func (s *Service) HandleSubmitFeedback(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)

	var req submitFeedbackRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Message == "" {
		httputil.WriteError(w, http.StatusBadRequest, "message is required")
		return
	}

	validTypes := map[string]bool{"positive": true, "negative": true, "bug": true, "feature": true, "general": true}
	if req.Type == "" {
		req.Type = "general"
	}
	if !validTypes[req.Type] {
		httputil.WriteError(w, http.StatusBadRequest, "type must be one of: positive, negative, bug, feature, general")
		return
	}

	if err := s.db.SaveFeedback(userID, req.Type, req.Message, req.AppVersion); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to save feedback")
		return
	}

	httputil.WriteJSON(w, http.StatusCreated, map[string]string{"status": "received"})
}
