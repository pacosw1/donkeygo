package sync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pacosw1/donkeygo/middleware"
)

// ── Mock DB ─────────────────────────────────────────────────────────────────

type mockSyncDB struct {
	tombstones []DeletedEntry
	recorded   []string
	serverTime time.Time
	err        error
}

func (m *mockSyncDB) ServerTime() (time.Time, error) {
	if m.serverTime.IsZero() {
		return time.Now(), m.err
	}
	return m.serverTime, m.err
}
func (m *mockSyncDB) Tombstones(userID string, since time.Time) ([]DeletedEntry, error) {
	return m.tombstones, m.err
}
func (m *mockSyncDB) RecordTombstone(userID, entityType, entityID string) error {
	m.recorded = append(m.recorded, entityType+":"+entityID)
	return m.err
}

// ── Mock EntityHandler ──────────────────────────────────────────────────────

type mockHandler struct {
	entities map[string]any
	items    []BatchResponseItem
	errors   []BatchError
	deleted  []string
}

func (m *mockHandler) ChangedSince(userID string, since time.Time) (map[string]any, error) {
	return m.entities, nil
}
func (m *mockHandler) BatchUpsert(userID string, items []map[string]any) ([]BatchResponseItem, []BatchError) {
	return m.items, m.errors
}
func (m *mockHandler) Delete(userID, entityType, entityID string) error {
	m.deleted = append(m.deleted, entityType+":"+entityID)
	return nil
}

func authReq(method, path, body string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	ctx := context.WithValue(r.Context(), middleware.CtxUserID, "user-1")
	return r.WithContext(ctx)
}

// ── HandleSyncChanges ───────────────────────────────────────────────────────

func TestHandleSyncChanges_FullSync(t *testing.T) {
	db := &mockSyncDB{
		tombstones: []DeletedEntry{
			{EntityType: "task", EntityID: "t1", DeletedAt: time.Now()},
		},
	}
	handler := &mockHandler{
		entities: map[string]any{"tasks": []string{"task1", "task2"}},
	}
	svc := New(db, handler)

	req := authReq("GET", "/api/v1/sync/changes", "")
	w := httptest.NewRecorder()
	svc.HandleSyncChanges(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["synced_at"] == nil {
		t.Fatal("expected synced_at")
	}
	if body["tasks"] == nil {
		t.Fatal("expected tasks from entity handler")
	}
	deleted := body["deleted"].([]any)
	if len(deleted) != 1 {
		t.Fatalf("expected 1 tombstone, got %d", len(deleted))
	}
}

func TestHandleSyncChanges_DeltaSync(t *testing.T) {
	db := &mockSyncDB{tombstones: []DeletedEntry{}}
	svc := New(db, &mockHandler{entities: map[string]any{}})

	req := authReq("GET", "/api/v1/sync/changes?since=2025-01-01T00:00:00Z", "")
	w := httptest.NewRecorder()
	svc.HandleSyncChanges(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleSyncChanges_InvalidSince(t *testing.T) {
	svc := New(&mockSyncDB{}, nil)

	req := authReq("GET", "/api/v1/sync/changes?since=not-a-date", "")
	w := httptest.NewRecorder()
	svc.HandleSyncChanges(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleSyncChanges_NilHandler(t *testing.T) {
	db := &mockSyncDB{tombstones: []DeletedEntry{}}
	svc := New(db, nil)

	req := authReq("GET", "/api/v1/sync/changes", "")
	w := httptest.NewRecorder()
	svc.HandleSyncChanges(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with nil handler, got %d", w.Code)
	}
}

// ── HandleSyncBatch ─────────────────────────────────────────────────────────

func TestHandleSyncBatch_Valid(t *testing.T) {
	handler := &mockHandler{
		items:  []BatchResponseItem{{ClientID: "c1", ServerID: "s1"}},
		errors: nil,
	}
	svc := New(&mockSyncDB{}, handler)

	body := `{"items":[{"client_id":"c1","entity_type":"task","title":"Test"}]}`
	req := authReq("POST", "/api/v1/sync/batch", body)
	w := httptest.NewRecorder()
	svc.HandleSyncBatch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	items := resp["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
}

func TestHandleSyncBatch_Empty(t *testing.T) {
	svc := New(&mockSyncDB{}, &mockHandler{})

	body := `{"items":[]}`
	req := authReq("POST", "/api/v1/sync/batch", body)
	w := httptest.NewRecorder()
	svc.HandleSyncBatch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleSyncBatch_TooMany(t *testing.T) {
	svc := New(&mockSyncDB{}, &mockHandler{})

	items := make([]string, 501)
	for i := range items {
		items[i] = `{"client_id":"c","entity_type":"t"}`
	}
	body := `{"items":[` + strings.Join(items, ",") + `]}`
	req := authReq("POST", "/api/v1/sync/batch", body)
	w := httptest.NewRecorder()
	svc.HandleSyncBatch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for >500 items, got %d", w.Code)
	}
}

func TestHandleSyncBatch_NilHandler(t *testing.T) {
	svc := New(&mockSyncDB{}, nil)

	body := `{"items":[{"client_id":"c1","entity_type":"task"}]}`
	req := authReq("POST", "/api/v1/sync/batch", body)
	w := httptest.NewRecorder()
	svc.HandleSyncBatch(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", w.Code)
	}
}

// ── HandleSyncDelete ────────────────────────────────────────────────────────

func TestHandleSyncDelete_Valid(t *testing.T) {
	db := &mockSyncDB{}
	handler := &mockHandler{}
	svc := New(db, handler)

	req := authReq("DELETE", "/api/v1/sync/task/t1", "")
	req.SetPathValue("entity_type", "task")
	req.SetPathValue("id", "t1")
	w := httptest.NewRecorder()
	svc.HandleSyncDelete(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(db.recorded) != 1 || db.recorded[0] != "task:t1" {
		t.Fatalf("expected tombstone recorded, got %v", db.recorded)
	}
	if len(handler.deleted) != 1 || handler.deleted[0] != "task:t1" {
		t.Fatalf("expected handler delete called, got %v", handler.deleted)
	}
}

func TestHandleSyncDelete_MissingFields(t *testing.T) {
	svc := New(&mockSyncDB{}, nil)

	req := authReq("DELETE", "/api/v1/sync//", "")
	req.SetPathValue("entity_type", "")
	req.SetPathValue("id", "")
	w := httptest.NewRecorder()
	svc.HandleSyncDelete(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestMigrations(t *testing.T) {
	m := Migrations()
	if len(m) == 0 {
		t.Fatal("expected at least one migration")
	}
}
