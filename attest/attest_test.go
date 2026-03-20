package attest

import (
	"testing"
)

func TestMigrations(t *testing.T) {
	m := Migrations()
	if len(m) == 0 {
		t.Fatal("expected at least one migration")
	}
}

func TestGenerateHexNonce(t *testing.T) {
	nonce, err := GenerateHexNonce(16)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nonce) != 32 { // 16 bytes = 32 hex chars
		t.Fatalf("expected 32 hex chars, got %d", len(nonce))
	}
}
