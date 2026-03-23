package admin

import (
	"net/http"
	"time"
)

// EventsConfig configures the built-in Events tab.
type EventsConfig struct {
	EventTypes   []string // Predefined event types for the filter dropdown
	ExtraColumns []Column // Additional columns
}

// EventsTab creates the built-in Events tab with filtering by type, user, and date.
func EventsTab(db AdminDB, cfg ...EventsConfig) Tab {
	var c EventsConfig
	if len(cfg) > 0 {
		c = cfg[0]
	}
	return Tab{
		ID:      "events",
		Label:   "Events",
		Icon:    "activity",
		Handler: &eventsHandler{db: db, cfg: c},
		Order:   30,
	}
}

type eventsHandler struct {
	db  AdminDB
	cfg EventsConfig
}

type eventsData struct {
	Events       []AdminEvent
	EventType    string
	UserID       string
	EventTypes   []string
	ExtraHeaders []string
}

func (h *eventsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	eventType := r.URL.Query().Get("event")
	userID := r.URL.Query().Get("user_id")
	limit := intQueryParam(r, "limit", 100)
	since := time.Now().AddDate(0, 0, -7)
	if v := r.URL.Query().Get("since"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			since = t
		}
	}

	events, err := h.db.AdminListEvents(eventType, userID, since, limit)
	if err != nil {
		http.Error(w, "failed to list events", http.StatusInternalServerError)
		return
	}

	headers := make([]string, len(h.cfg.ExtraColumns))
	for i, col := range h.cfg.ExtraColumns {
		headers[i] = col.Header
	}

	renderTemplate(w, "events.html", eventsData{
		Events:       events,
		EventType:    eventType,
		UserID:       userID,
		EventTypes:   h.cfg.EventTypes,
		ExtraHeaders: headers,
	})
}
