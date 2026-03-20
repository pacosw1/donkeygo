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

	// Should be a LogProvider
	if _, ok := p.(*LogProvider); !ok {
		t.Fatal("expected LogProvider when no key path")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Fatalf("expected hello, got %s", got)
	}
	if got := truncate("hello world", 5); got != "hello" {
		t.Fatalf("expected hello, got %s", got)
	}
}
