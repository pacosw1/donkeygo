package admin

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"
)

// intToStr converts an integer to string without strconv.
func intToStr(n int) string {
	if n < 0 {
		return "-" + intToStr(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return intToStr(n/10) + string(rune('0'+n%10))
}

// thirtyDaysAgo returns the time 30 days ago.
func thirtyDaysAgo() time.Time {
	return time.Now().AddDate(0, 0, -30)
}

// renderTemplate is a legacy function for HTMX tab rendering.
// When using the Svelte SPA frontend, these handlers are not called —
// the SPA fetches JSON from /admin/api/* endpoints instead.
func renderTemplate(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<p>Tab content rendered server-side. Use the Svelte SPA for the full experience.</p>")
}

// renderHandlerToString renders an http.Handler's output to a string.
func renderHandlerToString(h http.Handler, r *http.Request) string {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	var buf bytes.Buffer
	buf.ReadFrom(rec.Result().Body)
	return buf.String()
}
