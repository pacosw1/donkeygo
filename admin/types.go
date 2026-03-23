// Package admin provides a pre-built, extensible admin panel for donkeygo backends.
// Uses html/template + HTMX for server-rendered pages with zero JavaScript build step.
//
// Usage:
//
//	panel := admin.New(admin.Config{
//	    JWTSecret:     "secret",
//	    AllowedEmails: []string{"admin@example.com"},
//	    Production:    true,
//	})
//	panel.Register(admin.OverviewTab(analyticsSvc))
//	panel.Register(admin.UsersTab(adminDB))
//	panel.Register(admin.LogsTab(logBuf))
//	mux.Handle("/admin/", panel)
package admin

import "net/http"

// Tab represents a navigable admin panel tab.
type Tab struct {
	ID      string       // URL-safe identifier, e.g. "users"
	Label   string       // Display label, e.g. "Users"
	Icon    string       // Icon name (maps to SVG in template)
	Handler http.Handler // Returns HTML fragment for the tab content area
	Order   int          // Sort order (built-in tabs use 10, 20, 30...)
}

// Column defines an extra column appended to a table-based tab.
type Column struct {
	Header string             // Column header text
	Value  func(row any) string // Extracts cell value from the row
}

// Section defines an extra section appended to a detail view.
type Section struct {
	Title   string       // Section heading
	Handler http.Handler // Returns HTML fragment for this section
}

// Card defines a stat card for the overview tab.
type Card struct {
	Label string        // Card label, e.g. "Revenue"
	Value func() string // Dynamic value getter (called at render time)
	Color string        // CSS class: "cyan", "gold", "green", "red"
}
