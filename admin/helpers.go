package admin

import (
	"bytes"
	"html/template"
	"net/http"
	"net/http/httptest"
	"time"
)

// renderTemplate executes a named template and writes it to the response.
func renderTemplate(w http.ResponseWriter, name string, data any) {
	// Parse templates fresh for each render to pick up the panel's funcmap.
	// For production this should be cached — but since templates are embedded
	// and small, the parse cost is negligible.
	funcMap := template.FuncMap{
		"timeAgo": func(t time.Time) string {
			d := time.Since(t)
			switch {
			case d < time.Minute:
				return "just now"
			case d < time.Hour:
				return intToStr(int(d.Minutes())) + "m ago"
			case d < 24*time.Hour:
				return intToStr(int(d.Hours())) + "h ago"
			default:
				return intToStr(int(d.Hours()/24)) + "d ago"
			}
		},
		"shortDate": func(t time.Time) string {
			return t.Format("Jan 2")
		},
		"fullDate": func(t time.Time) string {
			return t.Format("Jan 2, 2006 3:04 PM")
		},
		"safeHTML": func(s string) template.HTML {
			return template.HTML(s)
		},
	}

	tmpl := template.Must(
		template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/*.html", "templates/partials/*.html"),
	)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// renderHandlerToString renders an http.Handler's output to a string.
// Used for embedding extra sections in detail views.
func renderHandlerToString(h http.Handler, r *http.Request) string {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	var buf bytes.Buffer
	buf.ReadFrom(rec.Result().Body)
	return buf.String()
}

// thirtyDaysAgo returns the time 30 days ago.
func thirtyDaysAgo() time.Time {
	return time.Now().AddDate(0, 0, -30)
}
