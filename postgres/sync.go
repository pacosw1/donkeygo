package postgres

import (
	"fmt"
	"time"

	"github.com/pacosw1/donkeygo/sync"
)

// ── sync.SyncDB ─────────────────────────────────────────────────────────────

// ServerTime returns the current database server time.
func (d *DB) ServerTime() (time.Time, error) {
	var t time.Time
	err := d.db.QueryRow(`SELECT NOW()`).Scan(&t)
	if err != nil {
		return time.Time{}, fmt.Errorf("server time: %w", err)
	}
	return t, nil
}

// Tombstones returns deleted entity records since the given time for a user.
func (d *DB) Tombstones(userID string, since time.Time) ([]sync.DeletedEntry, error) {
	rows, err := d.db.Query(`
		SELECT entity_type, entity_id, deleted_at
		FROM tombstones
		WHERE user_id = $1 AND deleted_at > $2
		ORDER BY deleted_at ASC`,
		userID, since,
	)
	if err != nil {
		return nil, fmt.Errorf("tombstones: %w", err)
	}
	defer rows.Close()

	var entries []sync.DeletedEntry
	for rows.Next() {
		var e sync.DeletedEntry
		if err := rows.Scan(&e.EntityType, &e.EntityID, &e.DeletedAt); err != nil {
			return nil, fmt.Errorf("scan tombstone: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// RecordTombstone records a deletion for sync propagation.
// Uses INSERT ON CONFLICT to update the deleted_at timestamp if the record already exists.
func (d *DB) RecordTombstone(userID, entityType, entityID string) error {
	_, err := d.db.Exec(`
		INSERT INTO tombstones (entity_type, entity_id, user_id, deleted_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (entity_type, entity_id, user_id)
		DO UPDATE SET deleted_at = NOW()`,
		entityType, entityID, userID,
	)
	if err != nil {
		return fmt.Errorf("record tombstone: %w", err)
	}
	return nil
}

// ── sync.DeviceTokenStore ───────────────────────────────────────────────────

// EnabledTokensForUser returns all active push tokens for the given user.
func (d *DB) EnabledTokensForUser(userID string) ([]sync.DeviceInfo, error) {
	rows, err := d.db.Query(`
		SELECT id, token
		FROM user_device_tokens
		WHERE user_id = $1 AND enabled = true`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("enabled tokens for user: %w", err)
	}
	defer rows.Close()

	var devices []sync.DeviceInfo
	for rows.Next() {
		var d sync.DeviceInfo
		if err := rows.Scan(&d.DeviceID, &d.Token); err != nil {
			return nil, fmt.Errorf("scan device info: %w", err)
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}
