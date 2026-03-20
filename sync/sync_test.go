package sync

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	gosync "sync"
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
	entities          map[string]any
	changedSinceErr   error
	items             []BatchResponseItem
	errors            []BatchError
	deleted           []string
	deleteErr         error
	lastDeviceID      string
	lastExcludeDevice string
}

func (m *mockHandler) ChangedSince(userID string, since time.Time, excludeDeviceID string) (map[string]any, error) {
	m.lastExcludeDevice = excludeDeviceID
	return m.entities, m.changedSinceErr
}
func (m *mockHandler) BatchUpsert(userID, deviceID string, items []BatchItem) ([]BatchResponseItem, []BatchError) {
	m.lastDeviceID = deviceID
	return m.items, m.errors
}
func (m *mockHandler) Delete(userID, entityType, entityID string) error {
	m.deleted = append(m.deleted, entityType+":"+entityID)
	return m.deleteErr
}

// ── Mock Push Provider (thread-safe) ────────────────────────────────────────

type mockPush struct {
	mu          gosync.Mutex
	silentCalls []string
}

func (m *mockPush) Send(token, title, body string) error                                 { return nil }
func (m *mockPush) SendWithData(token, title, body string, data map[string]string) error { return nil }
func (m *mockPush) SendSilent(token string, data map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.silentCalls = append(m.silentCalls, token)
	return nil
}

func (m *mockPush) getSilentCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]string, len(m.silentCalls))
	copy(cp, m.silentCalls)
	return cp
}

// ── Mock DeviceTokenStore ───────────────────────────────────────────────────

type mockTokenStore struct {
	devices []DeviceInfo
}

func (m *mockTokenStore) EnabledTokensForUser(userID string) ([]DeviceInfo, error) {
	return m.devices, nil
}

// ── Helpers ─────────────────────────────────────────────────────────────────

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

func authReqAs(method, path, body, userID string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	ctx := context.WithValue(r.Context(), middleware.CtxUserID, userID)
	return r.WithContext(ctx)
}

func authReqWithDevice(method, path, body, deviceID string) *http.Request {
	r := authReq(method, path, body)
	r.Header.Set(HeaderDeviceID, deviceID)
	return r
}

func decodeJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return body
}

