package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pacosw1/donkeygo/middleware"
	"github.com/pacosw1/donkeygo/push"
)

// ── Mock DB ─────────────────────────────────────────────────────────────────

type mockDB struct {
	devices      []*DeviceToken
	prefs        *NotificationPreferences
	deliveries   []*NotificationDelivery
	enabledUsers []string
	err          error
}

func (m *mockDB) UpsertDeviceToken(dt *DeviceToken) error { return m.err }
func (m *mockDB) DisableDeviceToken(userID, token string) error { return m.err }
func (m *mockDB) EnabledDeviceTokens(userID string) ([]*DeviceToken, error) {
	return m.devices, m.err
}
func (m *mockDB) EnsureNotificationPreferences(userID string) {}
func (m *mockDB) GetNotificationPreferences(userID string) (*NotificationPreferences, error) {
	if m.prefs != nil {
		return m.prefs, nil
	}
	return &NotificationPreferences{
		UserID: userID, PushEnabled: true, IntervalSeconds: 3600,
		WakeHour: 8, SleepHour: 22, Timezone: "America/New_York", StopAfterGoal: true,
	}, nil
}
func (m *mockDB) UpsertNotificationPreferences(p *NotificationPreferences) error {
	m.prefs = p
	return m.err
}
func (m *mockDB) AllUsersWithNotificationsEnabled() ([]string, error) {
	return m.enabledUsers, m.err
}
func (m *mockDB) LastNotificationDelivery(userID string) (*NotificationDelivery, error) {
	if len(m.deliveries) > 0 {
		return m.deliveries[len(m.deliveries)-1], nil
	}
	return nil, nil
}
func (m *mockDB) RecordNotificationDelivery(userID, kind, title, body string) {
	m.deliveries = append(m.deliveries, &NotificationDelivery{UserID: userID, Kind: kind, Title: title, Body: body})
}
func (m *mockDB) TrackNotificationOpened(userID, notificationID string) error { return m.err }

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

// ── HandleRegisterDevice ────────────────────────────────────────────────────

