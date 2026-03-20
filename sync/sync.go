// Package sync provides multi-device delta sync with version-based conflict
// detection, idempotent batch writes, and push-triggered propagation.
//
// Devices pull changes on app open via GET /sync/changes and push local
// changes via POST /sync/batch.  After a successful batch the service sends
// silent push notifications to the user's other devices so they pull
// immediately rather than waiting for the next open.
//
// Conflict detection uses optimistic locking: every entity carries a version
// counter.  A batch item with Version=0 is a create; Version>0 is an update
// that succeeds only if the server's current version matches.  On mismatch
// the server returns the item in the errors array with is_conflict=true and
// the current server_version so the client can merge and retry.
package sync

import (
	"log"
	"net/http"
	gosync "sync"
	"time"

	"github.com/pacosw1/donkeygo/httputil"
	"github.com/pacosw1/donkeygo/middleware"
	"github.com/pacosw1/donkeygo/push"
)

// Header keys the client must send.
const (
	HeaderDeviceID       = "X-Device-ID"
	HeaderIdempotencyKey = "X-Idempotency-Key"
)

// ── Database Interface ───────────────────────────────────────────────────────

// SyncDB is the database interface required by the sync package.
type SyncDB interface {
	// ServerTime returns the current database server time.
	ServerTime() (time.Time, error)
	// Tombstones returns deleted entity records since the given time for a user.
	Tombstones(userID string, since time.Time) ([]DeletedEntry, error)
	// RecordTombstone records a deletion for sync propagation.
	RecordTombstone(userID, entityType, entityID string) error
}

// ── Entity Handler Interface ─────────────────────────────────────────────────

// EntityHandler handles sync operations for app-specific entity types.
// Apps implement this interface to define what entities are synced and how
// conflicts are resolved.
type EntityHandler interface {
	// ChangedSince returns entities changed since the given time for a user.
	// Returns a map of entity type → slice of entities.
	// If excludeDeviceID is non-empty, changes originating from that device
	// should be excluded so a device does not receive its own writes back.
	ChangedSince(userID string, since time.Time, excludeDeviceID string) (map[string]any, error)

	// BatchUpsert processes a batch of entity upserts with version-based
	// conflict detection.  deviceID identifies the originating device for
	// change tracking.
	//
	// Items with Version==0 are creates.  Items with Version>0 are updates;
	// the implementation must compare the submitted version against the
	// server's current version and return a BatchError with IsConflict=true
	// and ServerVersion set when they diverge.
	BatchUpsert(userID, deviceID string, items []BatchItem) ([]BatchResponseItem, []BatchError)

	// Delete removes an entity.
	Delete(userID, entityType, entityID string) error
}

// ── Types ────────────────────────────────────────────────────────────────────

// DeletedEntry represents a tombstone record.
type DeletedEntry struct {
	EntityType string    `json:"entity_type"`
	EntityID   string    `json:"entity_id"`
	DeletedAt  time.Time `json:"deleted_at"`
}

// BatchItem is a single entity in a sync batch request.
type BatchItem struct {
	ClientID   string         `json:"client_id"`
	EntityType string         `json:"entity_type"`
	EntityID   string         `json:"entity_id,omitempty"` // set for updates, empty for creates
	Version    int            `json:"version"`             // 0 = create, >0 = expected server version
	Fields     map[string]any `json:"fields"`
}

// BatchResponseItem maps a client ID to a server ID with the new version.
type BatchResponseItem struct {
	ClientID string `json:"client_id"`
	ServerID string `json:"server_id"`
	Version  int    `json:"version"`
}

// BatchError records a sync error for a specific item.
type BatchError struct {
	ClientID      string `json:"client_id"`
	Error         string `json:"error"`
	IsConflict    bool   `json:"is_conflict,omitempty"`
	ServerVersion int    `json:"server_version,omitempty"`
}

// BatchResponse is the full response from a batch operation, also used as the
// cached value for idempotency deduplication.
type BatchResponse struct {
	Items    []BatchResponseItem `json:"items"`
	Errors   []BatchError        `json:"errors"`
	SyncedAt time.Time           `json:"synced_at"`
}

// ── Device Push Integration ──────────────────────────────────────────────────

