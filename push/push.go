// Package push provides a push notification provider interface and APNs implementation.
package push

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
)

// Provider is the interface for sending push notifications.
type Provider interface {
	Send(deviceToken, title, body string) error
	SendWithData(deviceToken, title, body string, data map[string]string) error
	// SendSilent sends a content-available background push.
	SendSilent(deviceToken string, data map[string]string) error
}

// Config holds push notification configuration.
type Config struct {
	KeyPath     string // path to .p8 key file
	KeyID       string
	TeamID      string
	Topic       string // bundle ID
	Environment string // "sandbox" or "production"
}

// NewProvider creates a push provider based on config.
// Returns APNs provider if KeyPath is set, LogProvider otherwise.
func NewProvider(cfg Config) (Provider, error) {
	if cfg.KeyPath == "" {
		log.Printf("[push] no key path — using log provider")
		return &LogProvider{}, nil
	}

	p, err := NewAPNsProvider(cfg)
	if err != nil {
		log.Printf("[push] WARNING: could not init APNs: %v — falling back to log provider", err)
		return &LogProvider{}, nil
	}

	log.Printf("[push] APNs provider initialized (env=%s)", cfg.Environment)
	return p, nil
}

// ── LogProvider (development fallback) ──────────────────────────────────────

// LogProvider logs push notifications instead of sending them.
type LogProvider struct{}

func (p *LogProvider) Send(deviceToken, title, body string) error {
	log.Printf("[push/log] token=%s title=%q body=%q", truncate(deviceToken, 16), title, body)
	return nil
}

func (p *LogProvider) SendWithData(deviceToken, title, body string, data map[string]string) error {
	log.Printf("[push/log] token=%s title=%q body=%q data=%v", truncate(deviceToken, 16), title, body, data)
	return nil
}

func (p *LogProvider) SendSilent(deviceToken string, data map[string]string) error {
	log.Printf("[push/log] SILENT token=%s data=%v", truncate(deviceToken, 16), data)
	return nil
}

// ── NoopProvider ────────────────────────────────────────────────────────────

// NoopProvider silently discards all push notifications. Useful for tests.
type NoopProvider struct{}

func (p *NoopProvider) Send(deviceToken, title, body string) error                              { return nil }
func (p *NoopProvider) SendWithData(deviceToken, title, body string, data map[string]string) error { return nil }
func (p *NoopProvider) SendSilent(deviceToken string, data map[string]string) error                { return nil }

// ── APNsProvider (production) ───────────────────────────────────────────────

// APNsProvider sends push notifications via Apple Push Notification service.
type APNsProvider struct {
	key     *ecdsa.PrivateKey
	keyID   string
	teamID  string
	topic   string
	baseURL string
	client  *http.Client

	mu          sync.Mutex
	cachedToken string
	tokenExpiry time.Time
}

// NewAPNsProvider creates an APNs provider from a .p8 key file.
func NewAPNsProvider(cfg Config) (*APNsProvider, error) {
	keyData, err := os.ReadFile(cfg.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("read apns key: %w", err)
	}

	block, _ := pem.Decode(keyData)
	if block == nil {
		return nil, fmt.Errorf("invalid PEM block in %s", cfg.KeyPath)
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse apns key: %w", err)
	}

	ecKey, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("apns key is not ES256")
	}

	baseURL := "https://api.sandbox.push.apple.com"
	if cfg.Environment == "production" {
		baseURL = "https://api.push.apple.com"
	}

	return &APNsProvider{
		key:     ecKey,
		keyID:   cfg.KeyID,
		teamID:  cfg.TeamID,
		topic:   cfg.Topic,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (p *APNsProvider) getToken() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cachedToken != "" && time.Now().Before(p.tokenExpiry) {
		return p.cachedToken, nil
	}

	now := time.Now()
	claims := gojwt.RegisteredClaims{
		Issuer:   p.teamID,
		IssuedAt: gojwt.NewNumericDate(now),
	}
	token := gojwt.NewWithClaims(gojwt.SigningMethodES256, claims)
	token.Header["kid"] = p.keyID

	signed, err := token.SignedString(p.key)
	if err != nil {
		return "", fmt.Errorf("sign apns token: %w", err)
	}

	p.cachedToken = signed
	p.tokenExpiry = now.Add(50 * time.Minute)
	return signed, nil
}

func (p *APNsProvider) Send(deviceToken, title, body string) error {
	return p.SendWithData(deviceToken, title, body, nil)
}

func (p *APNsProvider) SendWithData(deviceToken, title, body string, data map[string]string) error {
	payload := map[string]any{
		"aps": map[string]any{
			"alert": map[string]string{"title": title, "body": body},
			"sound": "default",
		},
	}
	for k, v := range data {
		payload[k] = v
	}

	return p.send(deviceToken, payload, "alert", "10")
}

func (p *APNsProvider) SendSilent(deviceToken string, data map[string]string) error {
	payload := map[string]any{
		"aps": map[string]any{"content-available": 1},
	}
	for k, v := range data {
		payload[k] = v
	}

	return p.send(deviceToken, payload, "background", "5")
}

func (p *APNsProvider) send(deviceToken string, payload map[string]any, pushType, priority string) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/3/device/%s", p.baseURL, deviceToken)
	req, err := http.NewRequest("POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return err
	}

	bearerToken, err := p.getToken()
	if err != nil {
		return err
	}

	req.Header.Set("authorization", "bearer "+bearerToken)
	req.Header.Set("apns-topic", p.topic)
	req.Header.Set("apns-push-type", pushType)
	req.Header.Set("apns-priority", priority)
	req.Header.Set("apns-expiration", "0")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("apns request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Reason string `json:"reason"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		return fmt.Errorf("apns error %d: %s", resp.StatusCode, errResp.Reason)
	}

	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
