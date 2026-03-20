// Package logbuf provides a ring-buffer log capture for admin panels.
package logbuf

import (
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/pacosw1/donkeygo/httputil"
)

// LogBuffer captures log output in a circular buffer.
type LogBuffer struct {
	mu    sync.RWMutex
	lines []string
	pos   int
	cap   int
	full  bool
}

// New creates a LogBuffer with the given capacity.
func New(capacity int) *LogBuffer {
	return &LogBuffer{
		lines: make([]string, capacity),
		cap:   capacity,
	}
}

// Write implements io.Writer to capture log output.
func (b *LogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	text := string(p)
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if line == "" {
			continue
		}
		b.lines[b.pos] = line
		b.pos = (b.pos + 1) % b.cap
		if b.pos == 0 {
			b.full = true
		}
	}
	return len(p), nil
}

// Lines returns the last n lines from the buffer.
func (b *LogBuffer) Lines(n int) []string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	total := b.pos
	if b.full {
		total = b.cap
	}
	if n <= 0 || n > total {
		n = total
	}

	result := make([]string, 0, n)
	if b.full {
		start := (b.pos - n + b.cap) % b.cap
		for i := 0; i < n; i++ {
			idx := (start + i) % b.cap
			result = append(result, b.lines[idx])
		}
	} else {
		start := b.pos - n
		if start < 0 {
			start = 0
		}
		for i := start; i < b.pos; i++ {
			result = append(result, b.lines[i])
		}
	}
	return result
}

// SetupLogCapture redirects Go's standard log output to both the buffer and the original writer.
func SetupLogCapture(buf *LogBuffer) {
	log.SetOutput(io.MultiWriter(buf, log.Writer()))
}

// HandleAdminLogs returns an http.HandlerFunc that serves buffered log lines.
// Query params: ?limit=500&filter=error
func HandleAdminLogs(buf *LogBuffer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n := 500
		if v := r.URL.Query().Get("limit"); v != "" {
			if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= 5000 {
				n = parsed
			}
		}

		filter := r.URL.Query().Get("filter")
		lines := buf.Lines(n)

		if filter != "" {
			filtered := make([]string, 0, len(lines))
			lower := strings.ToLower(filter)
			for _, l := range lines {
				if strings.Contains(strings.ToLower(l), lower) {
					filtered = append(filtered, l)
				}
			}
			lines = filtered
		}

		httputil.WriteJSON(w, http.StatusOK, map[string]any{"lines": lines, "count": len(lines)})
	}
}
