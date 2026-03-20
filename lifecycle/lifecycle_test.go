package lifecycle

import (
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
	createdAt      time.Time
	lastActiveAt   time.Time
	totalSessions  int
	recentSessions int
	eventDays      int
	isPro          bool
	lastPromptType string
	lastPromptAt   time.Time
	promptCount    int
	tokens         []string
	recorded       []string // recorded events
}

func (m *mockDB) UserCreatedAndLastActive(userID string) (time.Time, time.Time, error) {
	return m.createdAt, m.lastActiveAt, nil
}
func (m *mockDB) CountSessions(userID string) (int, error)      { return m.totalSessions, nil }
func (m *mockDB) CountRecentSessions(userID string, since time.Time) (int, error) {
	return m.recentSessions, nil
}
func (m *mockDB) CountDistinctEventDays(userID, eventName string, since time.Time) (int, error) {
	return m.eventDays, nil
}
func (m *mockDB) IsProUser(userID string) (bool, error) { return m.isPro, nil }
func (m *mockDB) LastPrompt(userID string) (string, time.Time, error) {
	return m.lastPromptType, m.lastPromptAt, nil
}
func (m *mockDB) CountPrompts(userID, promptType string, since time.Time) (int, error) {
	return m.promptCount, nil
}
func (m *mockDB) RecordPrompt(userID, event, metadata string) error {
	m.recorded = append(m.recorded, event)
	return nil
}
func (m *mockDB) EnabledDeviceTokens(userID string) ([]string, error) {
	return m.tokens, nil
}

// ── Score Tests ─────────────────────────────────────────────────────────────

func TestCalculateScore(t *testing.T) {
	tests := []struct {
		name            string
		recentSessions  int
		ahaReached      bool
		isPro           bool
		daysSinceActive int
		totalSessions   int
		want            int
	}{
		{"brand new user", 0, false, false, 0, 0, 10},
		{"active free user", 3, false, false, 0, 10, 31},      // 18 + 0 + 0 + 10 + 3
		{"power free user", 7, true, false, 0, 30, 80},        // 40 + 20 + 0 + 10 + 10
		{"pro user inactive 1d", 2, true, true, 1, 15, 62},    // 12 + 20 + 20 + 5 + 5
		{"pro power user", 7, true, true, 0, 30, 100},         // 40 + 20 + 20 + 10 + 10 = 100
		{"capped at 100", 10, true, true, 0, 100, 100},
		{"inactive 5 days", 0, false, false, 5, 2, 0},         // 0 + 0 + 0 + 0 + 0
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateScore(tt.recentSessions, tt.ahaReached, tt.isPro, tt.daysSinceActive, tt.totalSessions)
			if got != tt.want {
				t.Errorf("calculateScore() = %d, want %d", got, tt.want)
			}
		})
	}
}

// ── Stage Tests ─────────────────────────────────────────────────────────────

func TestDetermineStage(t *testing.T) {
	tests := []struct {
		name            string
		score           int
		daysSinceActive int
		createdDaysAgo  int
		ahaReached      bool
		isPro           bool
		want            Stage
	}{
		{"churned 30d", 50, 30, 60, true, true, StageChurned},
		{"churned 90d", 0, 90, 120, false, false, StageChurned},
		{"dormant 14d", 50, 14, 30, true, true, StageDormant},
		{"dormant 20d", 10, 20, 40, false, false, StageDormant},
		{"at_risk 7d inactive", 50, 7, 30, true, false, StageAtRisk},
		{"at_risk low score old user", 15, 3, 10, false, false, StageAtRisk},
		{"loyal pro high score", 60, 0, 60, true, true, StageLoyal},
		{"monetized pro low score", 30, 0, 30, false, true, StageMonetized},
		{"engaged high score", 40, 0, 14, false, false, StageEngaged},
		{"activated aha", 20, 0, 5, true, false, StageActivated},
		{"new user", 10, 0, 1, false, false, StageNew},
		{"new user low score recent", 15, 2, 5, false, false, StageNew},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := determineStage(tt.score, tt.daysSinceActive, tt.createdDaysAgo, tt.ahaReached, tt.isPro)
			if got != tt.want {
				t.Errorf("determineStage() = %s, want %s", got, tt.want)
			}
		})
	}
}

// ── EvaluateUser Tests ──────────────────────────────────────────────────────

