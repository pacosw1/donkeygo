// Package migrate provides a simple SQL migration runner.
// Each donkeygo package exports its own Migrations() and the app composes them.
package migrate

import (
	"database/sql"
	"fmt"
	"log"
)

// Migration represents a single SQL migration statement.
type Migration struct {
	Name string // human-readable name (e.g. "auth: create users table")
	SQL  string // the DDL/DML statement
}

// Runner executes migrations in order.
type Runner struct {
	db         *sql.DB
	migrations []Migration
}

// NewRunner creates a migration runner for the given database.
func NewRunner(db *sql.DB) *Runner {
	return &Runner{db: db}
}

// Add appends migrations to the runner.
func (r *Runner) Add(migrations ...Migration) {
	r.migrations = append(r.migrations, migrations...)
}

// Run executes all migrations in order.
// Uses CREATE TABLE IF NOT EXISTS / DO $$ ... pattern so migrations are idempotent.
func (r *Runner) Run() error {
	for _, m := range r.migrations {
		if _, err := r.db.Exec(m.SQL); err != nil {
			preview := m.SQL
			if len(preview) > 200 {
				preview = preview[:200]
			}
			return fmt.Errorf("migration %q failed: %w\nSQL: %s", m.Name, err, preview)
		}
	}
	log.Printf("[migrate] %d migrations complete", len(r.migrations))
	return nil
}
