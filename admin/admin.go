package admin

import (
	"embed"
	"io/fs"
	"net/http"
	"sort"
	"strings"
	"time"
)

//go:embed ui/build/*
var uiFS embed.FS

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
	mux  *http.ServeMux
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
	p.buildMux()
	return p
}

// Register adds or replaces a tab in the panel.
func (p *Panel) Register(tab Tab) {
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

	// Auth API (no admin auth required)
	mux.HandleFunc("POST /admin/auth", p.handleAuth)
	mux.HandleFunc("GET /admin/logout", p.handleLogout)

	// Tab API endpoints (auth required)
	for _, tab := range p.tabs {
		path := "GET /admin/api/tab/" + tab.ID
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

	// Svelte SPA — serve static files from ui/build/, with SPA fallback
	buildFS, _ := fs.Sub(uiFS, "ui/build")
	fileServer := http.FileServer(http.FS(buildFS))

	mux.HandleFunc("/admin/", func(w http.ResponseWriter, r *http.Request) {
		// Strip /admin prefix to get the file path within build/
		path := strings.TrimPrefix(r.URL.Path, "/admin")
		if path == "" || path == "/" {
			path = "/index.html"
		}

		// Try to serve the actual file first
		if path != "/index.html" {
			// Check if file exists in the embedded FS
			f, err := fs.Stat(buildFS, strings.TrimPrefix(path, "/"))
			if err == nil && !f.IsDir() {
				// File exists — serve it with proper caching
				if strings.Contains(path, "/immutable/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				http.StripPrefix("/admin", fileServer).ServeHTTP(w, r)
				return
			}
		}

		// SPA fallback — serve index.html for all routes
		r.URL.Path = "/admin/index.html"
		http.StripPrefix("/admin", fileServer).ServeHTTP(w, r)
	})

	p.mux = mux
}
