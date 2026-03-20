// Package notify provides device token management, notification preferences, and scheduling.
package notify

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/pacosw1/donkeygo/httputil"
	"github.com/pacosw1/donkeygo/middleware"
	"github.com/pacosw1/donkeygo/push"
)

// NotifyDB is the database interface required by the notify package.
type NotifyDB interface {
	UpsertDeviceToken(dt *DeviceToken) error
	DisableDeviceToken(userID, token string) error
	EnabledDeviceTokens(userID string) ([]*DeviceToken, error)
	EnsureNotificationPreferences(userID string)
	GetNotificationPreferences(userID string) (*NotificationPreferences, error)
	UpsertNotificationPreferences(p *NotificationPreferences) error
	AllUsersWithNotificationsEnabled() ([]string, error)
	LastNotificationDelivery(userID string) (*NotificationDelivery, error)
	RecordNotificationDelivery(userID, kind, title, body string)
	TrackNotificationOpened(userID, notificationID string) error
}

// DeviceToken represents a registered push token.
type DeviceToken struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Token       string    `json:"token"`
	Platform    string    `json:"platform"`
	DeviceModel string    `json:"device_model"`
	OSVersion   string    `json:"os_version"`
	AppVersion  string    `json:"app_version"`
	Enabled     bool      `json:"enabled"`
	LastSeenAt  time.Time `json:"last_seen_at"`
}

// NotificationPreferences holds per-user notification settings.
type NotificationPreferences struct {
	UserID          string `json:"user_id"`
	PushEnabled     bool   `json:"push_enabled"`
	IntervalSeconds int    `json:"interval_seconds"`
	WakeHour        int    `json:"wake_hour"`
	SleepHour       int    `json:"sleep_hour"`
	Timezone        string `json:"timezone"`
	StopAfterGoal   bool   `json:"stop_after_goal"`
}

// NotificationDelivery records a sent notification.
type NotificationDelivery struct {
	ID     string    `json:"id"`
	UserID string    `json:"user_id"`
	Kind   string    `json:"kind"`
	Title  string    `json:"title"`
	Body   string    `json:"body"`
	Status string    `json:"status"`
	SentAt time.Time `json:"sent_at"`
}

// Service provides notification management handlers.
type Service struct {
	db   NotifyDB
	push push.Provider
}

// New creates a notify service.
func New(db NotifyDB, pushProvider push.Provider) *Service {
	return &Service{db: db, push: pushProvider}
}

