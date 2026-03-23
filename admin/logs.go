package admin

import (
	"net/http"
	"strings"
)

// LogsTab creates the built-in Logs tab showing server log output.
func LogsTab(buf LogProvider) Tab {
	return Tab{
		ID:      "logs",
		Label:   "Logs",
		Icon:    "terminal",
		Handler: &logsHandler{buf: buf},
		Order:   80,
	}
}

type logsHandler struct {
	buf LogProvider
}

type logsData struct {
	Lines  []string
	Filter string
	Count  int
}

func (h *logsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	limit := intQueryParam(r, "limit", 500)
	filter := r.URL.Query().Get("filter")

	lines := h.buf.Lines(limit)

	if filter != "" {
		lower := strings.ToLower(filter)
		filtered := make([]string, 0, len(lines))
		for _, l := range lines {
			if strings.Contains(strings.ToLower(l), lower) {
				filtered = append(filtered, l)
			}
		}
		lines = filtered
	}

	renderTemplate(w, "logs.html", logsData{
		Lines:  lines,
		Filter: filter,
		Count:  len(lines),
	})
}
