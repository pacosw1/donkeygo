// Package receipt provides App Store Server API v2 receipt verification.
//
// It verifies JWS-signed transactions from StoreKit 2 by validating the
// certificate chain against Apple's Root CA, extracts transaction details,
// and updates the user's subscription status server-side.
//
// Two endpoints are provided:
//   - POST /api/v1/receipt/verify — client submits a signed transaction
//   - POST /api/v1/receipt/webhook — Apple Server Notifications V2
//
// The webhook endpoint requires no authentication (Apple calls it directly).
// The verify endpoint requires the standard auth middleware.
package receipt

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/pacosw1/donkeygo/httputil"
	"github.com/pacosw1/donkeygo/middleware"
)

// ── Database Interface ───────────────────────────────────────────────────────

// ReceiptDB is the database interface required by the receipt package.
type ReceiptDB interface {
	// UpsertSubscription updates a user's subscription from a verified transaction.
	UpsertSubscription(userID, productID, originalTransactionID, status string, expiresAt *time.Time, priceCents int, currencyCode string) error
	// UserIDByTransactionID looks up the user who owns a given original transaction.
	// Used by webhooks where Apple sends the transaction but not the user ID.
	// Returns empty string and nil error if not found.
	UserIDByTransactionID(originalTransactionID string) (string, error)
	// StoreTransaction records a verified transaction for audit.
	StoreTransaction(t *VerifiedTransaction) error
}

// ── Types ────────────────────────────────────────────────────────────────────

// TransactionInfo represents the decoded payload of an App Store signed transaction.
type TransactionInfo struct {
	TransactionID         string `json:"transactionId"`
	OriginalTransactionID string `json:"originalTransactionId"`
	BundleID              string `json:"bundleId"`
	ProductID             string `json:"productId"`
	PurchaseDate          int64  `json:"purchaseDate"`          // milliseconds since epoch
	ExpiresDate           int64  `json:"expiresDate"`           // milliseconds since epoch, 0 for non-subscription
	OriginalPurchaseDate  int64  `json:"originalPurchaseDate"`  // milliseconds since epoch
	Type                  string `json:"type"`                  // "Auto-Renewable Subscription", "Non-Consumable", etc.
	InAppOwnershipType    string `json:"inAppOwnershipType"`    // "PURCHASED" or "FAMILY_SHARED"
	Environment           string `json:"environment"`           // "Production" or "Sandbox"
	Price                 int    `json:"price"`                 // milliunits of local currency
	Currency              string `json:"currency"`              // ISO 4217 currency code
	Storefront            string `json:"storefront"`            // ISO 3166-1 alpha-3 country code
	OfferType             int    `json:"offerType,omitempty"`   // 1=introductory, 2=promotional, 3=offer code
	RevocationDate        int64  `json:"revocationDate,omitempty"`
	RevocationReason      int    `json:"revocationReason,omitempty"`
	AppAccountToken       string `json:"appAccountToken,omitempty"` // UUID set by the app, typically user ID
}

// NotificationPayload is the decoded payload of an App Store Server Notification V2.
type NotificationPayload struct {
	NotificationType string           `json:"notificationType"`
	Subtype          string           `json:"subtype"`
	Data             NotificationData `json:"data"`
	Version          string           `json:"version"`
	SignedDate       int64            `json:"signedDate"` // milliseconds since epoch
}

// NotificationData contains the signed transaction and renewal info.
type NotificationData struct {
	SignedTransactionInfo string `json:"signedTransactionInfo"`
	SignedRenewalInfo     string `json:"signedRenewalInfo"`
	Environment          string `json:"environment"`
	BundleID             string `json:"bundleId"`
	AppAppleID           int64  `json:"appAppleId"`
	BundleVersion        string `json:"bundleVersion"`
}

// VerifiedTransaction is a transaction record stored for audit purposes.
type VerifiedTransaction struct {
	TransactionID         string     `json:"transaction_id"`
	OriginalTransactionID string     `json:"original_transaction_id"`
	UserID                string     `json:"user_id"`
	ProductID             string     `json:"product_id"`
	Status                string     `json:"status"`
	PurchaseDate          time.Time  `json:"purchase_date"`
	ExpiresDate           *time.Time `json:"expires_date,omitempty"`
	Environment           string     `json:"environment"`
	PriceCents            int        `json:"price_cents"`
	CurrencyCode          string     `json:"currency_code"`
	NotificationType      string     `json:"notification_type,omitempty"` // set when from webhook
}

