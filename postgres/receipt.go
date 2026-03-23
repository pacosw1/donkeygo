package postgres

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/pacosw1/donkeygo/receipt"
)

// ── receipt.ReceiptDB ──────────────────────────────────────────────────────

// UpsertSubscription inserts or updates a user's subscription from a verified transaction.
func (d *DB) UpsertSubscription(userID, productID, originalTransactionID, status string, expiresAt *time.Time, priceCents int, currencyCode string) error {
	_, err := d.db.Exec(`
		INSERT INTO user_subscriptions (user_id, product_id, original_transaction_id, status, expires_at, price_cents, currency_code, started_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			product_id              = $2,
			original_transaction_id = $3,
			status                  = $4,
			expires_at              = $5,
			price_cents             = $6,
			currency_code           = $7,
			updated_at              = NOW()`,
		userID, productID, originalTransactionID, status, expiresAt, priceCents, currencyCode,
	)
	if err != nil {
		return fmt.Errorf("upsert subscription: %w", err)
	}
	return nil
}

// UserIDByTransactionID looks up the user who owns a given original transaction.
// It first checks user_subscriptions, then falls back to verified_transactions.
// Returns empty string and nil error if not found.
func (d *DB) UserIDByTransactionID(originalTransactionID string) (string, error) {
	var userID string

	// Try user_subscriptions first.
	err := d.db.QueryRow(`
		SELECT user_id FROM user_subscriptions WHERE original_transaction_id = $1`,
		originalTransactionID,
	).Scan(&userID)
	if err == nil {
		return userID, nil
	}
	if err != sql.ErrNoRows {
		return "", fmt.Errorf("user by txn (subscriptions): %w", err)
	}

	// Fall back to verified_transactions.
	err = d.db.QueryRow(`
		SELECT user_id FROM verified_transactions WHERE original_transaction_id = $1`,
		originalTransactionID,
	).Scan(&userID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("user by txn (transactions): %w", err)
	}
	return userID, nil
}

// StoreTransaction records a verified transaction for audit.
func (d *DB) StoreTransaction(t *receipt.VerifiedTransaction) error {
	_, err := d.db.Exec(`
		INSERT INTO verified_transactions
			(transaction_id, original_transaction_id, user_id, product_id, status,
			 purchase_date, expires_date, environment, price_cents, currency_code, notification_type)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (transaction_id) DO UPDATE SET
			original_transaction_id = $2,
			user_id                 = $3,
			product_id              = $4,
			status                  = $5,
			purchase_date           = $6,
			expires_date            = $7,
			environment             = $8,
			price_cents             = $9,
			currency_code           = $10,
			notification_type       = $11`,
		t.TransactionID, t.OriginalTransactionID, t.UserID, t.ProductID, t.Status,
		t.PurchaseDate, t.ExpiresDate, t.Environment, t.PriceCents, t.CurrencyCode, t.NotificationType,
	)
	if err != nil {
		return fmt.Errorf("store transaction: %w", err)
	}
	return nil
}
