package analytics

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ── Mock DB ─────────────────────────────────────────────────────────────────

type mockDB struct {
	dauRows      []DAURow
	eventRows    []EventRow
	subBreakdown []SubStats
	newSubs30d   int
	churned30d   int
	dauToday     int
	mau          int
	totalUsers   int
	activeSubs   int
	err          error
}

func (m *mockDB) DAUTimeSeries(since time.Time) ([]DAURow, error) {
	return m.dauRows, m.err
}
func (m *mockDB) EventCounts(since time.Time, event string) ([]EventRow, error) {
	return m.eventRows, m.err
}
func (m *mockDB) SubscriptionBreakdown() ([]SubStats, error) {
	return m.subBreakdown, m.err
}
func (m *mockDB) NewSubscriptions30d() (int, error)   { return m.newSubs30d, m.err }
func (m *mockDB) ChurnedSubscriptions30d() (int, error) { return m.churned30d, m.err }
func (m *mockDB) DAUToday() (int, error)               { return m.dauToday, m.err }
func (m *mockDB) MAU() (int, error)                     { return m.mau, m.err }
func (m *mockDB) TotalUsers() (int, error)              { return m.totalUsers, m.err }
func (m *mockDB) ActiveSubscriptions() (int, error)     { return m.activeSubs, m.err }

// ── HandleDAU ───────────────────────────────────────────────────────────────

