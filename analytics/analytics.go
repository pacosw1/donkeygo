// Package analytics provides admin analytics handlers for DAU, events, MRR, and summary stats.
package analytics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/pacosw1/donkeygo/httputil"
)

// AnalyticsDB is the database interface required by the analytics package.
type AnalyticsDB interface {
	DAUTimeSeries(since time.Time) ([]DAURow, error)
	EventCounts(since time.Time, event string) ([]EventRow, error)
	SubscriptionBreakdown() ([]SubStats, error)
	NewSubscriptions30d() (int, error)
	ChurnedSubscriptions30d() (int, error)
	DAUToday() (int, error)
	MAU() (int, error)
	TotalUsers() (int, error)
	ActiveSubscriptions() (int, error)
}

// DAURow represents a single day's active user count.
type DAURow struct {
	Date string `json:"date"`
	DAU  int    `json:"dau"`
}

// EventRow represents event counts for a single day and event type.
type EventRow struct {
	Date        string `json:"date"`
	Event       string `json:"event"`
	Count       int    `json:"count"`
	UniqueUsers int    `json:"unique_users"`
}

// SubStats represents a subscription status and its count.
type SubStats struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

// Config holds analytics configuration.
type Config struct{}

// Service provides admin analytics handlers.
type Service struct {
	cfg Config
	db  AnalyticsDB
}

// New creates an analytics service.
func New(cfg Config, db AnalyticsDB) *Service {
	return &Service{cfg: cfg, db: db}
}

// ── Handlers ────────────────────────────────────────────────────────────────

// HandleDAU handles GET /admin/api/analytics/dau.
func (s *Service) HandleDAU(w http.ResponseWriter, r *http.Request) {
	days := intQuery(r, "days", 30)
	since := time.Now().AddDate(0, 0, -days)

	rows, err := s.db.DAUTimeSeries(since)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if rows == nil {
		rows = []DAURow{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]any{"dau": rows})
}

// HandleEvents handles GET /admin/api/analytics/events.
func (s *Service) HandleEvents(w http.ResponseWriter, r *http.Request) {
	days := intQuery(r, "days", 30)
	event := r.URL.Query().Get("event")
	since := time.Now().AddDate(0, 0, -days)

	rows, err := s.db.EventCounts(since, event)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if rows == nil {
		rows = []EventRow{}
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]any{"events": rows})
}

// HandleMRR handles GET /admin/api/analytics/mrr.
func (s *Service) HandleMRR(w http.ResponseWriter, r *http.Request) {
	breakdown, err := s.db.SubscriptionBreakdown()
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if breakdown == nil {
		breakdown = []SubStats{}
	}

	totalActive := 0
	for _, s := range breakdown {
		if s.Status == "active" || s.Status == "trial" {
			totalActive += s.Count
		}
	}

	newSubs, _ := s.db.NewSubscriptions30d()
	churned, _ := s.db.ChurnedSubscriptions30d()

	httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"breakdown":    breakdown,
		"active_total": totalActive,
		"new_30d":      newSubs,
		"churned_30d":  churned,
	})
}

// HandleSummary handles GET /admin/api/analytics/summary.
func (s *Service) HandleSummary(w http.ResponseWriter, r *http.Request) {
	dauToday, _ := s.db.DAUToday()
	mau, _ := s.db.MAU()
	totalUsers, _ := s.db.TotalUsers()
	activeSubs, _ := s.db.ActiveSubscriptions()

	httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"dau_today":   dauToday,
		"mau":         mau,
		"total_users": totalUsers,
		"active_subs": activeSubs,
	})
}

// intQuery parses an integer query parameter with a fallback default.
func intQuery(r *http.Request, key string, fallback int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
