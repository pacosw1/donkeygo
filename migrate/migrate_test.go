package migrate

import (
	"testing"
)

func TestNewRunner(t *testing.T) {
	r := NewRunner(nil)
	if r == nil {
		t.Fatal("expected non-nil runner")
	}
	if len(r.migrations) != 0 {
		t.Fatalf("expected 0 migrations, got %d", len(r.migrations))
	}
}

func TestAdd(t *testing.T) {
	r := NewRunner(nil)
	r.Add(
		Migration{Name: "create users", SQL: "CREATE TABLE users (id TEXT)"},
		Migration{Name: "create tokens", SQL: "CREATE TABLE tokens (id TEXT)"},
	)

	if len(r.migrations) != 2 {
		t.Fatalf("expected 2 migrations, got %d", len(r.migrations))
	}
	if r.migrations[0].Name != "create users" {
		t.Fatalf("expected 'create users', got %s", r.migrations[0].Name)
	}
}

func TestAdd_Multiple(t *testing.T) {
	r := NewRunner(nil)
	r.Add(Migration{Name: "m1", SQL: "SELECT 1"})
	r.Add(Migration{Name: "m2", SQL: "SELECT 2"})

	if len(r.migrations) != 2 {
		t.Fatalf("expected 2 migrations after two Add calls, got %d", len(r.migrations))
	}
}