// DeviceTokenStore provides access to a user's registered push tokens so the
// sync service can notify other devices after a successful batch.
type DeviceTokenStore interface {
	// EnabledTokensForUser returns all active push tokens for the given user.
	EnabledTokensForUser(userID string) ([]DeviceInfo, error)
}

// DeviceInfo holds the minimal device information needed for sync push.
type DeviceInfo struct {
	DeviceID string
	Token    string
}

// ── Configuration ────────────────────────────────────────────────────────────

// Config configures optional sync service features.
type Config struct {
	// Push provider for sending silent sync notifications.  Optional.
	Push push.Provider
	// DeviceTokens provides device tokens for push.  Required when Push is set.
	DeviceTokens DeviceTokenStore
	// IdempotencyTTL controls how long completed batch results are cached for
	// deduplication.  Default: 24 hours.
	IdempotencyTTL time.Duration
}

// ── Service ──────────────────────────────────────────────────────────────────

// Service provides sync handlers.
type Service struct {
	db      SyncDB
	handler EntityHandler
	push    push.Provider
	tokens  DeviceTokenStore

	// In-memory idempotency cache.
	idempMu    gosync.RWMutex
	idempCache map[string]*idempEntry
	idempTTL   time.Duration
	stop       chan struct{}
	pushWg     gosync.WaitGroup
}

type idempEntry struct {
	resp      *BatchResponse
	expiresAt time.Time
}

// New creates a sync service.  Pass an optional Config to enable push
// notifications and idempotency settings.
func New(db SyncDB, handler EntityHandler, cfg ...Config) *Service {
	s := &Service{
		db:         db,
		handler:    handler,
		idempCache: make(map[string]*idempEntry),
		idempTTL:   24 * time.Hour,
		stop:       make(chan struct{}),
	}
	if len(cfg) > 0 {
		c := cfg[0]
		s.push = c.Push
		s.tokens = c.DeviceTokens
		if c.IdempotencyTTL > 0 {
			s.idempTTL = c.IdempotencyTTL
		}
	}
	go s.cleanupIdempotency()
	return s
}

// Close stops background goroutines and waits for in-flight push
// notifications to finish.
func (s *Service) Close() {
	close(s.stop)
	s.pushWg.Wait()
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
// The client should send X-Device-ID to exclude its own recent writes.
func (s *Service) HandleSyncChanges(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)
	deviceID := r.Header.Get(HeaderDeviceID)

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

	// Get tombstones.
	deleted, err := s.db.Tombstones(userID, since)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to query tombstones")
		return
	}
	if deleted == nil {
		deleted = []DeletedEntry{}
	}

	result := map[string]any{
		"deleted":   deleted,
		"synced_at": syncedAt,
	}

	// Get changed entities from app-specific handler.
	if s.handler != nil {
		entities, err := s.handler.ChangedSince(userID, since, deviceID)
		if err != nil {
			httputil.WriteError(w, http.StatusInternalServerError, "failed to query changes")
			return
		}
		for k, v := range entities {
			if k == "deleted" || k == "synced_at" {
				continue
			}
			result[k] = v
		}
	}

	httputil.WriteJSON(w, http.StatusOK, result)
}