func TestEvaluateUser(t *testing.T) {
	now := time.Now()

	db := &mockDB{
		createdAt:      now.Add(-30 * 24 * time.Hour),
		lastActiveAt:   now,
		totalSessions:  30,
		recentSessions: 7,
		eventDays:      5,
		isPro:          true,
		lastPromptAt:   now.Add(-5 * 24 * time.Hour), // last prompt 5 days ago
	}

	svc := New(Config{
		AhaMomentRules: []AhaMomentRule{
			{Name: "core_usage", EventName: "log_created", Threshold: 3, WindowDays: 7},
		},
	}, db, &push.NoopProvider{})

	es, err := svc.EvaluateUser("user1")
	if err != nil {
		t.Fatalf("EvaluateUser() error: %v", err)
	}

	if es.Stage != StageLoyal {
		t.Errorf("Stage = %s, want %s", es.Stage, StageLoyal)
	}
	if es.Score != 100 {
		t.Errorf("Score = %d, want 100", es.Score)
	}
	if !es.AhaReached {
		t.Error("AhaReached = false, want true")
	}
	if !es.IsPro {
		t.Error("IsPro = false, want true")
	}
	if es.Prompt == nil {
		t.Error("Prompt = nil, want milestone prompt")
	} else if es.Prompt.Type != PromptMilestone {
		t.Errorf("Prompt.Type = %s, want %s", es.Prompt.Type, PromptMilestone)
	}
}

func TestEvaluateUser_NoAhaRules(t *testing.T) {
	now := time.Now()
	db := &mockDB{
		createdAt:      now.Add(-2 * 24 * time.Hour),
		lastActiveAt:   now,
		totalSessions:  3,
		recentSessions: 2,
		eventDays:      0,
		isPro:          false,
		lastPromptAt:   time.Time{},
	}

	svc := New(Config{}, db, &push.NoopProvider{})

	es, err := svc.EvaluateUser("user2")
	if err != nil {
		t.Fatalf("EvaluateUser() error: %v", err)
	}

	if es.Stage != StageNew {
		t.Errorf("Stage = %s, want %s", es.Stage, StageNew)
	}
	if es.AhaReached {
		t.Error("AhaReached = true, want false")
	}
}

// ── Prompt Cooldown Tests ───────────────────────────────────────────────────

func TestDeterminePrompt_Cooldown(t *testing.T) {
	now := time.Now()

	db := &mockDB{
		createdAt:      now.Add(-30 * 24 * time.Hour),
		lastActiveAt:   now,
		totalSessions:  30,
		recentSessions: 7,
		eventDays:      5,
		isPro:          true,
		lastPromptType: "milestone",
		lastPromptAt:   now.Add(-1 * time.Hour), // prompted 1 hour ago
	}

	svc := New(Config{
		AhaMomentRules: []AhaMomentRule{
			{Name: "core_usage", EventName: "log_created", Threshold: 3, WindowDays: 7},
		},
	}, db, &push.NoopProvider{})

	es, err := svc.EvaluateUser("user1")
	if err != nil {
		t.Fatalf("EvaluateUser() error: %v", err)
	}

	if es.Prompt != nil {
		t.Errorf("Prompt = %+v, want nil (cooldown active)", es.Prompt)
	}
}

// ── HasBeenPrompted Tests ───────────────────────────────────────────────────

func TestHasBeenPrompted(t *testing.T) {
	db := &mockDB{promptCount: 2}
	svc := New(Config{}, db, &push.NoopProvider{})

	prompted, err := svc.HasBeenPrompted("user1", PromptReview, 7)
	if err != nil {
		t.Fatalf("HasBeenPrompted() error: %v", err)
	}
	if !prompted {
		t.Error("HasBeenPrompted() = false, want true")
	}

	db.promptCount = 0
	prompted, err = svc.HasBeenPrompted("user1", PromptReview, 7)
	if err != nil {
		t.Fatalf("HasBeenPrompted() error: %v", err)
	}
	if prompted {
		t.Error("HasBeenPrompted() = true, want false")
	}
}

// ── Handler Tests ───────────────────────────────────────────────────────────

func newAuthRequest(method, path, body string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r = r.WithContext(
		newContextWithUserID(r.Context(), "test-user"),
	)
	return r
}

func newContextWithUserID(parent interface{ Deadline() (time.Time, bool); Done() <-chan struct{}; Err() error; Value(any) any }, userID string) interface{ Deadline() (time.Time, bool); Done() <-chan struct{}; Err() error; Value(any) any } {
	return contextWithUserID{parent, userID}
}

type contextWithUserID struct {
	parent interface{ Deadline() (time.Time, bool); Done() <-chan struct{}; Err() error; Value(any) any }
	userID string
}

func (c contextWithUserID) Deadline() (time.Time, bool) { return c.parent.Deadline() }
func (c contextWithUserID) Done() <-chan struct{}        { return c.parent.Done() }
func (c contextWithUserID) Err() error                   { return c.parent.Err() }
func (c contextWithUserID) Value(key any) any {
	if key == middleware.CtxUserID {
		return c.userID
	}
	return c.parent.Value(key)
}