// waitPush waits for all in-flight push goroutines to complete.
func waitPush(svc *Service) {
	svc.pushWg.Wait()
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
	defer svc.Close()

	req := authReq("GET", "/api/v1/sync/changes", "")
	w := httptest.NewRecorder()
	svc.HandleSyncChanges(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	body := decodeJSON(t, w)
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
	defer svc.Close()

	req := authReq("GET", "/api/v1/sync/changes?since=2025-01-01T00:00:00Z", "")
	w := httptest.NewRecorder()
	svc.HandleSyncChanges(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleSyncChanges_InvalidSince(t *testing.T) {
	svc := New(&mockSyncDB{}, nil)
	defer svc.Close()

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
	defer svc.Close()

	req := authReq("GET", "/api/v1/sync/changes", "")
	w := httptest.NewRecorder()
	svc.HandleSyncChanges(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with nil handler, got %d", w.Code)
	}
}

func TestHandleSyncChanges_DeviceIDExcluded(t *testing.T) {
	handler := &mockHandler{entities: map[string]any{}}
	svc := New(&mockSyncDB{tombstones: []DeletedEntry{}}, handler)
	defer svc.Close()

	req := authReqWithDevice("GET", "/api/v1/sync/changes", "", "device-abc")
	w := httptest.NewRecorder()
	svc.HandleSyncChanges(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if handler.lastExcludeDevice != "device-abc" {
		t.Fatalf("expected excludeDeviceID 'device-abc', got %q", handler.lastExcludeDevice)
	}
}

func TestHandleSyncChanges_NullDeletedBecomesEmptyArray(t *testing.T) {
	db := &mockSyncDB{tombstones: nil} // returns nil slice
	svc := New(db, &mockHandler{entities: map[string]any{}})
	defer svc.Close()

	req := authReq("GET", "/api/v1/sync/changes", "")
	w := httptest.NewRecorder()
	svc.HandleSyncChanges(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// deleted must be [] not null — null crashes Swift Codable.
	body := decodeJSON(t, w)
	if body["deleted"] == nil {
		t.Fatal("deleted should be [], not null")
	}
}

func TestHandleSyncChanges_ChangedSinceError(t *testing.T) {
	handler := &mockHandler{
		entities:        nil,
		changedSinceErr: errors.New("db down"),
	}
	svc := New(&mockSyncDB{tombstones: []DeletedEntry{}}, handler)
	defer svc.Close()

	req := authReq("GET", "/api/v1/sync/changes", "")
	w := httptest.NewRecorder()
	svc.HandleSyncChanges(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when ChangedSince fails, got %d", w.Code)
	}
}

func TestHandleSyncChanges_ReservedKeysIgnored(t *testing.T) {
	handler := &mockHandler{
		entities: map[string]any{
			"tasks":     []string{"t1"},
			"deleted":   "should-be-ignored",
			"synced_at": "should-be-ignored",
		},
	}
	db := &mockSyncDB{
		tombstones: []DeletedEntry{{EntityType: "x", EntityID: "1", DeletedAt: time.Now()}},
	}
	svc := New(db, handler)
	defer svc.Close()

	req := authReq("GET", "/api/v1/sync/changes", "")
	w := httptest.NewRecorder()
	svc.HandleSyncChanges(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := decodeJSON(t, w)
	// deleted should be the real tombstones, not overwritten.
	deleted := body["deleted"].([]any)
	if len(deleted) != 1 {
		t.Fatalf("expected 1 tombstone, got %d — handler overwrote reserved key", len(deleted))
	}
	// tasks should still come through.
	if body["tasks"] == nil {
		t.Fatal("expected tasks from handler")
	}
}

func TestHandleSyncChanges_ServerTimeError(t *testing.T) {
	db := &mockSyncDB{err: errors.New("db down")}
	svc := New(db, nil)
	defer svc.Close()

	req := authReq("GET", "/api/v1/sync/changes", "")
	w := httptest.NewRecorder()
	svc.HandleSyncChanges(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

// ── HandleSyncBatch ─────────────────────────────────────────────────────────

func TestHandleSyncBatch_Valid(t *testing.T) {
	handler := &mockHandler{
		items:  []BatchResponseItem{{ClientID: "c1", ServerID: "s1", Version: 1}},
		errors: nil,
	}
	svc := New(&mockSyncDB{}, handler)
	defer svc.Close()

	body := `{"items":[{"client_id":"c1","entity_type":"task","version":0,"fields":{"title":"Test"}}]}`
	req := authReqWithDevice("POST", "/api/v1/sync/batch", body, "device-1")
	w := httptest.NewRecorder()
	svc.HandleSyncBatch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	resp := decodeJSON(t, w)
	items := resp["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if handler.lastDeviceID != "device-1" {
		t.Fatalf("expected deviceID 'device-1', got %q", handler.lastDeviceID)
	}
	if resp["synced_at"] == nil {
		t.Fatal("expected synced_at in response")
	}
}

func TestHandleSyncBatch_VersionConflict(t *testing.T) {
	handler := &mockHandler{
		items: nil,
		errors: []BatchError{
			{ClientID: "c1", Error: "version conflict", IsConflict: true, ServerVersion: 5},
		},
	}
	svc := New(&mockSyncDB{}, handler)
	defer svc.Close()

	body := `{"items":[{"client_id":"c1","entity_type":"task","entity_id":"s1","version":3,"fields":{"title":"Stale"}}]}`
	req := authReq("POST", "/api/v1/sync/batch", body)
	w := httptest.NewRecorder()
	svc.HandleSyncBatch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	resp := decodeJSON(t, w)
	errs := resp["errors"].([]any)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	errObj := errs[0].(map[string]any)
	if errObj["is_conflict"] != true {
		t.Fatal("expected is_conflict=true")
	}
	if int(errObj["server_version"].(float64)) != 5 {
		t.Fatalf("expected server_version=5, got %v", errObj["server_version"])
	}
}

func TestHandleSyncBatch_Empty(t *testing.T) {
	svc := New(&mockSyncDB{}, &mockHandler{})
	defer svc.Close()

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
	defer svc.Close()

	items := make([]string, 501)
	for i := range items {
		items[i] = `{"client_id":"c","entity_type":"t","version":0,"fields":{}}`
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
	defer svc.Close()

	body := `{"items":[{"client_id":"c1","entity_type":"task","version":0,"fields":{}}]}`
	req := authReq("POST", "/api/v1/sync/batch", body)
	w := httptest.NewRecorder()
	svc.HandleSyncBatch(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", w.Code)
	}
}

func TestHandleSyncBatch_MissingClientID(t *testing.T) {
	svc := New(&mockSyncDB{}, &mockHandler{})
	defer svc.Close()

	body := `{"items":[{"entity_type":"task","version":0,"fields":{}}]}`
	req := authReq("POST", "/api/v1/sync/batch", body)
	w := httptest.NewRecorder()
	svc.HandleSyncBatch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleSyncBatch_MissingEntityType(t *testing.T) {
	svc := New(&mockSyncDB{}, &mockHandler{})
	defer svc.Close()

	body := `{"items":[{"client_id":"c1","version":0,"fields":{}}]}`
	req := authReq("POST", "/api/v1/sync/batch", body)
	w := httptest.NewRecorder()
	svc.HandleSyncBatch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleSyncBatch_NegativeVersion(t *testing.T) {
	svc := New(&mockSyncDB{}, &mockHandler{})
	defer svc.Close()

	body := `{"items":[{"client_id":"c1","entity_type":"task","version":-1,"fields":{}}]}`
	req := authReq("POST", "/api/v1/sync/batch", body)
	w := httptest.NewRecorder()
	svc.HandleSyncBatch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleSyncBatch_NilFields(t *testing.T) {
	handler := &mockHandler{
		items: []BatchResponseItem{{ClientID: "c1", ServerID: "s1", Version: 1}},
	}
	svc := New(&mockSyncDB{}, handler)
	defer svc.Close()

	body := `{"items":[{"client_id":"c1","entity_type":"task","version":0}]}`
	req := authReq("POST", "/api/v1/sync/batch", body)
	w := httptest.NewRecorder()
	svc.HandleSyncBatch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleSyncBatch_NullResponseArrays(t *testing.T) {
	handler := &mockHandler{
		items:  nil,
		errors: nil,
	}
	svc := New(&mockSyncDB{}, handler)
	defer svc.Close()

	body := `{"items":[{"client_id":"c1","entity_type":"task","version":0,"fields":{}}]}`
	req := authReq("POST", "/api/v1/sync/batch", body)
	w := httptest.NewRecorder()
	svc.HandleSyncBatch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	resp := decodeJSON(t, w)
	if resp["items"] == nil {
		t.Fatal("items should be [], not null")
	}
	if resp["errors"] == nil {
		t.Fatal("errors should be [], not null")
	}
}

func TestHandleSyncBatch_ServerTimeError(t *testing.T) {
	handler := &mockHandler{
		items: []BatchResponseItem{{ClientID: "c1", ServerID: "s1", Version: 1}},
	}
	svc := New(&mockSyncDB{err: errors.New("db down")}, handler)
	defer svc.Close()

	body := `{"items":[{"client_id":"c1","entity_type":"task","version":0,"fields":{}}]}`
	req := authReq("POST", "/api/v1/sync/batch", body)
	w := httptest.NewRecorder()
	svc.HandleSyncBatch(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

// ── Idempotency ─────────────────────────────────────────────────────────────

func TestHandleSyncBatch_Idempotency(t *testing.T) {
	callCount := 0
	handler := &mockHandler{
		items: []BatchResponseItem{{ClientID: "c1", ServerID: "s1", Version: 1}},
	}

	wrappedHandler := &countingHandler{inner: handler, count: &callCount}
	svc := New(&mockSyncDB{}, wrappedHandler)
	defer svc.Close()

	body := `{"items":[{"client_id":"c1","entity_type":"task","version":0,"fields":{}}]}`

	// First request.
	req1 := authReq("POST", "/api/v1/sync/batch", body)
	req1.Header.Set(HeaderIdempotencyKey, "idemp-123")
	w1 := httptest.NewRecorder()
	svc.HandleSyncBatch(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w1.Code)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 handler call, got %d", callCount)
	}

	// Retry with same idempotency key — should return cached result.
	req2 := authReq("POST", "/api/v1/sync/batch", body)
	req2.Header.Set(HeaderIdempotencyKey, "idemp-123")
	w2 := httptest.NewRecorder()
	svc.HandleSyncBatch(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w2.Code)
	}
	if callCount != 1 {
		t.Fatalf("expected handler NOT called again, got %d calls", callCount)
	}

	// Different idempotency key — should call handler again.
	req3 := authReq("POST", "/api/v1/sync/batch", body)
	req3.Header.Set(HeaderIdempotencyKey, "idemp-456")
	w3 := httptest.NewRecorder()
	svc.HandleSyncBatch(w3, req3)

	if callCount != 2 {
		t.Fatalf("expected 2 handler calls for different key, got %d", callCount)
	}
}

func TestHandleSyncBatch_IdempotencyScopedToUser(t *testing.T) {
	callCount := 0
	handler := &mockHandler{
		items: []BatchResponseItem{{ClientID: "c1", ServerID: "s1", Version: 1}},
	}
	wrappedHandler := &countingHandler{inner: handler, count: &callCount}
	svc := New(&mockSyncDB{}, wrappedHandler)
	defer svc.Close()

	body := `{"items":[{"client_id":"c1","entity_type":"task","version":0,"fields":{}}]}`

	// User A sends with key "same-key".
	req1 := authReqAs("POST", "/api/v1/sync/batch", body, "user-A")
	req1.Header.Set(HeaderIdempotencyKey, "same-key")
	w1 := httptest.NewRecorder()
	svc.HandleSyncBatch(w1, req1)

	if callCount != 1 {
		t.Fatalf("expected 1 call, got %d", callCount)
	}

	// User B sends with same key — must NOT get user A's cached response.
	req2 := authReqAs("POST", "/api/v1/sync/batch", body, "user-B")
	req2.Header.Set(HeaderIdempotencyKey, "same-key")
	w2 := httptest.NewRecorder()
	svc.HandleSyncBatch(w2, req2)

	if callCount != 2 {
		t.Fatalf("expected 2 calls (different users same key), got %d", callCount)
	}
}

func TestHandleSyncBatch_NoIdempotencyKey(t *testing.T) {
	callCount := 0
	handler := &mockHandler{
		items: []BatchResponseItem{{ClientID: "c1", ServerID: "s1", Version: 1}},
	}
	wrappedHandler := &countingHandler{inner: handler, count: &callCount}
	svc := New(&mockSyncDB{}, wrappedHandler)
	defer svc.Close()

	body := `{"items":[{"client_id":"c1","entity_type":"task","version":0,"fields":{}}]}`

	for i := 0; i < 2; i++ {
		req := authReq("POST", "/api/v1/sync/batch", body)
		w := httptest.NewRecorder()
		svc.HandleSyncBatch(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}
	if callCount != 2 {
		t.Fatalf("expected 2 handler calls without idemp key, got %d", callCount)
	}
}

// countingHandler wraps a handler to count BatchUpsert calls.
type countingHandler struct {
	inner EntityHandler
	count *int
}

func (c *countingHandler) ChangedSince(userID string, since time.Time, excludeDeviceID string) (map[string]any, error) {
	return c.inner.ChangedSince(userID, since, excludeDeviceID)
}
func (c *countingHandler) BatchUpsert(userID, deviceID string, items []BatchItem) ([]BatchResponseItem, []BatchError) {
	*c.count++
	return c.inner.BatchUpsert(userID, deviceID, items)
}
func (c *countingHandler) Delete(userID, entityType, entityID string) error {
	return c.inner.Delete(userID, entityType, entityID)
}

// ── Push Notifications ──────────────────────────────────────────────────────

func TestHandleSyncBatch_NotifiesOtherDevices(t *testing.T) {
	handler := &mockHandler{
		items: []BatchResponseItem{{ClientID: "c1", ServerID: "s1", Version: 1}},
	}
	mp := &mockPush{}
	tokens := &mockTokenStore{
		devices: []DeviceInfo{
			{DeviceID: "device-1", Token: "token-1"},
			{DeviceID: "device-2", Token: "token-2"},
			{DeviceID: "device-3", Token: "token-3"},
		},
	}

	svc := New(&mockSyncDB{}, handler, Config{Push: mp, DeviceTokens: tokens})
	defer svc.Close()

	body := `{"items":[{"client_id":"c1","entity_type":"task","version":0,"fields":{}}]}`
	req := authReqWithDevice("POST", "/api/v1/sync/batch", body, "device-2")
	w := httptest.NewRecorder()
	svc.HandleSyncBatch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Wait for push goroutine to finish (deterministic, no sleep).
	waitPush(svc)

	calls := mp.getSilentCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 silent pushes, got %d: %v", len(calls), calls)
	}
	for _, token := range calls {
		if token == "token-2" {
			t.Fatal("should not push to the originating device")
		}
	}
}

func TestHandleSyncBatch_NoPushWithoutConfig(t *testing.T) {
	handler := &mockHandler{
		items: []BatchResponseItem{{ClientID: "c1", ServerID: "s1", Version: 1}},
	}
	svc := New(&mockSyncDB{}, handler)
	defer svc.Close()

	body := `{"items":[{"client_id":"c1","entity_type":"task","version":0,"fields":{}}]}`
	req := authReq("POST", "/api/v1/sync/batch", body)
	w := httptest.NewRecorder()
	svc.HandleSyncBatch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ── HandleSyncDelete ────────────────────────────────────────────────────────

func TestHandleSyncDelete_Valid(t *testing.T) {
	db := &mockSyncDB{}
	handler := &mockHandler{}
	svc := New(db, handler)
	defer svc.Close()

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
	defer svc.Close()

	req := authReq("DELETE", "/api/v1/sync//", "")
	req.SetPathValue("entity_type", "")
	req.SetPathValue("id", "")
	w := httptest.NewRecorder()
	svc.HandleSyncDelete(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleSyncDelete_HandlerError(t *testing.T) {
	handler := &mockHandler{deleteErr: errors.New("db error")}
	svc := New(&mockSyncDB{}, handler)
	defer svc.Close()

	req := authReq("DELETE", "/api/v1/sync/task/t1", "")
	req.SetPathValue("entity_type", "task")
	req.SetPathValue("id", "t1")
	w := httptest.NewRecorder()
	svc.HandleSyncDelete(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestHandleSyncDelete_TombstoneError(t *testing.T) {
	db := &mockSyncDB{err: errors.New("db error")}
	svc := New(db, &mockHandler{})
	defer svc.Close()

	req := authReq("DELETE", "/api/v1/sync/task/t1", "")
	req.SetPathValue("entity_type", "task")
	req.SetPathValue("id", "t1")
	w := httptest.NewRecorder()
	svc.HandleSyncDelete(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestHandleSyncDelete_NotifiesOtherDevices(t *testing.T) {
	mp := &mockPush{}
	tokens := &mockTokenStore{
		devices: []DeviceInfo{
			{DeviceID: "device-A", Token: "token-A"},
			{DeviceID: "device-B", Token: "token-B"},
		},
	}
	svc := New(&mockSyncDB{}, &mockHandler{}, Config{Push: mp, DeviceTokens: tokens})
	defer svc.Close()

	req := authReqWithDevice("DELETE", "/api/v1/sync/task/t1", "", "device-A")
	req.SetPathValue("entity_type", "task")
	req.SetPathValue("id", "t1")
	w := httptest.NewRecorder()
	svc.HandleSyncDelete(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	waitPush(svc)

	calls := mp.getSilentCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 silent push (to device-B only), got %d", len(calls))
	}
	if calls[0] != "token-B" {
		t.Fatalf("expected push to token-B, got %s", calls[0])
	}
}

// ── Idempotency Cache Cleanup ───────────────────────────────────────────────

func TestIdempotencyCacheExpiry(t *testing.T) {
	svc := New(&mockSyncDB{}, nil, Config{IdempotencyTTL: 1 * time.Millisecond})
	defer svc.Close()

	resp := &BatchResponse{
		Items:    []BatchResponseItem{{ClientID: "c1", ServerID: "s1", Version: 1}},
		Errors:   []BatchError{},
		SyncedAt: time.Now(),
	}
	svc.setIdempotency("test-key", resp)

	if got := svc.getIdempotency("test-key"); got == nil {
		t.Fatal("expected cached response")
	}

	time.Sleep(5 * time.Millisecond)

	if got := svc.getIdempotency("test-key"); got != nil {
		t.Fatal("expected expired cache entry to return nil")
	}
}

// ── Migrations ──────────────────────────────────────────────────────────────

func TestMigrations(t *testing.T) {
	m := Migrations()
	if len(m) == 0 {
		t.Fatal("expected at least one migration")
	}
}

// ── Config Defaults ─────────────────────────────────────────────────────────

func TestNewWithoutConfig(t *testing.T) {
	svc := New(&mockSyncDB{}, nil)
	defer svc.Close()

	if svc.idempTTL != 24*time.Hour {
		t.Fatalf("expected default TTL of 24h, got %v", svc.idempTTL)
	}
	if svc.push != nil {
		t.Fatal("expected nil push without config")
	}
}

func TestNewWithConfig(t *testing.T) {
	mp := &mockPush{}
	tokens := &mockTokenStore{}
	svc := New(&mockSyncDB{}, nil, Config{
		Push:           mp,
		DeviceTokens:   tokens,
		IdempotencyTTL: 1 * time.Hour,
	})
	defer svc.Close()

	if svc.idempTTL != 1*time.Hour {
		t.Fatalf("expected 1h TTL, got %v", svc.idempTTL)
	}
	if svc.push == nil {
		t.Fatal("expected push to be set")
	}
}