func TestHandleRegisterDevice_Valid(t *testing.T) {
	svc := New(&mockDB{}, &push.NoopProvider{})

	body := `{"token":"abc123","platform":"ios","device_model":"iPhone 15","os_version":"17.0","app_version":"1.0"}`
	req := authReq("POST", "/api/v1/notifications/devices", body)
	w := httptest.NewRecorder()
	svc.HandleRegisterDevice(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleRegisterDevice_MissingToken(t *testing.T) {
	svc := New(&mockDB{}, &push.NoopProvider{})

	body := `{"platform":"ios"}`
	req := authReq("POST", "/api/v1/notifications/devices", body)
	w := httptest.NewRecorder()
	svc.HandleRegisterDevice(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleRegisterDevice_DefaultPlatform(t *testing.T) {
	svc := New(&mockDB{}, &push.NoopProvider{})

	body := `{"token":"abc123"}`
	req := authReq("POST", "/api/v1/notifications/devices", body)
	w := httptest.NewRecorder()
	svc.HandleRegisterDevice(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
}

// ── HandleDisableDevice ─────────────────────────────────────────────────────

func TestHandleDisableDevice_Valid(t *testing.T) {
	svc := New(&mockDB{}, &push.NoopProvider{})

	body := `{"token":"abc123"}`
	req := authReq("DELETE", "/api/v1/notifications/devices", body)
	w := httptest.NewRecorder()
	svc.HandleDisableDevice(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleDisableDevice_MissingToken(t *testing.T) {
	svc := New(&mockDB{}, &push.NoopProvider{})

	body := `{}`
	req := authReq("DELETE", "/api/v1/notifications/devices", body)
	w := httptest.NewRecorder()
	svc.HandleDisableDevice(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ── HandleGetNotificationPrefs ──────────────────────────────────────────────

func TestHandleGetNotificationPrefs(t *testing.T) {
	db := &mockDB{prefs: &NotificationPreferences{
		UserID: "user-1", PushEnabled: true, IntervalSeconds: 1800,
		WakeHour: 9, SleepHour: 21, Timezone: "UTC",
	}}
	svc := New(db, &push.NoopProvider{})

	req := authReq("GET", "/api/v1/notifications/preferences", "")
	w := httptest.NewRecorder()
	svc.HandleGetNotificationPrefs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var prefs NotificationPreferences
	json.Unmarshal(w.Body.Bytes(), &prefs)
	if prefs.IntervalSeconds != 1800 {
		t.Fatalf("expected 1800, got %d", prefs.IntervalSeconds)
	}
}

// ── HandleUpdateNotificationPrefs ───────────────────────────────────────────

func TestHandleUpdateNotificationPrefs_Valid(t *testing.T) {
	db := &mockDB{}
	svc := New(db, &push.NoopProvider{})

	body := `{"push_enabled":false,"interval_seconds":600}`
	req := authReq("PUT", "/api/v1/notifications/preferences", body)
	w := httptest.NewRecorder()
	svc.HandleUpdateNotificationPrefs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var prefs NotificationPreferences
	json.Unmarshal(w.Body.Bytes(), &prefs)
	if prefs.PushEnabled != false {
		t.Fatal("expected push_enabled=false")
	}
	if prefs.IntervalSeconds != 600 {
		t.Fatalf("expected 600, got %d", prefs.IntervalSeconds)
	}
}

func TestHandleUpdateNotificationPrefs_IntervalTooLow(t *testing.T) {
	svc := New(&mockDB{}, &push.NoopProvider{})

	body := `{"interval_seconds":60}`
	req := authReq("PUT", "/api/v1/notifications/preferences", body)
	w := httptest.NewRecorder()
	svc.HandleUpdateNotificationPrefs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for interval <300, got %d", w.Code)
	}
}

func TestHandleUpdateNotificationPrefs_InvalidWakeHour(t *testing.T) {
	svc := New(&mockDB{}, &push.NoopProvider{})

	body := `{"wake_hour":25}`
	req := authReq("PUT", "/api/v1/notifications/preferences", body)
	w := httptest.NewRecorder()
	svc.HandleUpdateNotificationPrefs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid wake_hour, got %d", w.Code)
	}
}

func TestHandleUpdateNotificationPrefs_InvalidSleepHour(t *testing.T) {
	svc := New(&mockDB{}, &push.NoopProvider{})

	body := `{"sleep_hour":-1}`
	req := authReq("PUT", "/api/v1/notifications/preferences", body)
	w := httptest.NewRecorder()
	svc.HandleUpdateNotificationPrefs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid sleep_hour, got %d", w.Code)
	}
}

func TestHandleUpdateNotificationPrefs_InvalidTimezone(t *testing.T) {
	svc := New(&mockDB{}, &push.NoopProvider{})

	body := `{"timezone":"Mars/Olympus"}`
	req := authReq("PUT", "/api/v1/notifications/preferences", body)
	w := httptest.NewRecorder()
	svc.HandleUpdateNotificationPrefs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid timezone, got %d", w.Code)
	}
}

func TestHandleUpdateNotificationPrefs_ValidTimezone(t *testing.T) {
	svc := New(&mockDB{}, &push.NoopProvider{})

	body := `{"timezone":"America/Chicago"}`
	req := authReq("PUT", "/api/v1/notifications/preferences", body)
	w := httptest.NewRecorder()
	svc.HandleUpdateNotificationPrefs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ── HandleNotificationOpened ────────────────────────────────────────────────

func TestHandleNotificationOpened(t *testing.T) {
	svc := New(&mockDB{}, &push.NoopProvider{})

	body := `{"notification_id":"notif-123"}`
	req := authReq("POST", "/api/v1/notifications/opened", body)
	w := httptest.NewRecorder()
	svc.HandleNotificationOpened(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ── Scheduler ───────────────────────────────────────────────────────────────

func TestScheduler_StartStop(t *testing.T) {
	db := &mockDB{}
	sched := NewScheduler(db, &push.NoopProvider{}, SchedulerConfig{
		Interval: 100 * time.Millisecond,
	})
	sched.Start()
	time.Sleep(50 * time.Millisecond)
	sched.Stop()
}

func TestMigrations(t *testing.T) {
	m := Migrations()
	if len(m) == 0 {
		t.Fatal("expected at least one migration")
	}
}