func TestHandleGetLifecycle(t *testing.T) {
	now := time.Now()
	db := &mockDB{
		createdAt:      now.Add(-10 * 24 * time.Hour),
		lastActiveAt:   now,
		totalSessions:  15,
		recentSessions: 5,
		eventDays:      3,
		isPro:          false,
		lastPromptAt:   now.Add(-5 * 24 * time.Hour),
	}

	svc := New(Config{
		AhaMomentRules: []AhaMomentRule{
			{Name: "core_usage", EventName: "log_created", Threshold: 3, WindowDays: 7},
		},
	}, db, &push.NoopProvider{})

	w := httptest.NewRecorder()
	r := newAuthRequest("GET", "/api/v1/user/lifecycle", "")

	svc.HandleGetLifecycle(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var es EngagementScore
	if err := json.NewDecoder(w.Body).Decode(&es); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if es.UserID != "test-user" {
		t.Errorf("UserID = %s, want test-user", es.UserID)
	}
	if es.Stage != StageEngaged {
		t.Errorf("Stage = %s, want %s", es.Stage, StageEngaged)
	}
}

func TestHandleAckLifecyclePrompt(t *testing.T) {
	db := &mockDB{}
	svc := New(Config{}, db, &push.NoopProvider{})

	tests := []struct {
		name   string
		body   string
		status int
	}{
		{"valid shown", `{"prompt_type":"review","action":"shown"}`, http.StatusOK},
		{"valid accepted", `{"prompt_type":"paywall","action":"accepted"}`, http.StatusOK},
		{"valid dismissed", `{"prompt_type":"winback","action":"dismissed"}`, http.StatusOK},
		{"missing fields", `{"prompt_type":"review"}`, http.StatusBadRequest},
		{"invalid action", `{"prompt_type":"review","action":"clicked"}`, http.StatusBadRequest},
		{"empty body", `{}`, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := newAuthRequest("POST", "/api/v1/user/lifecycle/ack", tt.body)
			r.Header.Set("Content-Type", "application/json")

			svc.HandleAckLifecyclePrompt(w, r)

			if w.Code != tt.status {
				t.Errorf("status = %d, want %d, body: %s", w.Code, tt.status, w.Body.String())
			}
		})
	}

	// Verify recorded events
	if len(db.recorded) != 3 {
		t.Errorf("recorded %d events, want 3", len(db.recorded))
	}
}

// ── EvaluateNotifications Tests ─────────────────────────────────────────────

func TestEvaluateNotifications(t *testing.T) {
	now := time.Now()

	sent := []string{}
	mockPush := &trackingPush{sent: &sent}

	db := &mockDB{
		createdAt:      now.Add(-60 * 24 * time.Hour),
		lastActiveAt:   now.Add(-10 * 24 * time.Hour), // 10 days inactive → at_risk
		totalSessions:  20,
		recentSessions: 0,
		eventDays:      0,
		isPro:          false,
		lastPromptAt:   now.Add(-5 * 24 * time.Hour),
		tokens:         []string{"token1", "token2"},
	}

	svc := New(Config{}, db, mockPush)

	svc.EvaluateNotifications([]string{"user1"})

	if len(sent) != 2 {
		t.Errorf("sent %d pushes, want 2", len(sent))
	}
}

type trackingPush struct {
	sent *[]string
}

func (p *trackingPush) Send(token, title, body string) error {
	*p.sent = append(*p.sent, token)
	return nil
}
func (p *trackingPush) SendWithData(token, title, body string, data map[string]string) error {
	return nil
}
func (p *trackingPush) SendSilent(token string, data map[string]string) error { return nil }

// ── Prompt for Each Stage ───────────────────────────────────────────────────

func TestDeterminePrompt_AllStages(t *testing.T) {
	db := &mockDB{lastPromptAt: time.Time{}} // no cooldown
	svc := New(Config{}, db, &push.NoopProvider{})

	tests := []struct {
		stage    Stage
		isPro    bool
		wantType PromptType
		wantNil  bool
	}{
		{StageNew, false, "", true},
		{StageActivated, false, PromptPaywall, false},
		{StageEngaged, false, PromptPaywall, false},
		{StageEngaged, true, PromptReview, false},
		{StageMonetized, false, "", true}, // monetized but not matched in prompt logic (only engaged/loyal/etc)
		{StageLoyal, true, PromptMilestone, false},
		{StageAtRisk, false, PromptWinback, false},
		{StageDormant, false, PromptWinback, false},
		{StageChurned, false, PromptWinback, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.stage), func(t *testing.T) {
			es := &EngagementScore{Stage: tt.stage, IsPro: tt.isPro}
			prompt, err := svc.DeterminePrompt("user1", es)
			if err != nil {
				t.Fatalf("DeterminePrompt() error: %v", err)
			}
			if tt.wantNil {
				if prompt != nil {
					t.Errorf("prompt = %+v, want nil", prompt)
				}
				return
			}
			if prompt == nil {
				t.Fatal("prompt = nil, want non-nil")
			}
			if prompt.Type != tt.wantType {
				t.Errorf("prompt.Type = %s, want %s", prompt.Type, tt.wantType)
			}
		})
	}
}
