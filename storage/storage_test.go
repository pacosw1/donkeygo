package storage

import (
	"testing"
)

func TestNew_Defaults(t *testing.T) {
	c := New(Config{Bucket: "test-bucket"})
	if c.cfg.Region != "us-east-1" {
		t.Fatalf("expected us-east-1, got %s", c.cfg.Region)
	}
	if c.cfg.Endpoint != "s3.amazonaws.com" {
		t.Fatalf("expected s3.amazonaws.com, got %s", c.cfg.Endpoint)
	}
}

func TestConfigured(t *testing.T) {
	c := New(Config{})
	if c.Configured() {
		t.Fatal("empty config should not be configured")
	}

	c = New(Config{Bucket: "b", AccessKey: "ak", SecretKey: "sk"})
	if !c.Configured() {
		t.Fatal("full config should be configured")
	}
}

func TestS3URL(t *testing.T) {
	c := New(Config{Bucket: "mybucket", Endpoint: "s3.amazonaws.com"})
	host, url := c.s3URL("path/to/file.jpg")

	if host != "mybucket.s3.amazonaws.com" {
		t.Fatalf("expected mybucket.s3.amazonaws.com, got %s", host)
	}
	if url != "https://mybucket.s3.amazonaws.com/path/to/file.jpg" {
		t.Fatalf("unexpected url: %s", url)
	}
}

func TestSHA256Hex(t *testing.T) {
	got := sha256Hex([]byte("hello"))
	expected := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != expected {
		t.Fatalf("expected %s, got %s", expected, got)
	}
}

func TestGetSignatureKey(t *testing.T) {
	key := getSignatureKey("secret", "20240101", "us-east-1", "s3")
	if len(key) != 32 {
		t.Fatalf("expected 32-byte key, got %d", len(key))
	}
}
