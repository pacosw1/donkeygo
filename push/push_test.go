package push

import (
	"testing"
)

func TestLogProvider(t *testing.T) {
	p := &LogProvider{}

	if err := p.Send("token123456789012345", "Title", "Body"); err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if err := p.SendWithData("token123456789012345", "Title", "Body", map[string]string{"key": "val"}); err != nil {
		t.Fatalf("SendWithData failed: %v", err)
	}
	if err := p.SendSilent("token123456789012345", map[string]string{"sync": "true"}); err != nil {
		t.Fatalf("SendSilent failed: %v", err)
	}
}

func TestNoopProvider(t *testing.T) {
	p := &NoopProvider{}

	if err := p.Send("token", "Title", "Body"); err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if err := p.SendWithData("token", "Title", "Body", nil); err != nil {
		t.Fatalf("SendWithData failed: %v", err)
	}
	if err := p.SendSilent("token", nil); err != nil {
		t.Fatalf("SendSilent failed: %v", err)
	}
}

func TestNewProvider_NoKey(t *testing.T) {
	p, err := NewProvider(Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := p.(*LogProvider); !ok {
		t.Fatal("expected LogProvider when no key path")
	}
}

func TestNewProvider_InvalidKeyPath(t *testing.T) {
	p, err := NewProvider(Config{KeyPath: "/nonexistent/key.p8"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should fallback to LogProvider
	if _, ok := p.(*LogProvider); !ok {
		t.Fatal("expected LogProvider fallback for invalid key path")
	}
}

func TestNewAPNsProvider_NonexistentFile(t *testing.T) {
	_, err := NewAPNsProvider(Config{KeyPath: "/nonexistent/key.p8"})
	if err == nil {
		t.Fatal("expected error for nonexistent key file")
	}
}

func TestProviderInterface(t *testing.T) {
	// Compile-time check that all types implement Provider
	var _ Provider = &LogProvider{}
	var _ Provider = &NoopProvider{}
	var _ Provider = &APNsProvider{}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		n     int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello"},
		{"", 5, ""},
		{"ab", 2, "ab"},
	}
	for _, tt := range tests {
		if got := truncate(tt.input, tt.n); got != tt.want {
			t.Fatalf("truncate(%q, %d) = %q, want %q", tt.input, tt.n, got, tt.want)
		}
	}
}

func TestLogProvider_ShortToken(t *testing.T) {
	p := &LogProvider{}
	// Should not panic with short tokens
	if err := p.Send("short", "Title", "Body"); err != nil {
		t.Fatalf("Send with short token failed: %v", err)
	}
}
