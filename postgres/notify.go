package postgres

import (
	"database/sql"
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/pacosw1/donkeygo/notify"
)

// ── notify.NotifyDB ──────────────────────────────────────────────────────────

// UpsertDeviceToken inserts or updates a device token for the given user.
func (d *DB) UpsertDeviceToken(dt *notify.DeviceToken) error {
	_, err := d.db.Exec(`
		INSERT INTO user_device_tokens (id, user_id, token, platform, device_model, os_version, app_version, enabled, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, TRUE, NOW())
		ON CONFLICT (user_id, token) DO UPDATE SET
			id           = $1,
			platform     = $4,
			device_model = $5,
			os_version   = $6,
			app_version  = $7,
			enabled      = TRUE,
			last_seen_at = NOW()`,
		dt.ID, dt.UserID, dt.Token, dt.Platform, dt.DeviceModel, dt.OSVersion, dt.AppVersion,
	)
	if err != nil {
		return fmt.Errorf("upsert device token: %w", err)
	}
	return nil
}

// DisableDeviceToken marks a device token as disabled.
func (d *DB) DisableDeviceToken(userID, token string) error {
	_, err := d.db.Exec(`
		UPDATE user_device_tokens SET enabled = FALSE
		WHERE user_id = $1 AND token = $2`,
		userID, token,
	)
	if err != nil {
		return fmt.Errorf("disable device token: %w", err)
	}
	return nil
}

// EnabledDeviceTokens returns all enabled tokens for a user.
func (d *DB) EnabledDeviceTokens(userID string) ([]*notify.DeviceToken, error) {
	rows, err := d.db.Query(`
		SELECT id, user_id, token, platform, device_model, os_version, app_version, enabled, last_seen_at
		FROM user_device_tokens
		WHERE user_id = $1 AND enabled = TRUE`, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("enabled device tokens: %w", err)
	}
	defer rows.Close()

	var tokens []*notify.DeviceToken
	for rows.Next() {
		t := &notify.DeviceToken{}
		if err := rows.Scan(&t.ID, &t.UserID, &t.Token, &t.Platform, &t.DeviceModel, &t.OSVersion, &t.AppVersion, &t.Enabled, &t.LastSeenAt); err != nil {
			return nil, fmt.Errorf("scan device token: %w", err)
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

// EnsureNotificationPreferences inserts default preferences if none exist.
func (d *DB) EnsureNotificationPreferences(userID string) {
	_, err := d.db.Exec(`
		INSERT INTO user_notification_preferences (user_id)
		VALUES ($1)
		ON CONFLICT DO NOTHING`, userID,
	)
	if err != nil {
		log.Printf("ensure notification preferences: %v", err)
	}
}

// GetNotificationPreferences returns notification preferences for a user.
func (d *DB) GetNotificationPreferences(userID string) (*notify.NotificationPreferences, error) {
	p := &notify.NotificationPreferences{}
	err := d.db.QueryRow(`
		SELECT user_id, push_enabled, interval_seconds, wake_hour, sleep_hour, timezone, stop_after_goal
		FROM user_notification_preferences
		WHERE user_id = $1`, userID,
	).Scan(&p.UserID, &p.PushEnabled, &p.IntervalSeconds, &p.WakeHour, &p.SleepHour, &p.Timezone, &p.StopAfterGoal)
	if err == sql.ErrNoRows {
		return &notify.NotificationPreferences{
			UserID:          userID,
			PushEnabled:     true,
			IntervalSeconds: 3600,
			WakeHour:        8,
			SleepHour:       22,
			Timezone:        "America/New_York",
			StopAfterGoal:   true,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get notification preferences: %w", err)
	}
	return p, nil
}

// UpsertNotificationPreferences inserts or updates notification preferences.
func (d *DB) UpsertNotificationPreferences(p *notify.NotificationPreferences) error {
	_, err := d.db.Exec(`
		INSERT INTO user_notification_preferences (user_id, push_enabled, interval_seconds, wake_hour, sleep_hour, timezone, stop_after_goal)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (user_id) DO UPDATE SET
			push_enabled     = $2,
			interval_seconds = $3,
			wake_hour        = $4,
			sleep_hour       = $5,
			timezone         = $6,
			stop_after_goal  = $7`,
		p.UserID, p.PushEnabled, p.IntervalSeconds, p.WakeHour, p.SleepHour, p.Timezone, p.StopAfterGoal,
	)
	if err != nil {
		return fmt.Errorf("upsert notification preferences: %w", err)
	}
	return nil
}

// AllUsersWithNotificationsEnabled returns user IDs that have push notifications enabled.
func (d *DB) AllUsersWithNotificationsEnabled() ([]string, error) {
	rows, err := d.db.Query(`
		SELECT user_id FROM user_notification_preferences
		WHERE push_enabled = TRUE`)
	if err != nil {
		return nil, fmt.Errorf("all users with notifications enabled: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan user id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// LastNotificationDelivery returns the most recent delivery for a user.
func (d *DB) LastNotificationDelivery(userID string) (*notify.NotificationDelivery, error) {
	nd := &notify.NotificationDelivery{}
	err := d.db.QueryRow(`
		SELECT id, user_id, kind, title, body, status, sent_at
		FROM notification_deliveries
		WHERE user_id = $1
		ORDER BY sent_at DESC
		LIMIT 1`, userID,
	).Scan(&nd.ID, &nd.UserID, &nd.Kind, &nd.Title, &nd.Body, &nd.Status, &nd.SentAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("last notification delivery: %w", err)
	}
	return nd, nil
}

// RecordNotificationDelivery inserts a notification delivery record.
func (d *DB) RecordNotificationDelivery(userID, kind, title, body string) {
	id := uuid.New().String()
	_, err := d.db.Exec(`
		INSERT INTO notification_deliveries (id, user_id, kind, title, body)
		VALUES ($1, $2, $3, $4, $5)`,
		id, userID, kind, title, body,
	)
	if err != nil {
		log.Printf("record notification delivery: %v", err)
	}
}

// TrackNotificationOpened updates the status of a notification delivery to "opened".
func (d *DB) TrackNotificationOpened(userID, notificationID string) error {
	_, err := d.db.Exec(`
		UPDATE notification_deliveries SET status = 'opened'
		WHERE id = $1 AND user_id = $2`,
		notificationID, userID,
	)
	if err != nil {
		return fmt.Errorf("track notification opened: %w", err)
	}
	return nil
}
