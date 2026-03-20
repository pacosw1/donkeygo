package logbuf

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLogBuffer_Write(t *testing.T) {
	buf := New(5)
	buf.Write([]byte("line1\nline2\nline3\n"))

	lines := buf.Lines(10)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "line1" || lines[2] != "line3" {
		t.Fatalf("unexpected lines: %v", lines)
	}
}

func TestLogBuffer_RingWrap(t *testing.T) {
	buf := New(3)
	buf.Write([]byte("a\nb\nc\nd\ne\n"))

	lines := buf.Lines(3)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "c" || lines[1] != "d" || lines[2] != "e" {
		t.Fatalf("expected [c d e], got %v", lines)
	}
}

func TestLogBuffer_LinesLimit(t *testing.T) {
	buf := New(10)
	buf.Write([]byte("a\nb\nc\nd\ne\n"))

	lines := buf.Lines(2)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0] != "d" || lines[1] != "e" {
		t.Fatalf("expected [d e], got %v", lines)
	}
}

func TestHandleAdminLogs(t *testing.T) {
	buf := New(100)
	buf.Write([]byte("info: started\nerror: something broke\ninfo: done\n"))

	handler := HandleAdminLogs(buf)

	// Without filter
	req := httptest.NewRequest("GET", "/admin/api/logs", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if int(body["count"].(float64)) != 3 {
		t.Fatalf("expected 3 lines, got %v", body["count"])
	}

	// With filter
	req = httptest.NewRequest("GET", "/admin/api/logs?filter=error", nil)
	w = httptest.NewRecorder()
	handler(w, req)

	json.Unmarshal(w.Body.Bytes(), &body)
	if int(body["count"].(float64)) != 1 {
		t.Fatalf("expected 1 filtered line, got %v", body["count"])
	}
}
