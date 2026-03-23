package postgres

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/pacosw1/donkeygo/admin"
)

// ── admin.AdminDB ──────────────────────────────────────────────────────────

// AdminListUsers returns a paginated list of users with search and subscription status.
func (d *DB) AdminListUsers(search string, limit, offset int) ([]admin.AdminUser, int, error) {
	// Count total
	var total int
	if search != "" {
		like := "%" + search + "%"
		d.db.QueryRow(`SELECT COUNT(*) FROM users WHERE name ILIKE $1 OR email ILIKE $2`, like, like).Scan(&total)
	} else {
		d.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&total)
	}

	query := `
		SELECT u.id, u.email, u.name,
			COALESCE(s.status, 'free') AS status,
			u.created_at, u.last_login_at
		FROM users u
		LEFT JOIN user_subscriptions s ON s.user_id = u.id
	`
	args := []any{}
	paramIdx := 1

	if search != "" {
		like := "%" + search + "%"
		query += fmt.Sprintf(` WHERE u.name ILIKE $%d OR u.email ILIKE $%d`, paramIdx, paramIdx+1)
		args = append(args, like, like)
		paramIdx += 2
	}
	query += fmt.Sprintf(` ORDER BY u.last_login_at DESC LIMIT $%d OFFSET $%d`, paramIdx, paramIdx+1)
	args = append(args, limit, offset)

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("admin list users: %w", err)
	}
	defer rows.Close()

	var users []admin.AdminUser
	for rows.Next() {
		var u admin.AdminUser
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Status, &u.CreatedAt, &u.LastLoginAt); err != nil {
			return nil, 0, fmt.Errorf("admin list users scan: %w", err)
		}
		users = append(users, u)
	}
	return users, total, rows.Err()
}

// AdminGetUser returns the full detail for a single user.
func (d *DB) AdminGetUser(userID string) (*admin.AdminUserDetail, error) {
	var u admin.AdminUser
	err := d.db.QueryRow(`
		SELECT id, email, name, created_at, last_login_at
		FROM users WHERE id = $1`, userID,
	).Scan(&u.ID, &u.Email, &u.Name, &u.CreatedAt, &u.LastLoginAt)
	if err != nil {
		return nil, fmt.Errorf("admin get user: %w", err)
	}

	detail := &admin.AdminUserDetail{AdminUser: u}

	// Subscription
	var sub admin.SubInfo
	var expiresAt sql.NullTime
	err = d.db.QueryRow(`
		SELECT product_id, status, expires_at
		FROM user_subscriptions WHERE user_id = $1`, userID,
	).Scan(&sub.ProductID, &sub.Status, &expiresAt)
	if err == nil {
		if expiresAt.Valid {
			sub.ExpiresAt = &expiresAt.Time
		}
		detail.Subscription = &sub
		detail.Status = sub.Status
	} else {
		detail.Status = "free"
	}

	// Counts
	d.db.QueryRow(`SELECT COUNT(*) FROM user_activity WHERE user_id = $1`, userID).Scan(&detail.EventCount)
	d.db.QueryRow(`SELECT COUNT(*) FROM user_sessions WHERE user_id = $1`, userID).Scan(&detail.SessionCount)
	d.db.QueryRow(`SELECT COUNT(*) FROM user_device_tokens WHERE user_id = $1 AND enabled = TRUE`, userID).Scan(&detail.DeviceCount)

	return detail, nil
}

// AdminListEvents returns recent events with optional filters.
func (d *DB) AdminListEvents(eventType, userID string, since time.Time, limit int) ([]admin.AdminEvent, error) {
	query := `
		SELECT a.user_id, COALESCE(u.email, '') AS user_email,
			a.event, COALESCE(a.metadata::text, '{}'), a.created_at
		FROM user_activity a
		LEFT JOIN users u ON u.id = a.user_id
		WHERE a.created_at >= $1
	`
	args := []any{since}
	paramIdx := 2

	if eventType != "" {
		query += fmt.Sprintf(` AND a.event = $%d`, paramIdx)
		args = append(args, eventType)
		paramIdx++
	}
	if userID != "" {
		query += fmt.Sprintf(` AND a.user_id = $%d`, paramIdx)
		args = append(args, userID)
		paramIdx++
	}

	query += fmt.Sprintf(` ORDER BY a.created_at DESC LIMIT $%d`, paramIdx)
	args = append(args, limit)

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("admin list events: %w", err)
	}
	defer rows.Close()

	var events []admin.AdminEvent
	for rows.Next() {
		var e admin.AdminEvent
		if err := rows.Scan(&e.UserID, &e.UserEmail, &e.Event, &e.Metadata, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("admin list events scan: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// AdminListNotifications returns recent notification deliveries.
func (d *DB) AdminListNotifications(limit int) ([]admin.AdminNotification, error) {
	rows, err := d.db.Query(`
		SELECT user_id, kind, title, body, status, sent_at
		FROM notification_deliveries
		ORDER BY sent_at DESC
		LIMIT $1`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("admin list notifications: %w", err)
	}
	defer rows.Close()

	var notifs []admin.AdminNotification
	for rows.Next() {
		var n admin.AdminNotification
		if err := rows.Scan(&n.UserID, &n.Kind, &n.Title, &n.Body, &n.Status, &n.SentAt); err != nil {
			return nil, fmt.Errorf("admin list notifications scan: %w", err)
		}
		notifs = append(notifs, n)
	}
	return notifs, rows.Err()
}

// AdminSubscriptionBreakdown returns subscription counts grouped by status.
func (d *DB) AdminSubscriptionBreakdown() ([]admin.SubBreakdownRow, error) {
	rows, err := d.db.Query(`SELECT status, COUNT(*) FROM user_subscriptions GROUP BY status ORDER BY COUNT(*) DESC`)
	if err != nil {
		return nil, fmt.Errorf("admin subscription breakdown: %w", err)
	}
	defer rows.Close()

	var result []admin.SubBreakdownRow
	for rows.Next() {
		var r admin.SubBreakdownRow
		if err := rows.Scan(&r.Status, &r.Count); err != nil {
			return nil, fmt.Errorf("admin subscription breakdown scan: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// AdminListFeedback returns recent user feedback submissions.
func (d *DB) AdminListFeedback(limit int) ([]admin.AdminFeedback, error) {
	rows, err := d.db.Query(`
		SELECT f.user_id, COALESCE(u.email, '') AS user_email,
			f.type, f.message, f.app_version, f.created_at
		FROM user_feedback f
		LEFT JOIN users u ON u.id = f.user_id
		ORDER BY f.created_at DESC
		LIMIT $1`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("admin list feedback: %w", err)
	}
	defer rows.Close()

	var feedback []admin.AdminFeedback
	for rows.Next() {
		var fb admin.AdminFeedback
		if err := rows.Scan(&fb.UserID, &fb.UserEmail, &fb.Type, &fb.Message, &fb.AppVersion, &fb.CreatedAt); err != nil {
			return nil, fmt.Errorf("admin list feedback scan: %w", err)
		}
		feedback = append(feedback, fb)
	}
	return feedback, rows.Err()
}
