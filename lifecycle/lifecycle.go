// Package lifecycle provides user lifecycle stage tracking and smart prompt delivery.
package lifecycle

import (
	"log"
	"net/http"
	"time"

	"github.com/pacosw1/donkeygo/httputil"
	"github.com/pacosw1/donkeygo/middleware"
	"github.com/pacosw1/donkeygo/push"
)

// ── Lifecycle Stages ────────────────────────────────────────────────────────

// Stage represents a user's lifecycle stage.
type Stage string

const (
	StageNew       Stage = "new"
	StageActivated Stage = "activated"
	StageEngaged   Stage = "engaged"
	StageMonetized Stage = "monetized"
	StageLoyal     Stage = "loyal"
	StageAtRisk    Stage = "at_risk"
	StageDormant   Stage = "dormant"
	StageChurned   Stage = "churned"
)

// ── Types ───────────────────────────────────────────────────────────────────

// AhaMomentRule defines a rule for detecting a user's "aha moment".
type AhaMomentRule struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	EventName   string `json:"event_name"`
	Threshold   int    `json:"threshold"`
	WindowDays  int    `json:"window_days"`
}

// EngagementScore holds computed lifecycle data for a user.
type EngagementScore struct {
	UserID         string `json:"user_id"`
	Stage          Stage  `json:"stage"`
	Score          int    `json:"score"`
	DaysSinceActive int   `json:"days_since_active"`
	TotalSessions  int    `json:"total_sessions"`
	AhaReached     bool   `json:"aha_reached"`
	IsPro          bool   `json:"is_pro"`
	CreatedDaysAgo int    `json:"created_days_ago"`
	Prompt         *Prompt `json:"prompt,omitempty"`
}

// PromptType is the type of lifecycle prompt.
type PromptType string

const (
	PromptReview    PromptType = "review"
	PromptPaywall   PromptType = "paywall"
	PromptWinback   PromptType = "winback"
	PromptMilestone PromptType = "milestone"
)

// Prompt is a lifecycle prompt to show the user.
type Prompt struct {
	Type   PromptType `json:"type"`
	Title  string     `json:"title"`
	Body   string     `json:"body"`
	Reason string     `json:"reason"`
}

// ── Database Interface ──────────────────────────────────────────────────────

// LifecycleDB is the database interface required by the lifecycle package.
type LifecycleDB interface {
	UserCreatedAndLastActive(userID string) (createdAt, lastActiveAt time.Time, err error)
	CountSessions(userID string) (int, error)
	CountRecentSessions(userID string, since time.Time) (int, error)
	CountDistinctEventDays(userID, eventName string, since time.Time) (int, error)
	IsProUser(userID string) (bool, error)
	LastPrompt(userID string) (promptType string, promptAt time.Time, err error)
	CountPrompts(userID, promptType string, since time.Time) (int, error)
	RecordPrompt(userID, event, metadata string) error
	EnabledDeviceTokens(userID string) ([]string, error)
}

// ── Config & Service ────────────────────────────────────────────────────────

// Config holds lifecycle configuration.
type Config struct {
	AhaMomentRules []AhaMomentRule
}

// Service provides lifecycle tracking handlers.
type Service struct {
	cfg  Config
	db   LifecycleDB
	push push.Provider
}

// New creates a lifecycle service.
func New(cfg Config, db LifecycleDB, push push.Provider) *Service {
	return &Service{cfg: cfg, db: db, push: push}
}

// Migrations returns the SQL migrations needed by the lifecycle package.
func Migrations() []string {
	return []string{
		// lifecycle uses the user_activity table from the engage package for prompt tracking
	}
}

// ── Core Logic ──────────────────────────────────────────────────────────────

