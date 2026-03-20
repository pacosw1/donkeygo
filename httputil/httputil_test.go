package httputil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, http.StatusOK, map[string]string{"key": "value"})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}

	var body map[string]string
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["key"] != "value" {
		t.Fatalf("expected key=value, got %s", body["key"])
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, http.StatusBadRequest, "test error")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	var body map[string]string
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["error"] != "test error" {
		t.Fatalf("expected error=test error, got %s", body["error"])
	}
}

func TestDecodeJSON(t *testing.T) {
	body := strings.NewReader(`{"name":"test"}`)
	req := httptest.NewRequest("POST", "/", body)

	var v struct {
		Name string `json:"name"`
	}
	if err := DecodeJSON(req, &v); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Name != "test" {
		t.Fatalf("expected name=test, got %s", v.Name)
	}
}

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		remoteIP string
		want     string
	}{
		{"X-Forwarded-For", map[string]string{"X-Forwarded-For": "1.2.3.4"}, "5.6.7.8:1234", "1.2.3.4"},
		{"X-Real-IP", map[string]string{"X-Real-IP": "9.8.7.6"}, "5.6.7.8:1234", "9.8.7.6"},
		{"RemoteAddr", map[string]string{}, "5.6.7.8:1234", "5.6.7.8:1234"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteIP
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			if got := GetClientIP(req); got != tt.want {
				t.Fatalf("expected %s, got %s", tt.want, got)
			}
		})
	}
}
