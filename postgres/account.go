package postgres

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/pacosw1/donkeygo/account"
)

// ── account.AccountDB ──────────────────────────────────────────────────────

// GetUserEmail returns the user's email.
func (d *DB) GetUserEmail(userID string) (string, error) {
	var email string
	err := d.db.QueryRow(`SELECT email FROM users WHERE id = $1`, userID).Scan(&email)
	if err != nil {
		return "", fmt.Errorf("get user email: %w", err)
	}
	return email, nil
}

// DeleteUserData removes all user data from donkeygo-managed tables (except users).
func (d *DB) DeleteUserData(userID string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	tables := []string{
		"feature_flag_overrides WHERE user_id = $1",
		"verified_transactions WHERE user_id = $1",
		"tombstones WHERE user_id = $1",
		"chat_messages WHERE user_id = $1",
		"notification_deliveries WHERE user_id = $1",
		"user_notification_preferences WHERE user_id = $1",
		"user_device_tokens WHERE user_id = $1",
		"user_sessions WHERE user_id = $1",
		"user_feedback WHERE user_id = $1",
		"user_activity WHERE user_id = $1",
		"user_subscriptions WHERE user_id = $1",
	}

	for _, clause := range tables {
		if _, err := tx.Exec("DELETE FROM "+clause, userID); err != nil {
			// Table might not exist if that package wasn't used — skip
			continue
		}
	}

	return tx.Commit()
}

// DeleteUser removes the user from the users table.
func (d *DB) DeleteUser(userID string) error {
	_, err := d.db.Exec(`DELETE FROM users WHERE id = $1`, userID)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}

// AnonymizeUser replaces PII with anonymized values but keeps the record.
func (d *DB) AnonymizeUser(userID string) error {
	_, err := d.db.Exec(`
		UPDATE users SET
			email = 'deleted-' || id || '@anonymized',
			name = 'Deleted User',
			apple_sub = 'anon-' || id
		WHERE id = $1`, userID)
	if err != nil {
		return fmt.Errorf("anonymize user: %w", err)
	}
	return nil
}

// ExportUserData returns all user data across donkeygo tables.
func (d *DB) ExportUserData(userID string) (*account.UserDataExport, error) {
	export := &account.UserDataExport{}

	// User profile
	var user struct {
		ID        string    `json:"id"`
		Email     string    `json:"email"`
		Name      string    `json:"name"`
		CreatedAt time.Time `json:"created_at"`
	}
	err := d.db.QueryRow(`SELECT id, email, name, created_at FROM users WHERE id = $1`, userID).
		Scan(&user.ID, &user.Email, &user.Name, &user.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("export user: %w", err)
	}
	export.User = user

	// Subscription
	var sub struct {
		ProductID string     `json:"product_id"`
		Status    string     `json:"status"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	var expiresAt sql.NullTime
	if err := d.db.QueryRow(`SELECT product_id, status, expires_at FROM user_subscriptions WHERE user_id = $1`, userID).
		Scan(&sub.ProductID, &sub.Status, &expiresAt); err == nil {
		if expiresAt.Valid {
			sub.ExpiresAt = &expiresAt.Time
		}
		export.Subscription = sub
	}

	// Events (last 1000)
	export.Events = d.exportRows(`SELECT event, metadata::text, created_at FROM user_activity WHERE user_id = $1 ORDER BY created_at DESC LIMIT 1000`, userID)

	// Sessions
	export.Sessions = d.exportRows(`SELECT id, started_at, ended_at, duration_s FROM user_sessions WHERE user_id = $1 ORDER BY started_at DESC LIMIT 500`, userID)

	// Feedback
	export.Feedback = d.exportRows(`SELECT type, message, app_version, created_at FROM user_feedback WHERE user_id = $1 ORDER BY created_at DESC`, userID)

	// Chat messages
	export.ChatMessages = d.exportRows(`SELECT sender, message, message_type, created_at FROM chat_messages WHERE user_id = $1 ORDER BY created_at`, userID)

	// Device tokens
	export.DeviceTokens = d.exportRows(`SELECT platform, device_model, app_version, enabled FROM user_device_tokens WHERE user_id = $1`, userID)

	// Notification preferences
	export.Preferences = d.exportRows(`SELECT push_enabled, timezone, wake_hour, sleep_hour FROM user_notification_preferences WHERE user_id = $1`, userID)

	// Transactions
	export.Transactions = d.exportRows(`SELECT product_id, status, purchase_date, expires_date, price_cents, currency_code FROM verified_transactions WHERE user_id = $1`, userID)

	return export, nil
}

// exportRows runs a query and returns results as []map[string]any.
func (d *DB) exportRows(query, userID string) any {
	rows, err := d.db.Query(query, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil
	}

	var result []map[string]any
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			continue
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			row[col] = values[i]
		}
		result = append(result, row)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