// Migrations returns the SQL migrations needed by the notify package.
func Migrations() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS user_device_tokens (
			id           TEXT PRIMARY KEY,
			user_id      TEXT NOT NULL,
			token        TEXT NOT NULL,
			platform     TEXT NOT NULL DEFAULT 'ios',
			device_model TEXT NOT NULL DEFAULT '',
			os_version   TEXT NOT NULL DEFAULT '',
			app_version  TEXT NOT NULL DEFAULT '',
			enabled      BOOLEAN NOT NULL DEFAULT TRUE,
			last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(user_id, token)
		)`,
		`CREATE TABLE IF NOT EXISTS user_notification_preferences (
			user_id          TEXT PRIMARY KEY,
			push_enabled     BOOLEAN NOT NULL DEFAULT TRUE,
			interval_seconds INTEGER NOT NULL DEFAULT 3600,
			wake_hour        INTEGER NOT NULL DEFAULT 8,
			sleep_hour       INTEGER NOT NULL DEFAULT 22,
			timezone         TEXT NOT NULL DEFAULT 'America/New_York',
			stop_after_goal  BOOLEAN NOT NULL DEFAULT TRUE
		)`,
		`CREATE TABLE IF NOT EXISTS notification_deliveries (
			id      TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			kind    TEXT NOT NULL DEFAULT 'reminder',
			title   TEXT NOT NULL DEFAULT '',
			body    TEXT NOT NULL DEFAULT '',
			status  TEXT NOT NULL DEFAULT 'sent',
			sent_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_notification_deliveries_user ON notification_deliveries(user_id, sent_at)`,
	}
}

// ── Handlers ────────────────────────────────────────────────────────────────

type registerDeviceRequest struct {
	Token       string `json:"token"`
	Platform    string `json:"platform"`
	DeviceModel string `json:"device_model"`
	OSVersion   string `json:"os_version"`
	AppVersion  string `json:"app_version"`
}

// HandleRegisterDevice handles POST /api/v1/notifications/devices.
func (s *Service) HandleRegisterDevice(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)

	var req registerDeviceRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Token == "" {
		httputil.WriteError(w, http.StatusBadRequest, "token is required")
		return
	}
	if req.Platform == "" {
		req.Platform = "ios"
	}

	dt := &DeviceToken{
		ID:          uuid.New().String(),
		UserID:      userID,
		Token:       req.Token,
		Platform:    req.Platform,
		DeviceModel: req.DeviceModel,
		OSVersion:   req.OSVersion,
		AppVersion:  req.AppVersion,
		Enabled:     true,
		LastSeenAt:  time.Now().UTC(),
	}

	if err := s.db.UpsertDeviceToken(dt); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to register device")
		return
	}

	s.db.EnsureNotificationPreferences(userID)

	log.Printf("[device] registered %s for %s (%s %s app=%s)", req.Platform, userID, req.DeviceModel, req.OSVersion, req.AppVersion)
	httputil.WriteJSON(w, http.StatusCreated, map[string]string{"status": "registered"})
}

// HandleDisableDevice handles DELETE /api/v1/notifications/devices.
func (s *Service) HandleDisableDevice(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)

	var req struct {
		Token string `json:"token"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Token == "" {
		httputil.WriteError(w, http.StatusBadRequest, "token is required")
		return
	}

	if err := s.db.DisableDeviceToken(userID, req.Token); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to disable device")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
}

// HandleGetNotificationPrefs handles GET /api/v1/notifications/preferences.
func (s *Service) HandleGetNotificationPrefs(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)

	prefs, err := s.db.GetNotificationPreferences(userID)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to get preferences")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, prefs)
}

type updateNotificationPrefsRequest struct {
	PushEnabled     *bool   `json:"push_enabled"`
	IntervalSeconds *int    `json:"interval_seconds"`
	WakeHour        *int    `json:"wake_hour"`
	SleepHour       *int    `json:"sleep_hour"`
	Timezone        *string `json:"timezone"`
	StopAfterGoal   *bool   `json:"stop_after_goal"`
}

// HandleUpdateNotificationPrefs handles PUT /api/v1/notifications/preferences.
func (s *Service) HandleUpdateNotificationPrefs(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)

	var req updateNotificationPrefsRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	prefs, err := s.db.GetNotificationPreferences(userID)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to get preferences")
		return
	}

	if req.PushEnabled != nil {
		prefs.PushEnabled = *req.PushEnabled
	}
	if req.IntervalSeconds != nil {
		if *req.IntervalSeconds < 300 {
			httputil.WriteError(w, http.StatusBadRequest, "interval_seconds must be at least 300 (5 minutes)")
			return
		}
		prefs.IntervalSeconds = *req.IntervalSeconds
	}
	if req.WakeHour != nil {
		if *req.WakeHour < 0 || *req.WakeHour > 23 {
			httputil.WriteError(w, http.StatusBadRequest, "wake_hour must be 0-23")
			return
		}
		prefs.WakeHour = *req.WakeHour
	}
	if req.SleepHour != nil {
		if *req.SleepHour < 0 || *req.SleepHour > 23 {
			httputil.WriteError(w, http.StatusBadRequest, "sleep_hour must be 0-23")
			return
		}
		prefs.SleepHour = *req.SleepHour
	}
	if req.Timezone != nil {
		if _, err := time.LoadLocation(*req.Timezone); err != nil {
			httputil.WriteError(w, http.StatusBadRequest, "invalid timezone")
			return
		}
		prefs.Timezone = *req.Timezone
	}
	if req.StopAfterGoal != nil {
		prefs.StopAfterGoal = *req.StopAfterGoal
	}

	if err := s.db.UpsertNotificationPreferences(prefs); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to update preferences")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, prefs)
}

