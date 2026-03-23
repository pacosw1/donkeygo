package admin

import (
	"net/http"

	"github.com/pacosw1/donkeygo/analytics"
)

// SubscriptionsConfig configures the built-in Subscriptions tab.
type SubscriptionsConfig struct {
	ExtraCards []Card // Additional stat cards
}

// SubscriptionsTab creates the built-in Subscriptions tab with breakdown and stats.
func SubscriptionsTab(analyticsDB analytics.AnalyticsDB, adminDB AdminDB, cfg ...SubscriptionsConfig) Tab {
	var c SubscriptionsConfig
	if len(cfg) > 0 {
		c = cfg[0]
	}
	return Tab{
		ID:      "subscriptions",
		Label:   "Subscriptions",
		Icon:    "credit-card",
		Handler: &subscriptionsHandler{analyticsDB: analyticsDB, adminDB: adminDB, cfg: c},
		Order:   40,
	}
}

type subscriptionsHandler struct {
	analyticsDB analytics.AnalyticsDB
	adminDB     AdminDB
	cfg         SubscriptionsConfig
}

type subscriptionsData struct {
	Breakdown []SubBreakdownRow
	Cards     []cardData
}

func (h *subscriptionsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	breakdown, _ := h.adminDB.AdminSubscriptionBreakdown()

	activeSubs, _ := h.analyticsDB.ActiveSubscriptions()
	newSubs, _ := h.analyticsDB.NewSubscriptions30d()
	churned, _ := h.analyticsDB.ChurnedSubscriptions30d()

	cards := []cardData{
		{Label: "Active", Value: intToStr(activeSubs), Color: "green"},
		{Label: "New (30d)", Value: intToStr(newSubs), Color: "cyan"},
		{Label: "Churned (30d)", Value: intToStr(churned), Color: "red"},
	}

	for _, c := range h.cfg.ExtraCards {
		cards = append(cards, cardData{
			Label: c.Label,
			Value: c.Value(),
			Color: c.Color,
		})
	}

	renderTemplate(w, "subscriptions.html", subscriptionsData{
		Breakdown: breakdown,
		Cards:     cards,
	})
}
