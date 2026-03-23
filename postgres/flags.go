package postgres

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/pacosw1/donkeygo/flags"
)

// ── flags.FlagsDB ──────────────────────────────────────────────────────────

// UpsertFlag creates or updates a feature flag.
func (d *DB) UpsertFlag(f *flags.Flag) error {
	_, err := d.db.Exec(`
		INSERT INTO feature_flags (key, enabled, rollout_pct, description, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
		ON CONFLICT (key) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			rollout_pct = EXCLUDED.rollout_pct,
			description = EXCLUDED.description,
			updated_at = NOW()`,
		f.Key, f.Enabled, f.RolloutPct, f.Description,
	)
	if err != nil {
		return fmt.Errorf("upsert flag: %w", err)
	}
	return nil
}

// GetFlag retrieves a feature flag by key.
func (d *DB) GetFlag(key string) (*flags.Flag, error) {
	var f flags.Flag
	err := d.db.QueryRow(`
		SELECT key, enabled, rollout_pct, description, created_at, updated_at
		FROM feature_flags WHERE key = $1`, key,
	).Scan(&f.Key, &f.Enabled, &f.RolloutPct, &f.Description, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get flag: %w", err)
	}
	return &f, nil
}

// ListFlags returns all feature flags.
func (d *DB) ListFlags() ([]*flags.Flag, error) {
	rows, err := d.db.Query(`
		SELECT key, enabled, rollout_pct, description, created_at, updated_at
		FROM feature_flags ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("list flags: %w", err)
	}
	defer rows.Close()

	var result []*flags.Flag
	for rows.Next() {
		var f flags.Flag
		if err := rows.Scan(&f.Key, &f.Enabled, &f.RolloutPct, &f.Description, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("list flags scan: %w", err)
		}
		result = append(result, &f)
	}
	return result, rows.Err()
}

// DeleteFlag removes a feature flag and its overrides.
func (d *DB) DeleteFlag(key string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	tx.Exec(`DELETE FROM feature_flag_overrides WHERE flag_key = $1`, key)
	if _, err := tx.Exec(`DELETE FROM feature_flags WHERE key = $1`, key); err != nil {
		return fmt.Errorf("delete flag: %w", err)
	}
	return tx.Commit()
}

// GetUserOverride returns a user-specific flag override, or nil if none exists.
func (d *DB) GetUserOverride(key, userID string) (*bool, error) {
	var enabled bool
	err := d.db.QueryRow(`
		SELECT enabled FROM feature_flag_overrides
		WHERE flag_key = $1 AND user_id = $2`, key, userID,
	).Scan(&enabled)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user override: %w", err)
	}
	return &enabled, nil
}

// SetUserOverride creates or updates a user-specific flag override.
func (d *DB) SetUserOverride(key, userID string, enabled bool) error {
	_, err := d.db.Exec(`
		INSERT INTO feature_flag_overrides (flag_key, user_id, enabled)
		VALUES ($1, $2, $3)
		ON CONFLICT (flag_key, user_id) DO UPDATE SET enabled = EXCLUDED.enabled`,
		key, userID, enabled,
	)
	if err != nil {
		return fmt.Errorf("set user override: %w", err)
	}
	return nil
}

// DeleteUserOverride removes a user-specific flag override.
func (d *DB) DeleteUserOverride(key, userID string) error {
	_, err := d.db.Exec(`
		DELETE FROM feature_flag_overrides WHERE flag_key = $1 AND user_id = $2`,
		key, userID,
	)
	if err != nil {
		return fmt.Errorf("delete user override: %w", err)
	}
	return nil
}

// ensure compile-time interface check
var _ flags.FlagsDB = (*DB)(nil)
var _ = (*DB)(nil).UpsertFlag
var _ = (*DB)(nil).GetUserOverride
var _ time.Time
