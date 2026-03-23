package postgres

import (
	"fmt"
	"time"

	"github.com/pacosw1/donkeygo/analytics"
)

// ── analytics.AnalyticsDB ──────────────────────────────────────────────────

// DAUTimeSeries returns daily active user counts since the given time.
func (d *DB) DAUTimeSeries(since time.Time) ([]analytics.DAURow, error) {
	rows, err := d.db.Query(`
		SELECT DATE(created_at) AS date, COUNT(DISTINCT user_id) AS dau
		FROM user_activity
		WHERE created_at >= $1
		GROUP BY DATE(created_at)
		ORDER BY date`, since,
	)
	if err != nil {
		return nil, fmt.Errorf("dau time series: %w", err)
	}
	defer rows.Close()

	var result []analytics.DAURow
	for rows.Next() {
		var r analytics.DAURow
		if err := rows.Scan(&r.Date, &r.DAU); err != nil {
			return nil, fmt.Errorf("dau time series scan: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// EventCounts returns event counts grouped by day and event type.
// If event is empty, all events are returned.
func (d *DB) EventCounts(since time.Time, event string) ([]analytics.EventRow, error) {
	rows, err := d.db.Query(`
		SELECT DATE(created_at) AS date, event, COUNT(*), COUNT(DISTINCT user_id)
		FROM user_activity
		WHERE created_at >= $1 AND ($2 = '' OR event = $2)
		GROUP BY DATE(created_at), event
		ORDER BY date`, since, event,
	)
	if err != nil {
		return nil, fmt.Errorf("event counts: %w", err)
	}
	defer rows.Close()

	var result []analytics.EventRow
	for rows.Next() {
		var r analytics.EventRow
		if err := rows.Scan(&r.Date, &r.Event, &r.Count, &r.UniqueUsers); err != nil {
			return nil, fmt.Errorf("event counts scan: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// SubscriptionBreakdown returns the count of subscriptions grouped by status.
func (d *DB) SubscriptionBreakdown() ([]analytics.SubStats, error) {
	rows, err := d.db.Query(`
		SELECT status, COUNT(*) FROM user_subscriptions GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("subscription breakdown: %w", err)
	}
	defer rows.Close()

	var result []analytics.SubStats
	for rows.Next() {
		var s analytics.SubStats
		if err := rows.Scan(&s.Status, &s.Count); err != nil {
			return nil, fmt.Errorf("subscription breakdown scan: %w", err)
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

// NewSubscriptions30d returns the number of new subscriptions in the last 30 days.
func (d *DB) NewSubscriptions30d() (int, error) {
	var count int
	err := d.db.QueryRow(`
		SELECT COUNT(*) FROM user_subscriptions
		WHERE started_at >= NOW() - INTERVAL '30 days'`,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("new subscriptions 30d: %w", err)
	}
	return count, nil
}

// ChurnedSubscriptions30d returns the number of expired or cancelled subscriptions in the last 30 days.
func (d *DB) ChurnedSubscriptions30d() (int, error) {
	var count int
	err := d.db.QueryRow(`
		SELECT COUNT(*) FROM user_subscriptions
		WHERE status IN ('expired', 'cancelled')
		  AND updated_at >= NOW() - INTERVAL '30 days'`,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("churned subscriptions 30d: %w", err)
	}
	return count, nil
}

// DAUToday returns the number of distinct active users today.
func (d *DB) DAUToday() (int, error) {
	var count int
	err := d.db.QueryRow(`
		SELECT COUNT(DISTINCT user_id) FROM user_activity
		WHERE DATE(created_at) = CURRENT_DATE`,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("dau today: %w", err)
	}
	return count, nil
}

// MAU returns the number of distinct active users in the last 30 days.
func (d *DB) MAU() (int, error) {
	var count int
	err := d.db.QueryRow(`
		SELECT COUNT(DISTINCT user_id) FROM user_activity
		WHERE created_at >= NOW() - INTERVAL '30 days'`,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("mau: %w", err)
	}
	return count, nil
}

// TotalUsers returns the total number of registered users.
func (d *DB) TotalUsers() (int, error) {
	var count int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("total users: %w", err)
	}
	return count, nil
}

// ActiveSubscriptions returns the number of active or trial subscriptions.
func (d *DB) ActiveSubscriptions() (int, error) {
	var count int
	err := d.db.QueryRow(`
		SELECT COUNT(*) FROM user_subscriptions
		WHERE status IN ('active', 'trial')`,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("active subscriptions: %w", err)
	}
	return count, nil
}
