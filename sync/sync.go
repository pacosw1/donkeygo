// Package sync provides multi-device delta sync with tombstones and batch operations.
package sync

import (
	"net/http"
	"time"

	"github.com/pacosw1/donkeygo/httputil"
	"github.com/pacosw1/donkeygo/middleware"
)

// SyncDB is the database interface required by the sync package.
type SyncDB interface {
	// ServerTime returns the current database server time.
	ServerTime() (time.Time, error)
	// Tombstones returns deleted entity records since the given time for a user.
	Tombstones(userID string, since time.Time) ([]DeletedEntry, error)
	// RecordTombstone records a deletion for sync.
	RecordTombstone(userID, entityType, entityID string) error
}

// EntityHandler handles sync operations for app-specific entity types.
// Apps implement this to define what entities are synced.
type EntityHandler interface {
	// ChangedSince returns entities changed since the given time for a user.
	// Returns a map of entity type -> slice of entities.
	ChangedSince(userID string, since time.Time) (map[string]any, error)
	// BatchUpsert processes a batch of entity upserts.
	// Returns server IDs for each client ID, and any errors.
	BatchUpsert(userID string, items []map[string]any) ([]BatchResponseItem, []BatchError)
	// Delete removes an entity and records a tombstone.
	Delete(userID, entityType, entityID string) error
}

// DeletedEntry represents a tombstone record.
type DeletedEntry struct {
	EntityType string    `json:"entity_type"`
	EntityID   string    `json:"entity_id"`
	DeletedAt  time.Time `json:"deleted_at"`
}

// BatchResponseItem maps a client ID to a server ID.
type BatchResponseItem struct {
	ClientID string `json:"client_id"`
	ServerID string `json:"server_id"`
}

// BatchError records a sync error for a specific item.
type BatchError struct {
	ClientID string `json:"client_id"`
	Error    string `json:"error"`
}

// Service provides sync handlers.
type Service struct {
	db      SyncDB
	handler EntityHandler
}

// New creates a sync service.
func New(db SyncDB, handler EntityHandler) *Service {
	return &Service{db: db, handler: handler}
}

// Migrations returns the SQL migrations needed by the sync package.
func Migrations() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS tombstones (
			entity_type VARCHAR NOT NULL,
			entity_id   TEXT NOT NULL,
			user_id     TEXT NOT NULL,
			deleted_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (entity_type, entity_id, user_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tombstones_user_deleted ON tombstones(user_id, deleted_at)`,
	}
}

// ── Handlers ────────────────────────────────────────────────────────────────

// HandleSyncChanges handles GET /api/v1/sync/changes?since={ISO8601}.
func (s *Service) HandleSyncChanges(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)

	syncedAt, err := s.db.ServerTime()
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to get server time")
		return
	}

	sinceStr := r.URL.Query().Get("since")
	since := time.Time{}
	if sinceStr != "" {
		parsed, err := time.Parse(time.RFC3339Nano, sinceStr)
		if err != nil {
			parsed, err = time.Parse(time.RFC3339, sinceStr)
			if err != nil {
				httputil.WriteError(w, http.StatusBadRequest, "invalid 'since' format, use ISO8601")
				return
			}
		}
		since = parsed
	}

	// Get tombstones
	deleted, err := s.db.Tombstones(userID, since)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to query tombstones")
		return
	}

	// Get changed entities from app-specific handler
	result := map[string]any{
		"deleted":   deleted,
		"synced_at": syncedAt,
	}

	if s.handler != nil {
		entities, err := s.handler.ChangedSince(userID, since)
		if err == nil {
			for k, v := range entities {
				result[k] = v
			}
		}
	}

	httputil.WriteJSON(w, http.StatusOK, result)
}

// HandleSyncBatch handles POST /api/v1/sync/batch.
func (s *Service) HandleSyncBatch(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)

	var req struct {
		Items []map[string]any `json:"items"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Items) == 0 {
		httputil.WriteError(w, http.StatusBadRequest, "items array is required")
		return
	}
	if len(req.Items) > 500 {
		httputil.WriteError(w, http.StatusBadRequest, "maximum 500 items per batch")
		return
	}

	if s.handler == nil {
		httputil.WriteError(w, http.StatusNotImplemented, "sync handler not configured")
		return
	}

	items, errors := s.handler.BatchUpsert(userID, req.Items)

	httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"errors": errors,
	})
}

// HandleSyncDelete handles DELETE /api/v1/sync/{entity_type}/{id}.
func (s *Service) HandleSyncDelete(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)
	entityType := r.PathValue("entity_type")
	entityID := r.PathValue("id")

	if entityType == "" || entityID == "" {
		httputil.WriteError(w, http.StatusBadRequest, "entity_type and id are required")
		return
	}

	if s.handler != nil {
		_ = s.handler.Delete(userID, entityType, entityID)
	}

	s.db.RecordTombstone(userID, entityType, entityID)

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
