package postgres

import (
	"database/sql"
	"fmt"

	"github.com/pacosw1/donkeygo/auth"
)

// ── auth.AuthDB ─────────────────────────────────────────────────────────────

// UpsertUserByAppleSub inserts a new user or updates an existing one matched by apple_sub.
func (d *DB) UpsertUserByAppleSub(id, appleSub, email, name string) (*auth.User, error) {
	u := &auth.User{}
	err := d.db.QueryRow(`
		INSERT INTO users (id, apple_sub, email, name, created_at, last_login_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
		ON CONFLICT (apple_sub) DO UPDATE SET
			email         = COALESCE(NULLIF($3, ''), users.email),
			name          = COALESCE(NULLIF($4, ''), users.name),
			last_login_at = NOW()
		RETURNING id, apple_sub, email, name, created_at, last_login_at`,
		id, appleSub, email, name,
	).Scan(&u.ID, &u.AppleSub, &u.Email, &u.Name, &u.CreatedAt, &u.LastLoginAt)
	if err != nil {
		return nil, fmt.Errorf("upsert user: %w", err)
	}
	return u, nil
}

// UserByID returns a user by primary key.
func (d *DB) UserByID(id string) (*auth.User, error) {
	u := &auth.User{}
	err := d.db.QueryRow(`
		SELECT id, apple_sub, email, name, created_at, last_login_at
		FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.AppleSub, &u.Email, &u.Name, &u.CreatedAt, &u.LastLoginAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return u, nil
}

// ── attest.AttestDB ─────────────────────────────────────────────────────────

// StoreAttestKey upserts a device attestation key for the given user.
func (d *DB) StoreAttestKey(userID, keyID string) error {
	_, err := d.db.Exec(`
		INSERT INTO user_attest_keys (user_id, key_id)
		VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET key_id = $2`,
		userID, keyID,
	)
	if err != nil {
		return fmt.Errorf("store attest key: %w", err)
	}
	return nil
}

// GetAttestKey returns the attestation key ID for a user.
func (d *DB) GetAttestKey(userID string) (string, error) {
	var keyID string
	err := d.db.QueryRow(`
		SELECT key_id FROM user_attest_keys WHERE user_id = $1`, userID,
	).Scan(&keyID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get attest key: %w", err)
	}
	return keyID, nil
}