// EvaluateUser gathers user data and computes their lifecycle stage and engagement score.
func (s *Service) EvaluateUser(userID string) (*EngagementScore, error) {
	createdAt, lastActiveAt, err := s.db.UserCreatedAndLastActive(userID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	daysSinceActive := int(now.Sub(lastActiveAt).Hours() / 24)
	createdDaysAgo := int(now.Sub(createdAt).Hours() / 24)

	totalSessions, err := s.db.CountSessions(userID)
	if err != nil {
		return nil, err
	}

	recentSessions, err := s.db.CountRecentSessions(userID, now.Add(-7*24*time.Hour))
	if err != nil {
		return nil, err
	}

	ahaReached, err := s.checkAhaMoment(userID, now)
	if err != nil {
		return nil, err
	}

	isPro, err := s.db.IsProUser(userID)
	if err != nil {
		return nil, err
	}

	score := calculateScore(recentSessions, ahaReached, isPro, daysSinceActive, totalSessions)
	stage := determineStage(score, daysSinceActive, createdDaysAgo, ahaReached, isPro)

	es := &EngagementScore{
		UserID:          userID,
		Stage:           stage,
		Score:           score,
		DaysSinceActive: daysSinceActive,
		TotalSessions:   totalSessions,
		AhaReached:      ahaReached,
		IsPro:           isPro,
		CreatedDaysAgo:  createdDaysAgo,
	}

	prompt, err := s.DeterminePrompt(userID, es)
	if err != nil {
		return nil, err
	}
	es.Prompt = prompt

	return es, nil
}

func (s *Service) checkAhaMoment(userID string, now time.Time) (bool, error) {
	for _, rule := range s.cfg.AhaMomentRules {
		since := now.Add(-time.Duration(rule.WindowDays) * 24 * time.Hour)
		count, err := s.db.CountDistinctEventDays(userID, rule.EventName, since)
		if err != nil {
			return false, err
		}
		if count >= rule.Threshold {
			return true, nil
		}
	}
	return false, nil
}

func calculateScore(recentSessions int, ahaReached, isPro bool, daysSinceActive, totalSessions int) int {
	score := 0

	// Recent sessions (7d): >=7 → +40, else sessions*6
	if recentSessions >= 7 {
		score += 40
	} else {
		score += recentSessions * 6
	}

	// Aha moment: +20
	if ahaReached {
		score += 20
	}

	// Pro subscriber: +20
	if isPro {
		score += 20
	}

	// Days since active: 0 → +10, <=2 → +5
	if daysSinceActive == 0 {
		score += 10
	} else if daysSinceActive <= 2 {
		score += 5
	}

	// Total sessions: >=30 → +10, else sessions/3
	if totalSessions >= 30 {
		score += 10
	} else {
		score += totalSessions / 3
	}

	// Cap at 100
	if score > 100 {
		score = 100
	}

	return score
}

func determineStage(score, daysSinceActive, createdDaysAgo int, ahaReached, isPro bool) Stage {
	switch {
	case daysSinceActive >= 30:
		return StageChurned
	case daysSinceActive >= 14:
		return StageDormant
	case daysSinceActive >= 7 || (score < 20 && createdDaysAgo > 7):
		return StageAtRisk
	case isPro && score >= 60:
		return StageLoyal
	case isPro:
		return StageMonetized
	case score >= 40:
		return StageEngaged
	case ahaReached:
		return StageActivated
	default:
		return StageNew
	}
}

// DeterminePrompt checks cooldown and returns the appropriate prompt for the user's stage.
func (s *Service) DeterminePrompt(userID string, es *EngagementScore) (*Prompt, error) {
	// Check if user was prompted within last 3 days
	_, promptAt, err := s.db.LastPrompt(userID)
	if err == nil && !promptAt.IsZero() {
		if time.Since(promptAt) < 3*24*time.Hour {
			return nil, nil
		}
	}

	switch es.Stage {
	case StageEngaged:
		// Suggest review for engaged free users
		if !es.IsPro {
			return &Prompt{
				Type:   PromptPaywall,
				Title:  "Unlock Premium",
				Body:   "You're getting great value — upgrade to unlock everything.",
				Reason: "engaged_free_user",
			}, nil
		}
		return &Prompt{
			Type:   PromptReview,
			Title:  "Enjoying the app?",
			Body:   "Your feedback helps us improve. Leave a review?",
			Reason: "engaged_pro_user",
		}, nil

	case StageLoyal:
		return &Prompt{
			Type:   PromptMilestone,
			Title:  "You're a power user!",
			Body:   "Thanks for being a loyal subscriber.",
			Reason: "loyal_user",
		}, nil

	case StageActivated:
		return &Prompt{
			Type:   PromptPaywall,
			Title:  "Ready for more?",
			Body:   "You've discovered the core experience — unlock premium features.",
			Reason: "aha_moment_reached",
		}, nil

	case StageAtRisk:
		return &Prompt{
			Type:   PromptWinback,
			Title:  "We miss you!",
			Body:   "Come back and check out what's new.",
			Reason: "at_risk",
		}, nil

	case StageDormant:
		return &Prompt{
			Type:   PromptWinback,
			Title:  "It's been a while",
			Body:   "We've made improvements since your last visit.",
			Reason: "dormant",
		}, nil

	case StageChurned:
		return &Prompt{
			Type:   PromptWinback,
			Title:  "Welcome back",
			Body:   "A lot has changed — give us another try.",
			Reason: "churned",
		}, nil

	default:
		return nil, nil
	}
}

// HasBeenPrompted checks if a user has been shown a specific prompt type within the given days.
func (s *Service) HasBeenPrompted(userID string, promptType PromptType, withinDays int) (bool, error) {
	since := time.Now().Add(-time.Duration(withinDays) * 24 * time.Hour)
	count, err := s.db.CountPrompts(userID, string(promptType), since)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// ── Handlers ────────────────────────────────────────────────────────────────

// HandleGetLifecycle handles GET /api/v1/user/lifecycle.
func (s *Service) HandleGetLifecycle(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)

	es, err := s.EvaluateUser(userID)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to evaluate lifecycle")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, es)
}

type ackPromptRequest struct {
	PromptType string `json:"prompt_type"`
	Action     string `json:"action"` // "shown", "accepted", "dismissed"
}

// HandleAckLifecyclePrompt handles POST /api/v1/user/lifecycle/ack.
func (s *Service) HandleAckLifecyclePrompt(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(middleware.CtxUserID).(string)

	var req ackPromptRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.PromptType == "" || req.Action == "" {
		httputil.WriteError(w, http.StatusBadRequest, "prompt_type and action are required")
		return
	}

	validActions := map[string]bool{"shown": true, "accepted": true, "dismissed": true}
	if !validActions[req.Action] {
		httputil.WriteError(w, http.StatusBadRequest, "action must be one of: shown, accepted, dismissed")
		return
	}

	event := "lifecycle_prompt_" + req.Action
	metadata := `{"prompt_type":"` + req.PromptType + `"}`

	if err := s.db.RecordPrompt(userID, event, metadata); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to record prompt")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ── Scheduler Integration ───────────────────────────────────────────────────

// EvaluateNotifications evaluates a list of users and sends winback pushes to at-risk/dormant/churned users.
func (s *Service) EvaluateNotifications(userIDs []string) {
	for _, userID := range userIDs {
		es, err := s.EvaluateUser(userID)
		if err != nil {
			log.Printf("[lifecycle] evaluate %s: %v", userID, err)
			continue
		}

		if es.Stage != StageAtRisk && es.Stage != StageDormant && es.Stage != StageChurned {
			continue
		}

		if es.Prompt == nil || es.Prompt.Type != PromptWinback {
			continue
		}

		tokens, err := s.db.EnabledDeviceTokens(userID)
		if err != nil {
			log.Printf("[lifecycle] tokens %s: %v", userID, err)
			continue
		}

		for _, token := range tokens {
			if err := s.push.Send(token, es.Prompt.Title, es.Prompt.Body); err != nil {
				log.Printf("[lifecycle] push %s: %v", userID, err)
			}
		}

		_ = s.db.RecordPrompt(userID, "lifecycle_prompt_sent", `{"prompt_type":"`+string(es.Prompt.Type)+`"}`)
	}
}