// HandleNotificationOpened handles POST /api/v1/notifications/opened.
func (s *Service) HandleNotificationOpened(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)

	var req struct {
		NotificationID string `json:"notification_id"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	_ = s.db.TrackNotificationOpened(userID, req.NotificationID)
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

// ── Scheduler ───────────────────────────────────────────────────────────────

// TickFunc is a function called by the scheduler for each eligible user.
// Apps provide this to customize notification content.
type TickFunc func(userID string, prefs *NotificationPreferences, tokens []*DeviceToken, pushProvider push.Provider)

// Scheduler runs periodic notification evaluation.
type Scheduler struct {
	db       NotifyDB
	push     push.Provider
	interval time.Duration
	tick     TickFunc
	extra    func() // optional extra work per tick (e.g. churn analysis)
	stop     chan struct{}
}

// SchedulerConfig configures the notification scheduler.
type SchedulerConfig struct {
	Interval  time.Duration
	TickFunc  TickFunc  // custom per-user notification logic
	ExtraTick func()    // optional extra work each tick cycle
}

// NewScheduler creates a notification scheduler.
// If cfg.TickFunc is nil, uses a default that respects preferences and sends a generic reminder.
func NewScheduler(db NotifyDB, pushProvider push.Provider, cfg SchedulerConfig) *Scheduler {
	if cfg.Interval == 0 {
		cfg.Interval = 15 * time.Minute
	}
	tickFn := cfg.TickFunc
	if tickFn == nil {
		tickFn = defaultTick
	}
	return &Scheduler{
		db:       db,
		push:     pushProvider,
		interval: cfg.Interval,
		tick:     tickFn,
		extra:    cfg.ExtraTick,
		stop:     make(chan struct{}),
	}
}

// Start begins the scheduler loop.
func (s *Scheduler) Start() {
	go s.run()
	log.Printf("[scheduler] started with interval %v", s.interval)
}

// Stop shuts down the scheduler.
func (s *Scheduler) Stop() {
	close(s.stop)
	log.Println("[scheduler] stopped")
}

func (s *Scheduler) run() {
	s.evaluate()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.evaluate()
		case <-s.stop:
			return
		}
	}
}

func (s *Scheduler) evaluate() {
	start := time.Now()

	userIDs, err := s.db.AllUsersWithNotificationsEnabled()
	if err != nil {
		log.Printf("[scheduler] error fetching users: %v", err)
		return
	}

	if len(userIDs) == 0 {
		return
	}

	log.Printf("[scheduler] evaluating %d users", len(userIDs))

	for _, uid := range userIDs {
		s.maybeNotify(uid)
	}

	if s.extra != nil {
		s.extra()
	}

	log.Printf("[scheduler] tick complete in %v", time.Since(start))
}

func (s *Scheduler) maybeNotify(userID string) {
	prefs, err := s.db.GetNotificationPreferences(userID)
	if err != nil || !prefs.PushEnabled {
		return
	}

	// Check user's local time
	loc, err := time.LoadLocation(prefs.Timezone)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	currentHour := now.Hour()

	// Only send during waking hours
	if currentHour < prefs.WakeHour || currentHour >= prefs.SleepHour {
		return
	}

	// Check interval since last notification
	lastDelivery, err := s.db.LastNotificationDelivery(userID)
	if err == nil && lastDelivery != nil {
		elapsed := time.Since(lastDelivery.SentAt)
		if elapsed < time.Duration(prefs.IntervalSeconds)*time.Second {
			return
		}
	}

	// Get device tokens
	tokens, err := s.db.EnabledDeviceTokens(userID)
	if err != nil || len(tokens) == 0 {
		return
	}

	s.tick(userID, prefs, tokens, s.push)
}

func defaultTick(userID string, prefs *NotificationPreferences, tokens []*DeviceToken, pushProvider push.Provider) {
	title := "Hey!"
	body := "Don't forget to check in today."

	for _, token := range tokens {
		if err := pushProvider.Send(token.Token, title, body); err != nil {
			log.Printf("[scheduler] push failed for %s: %v", userID, err)
		}
	}

	// Note: apps should record delivery via their DB implementation
	_ = fmt.Sprintf("sent to %s", userID)
}