// VerifyResponse is returned to the client after successful verification.
type VerifyResponse struct {
	Verified      bool       `json:"verified"`
	Status        string     `json:"status"`
	ProductID     string     `json:"product_id"`
	TransactionID string     `json:"transaction_id"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
}

// ── Configuration ────────────────────────────────────────────────────────────

// Config configures the receipt verification service.
type Config struct {
	// BundleID is the expected app bundle ID. Transactions with a different
	// bundle ID are rejected.
	BundleID string
	// Environment is the expected environment: "Production" or "Sandbox".
	// If empty, both environments are accepted.
	Environment string
}

// ── Service ──────────────────────────────────────────────────────────────────

// Service provides receipt verification handlers.
type Service struct {
	db     ReceiptDB
	cfg    Config
	rootCA *x509.Certificate
}

// New creates a receipt verification service.
func New(db ReceiptDB, cfg Config) *Service {
	rootCA, err := parseAppleRootCA()
	if err != nil {
		log.Fatalf("[receipt] failed to parse Apple Root CA: %v", err)
	}
	return &Service{db: db, cfg: cfg, rootCA: rootCA}
}

// Migrations returns the SQL migrations needed by the receipt package.
func Migrations() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS verified_transactions (
			transaction_id          TEXT PRIMARY KEY,
			original_transaction_id TEXT NOT NULL,
			user_id                 TEXT NOT NULL,
			product_id              TEXT NOT NULL,
			status                  TEXT NOT NULL,
			purchase_date           TIMESTAMPTZ NOT NULL,
			expires_date            TIMESTAMPTZ,
			environment             TEXT NOT NULL DEFAULT 'Production',
			price_cents             INTEGER NOT NULL DEFAULT 0,
			currency_code           TEXT NOT NULL DEFAULT 'USD',
			notification_type       TEXT NOT NULL DEFAULT '',
			verified_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_verified_tx_orig ON verified_transactions(original_transaction_id)`,
		`CREATE INDEX IF NOT EXISTS idx_verified_tx_user ON verified_transactions(user_id, verified_at)`,
	}
}

// ── Handlers ────────────────────────────────────────────────────────────────

