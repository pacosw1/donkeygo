// Package postgres provides a PostgreSQL implementation of all donkeygo DB interfaces.
//
// A single DB struct implements auth.AuthDB, attest.AttestDB, engage.EngageDB,
// notify.NotifyDB, chat.ChatDB, sync.SyncDB, sync.DeviceTokenStore,
// receipt.ReceiptDB, analytics.AnalyticsDB, and lifecycle.LifecycleDB.
//
// The package uses database/sql with PostgreSQL-specific queries ($1 placeholders,
// COALESCE, ON CONFLICT, etc.) but does not import a driver — the app must import
// one (e.g. github.com/lib/pq or github.com/jackc/pgx/v5/stdlib).
//
// Usage:
//
//	import _ "github.com/lib/pq"
//	db, _ := sql.Open("postgres", connStr)
//	store := postgres.New(db)
//	authSvc := auth.New(authCfg, store)
//	engageSvc := engage.New(engage.Config{}, store)
//	// ... all packages share the same store
package postgres

import "database/sql"

// DB implements all donkeygo DB interfaces using PostgreSQL.
type DB struct {
	db *sql.DB
}

// New creates a new PostgreSQL store.
func New(db *sql.DB) *DB {
	return &DB{db: db}
}

// SQL returns the underlying *sql.DB for custom queries.
func (d *DB) SQL() *sql.DB {
	return d.db
}
