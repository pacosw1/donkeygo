// Package paywall provides server-driven paywall configuration with multi-locale support.
package paywall

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/pacosw1/donkeygo/httputil"
)

// Feature describes a paywall feature item.
type Feature struct {
	Emoji string `json:"emoji"`
	Color string `json:"color"`
	Text  string `json:"text"`
	Bold  string `json:"bold"`
}

// Review describes a paywall review item.
type Review struct {
	Title       string `json:"title"`
	Username    string `json:"username"`
	TimeLabel   string `json:"time_label"`
	Description string `json:"description"`
	Rating      int    `json:"rating"`
}

// Config holds the paywall configuration for a single locale.
type Config struct {
	Headline       string    `json:"headline"`
	HeadlineAccent string    `json:"headline_accent"`
	Subtitle       string    `json:"subtitle"`
	MemberCount    string    `json:"member_count"`
	Rating         string    `json:"rating"`
	Features       []Feature `json:"features"`
	Reviews        []Review  `json:"reviews"`
	FooterText     string    `json:"footer_text"`
	TrialText      string    `json:"trial_text"`
	CTAText        string    `json:"cta_text"`
	Version        int       `json:"version"`
}

// Store holds localized paywall configurations.
type Store struct {
	mu      sync.RWMutex
	configs map[string]*Config
}

// NewStore creates a paywall store with optional initial configs.
func NewStore(initial map[string]*Config) *Store {
	if initial == nil {
		initial = make(map[string]*Config)
	}
	return &Store{configs: initial}
}

// Get returns the paywall config for a locale, with fallback.
func (s *Store) Get(locale string) *Config {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if cfg, ok := s.configs[locale]; ok {
		return cfg
	}
	// Try language prefix
	if len(locale) >= 2 {
		if cfg, ok := s.configs[locale[:2]]; ok {
			return cfg
		}
	}
	// Fallback to English
	if cfg, ok := s.configs["en"]; ok {
		return cfg
	}
	return nil
}

// Set updates the paywall config for a locale, auto-incrementing the version.
func (s *Store) Set(locale string, cfg *Config) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.configs[locale]; ok {
		cfg.Version = existing.Version + 1
	} else {
		cfg.Version = 1
	}
	s.configs[locale] = cfg
}

// HandleGetConfig returns an http.HandlerFunc for GET /api/v1/paywall/config?locale=en.
func HandleGetConfig(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		locale := r.URL.Query().Get("locale")
		if locale == "" {
			locale = "en"
		}

		config := store.Get(locale)
		if config == nil {
			httputil.WriteError(w, http.StatusNotFound, "no paywall config available")
			return
		}

		httputil.WriteJSON(w, http.StatusOK, config)
	}
}

// HandleUpdateConfig returns an http.HandlerFunc for PUT /admin/api/paywall/config?locale=en.
func HandleUpdateConfig(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		locale := r.URL.Query().Get("locale")
		if locale == "" {
			locale = "en"
		}

		var config Config
		if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
			httputil.WriteError(w, http.StatusBadRequest, "invalid config JSON")
			return
		}

		store.Set(locale, &config)

		httputil.WriteJSON(w, http.StatusOK, map[string]any{
			"status":  "updated",
			"locale":  locale,
			"version": config.Version,
		})
	}
}
