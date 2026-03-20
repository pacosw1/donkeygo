package paywall

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStore_GetFallback(t *testing.T) {
	store := NewStore(map[string]*Config{
		"en": {Headline: "Unlock", Version: 1},
		"es": {Headline: "Desbloquea", Version: 1},
	})

	// Exact match
	cfg := store.Get("es")
	if cfg.Headline != "Desbloquea" {
		t.Fatalf("expected Desbloquea, got %s", cfg.Headline)
	}

	// Language prefix fallback
	cfg = store.Get("es-MX")
	if cfg.Headline != "Desbloquea" {
		t.Fatalf("expected Desbloquea for es-MX, got %s", cfg.Headline)
	}

	// English fallback
	cfg = store.Get("xx")
	if cfg.Headline != "Unlock" {
		t.Fatalf("expected Unlock for unknown locale, got %s", cfg.Headline)
	}
}

func TestStore_SetVersionIncrement(t *testing.T) {
	store := NewStore(map[string]*Config{
		"en": {Headline: "V1", Version: 1},
	})

	store.Set("en", &Config{Headline: "V2"})
	cfg := store.Get("en")
	if cfg.Version != 2 {
		t.Fatalf("expected version 2, got %d", cfg.Version)
	}
	if cfg.Headline != "V2" {
		t.Fatalf("expected V2, got %s", cfg.Headline)
	}

	// New locale starts at 1
	store.Set("fr", &Config{Headline: "Débloquer"})
	cfg = store.Get("fr")
	if cfg.Version != 1 {
		t.Fatalf("expected version 1 for new locale, got %d", cfg.Version)
	}
}

func TestHandleGetConfig(t *testing.T) {
	store := NewStore(map[string]*Config{
		"en": {Headline: "Unlock", Features: []Feature{{Emoji: "✨", Text: "Features"}}},
	})

	handler := HandleGetConfig(store)

	req := httptest.NewRequest("GET", "/api/v1/paywall/config?locale=en", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var config Config
	json.Unmarshal(w.Body.Bytes(), &config)
	if config.Headline != "Unlock" {
		t.Fatalf("expected Unlock, got %s", config.Headline)
	}
}

func TestHandleUpdateConfig(t *testing.T) {
	store := NewStore(map[string]*Config{
		"en": {Headline: "V1", Version: 1},
	})

	handler := HandleUpdateConfig(store)

	body := `{"headline":"V2","headline_accent":"Everything"}`
	req := httptest.NewRequest("PUT", "/admin/api/paywall/config?locale=en", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	cfg := store.Get("en")
	if cfg.Headline != "V2" {
		t.Fatalf("expected V2, got %s", cfg.Headline)
	}
	if cfg.Version != 2 {
		t.Fatalf("expected version 2, got %d", cfg.Version)
	}
}
