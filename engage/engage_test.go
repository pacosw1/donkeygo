package engage

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

type mockDB struct {
	trackedEvents []EventInput
	subscription  *UserSubscription
	engagement    *EngagementData
	isPro         bool
	feedback      []string
	sessions      []string
	err           error
}

func (m *mockDB) TrackEvents(userID string, events []EventInput) error {
	m.trackedEvents = append(m.trackedEvents, events...)
	return m.err
}
func (m *mockDB) UpdateSubscription(userID, productID, status string, expiresAt *time.Time) error {
	return m.err
}
func (m *mockDB) UpdateSubscriptionDetails(userID, tid string, price int, currency string) error {
	return m.err
}
func (m *mockDB) GetSubscription(userID string) (*UserSubscription, error) {
	return m.subscription, m.err
}
func (m *mockDB) IsProUser(userID string) (bool, error) { return m.isPro, m.err }
func (m *mockDB) GetEngagementData(userID string) (*EngagementData, error) {
	if m.engagement == nil {
		return &EngagementData{}, m.err
	}
	return m.engagement, m.err
}
func (m *mockDB) StartSession(userID, sessionID, appVersion, osVersion, country string) error {
	m.sessions = append(m.sessions, "start:"+sessionID)
	return m.err
}
func (m *mockDB) EndSession(userID, sessionID string, durationS int) error {
	m.sessions = append(m.sessions, "end:"+sessionID)
	return m.err
}
func (m *mockDB) SaveFeedback(userID, feedbackType, message, appVersion string) error {
	m.feedback = append(m.feedback, feedbackType+":"+message)
	return m.err
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

// ── HandleTrackEvents ───────────────────────────────────────────────────────

func TestHandleTrackEvents_Valid(t *testing.T) {
	db := &mockDB{}
	svc := New(Config{}, db)

	body := `{"events":[{"event":"tap_button","metadata":{"screen":"home"}},{"event":"scroll"}]}`
	req := authReq("POST", "/api/v1/events", body)
	w := httptest.NewRecorder()
	svc.HandleTrackEvents(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(db.trackedEvents) != 2 {
		t.Fatalf("expected 2 tracked events, got %d", len(db.trackedEvents))
	}
}

func TestHandleTrackEvents_Empty(t *testing.T) {
	svc := New(Config{}, &mockDB{})

	req := authReq("POST", "/api/v1/events", `{"events":[]}`)
	w := httptest.NewRecorder()
	svc.HandleTrackEvents(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty events, got %d", w.Code)
	}
}

func TestHandleTrackEvents_TooMany(t *testing.T) {
	svc := New(Config{}, &mockDB{})

	events := make([]string, 101)
	for i := range events {
		events[i] = `{"event":"e"}`
	}
	body := `{"events":[` + strings.Join(events, ",") + `]}`
	req := authReq("POST", "/api/v1/events", body)
	w := httptest.NewRecorder()
	svc.HandleTrackEvents(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for >100 events, got %d", w.Code)
	}
}

func TestHandleTrackEvents_InvalidJSON(t *testing.T) {
	svc := New(Config{}, &mockDB{})

	req := authReq("POST", "/api/v1/events", `{invalid}`)
	w := httptest.NewRecorder()
	svc.HandleTrackEvents(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

// ── HandleUpdateSubscription ────────────────────────────────────────────────

func TestHandleUpdateSubscription_Valid(t *testing.T) {
	db := &mockDB{subscription: &UserSubscription{UserID: "user-1", Status: "active", ProductID: "pro_monthly"}}
	svc := New(Config{}, db)

	body := `{"product_id":"pro_monthly","status":"active"}`
	req := authReq("PUT", "/api/v1/subscription", body)
	w := httptest.NewRecorder()
	svc.HandleUpdateSubscription(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpdateSubscription_InvalidStatus(t *testing.T) {
	svc := New(Config{}, &mockDB{})

	body := `{"status":"premium"}`
	req := authReq("PUT", "/api/v1/subscription", body)
	w := httptest.NewRecorder()
	svc.HandleUpdateSubscription(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid status, got %d", w.Code)
	}
}

func TestHandleUpdateSubscription_MissingStatus(t *testing.T) {
	svc := New(Config{}, &mockDB{})

	body := `{"product_id":"pro"}`
	req := authReq("PUT", "/api/v1/subscription", body)
	w := httptest.NewRecorder()
	svc.HandleUpdateSubscription(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing status, got %d", w.Code)
	}
}

func TestHandleUpdateSubscription_WithExpiry(t *testing.T) {
	db := &mockDB{}
	svc := New(Config{}, db)

	body := `{"status":"active","expires_at":"2025-12-31T23:59:59Z"}`
	req := authReq("PUT", "/api/v1/subscription", body)
	w := httptest.NewRecorder()
	svc.HandleUpdateSubscription(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpdateSubscription_WithDetails(t *testing.T) {
	db := &mockDB{subscription: &UserSubscription{UserID: "user-1", Status: "active"}}
	svc := New(Config{}, db)

	body := `{"status":"active","original_transaction_id":"txn_123","price_cents":999,"currency_code":"EUR"}`
	req := authReq("PUT", "/api/v1/subscription", body)
	w := httptest.NewRecorder()
	svc.HandleUpdateSubscription(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ── HandleSessionReport ─────────────────────────────────────────────────────

func TestHandleSessionReport_Start(t *testing.T) {
	db := &mockDB{}
	svc := New(Config{}, db)

	body := `{"session_id":"s1","action":"start","app_version":"1.0"}`
	req := authReq("POST", "/api/v1/sessions", body)
	w := httptest.NewRecorder()
	svc.HandleSessionReport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(db.sessions) != 1 || db.sessions[0] != "start:s1" {
		t.Fatalf("expected start session, got %v", db.sessions)
	}
}

func TestHandleSessionReport_End(t *testing.T) {
	db := &mockDB{}
	svc := New(Config{}, db)

	body := `{"session_id":"s1","action":"end","duration_s":120}`
	req := authReq("POST", "/api/v1/sessions", body)
	w := httptest.NewRecorder()
	svc.HandleSessionReport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(db.sessions) != 1 || db.sessions[0] != "end:s1" {
		t.Fatalf("expected end session, got %v", db.sessions)
	}
}

func TestHandleSessionReport_InvalidAction(t *testing.T) {
	svc := New(Config{}, &mockDB{})

	body := `{"session_id":"s1","action":"pause"}`
	req := authReq("POST", "/api/v1/sessions", body)
	w := httptest.NewRecorder()
	svc.HandleSessionReport(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleSessionReport_MissingFields(t *testing.T) {
	svc := New(Config{}, &mockDB{})

	body := `{"session_id":"s1"}`
	req := authReq("POST", "/api/v1/sessions", body)
	w := httptest.NewRecorder()
	svc.HandleSessionReport(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ── HandleGetEligibility ────────────────────────────────────────────────────

func TestHandleGetEligibility_FreeUser(t *testing.T) {
	db := &mockDB{
		engagement: &EngagementData{DaysActive: 20, TotalLogs: 60},
		isPro:      false,
	}
	svc := New(Config{}, db)

	req := authReq("GET", "/api/v1/user/eligibility", "")
	w := httptest.NewRecorder()
	svc.HandleGetEligibility(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["is_pro"] != false {
		t.Fatal("expected is_pro=false")
	}
	if body["paywall_trigger"] == nil {
		t.Fatal("expected paywall_trigger for power user")
	}
}

func TestHandleGetEligibility_ProUser(t *testing.T) {
	db := &mockDB{
		engagement: &EngagementData{DaysActive: 20, TotalLogs: 60},
		isPro:      true,
	}
	svc := New(Config{}, db)

	req := authReq("GET", "/api/v1/user/eligibility", "")
	w := httptest.NewRecorder()
	svc.HandleGetEligibility(w, req)

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["is_pro"] != true {
		t.Fatal("expected is_pro=true")
	}
	if body["paywall_trigger"] != nil {
		t.Fatal("pro users should not get paywall triggers")
	}
}

// ── HandleSubmitFeedback ────────────────────────────────────────────────────

func TestHandleSubmitFeedback_Valid(t *testing.T) {
	db := &mockDB{}
	svc := New(Config{}, db)

	body := `{"type":"bug","message":"Crash on launch"}`
	req := authReq("POST", "/api/v1/feedback", body)
	w := httptest.NewRecorder()
	svc.HandleSubmitFeedback(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
	if len(db.feedback) != 1 {
		t.Fatalf("expected 1 feedback, got %d", len(db.feedback))
	}
}

func TestHandleSubmitFeedback_DefaultType(t *testing.T) {
	db := &mockDB{}
	svc := New(Config{}, db)

	body := `{"message":"Great app!"}`
	req := authReq("POST", "/api/v1/feedback", body)
	w := httptest.NewRecorder()
	svc.HandleSubmitFeedback(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
	if db.feedback[0] != "general:Great app!" {
		t.Fatalf("expected general type, got %s", db.feedback[0])
	}
}

func TestHandleSubmitFeedback_MissingMessage(t *testing.T) {
	svc := New(Config{}, &mockDB{})

	body := `{"type":"bug"}`
	req := authReq("POST", "/api/v1/feedback", body)
	w := httptest.NewRecorder()
	svc.HandleSubmitFeedback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleSubmitFeedback_InvalidType(t *testing.T) {
	svc := New(Config{}, &mockDB{})

	body := `{"type":"complaint","message":"bad"}`
	req := authReq("POST", "/api/v1/feedback", body)
	w := httptest.NewRecorder()
	svc.HandleSubmitFeedback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestDefaultPaywallTrigger(t *testing.T) {
	tests := []struct {
		name string
		data *EngagementData
		want string
	}{
		{"power user", &EngagementData{DaysActive: 20, TotalLogs: 60}, "power_user"},
		{"milestone", &EngagementData{GoalsCompletedTotal: 15, PaywallShownCount: 1}, "milestone"},
		{"new user", &EngagementData{DaysActive: 2, TotalLogs: 5}, ""},
		{"already shown", &EngagementData{GoalsCompletedTotal: 15, PaywallShownCount: 5}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DefaultPaywallTrigger(tt.data)
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestMigrations(t *testing.T) {
	m := Migrations()
	if len(m) == 0 {
		t.Fatal("expected at least one migration")
	}
}
