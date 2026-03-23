package postgres

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ── lifecycle.LifecycleDB ──────────────────────────────────────────────────

// UserCreatedAndLastActive returns when the user was created and their most recent activity.
func (d *DB) UserCreatedAndLastActive(userID string) (createdAt, lastActiveAt time.Time, err error) {
	err = d.db.QueryRow(`
		SELECT u.created_at, COALESCE(MAX(a.created_at), u.created_at)
		FROM users u
		LEFT JOIN user_activity a ON a.user_id = u.id
		WHERE u.id = $1
		GROUP BY u.created_at`, userID,
	).Scan(&createdAt, &lastActiveAt)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("user created and last active: %w", err)
	}
	return createdAt, lastActiveAt, nil
}

// CountSessions returns the total number of sessions for a user.
func (d *DB) CountSessions(userID string) (int, error) {
	var count int
	err := d.db.QueryRow(`
		SELECT COUNT(*) FROM user_sessions WHERE user_id = $1`, userID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count sessions: %w", err)
	}
	return count, nil
}

// CountRecentSessions returns the number of sessions for a user since the given time.
func (d *DB) CountRecentSessions(userID string, since time.Time) (int, error) {
	var count int
	err := d.db.QueryRow(`
		SELECT COUNT(*) FROM user_sessions
		WHERE user_id = $1 AND started_at >= $2`, userID, since,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count recent sessions: %w", err)
	}
	return count, nil
}

// CountDistinctEventDays returns the number of distinct days with a given event for a user.
func (d *DB) CountDistinctEventDays(userID, eventName string, since time.Time) (int, error) {
	var count int
	err := d.db.QueryRow(`
		SELECT COUNT(DISTINCT DATE(created_at))
		FROM user_activity
		WHERE user_id = $1 AND event = $2 AND created_at >= $3`, userID, eventName, since,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count distinct event days: %w", err)
	}
	return count, nil
}

// LastPrompt returns the most recent prompt event type and timestamp for a user.
// Returns empty string and zero time if no prompt has been recorded.
func (d *DB) LastPrompt(userID string) (promptType string, promptAt time.Time, err error) {
	var event string
	err = d.db.QueryRow(`
		SELECT event, created_at FROM user_activity
		WHERE user_id = $1 AND event LIKE 'prompt_%'
		ORDER BY created_at DESC LIMIT 1`, userID,
	).Scan(&event, &promptAt)
	if err == sql.ErrNoRows {
		return "", time.Time{}, nil
	}
	if err != nil {
		return "", time.Time{}, fmt.Errorf("last prompt: %w", err)
	}
	promptType = strings.TrimPrefix(event, "prompt_")
	return promptType, promptAt, nil
}

// CountPrompts returns the number of times a specific prompt type was shown to a user since the given time.
func (d *DB) CountPrompts(userID, promptType string, since time.Time) (int, error) {
	var count int
	err := d.db.QueryRow(`
		SELECT COUNT(*) FROM user_activity
		WHERE user_id = $1 AND event = $2 AND created_at >= $3`, userID, promptType, since,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count prompts: %w", err)
	}
	return count, nil
}

// RecordPrompt inserts a prompt event into user_activity.
func (d *DB) RecordPrompt(userID, event, metadata string) error {
	_, err := d.db.Exec(`
		INSERT INTO user_activity (user_id, event, metadata) VALUES ($1, $2, $3)`,
		userID, event, metadata,
	)
	if err != nil {
		return fmt.Errorf("record prompt: %w", err)
	}
	return nil
}

// ── LifecycleDBAdapter ─────────────────────────────────────────────────────

// LifecycleDBAdapter wraps *DB to satisfy lifecycle.LifecycleDB.
//
// The lifecycle.LifecycleDB interface defines EnabledDeviceTokens(userID string) ([]string, error),
// which returns plain token strings. The notify.NotifyDB interface defines the same method name
// but returns []*notify.DeviceToken. Since Go does not allow a single struct to implement two
// interfaces with the same method name but different return types, this adapter bridges the gap.
//
// All other lifecycle.LifecycleDB methods delegate directly to *DB.
type LifecycleDBAdapter struct {
	*DB
}

// EnabledDeviceTokens returns enabled push token strings for a user, satisfying lifecycle.LifecycleDB.
func (a *LifecycleDBAdapter) EnabledDeviceTokens(userID string) ([]string, error) {
	rows, err := a.db.Query(`
		SELECT token
		FROM user_device_tokens
		WHERE user_id = $1 AND enabled = true`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("enabled device tokens (lifecycle): %w", err)
	}
	defer rows.Close()

	var tokens []string
	for rows.Next() {
		var tok string
		if err := rows.Scan(&tok); err != nil {
			return nil, fmt.Errorf("enabled device tokens scan: %w", err)
		}
		tokens = append(tokens, tok)
	}
	return tokens, rows.Err()
}