// HandleVerifyReceipt handles POST /api/v1/receipt/verify.
// The client sends a StoreKit 2 signed transaction JWS for server-side verification.
func (s *Service) HandleVerifyReceipt(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(middleware.CtxUserID).(string)
	if !ok || userID == "" {
		httputil.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Limit body size — a JWS transaction is typically under 10 KB.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB

	var req struct {
		Transaction string `json:"transaction"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Transaction == "" {
		httputil.WriteError(w, http.StatusBadRequest, "transaction is required")
		return
	}

	txn, err := s.verifyAndParseTransaction(req.Transaction)
	if err != nil {
		log.Printf("[receipt] verification failed for user %s: %v", userID, err)
		httputil.WriteError(w, http.StatusBadRequest, "transaction verification failed")
		return
	}

	if err := s.validateTransaction(txn); err != nil {
		log.Printf("[receipt] validation failed for user %s: %v", userID, err)
		httputil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	status := s.transactionToStatus(txn)
	expiresAt := millisToTimePtr(txn.ExpiresDate)
	priceCents := txn.Price / 10 // Apple uses milliunits
	currency := txn.Currency
	if currency == "" {
		currency = "USD"
	}

	// Update subscription.
	if err := s.db.UpsertSubscription(userID, txn.ProductID, txn.OriginalTransactionID, status, expiresAt, priceCents, currency); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "failed to update subscription")
		return
	}

	// Store audit record.
	if err := s.db.StoreTransaction(&VerifiedTransaction{
		TransactionID:         txn.TransactionID,
		OriginalTransactionID: txn.OriginalTransactionID,
		UserID:                userID,
		ProductID:             txn.ProductID,
		Status:                status,
		PurchaseDate:          millisToTime(txn.PurchaseDate),
		ExpiresDate:           expiresAt,
		Environment:           txn.Environment,
		PriceCents:            priceCents,
		CurrencyCode:          currency,
	}); err != nil {
		log.Printf("[receipt] failed to store audit transaction %s: %v", txn.TransactionID, err)
	}

	httputil.WriteJSON(w, http.StatusOK, &VerifyResponse{
		Verified:      true,
		Status:        status,
		ProductID:     txn.ProductID,
		TransactionID: txn.TransactionID,
		ExpiresAt:     expiresAt,
	})
}

// HandleWebhook handles POST /api/v1/receipt/webhook.
// Apple sends Server Notifications V2 to this endpoint.
// No auth middleware — Apple calls this directly.
func (s *Service) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB limit
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "failed to read body")
		return
	}

	var req struct {
		SignedPayload string `json:"signedPayload"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.SignedPayload == "" {
		httputil.WriteError(w, http.StatusBadRequest, "invalid webhook payload")
		return
	}

	// Verify and decode the outer notification JWS.
	notifPayload, err := s.verifyAndParsePayload(req.SignedPayload)
	if err != nil {
		log.Printf("[receipt] webhook JWS verification failed: %v", err)
		httputil.WriteError(w, http.StatusBadRequest, "invalid signature")
		return
	}

	var notification NotificationPayload
	if err := json.Unmarshal(notifPayload, &notification); err != nil {
		log.Printf("[receipt] webhook payload decode failed: %v", err)
		httputil.WriteError(w, http.StatusBadRequest, "invalid notification payload")
		return
	}

	log.Printf("[receipt] webhook: type=%s subtype=%s env=%s", notification.NotificationType, notification.Subtype, notification.Data.Environment)

	// TEST notifications don't contain transaction data.
	if notification.NotificationType == "TEST" {
		httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	if notification.Data.SignedTransactionInfo == "" {
		httputil.WriteError(w, http.StatusBadRequest, "missing signed transaction info")
		return
	}

	// Verify and decode the signed transaction info.
	txn, err := s.verifyAndParseTransaction(notification.Data.SignedTransactionInfo)
	if err != nil {
		log.Printf("[receipt] webhook transaction verification failed: %v", err)
		httputil.WriteError(w, http.StatusBadRequest, "invalid transaction signature")
		return
	}

	if err := s.validateTransaction(txn); err != nil {
		log.Printf("[receipt] webhook transaction validation failed: %v", err)
		httputil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Look up the user by original transaction ID.
	userID, err := s.db.UserIDByTransactionID(txn.OriginalTransactionID)
	if err != nil {
		log.Printf("[receipt] webhook db lookup failed: %v", err)
		httputil.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if userID == "" {
		// Fall back to appAccountToken if set.
		if txn.AppAccountToken != "" {
			userID = txn.AppAccountToken
		} else {
			log.Printf("[receipt] webhook: unknown transaction %s, no user mapping", txn.OriginalTransactionID)
			// Acknowledge to Apple so they don't retry, but we can't process it.
			httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "unknown_transaction"})
			return
		}
	}

	status := s.notificationToStatus(notification.NotificationType, notification.Subtype, txn)
	expiresAt := millisToTimePtr(txn.ExpiresDate)
	priceCents := txn.Price / 10
	currency := txn.Currency
	if currency == "" {
		currency = "USD"
	}

	if err := s.db.UpsertSubscription(userID, txn.ProductID, txn.OriginalTransactionID, status, expiresAt, priceCents, currency); err != nil {
		log.Printf("[receipt] webhook subscription update failed for user %s: %v", userID, err)
		httputil.WriteError(w, http.StatusInternalServerError, "failed to update subscription")
		return
	}

	if err := s.db.StoreTransaction(&VerifiedTransaction{
		TransactionID:         txn.TransactionID,
		OriginalTransactionID: txn.OriginalTransactionID,
		UserID:                userID,
		ProductID:             txn.ProductID,
		Status:                status,
		PurchaseDate:          millisToTime(txn.PurchaseDate),
		ExpiresDate:           expiresAt,
		Environment:           txn.Environment,
		PriceCents:            priceCents,
		CurrencyCode:          currency,
		NotificationType:      notification.NotificationType,
	}); err != nil {
		log.Printf("[receipt] failed to store audit transaction %s: %v", txn.TransactionID, err)
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ── JWS Verification ────────────────────────────────────────────────────────

// verifyAndParsePayload verifies an Apple JWS and returns the raw payload bytes.
func (s *Service) verifyAndParsePayload(jwsString string) ([]byte, error) {
	// Extract the raw payload before JWT parsing — we need the original
	// bytes since MapClaims re-serialisation may alter the JSON.
	parts := strings.SplitN(jwsString, ".", 3)
	if len(parts) != 3 {
		return nil, errors.New("invalid JWS format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}

	// Verify the signature using the x5c certificate chain.
	parser := gojwt.NewParser(gojwt.WithoutClaimsValidation())
	_, err = parser.Parse(jwsString, s.keyFunc)
	if err != nil {
		return nil, fmt.Errorf("verify JWS: %w", err)
	}

	return payload, nil
}

// verifyAndParseTransaction verifies a signed transaction JWS and decodes it.
func (s *Service) verifyAndParseTransaction(jwsString string) (*TransactionInfo, error) {
	payload, err := s.verifyAndParsePayload(jwsString)
	if err != nil {
		return nil, err
	}

	var txn TransactionInfo
	if err := json.Unmarshal(payload, &txn); err != nil {
		return nil, fmt.Errorf("decode transaction: %w", err)
	}

	return &txn, nil
}

// keyFunc is the JWT key function that verifies Apple's x5c certificate chain
// and returns the leaf certificate's public key for signature verification.
func (s *Service) keyFunc(token *gojwt.Token) (interface{}, error) {
	// Ensure ES256 signing method.
	if _, ok := token.Method.(*gojwt.SigningMethodECDSA); !ok {
		return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
	}

	// Extract x5c certificate chain from header.
	x5cRaw, ok := token.Header["x5c"].([]interface{})
	if !ok || len(x5cRaw) < 2 {
		return nil, errors.New("missing or invalid x5c header")
	}

	// Parse all certificates in the chain.
	certs := make([]*x509.Certificate, len(x5cRaw))
	for i, raw := range x5cRaw {
		certB64, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("x5c[%d] is not a string", i)
		}
		certDER, err := base64.StdEncoding.DecodeString(certB64)
		if err != nil {
			return nil, fmt.Errorf("decode x5c[%d]: %w", i, err)
		}
		cert, err := x509.ParseCertificate(certDER)
		if err != nil {
			return nil, fmt.Errorf("parse x5c[%d]: %w", i, err)
		}
		certs[i] = cert
	}

	// Build certificate pools for chain verification.
	rootPool := x509.NewCertPool()
	rootPool.AddCert(s.rootCA)

	intermediatePool := x509.NewCertPool()
	for _, cert := range certs[1:] {
		intermediatePool.AddCert(cert)
	}

	// Verify the leaf certificate chains to Apple's root CA.
	leaf := certs[0]
	_, err := leaf.Verify(x509.VerifyOptions{
		Roots:         rootPool,
		Intermediates: intermediatePool,
		// Apple certs have specific key usages; we only need signature verification.
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	if err != nil {
		return nil, fmt.Errorf("certificate chain verification failed: %w", err)
	}

	// Return the leaf certificate's public key for JWS signature verification.
	pubKey, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("leaf certificate does not contain an ECDSA public key")
	}

	return pubKey, nil
}

// ── Validation ──────────────────────────────────────────────────────────────

// validateTransaction checks that the transaction belongs to our app and environment.
func (s *Service) validateTransaction(txn *TransactionInfo) error {
	if txn.TransactionID == "" || txn.OriginalTransactionID == "" {
		return errors.New("missing transaction ID")
	}
	if txn.ProductID == "" {
		return errors.New("missing product ID")
	}
	if s.cfg.BundleID != "" && txn.BundleID != s.cfg.BundleID {
		return fmt.Errorf("bundle ID mismatch: got %q, expected %q", txn.BundleID, s.cfg.BundleID)
	}
	if s.cfg.Environment != "" && txn.Environment != s.cfg.Environment {
		return fmt.Errorf("environment mismatch: got %q, expected %q", txn.Environment, s.cfg.Environment)
	}
	if txn.RevocationDate > 0 {
		return errors.New("transaction has been revoked")
	}
	return nil
}

// ── Status Mapping ──────────────────────────────────────────────────────────

// transactionToStatus maps a verified transaction to a subscription status.
func (s *Service) transactionToStatus(txn *TransactionInfo) string {
	// Non-subscription purchases are always "active" (lifetime).
	if txn.Type != "Auto-Renewable Subscription" {
		return "active"
	}
	// Check if it's a trial (introductory offer).
	if txn.OfferType == 1 {
		return "trial"
	}
	// Check if subscription has expired.
	if txn.ExpiresDate > 0 && millisToTime(txn.ExpiresDate).Before(time.Now()) {
		return "expired"
	}
	return "active"
}

// notificationToStatus maps an Apple notification type to a subscription status.
func (s *Service) notificationToStatus(notifType, subtype string, txn *TransactionInfo) string {
	switch notifType {
	case "SUBSCRIBED":
		if txn.OfferType == 1 {
			return "trial"
		}
		return "active"
	case "DID_RENEW":
		return "active"
	case "EXPIRED":
		return "expired"
	case "REFUND", "REVOKE":
		return "expired"
	case "DID_CHANGE_RENEWAL_STATUS":
		if subtype == "AUTO_RENEW_DISABLED" {
			return "cancelled"
		}
		return "active"
	case "DID_FAIL_TO_RENEW":
		// Keep active during grace period, otherwise expired.
		if subtype == "GRACE_PERIOD" {
			return "active"
		}
		return "expired"
	case "OFFER_REDEEMED":
		return "active"
	default:
		// For unknown types, derive from the transaction itself.
		return s.transactionToStatus(txn)
	}
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func millisToTime(ms int64) time.Time {
	return time.UnixMilli(ms).UTC()
}

func millisToTimePtr(ms int64) *time.Time {
	if ms == 0 {
		return nil
	}
	t := millisToTime(ms)
	return &t
}

// ── Apple Root CA ───────────────────────────────────────────────────────────

// Apple Root CA - G3 (ECC P-384).
// This is Apple's root certificate authority used to sign App Store
// Server API v2 JWS tokens.  It is a public, well-known certificate.
const appleRootCAPEM = `-----BEGIN CERTIFICATE-----
MIICQzCCAcmgAwIBAgIILcX8iNLFS5UwCgYIKoZIzj0EAwMwZzEbMBkGA1UEAwwS
QXBwbGUgUm9vdCBDQSAtIEczMSYwJAYDVQQLDB1BcHBsZSBDZXJ0aWZpY2F0aW9u
IEF1dGhvcml0eTETMBEGA1UECgwKQXBwbGUgSW5jLjELMAkGA1UEBhMCVVMwHhcN
MTQwNDMwMTgxOTA2WhcNMzkwNDMwMTgxOTA2WjBnMRswGQYDVQQDDBJBcHBsZSBS
b290IENBIC0gRzMxJjAkBgNVBAsMHUFwcGxlIENlcnRpZmljYXRpb24gQXV0aG9y
aXR5MRMwEQYDVQQKDApBcHBsZSBJbmMuMQswCQYDVQQGEwJVUzB2MBAGByqGSM49
AgEGBSuBBAAiA2IABJjpLz1AcqTtkyJygRMc3RCV8cWjTnHcFBbZDuWmBSp3ZHtf
TjjTuxxEtX/1H7YyYl3J6YRbTzBPEVoA/VhYDKX1DyxNB0cTddqXl5dvMVztK517
IDvYuVTZXpmkOlEKMaNCMEAwHQYDVR0OBBYEFLuw3qFYM4iapIqZ3r6966/ayySr
MA8GA1UdEwEB/wQFMAMBAf8wDgYDVR0PAQH/BAQDAgEGMAoGCCqGSM49BAMDA2gA
MGUCMQCD6cHEFl4aXTQY2e3v9GwOAEZLuN+yRhHFD/3meoyhpmvOwgPUnPWTxnS4
at+qIxUCMG1mihDK1A3UT82NQz60imOlM27jbdoXt2QfyFMm+YhidDkLF1vLUagM
6BgD56KyKA==
-----END CERTIFICATE-----`

func parseAppleRootCA() (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(appleRootCAPEM))
	if block == nil {
		return nil, errors.New("failed to decode Apple Root CA PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse Apple Root CA: %w", err)
	}
	return cert, nil
}
