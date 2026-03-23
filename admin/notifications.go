package admin

import "net/http"

// NotificationsConfig configures the built-in Notifications tab.
type NotificationsConfig struct {
	ExtraColumns []Column
}

// NotificationsTab creates the built-in Notifications tab showing delivery logs.
func NotificationsTab(db AdminDB, cfg ...NotificationsConfig) Tab {
	var c NotificationsConfig
	if len(cfg) > 0 {
		c = cfg[0]
	}
	return Tab{
		ID:      "notifications",
		Label:   "Notifications",
		Icon:    "bell",
		Handler: &notificationsHandler{db: db, cfg: c},
		Order:   50,
	}
}

type notificationsHandler struct {
	db  AdminDB
	cfg NotificationsConfig
}

type notificationsData struct {
	Notifications []AdminNotification
	ExtraHeaders  []string
}

func (h *notificationsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	limit := intQueryParam(r, "limit", 100)
	notifs, err := h.db.AdminListNotifications(limit)
	if err != nil {
		http.Error(w, "failed to list notifications", http.StatusInternalServerError)
		return
	}

	headers := make([]string, len(h.cfg.ExtraColumns))
	for i, col := range h.cfg.ExtraColumns {
		headers[i] = col.Header
	}

	renderTemplate(w, "notifications.html", notificationsData{
		Notifications: notifs,
		ExtraHeaders:  headers,
	})
}
