package admin

import (
	"net/http"

	"github.com/pacosw1/donkeygo/analytics"
)

// OverviewConfig configures the built-in Overview tab.
type OverviewConfig struct {
	ExtraCards []Card // Additional stat cards appended after built-in ones
}

// OverviewTab creates the built-in Overview tab showing DAU, MAU, subscriptions, and stat cards.
func OverviewTab(db analytics.AnalyticsDB, cfg ...OverviewConfig) Tab {
	var c OverviewConfig
	if len(cfg) > 0 {
		c = cfg[0]
	}
	return Tab{
		ID:      "overview",
		Label:   "Overview",
		Icon:    "chart",
		Handler: &overviewHandler{db: db, cfg: c},
		Order:   10,
	}
}

type overviewHandler struct {
	db  analytics.AnalyticsDB
	cfg OverviewConfig
	p   *Panel
}

type overviewData struct {
	Cards []cardData
	DAU   []dauBar
}

type cardData struct {
	Label string
	Value string
	Color string
}

type dauBar struct {
	Date      string
	DAU       int
	HeightPct float64
}

func (h *overviewHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	dauToday, _ := h.db.DAUToday()
	mau, _ := h.db.MAU()
	totalUsers, _ := h.db.TotalUsers()
	activeSubs, _ := h.db.ActiveSubscriptions()

	cards := []cardData{
		{Label: "DAU", Value: intToStr(dauToday), Color: "cyan"},
		{Label: "MAU", Value: intToStr(mau), Color: "cyan"},
		{Label: "Total Users", Value: intToStr(totalUsers), Color: "gold"},
		{Label: "Active Subscriptions", Value: intToStr(activeSubs), Color: "green"},
	}

	// Append app-specific extra cards
	for _, c := range h.cfg.ExtraCards {
		cards = append(cards, cardData{
			Label: c.Label,
			Value: c.Value(),
			Color: c.Color,
		})
	}

	// DAU chart data
	rows, _ := h.db.DAUTimeSeries(thirtyDaysAgo())
	maxDAU := 1
	for _, r := range rows {
		if r.DAU > maxDAU {
			maxDAU = r.DAU
		}
	}
	bars := make([]dauBar, len(rows))
	for i, r := range rows {
		bars[i] = dauBar{
			Date:      r.Date,
			DAU:       r.DAU,
			HeightPct: float64(r.DAU) / float64(maxDAU) * 100,
		}
	}

	data := overviewData{Cards: cards, DAU: bars}
	renderTemplate(w, "overview.html", data)
}
