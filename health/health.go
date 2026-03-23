// Package health provides liveness and readiness check endpoints.
//
// Usage:
//
//	healthSvc := health.New(health.Config{
//	    Checks: []health.Check{
//	        {Name: "db", Fn: func() error { return db.Ping() }},
//	        {Name: "redis", Fn: func() error { return redis.Ping(ctx).Err() }},
//	    },
//	})
//	mux.HandleFunc("GET /health", healthSvc.HandleHealth)
//	mux.HandleFunc("GET /ready", healthSvc.HandleReady)
package health

import (
	"net/http"

	"github.com/pacosw1/donkeygo/httputil"
)

// Check is a named health check.
type Check struct {
	Name string
	Fn   func() error
}

// Config holds health check configuration.
type Config struct {
	Checks []Check
}

// Service provides health check handlers.
type Service struct {
	cfg Config
}

// New creates a health check service.
func New(cfg Config) *Service {
	return &Service{cfg: cfg}
}

// HandleHealth handles GET /health — always returns 200. Use as a liveness probe.
func (s *Service) HandleHealth(w http.ResponseWriter, r *http.Request) {
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleReady handles GET /ready — runs all checks, returns 503 if any fail.
func (s *Service) HandleReady(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{}
	allOk := true

	for _, c := range s.cfg.Checks {
		if err := c.Fn(); err != nil {
			checks[c.Name] = err.Error()
			allOk = false
		} else {
			checks[c.Name] = "ok"
		}
	}

	if !allOk {
		httputil.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "not_ready",
			"checks": checks,
		})
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"status": "ready",
		"checks": checks,
	})
}
