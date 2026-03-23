package admin

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"sort"
	"strings"
	"time"
)

//go:embed templates/*.html templates/partials/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Config configures the admin panel.
type Config struct {
	// Auth
	JWTSecret     string        // Required: secret for signing admin JWT sessions
	SessionExpiry time.Duration // Default: 7 days
	AllowedEmails []string      // Email whitelist for admin access
	AdminKey      string        // Optional: API key for backwards compat / dev access
	Production    bool          // Secure cookies

	// Apple Sign-In (optional — if nil, only AdminKey auth works)
	VerifyToken      func(idToken string) (sub, email string, err error)
	AppleWebClientID string // Apple Services ID for web Sign-In

	// Appearance
	AppName string // Default: "Admin"
}

// ChatProvider is the subset of chat.Service the admin panel needs.
type ChatProvider interface {
	HandleAdminListChats(w http.ResponseWriter, r *http.Request)
	HandleAdminGetChat(w http.ResponseWriter, r *http.Request)
	HandleAdminReplyChat(w http.ResponseWriter, r *http.Request)
	HandleAdminWS(w http.ResponseWriter, r *http.Request)
}

// LogProvider is the interface for logbuf.LogBuffer.
type LogProvider interface {
	Lines(n int) []string
}

// Panel is the admin panel HTTP handler. Mount it on your mux:
//
//	mux.Handle("/admin/", panel)
type Panel struct {
	cfg  Config
	tabs []Tab
	tmpl *template.Template
	mux  *http.ServeMux
}

// templateData is passed to the layout template.
type templateData struct {
	AppName          string
	Tabs             []Tab
	ActiveTab        string
	AppleWebClientID string
}

// New creates a new admin panel.
func New(cfg Config) *Panel {
	if cfg.AppName == "" {
		cfg.AppName = "Admin"
	}
	if cfg.SessionExpiry == 0 {
		cfg.SessionExpiry = 7 * 24 * time.Hour
	}

	p := &Panel{cfg: cfg}

	funcMap := template.FuncMap{
		"timeAgo": func(t time.Time) string {
			d := time.Since(t)
			switch {
			case d < time.Minute:
				return "just now"
			case d < time.Hour:
				return formatDuration(int(d.Minutes()), "minute")
			case d < 24*time.Hour:
				return formatDuration(int(d.Hours()), "hour")
			default:
				return formatDuration(int(d.Hours()/24), "day")
			}
		},
		"shortDate": func(t time.Time) string {
			return t.Format("Jan 2")
		},
		"fullDate": func(t time.Time) string {
			return t.Format("Jan 2, 2006 3:04 PM")
		},
		"lower": strings.ToLower,
		"safeHTML": func(s string) template.HTML {
			return template.HTML(s)
		},
	}

	p.tmpl = template.Must(
		template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/*.html", "templates/partials/*.html"),
	)

	p.buildMux()
	return p
}

func formatDuration(n int, unit string) string {
	s := intToStr(n) + " " + unit
	if n != 1 {
		s += "s"
	}
	return s + " ago"
}

func intToStr(n int) string {
	if n < 0 {
		return "-" + intToStr(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return intToStr(n/10) + string(rune('0'+n%10))
}

// Register adds or replaces a tab in the panel.
func (p *Panel) Register(tab Tab) {
	// Replace if tab with same ID already exists
	for i, t := range p.tabs {
		if t.ID == tab.ID {
			p.tabs[i] = tab
			p.buildMux()
			return
		}
	}
	p.tabs = append(p.tabs, tab)
	sort.Slice(p.tabs, func(i, j int) bool { return p.tabs[i].Order < p.tabs[j].Order })
	p.buildMux()
}

// ServeHTTP implements http.Handler.
func (p *Panel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mux.ServeHTTP(w, r)
}

func (p *Panel) buildMux() {
	mux := http.NewServeMux()

	// Static assets (no auth)
	staticSub, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /admin/static/", http.StripPrefix("/admin/static/", http.FileServerFS(staticSub)))

	// Auth endpoints (no admin auth)
	mux.HandleFunc("POST /admin/auth", p.handleAuth)
	mux.HandleFunc("GET /admin/logout", p.handleLogout)

	// Main page — serves login or layout
	mux.HandleFunc("GET /admin/", p.handleIndex)
	mux.HandleFunc("GET /admin/{tab}", p.handleIndex)

	// Tab fragment endpoints (auth required)
	for _, tab := range p.tabs {
		path := "GET /admin/tab/" + tab.ID
		handler := tab.Handler
		mux.Handle(path, p.requireAdmin(handler))

		// Wire up chat API routes if this is a chat tab
		if ch, ok := handler.(*chatHandler); ok && ch.svc != nil {
			mux.HandleFunc("GET /admin/api/chats", p.requireAdminFunc(ch.svc.HandleAdminListChats))
			mux.HandleFunc("GET /admin/api/chats/{user_id}", p.requireAdminFunc(ch.svc.HandleAdminGetChat))
			mux.HandleFunc("POST /admin/api/chats/{user_id}/reply", p.requireAdminFunc(ch.svc.HandleAdminReplyChat))
			mux.HandleFunc("GET /admin/api/chats/ws", ch.svc.HandleAdminWS)
		}
	}

	p.mux = mux
}

// handleIndex serves the login page or dashboard layout.
func (p *Panel) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if !p.isAuthenticated(r) {
		p.tmpl.ExecuteTemplate(w, "login.html", templateData{
			AppName:          p.cfg.AppName,
			AppleWebClientID: p.cfg.AppleWebClientID,
		})
		return
	}

	// Determine active tab from URL
	activeTab := r.PathValue("tab")
	if activeTab == "" && len(p.tabs) > 0 {
		activeTab = p.tabs[0].ID
	}

	p.tmpl.ExecuteTemplate(w, "layout.html", templateData{
		AppName:   p.cfg.AppName,
		Tabs:      p.tabs,
		ActiveTab: activeTab,
	})
}
