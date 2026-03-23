package admin

import "net/http"

// FeedbackConfig configures the built-in Feedback tab.
type FeedbackConfig struct {
	ExtraColumns []Column
}

// FeedbackTab creates the built-in Feedback tab showing user submissions.
func FeedbackTab(db AdminDB, cfg ...FeedbackConfig) Tab {
	var c FeedbackConfig
	if len(cfg) > 0 {
		c = cfg[0]
	}
	return Tab{
		ID:      "feedback",
		Label:   "Feedback",
		Icon:    "message",
		Handler: &feedbackHandler{db: db, cfg: c},
		Order:   60,
	}
}

type feedbackHandler struct {
	db  AdminDB
	cfg FeedbackConfig
}

type feedbackData struct {
	Feedback     []AdminFeedback
	ExtraHeaders []string
}

func (h *feedbackHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	limit := intQueryParam(r, "limit", 100)
	feedback, err := h.db.AdminListFeedback(limit)
	if err != nil {
		http.Error(w, "failed to list feedback", http.StatusInternalServerError)
		return
	}

	headers := make([]string, len(h.cfg.ExtraColumns))
	for i, col := range h.cfg.ExtraColumns {
		headers[i] = col.Header
	}

	renderTemplate(w, "feedback.html", feedbackData{
		Feedback:     feedback,
		ExtraHeaders: headers,
	})
}