func TestHandleDAU_WithData(t *testing.T) {
	db := &mockDB{dauRows: []DAURow{
		{Date: "2025-01-01", DAU: 100},
		{Date: "2025-01-02", DAU: 150},
	}}
	svc := New(Config{}, db)

	req := httptest.NewRequest("GET", "/admin/api/analytics/dau?days=7", nil)
	w := httptest.NewRecorder()
	svc.HandleDAU(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	dau, ok := body["dau"].([]any)
	if !ok {
		t.Fatal("expected dau array in response")
	}
	if len(dau) != 2 {
		t.Fatalf("expected 2 dau rows, got %d", len(dau))
	}
}

func TestHandleDAU_Empty(t *testing.T) {
	svc := New(Config{}, &mockDB{})

	req := httptest.NewRequest("GET", "/admin/api/analytics/dau", nil)
	w := httptest.NewRecorder()
	svc.HandleDAU(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	dau := body["dau"].([]any)
	if len(dau) != 0 {
		t.Fatalf("expected empty dau array, got %d items", len(dau))
	}
}

func TestHandleDAU_DefaultDays(t *testing.T) {
	svc := New(Config{}, &mockDB{})

	req := httptest.NewRequest("GET", "/admin/api/analytics/dau", nil)
	w := httptest.NewRecorder()
	svc.HandleDAU(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleDAU_DBError(t *testing.T) {
	db := &mockDB{err: errMock}
	svc := New(Config{}, db)

	req := httptest.NewRequest("GET", "/admin/api/analytics/dau", nil)
	w := httptest.NewRecorder()
	svc.HandleDAU(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

// ── HandleEvents ────────────────────────────────────────────────────────────

func TestHandleEvents_WithData(t *testing.T) {
	db := &mockDB{eventRows: []EventRow{
		{Date: "2025-01-01", Event: "app_open", Count: 500, UniqueUsers: 100},
		{Date: "2025-01-01", Event: "purchase", Count: 10, UniqueUsers: 8},
	}}
	svc := New(Config{}, db)

	req := httptest.NewRequest("GET", "/admin/api/analytics/events?days=14", nil)
	w := httptest.NewRecorder()
	svc.HandleEvents(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	events := body["events"].([]any)
	if len(events) != 2 {
		t.Fatalf("expected 2 event rows, got %d", len(events))
	}
}

func TestHandleEvents_WithFilter(t *testing.T) {
	db := &mockDB{eventRows: []EventRow{
		{Date: "2025-01-01", Event: "app_open", Count: 500, UniqueUsers: 100},
	}}
	svc := New(Config{}, db)

	req := httptest.NewRequest("GET", "/admin/api/analytics/events?event=app_open", nil)
	w := httptest.NewRecorder()
	svc.HandleEvents(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleEvents_Empty(t *testing.T) {
	svc := New(Config{}, &mockDB{})

	req := httptest.NewRequest("GET", "/admin/api/analytics/events", nil)
	w := httptest.NewRecorder()
	svc.HandleEvents(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	events := body["events"].([]any)
	if len(events) != 0 {
		t.Fatalf("expected empty events array, got %d items", len(events))
	}
}

func TestHandleEvents_DBError(t *testing.T) {
	svc := New(Config{}, &mockDB{err: errMock})

	req := httptest.NewRequest("GET", "/admin/api/analytics/events", nil)
	w := httptest.NewRecorder()
	svc.HandleEvents(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

// ── HandleMRR ───────────────────────────────────────────────────────────────

func TestHandleMRR_WithData(t *testing.T) {
	db := &mockDB{
		subBreakdown: []SubStats{
			{Status: "active", Count: 200},
			{Status: "trial", Count: 50},
			{Status: "expired", Count: 30},
		},
		newSubs30d: 25,
		churned30d: 10,
	}
	svc := New(Config{}, db)

	req := httptest.NewRequest("GET", "/admin/api/analytics/mrr", nil)
	w := httptest.NewRecorder()
	svc.HandleMRR(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)

	activeTotal := int(body["active_total"].(float64))
	if activeTotal != 250 {
		t.Fatalf("expected active_total=250, got %d", activeTotal)
	}
	if int(body["new_30d"].(float64)) != 25 {
		t.Fatalf("expected new_30d=25, got %v", body["new_30d"])
	}
	if int(body["churned_30d"].(float64)) != 10 {
		t.Fatalf("expected churned_30d=10, got %v", body["churned_30d"])
	}

	breakdown := body["breakdown"].([]any)
	if len(breakdown) != 3 {
		t.Fatalf("expected 3 breakdown rows, got %d", len(breakdown))
	}
}

func TestHandleMRR_Empty(t *testing.T) {
	svc := New(Config{}, &mockDB{})

	req := httptest.NewRequest("GET", "/admin/api/analytics/mrr", nil)
	w := httptest.NewRecorder()
	svc.HandleMRR(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if int(body["active_total"].(float64)) != 0 {
		t.Fatal("expected active_total=0 for empty breakdown")
	}
}

func TestHandleMRR_DBError(t *testing.T) {
	svc := New(Config{}, &mockDB{err: errMock})

	req := httptest.NewRequest("GET", "/admin/api/analytics/mrr", nil)
	w := httptest.NewRecorder()
	svc.HandleMRR(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

// ── HandleSummary ───────────────────────────────────────────────────────────

func TestHandleSummary_WithData(t *testing.T) {
	db := &mockDB{
		dauToday:   120,
		mau:        3500,
		totalUsers: 10000,
		activeSubs: 800,
	}
	svc := New(Config{}, db)

	req := httptest.NewRequest("GET", "/admin/api/analytics/summary", nil)
	w := httptest.NewRecorder()
	svc.HandleSummary(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)

	if int(body["dau_today"].(float64)) != 120 {
		t.Fatalf("expected dau_today=120, got %v", body["dau_today"])
	}
	if int(body["mau"].(float64)) != 3500 {
		t.Fatalf("expected mau=3500, got %v", body["mau"])
	}
	if int(body["total_users"].(float64)) != 10000 {
		t.Fatalf("expected total_users=10000, got %v", body["total_users"])
	}
	if int(body["active_subs"].(float64)) != 800 {
		t.Fatalf("expected active_subs=800, got %v", body["active_subs"])
	}
}

func TestHandleSummary_Zeroes(t *testing.T) {
	svc := New(Config{}, &mockDB{})

	req := httptest.NewRequest("GET", "/admin/api/analytics/summary", nil)
	w := httptest.NewRecorder()
	svc.HandleSummary(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	for _, key := range []string{"dau_today", "mau", "total_users", "active_subs"} {
		if int(body[key].(float64)) != 0 {
			t.Fatalf("expected %s=0, got %v", key, body[key])
		}
	}
}

// ── intQuery ────────────────────────────────────────────────────────────────

func TestIntQuery_Valid(t *testing.T) {
	req := httptest.NewRequest("GET", "/test?days=7", nil)
	got := intQuery(req, "days", 30)
	if got != 7 {
		t.Fatalf("expected 7, got %d", got)
	}
}

func TestIntQuery_Missing(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	got := intQuery(req, "days", 30)
	if got != 30 {
		t.Fatalf("expected fallback 30, got %d", got)
	}
}

func TestIntQuery_Invalid(t *testing.T) {
	req := httptest.NewRequest("GET", "/test?days=abc", nil)
	got := intQuery(req, "days", 30)
	if got != 30 {
		t.Fatalf("expected fallback 30 for invalid value, got %d", got)
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

var errMock = &mockError{}

type mockError struct{}

func (e *mockError) Error() string { return "mock error" }
