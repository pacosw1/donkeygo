package postgres

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/pacosw1/donkeygo/engage"
)

// ── engage.EngageDB ──────────────────────────────────────────────────────────

// TrackEvents batch-inserts user activity events.
func (d *DB) TrackEvents(userID string, events []engage.EventInput) error {
	if len(events) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString(`INSERT INTO user_activity (user_id, event, metadata, created_at) VALUES `)
	args := make([]any, 0, len(events)*4)

	for i, e := range events {
		if i > 0 {
			b.WriteString(", ")
		}
		idx := i * 4
		fmt.Fprintf(&b, "($%d, $%d, $%d, $%d)", idx+1, idx+2, idx+3, idx+4)

		meta := e.Metadata
		if meta == "" {
			meta = "{}"
		}

		var ts time.Time
		if e.Timestamp != "" {
			if parsed, err := time.Parse(time.RFC3339, e.Timestamp); err == nil {
				ts = parsed.UTC()
			} else {
				ts = time.Now().UTC()
			}
		} else {
			ts = time.Now().UTC()
		}

		args = append(args, userID, e.Event, meta, ts)
	}

	_, err := d.db.Exec(b.String(), args...)
	if err != nil {
		return fmt.Errorf("track events: %w", err)
	}
	return nil
}

// UpdateSubscription upserts the user's subscription record.
func (d *DB) UpdateSubscription(userID, productID, status string, expiresAt *time.Time) error {
	_, err := d.db.Exec(`
		INSERT INTO user_subscriptions (user_id, product_id, status, expires_at, started_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			product_id = $2,
			status     = $3,
			expires_at = $4,
			updated_at = NOW()`,
		userID, productID, status, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("update subscription: %w", err)
	}
	return nil
}

// UpdateSubscriptionDetails updates transaction and pricing info on an existing subscription.
func (d *DB) UpdateSubscriptionDetails(userID, originalTransactionID string, priceCents int, currencyCode string) error {
	_, err := d.db.Exec(`
		UPDATE user_subscriptions
		SET original_transaction_id = $2, price_cents = $3, currency_code = $4, updated_at = NOW()
		WHERE user_id = $1`,
		userID, originalTransactionID, priceCents, currencyCode,
	)
	if err != nil {
		return fmt.Errorf("update subscription details: %w", err)
	}
	return nil
}

// GetSubscription returns the user's subscription or nil if none exists.
func (d *DB) GetSubscription(userID string) (*engage.UserSubscription, error) {
	s := &engage.UserSubscription{}
	err := d.db.QueryRow(`
		SELECT user_id, product_id, status, expires_at, started_at, updated_at
		FROM user_subscriptions WHERE user_id = $1`, userID,
	).Scan(&s.UserID, &s.ProductID, &s.Status, &s.ExpiresAt, &s.StartedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get subscription: %w", err)
	}
	return s, nil
}

// IsProUser returns true if the user has an active or trial subscription that has not expired.
func (d *DB) IsProUser(userID string) (bool, error) {
	var exists bool
	err := d.db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM user_subscriptions
			WHERE user_id = $1
			  AND status IN ('active', 'trial')
			  AND (expires_at IS NULL OR expires_at > NOW())
		)`, userID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("is pro user: %w", err)
	}
	return exists, nil
}

// GetEngagementData computes engagement metrics from activity and subscription data.
func (d *DB) GetEngagementData(userID string) (*engage.EngagementData, error) {
	data := &engage.EngagementData{}

	// Aggregate activity metrics in a single query.
	err := d.db.QueryRow(`
		SELECT
			COUNT(DISTINCT DATE(created_at)),
			COUNT(*),
			COUNT(*) FILTER (WHERE event = 'paywall_shown'),
			COALESCE(MAX(created_at) FILTER (WHERE event = 'paywall_shown'), '1970-01-01')::TEXT,
			COUNT(*) FILTER (WHERE event = 'goal_completed')
		FROM user_activity
		WHERE user_id = $1`, userID,
	).Scan(
		&data.DaysActive,
		&data.TotalLogs,
		&data.PaywallShownCount,
		&data.LastPaywallDate,
		&data.GoalsCompletedTotal,
	)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("get engagement data: %w", err)
	}

	// Subscription status.
	var status sql.NullString
	_ = d.db.QueryRow(`
		SELECT status FROM user_subscriptions WHERE user_id = $1`, userID,
	).Scan(&status)
	if status.Valid {
		data.SubscriptionStatus = status.String
	} else {
		data.SubscriptionStatus = "free"
	}

	// Current streak: consecutive days with activity counting back from today.
	data.CurrentStreak = d.computeStreak(userID)

	return data, nil
}

// computeStreak counts consecutive days (from today backwards) with at least one activity row.
func (d *DB) computeStreak(userID string) int {
	rows, err := d.db.Query(`
		SELECT DISTINCT DATE(created_at) AS d
		FROM user_activity
		WHERE user_id = $1
		ORDER BY d DESC`, userID,
	)
	if err != nil {
		return 0
	}
	defer rows.Close()

	streak := 0
	expected := time.Now().UTC().Truncate(24 * time.Hour)

	for rows.Next() {
		var day time.Time
		if err := rows.Scan(&day); err != nil {
			break
		}
		day = day.Truncate(24 * time.Hour)
		if day.Equal(expected) {
			streak++
			expected = expected.AddDate(0, 0, -1)
		} else if day.Before(expected) {
			break
		}
	}
	return streak
}

// StartSession inserts a new session record.
func (d *DB) StartSession(userID, sessionID, appVersion, osVersion, country string) error {
	_, err := d.db.Exec(`
		INSERT INTO user_sessions (id, user_id, app_version, os_version, country)
		VALUES ($1, $2, $3, $4, $5)`,
		sessionID, userID, appVersion, osVersion, country,
	)
	if err != nil {
		return fmt.Errorf("start session: %w", err)
	}
	return nil
}

// EndSession updates an existing session with its end time and duration.
func (d *DB) EndSession(userID, sessionID string, durationS int) error {
	_, err := d.db.Exec(`
		UPDATE user_sessions
		SET ended_at = NOW(), duration_s = $1
		WHERE id = $2 AND user_id = $3`,
		durationS, sessionID, userID,
	)
	if err != nil {
		return fmt.Errorf("end session: %w", err)
	}
	return nil
}

// SaveFeedback inserts a user feedback record.
func (d *DB) SaveFeedback(userID, feedbackType, message, appVersion string) error {
	_, err := d.db.Exec(`
		INSERT INTO user_feedback (user_id, type, message, app_version)
		VALUES ($1, $2, $3, $4)`,
		userID, feedbackType, message, appVersion,
	)
	if err != nil {
		return fmt.Errorf("save feedback: %w", err)
	}
	return nil
}