// HandleSyncBatch handles POST /api/v1/sync/batch.
//
// The client must send X-Device-ID to identify the originating device.
// The client should send X-Idempotency-Key (a UUID) so that retried requests
// return the cached result instead of re-applying writes.
//
// Request body:
//
//	{"items": [{"client_id":"…","entity_type":"…","version":0,"fields":{…}}, …]}
//
// Response:
//
//	{"items": [...], "errors": [...], "synced_at": "…"}
func (s *Service) HandleSyncBatch(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)
	deviceID := r.Header.Get(HeaderDeviceID)
	rawIdempKey := r.Header.Get(HeaderIdempotencyKey)

	// Scope idempotency key to user to prevent cross-user cache hits.
	idempKey := ""
	if rawIdempKey != "" {
		idempKey = userID + ":" + rawIdempKey
	}

	// Check idempotency cache.
	if idempKey != "" {
		if cached := s.getIdempotency(idempKey); cached != nil {
			httputil.WriteJSON(w, http.StatusOK, cached)
			return
		}
	}

	var req struct {
		Items []BatchItem `json:"items"`
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

	// Validate items.
	for i, item := range req.Items {
		if item.ClientID == "" {
			httputil.WriteError(w, http.StatusBadRequest, "items[].client_id is required")
			return
		}
		if item.EntityType == "" {
			httputil.WriteError(w, http.StatusBadRequest, "items[].entity_type is required")
			return
		}
		if item.Version < 0 {
			httputil.WriteError(w, http.StatusBadRequest, "items[].version must be >= 0")
			return
		}
		if item.Fields == nil {
			req.Items[i].Fields = make(map[string]any)
		}
	}

	if s.handler == nil {
		httputil.WriteError(w, http.StatusNotImplemented, "sync handler not configured")
		return
	}

	syncedAt, err := s.db.ServerTime()
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to get server time")
		return
	}

	items, errors := s.handler.BatchUpsert(userID, deviceID, req.Items)

	resp := &BatchResponse{
		Items:    items,
		Errors:   errors,
		SyncedAt: syncedAt,
	}

	// Ensure non-nil slices for JSON.
	if resp.Items == nil {
		resp.Items = []BatchResponseItem{}
	}
	if resp.Errors == nil {
		resp.Errors = []BatchError{}
	}

	// Cache for idempotency.
	if idempKey != "" {
		s.setIdempotency(idempKey, resp)
	}

	// Notify other devices if we have successful writes and push is configured.
	if len(resp.Items) > 0 {
		s.notifyOtherDevices(userID, deviceID)
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// HandleSyncDelete handles DELETE /api/v1/sync/{entity_type}/{id}.
func (s *Service) HandleSyncDelete(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)
	deviceID := r.Header.Get(HeaderDeviceID)
	entityType := r.PathValue("entity_type")
	entityID := r.PathValue("id")

	if entityType == "" || entityID == "" {
		httputil.WriteError(w, http.StatusBadRequest, "entity_type and id are required")
		return
	}

	if s.handler != nil {
		if err := s.handler.Delete(userID, entityType, entityID); err != nil {
			httputil.WriteError(w, http.StatusInternalServerError, "failed to delete entity")
			return
		}
	}

	if err := s.db.RecordTombstone(userID, entityType, entityID); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to record tombstone")
		return
	}

	// Notify other devices.
	s.notifyOtherDevices(userID, deviceID)

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ── Push Notification ───────────────────────────────────────────────────────

// notifyOtherDevices sends a silent push to all of the user's devices except
// the one that originated the change.  Runs in a goroutine to avoid blocking
// the HTTP response.  Errors are logged, never surfaced to the client.
func (s *Service) notifyOtherDevices(userID, excludeDeviceID string) {
	if s.push == nil || s.tokens == nil {
		return
	}

	s.pushWg.Add(1)
	go func() {
		defer s.pushWg.Done()

		devices, err := s.tokens.EnabledTokensForUser(userID)
		if err != nil {
			log.Printf("[sync] failed to get device tokens for %s: %v", userID, err)
			return
		}

		data := map[string]string{"action": "sync"}
		for _, d := range devices {
			if d.DeviceID == excludeDeviceID {
				continue
			}
			if err := s.push.SendSilent(d.Token, data); err != nil {
				log.Printf("[sync] silent push failed for device %s: %v", d.DeviceID, err)
			}
		}
	}()
}

// ── Idempotency Cache ───────────────────────────────────────────────────────

func (s *Service) getIdempotency(key string) *BatchResponse {
	s.idempMu.RLock()
	defer s.idempMu.RUnlock()
	if entry, ok := s.idempCache[key]; ok && time.Now().Before(entry.expiresAt) {
		return entry.resp
	}
	return nil
}

func (s *Service) setIdempotency(key string, resp *BatchResponse) {
	s.idempMu.Lock()
	defer s.idempMu.Unlock()
	s.idempCache[key] = &idempEntry{
		resp:      resp,
		expiresAt: time.Now().Add(s.idempTTL),
	}
}

func (s *Service) cleanupIdempotency() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.idempMu.Lock()
			now := time.Now()
			for k, entry := range s.idempCache {
				if now.After(entry.expiresAt) {
					delete(s.idempCache, k)
				}
			}
			s.idempMu.Unlock()
		case <-s.stop:
			return
		}
	}
}
