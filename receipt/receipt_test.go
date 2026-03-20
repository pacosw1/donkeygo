package receipt

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/pacosw1/donkeygo/middleware"
)

// ── Test CA and Cert Chain ──────────────────────────────────────────────────

// testCA generates a self-signed root CA, intermediate CA, and leaf cert
// for testing the JWS verification flow.
type testCA struct {
	rootKey   *ecdsa.PrivateKey
	rootCert  *x509.Certificate
	interKey  *ecdsa.PrivateKey
	interCert *x509.Certificate
	leafKey   *ecdsa.PrivateKey
	leafCert  *x509.Certificate
	x5c       []string // base64-encoded DER certs [leaf, intermediate, root]
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()

	// Root CA (P-384, like Apple Root CA - G3).
	rootKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("generate root key: %v", err)
	}
	rootTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test Root CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatalf("create root cert: %v", err)
	}
	rootCert, _ := x509.ParseCertificate(rootDER)

	// Intermediate CA (P-384).
	interKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("generate intermediate key: %v", err)
	}
	interTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "Test Intermediate CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	interDER, err := x509.CreateCertificate(rand.Reader, interTemplate, rootCert, &interKey.PublicKey, rootKey)
	if err != nil {
		t.Fatalf("create intermediate cert: %v", err)
	}
	interCert, _ := x509.ParseCertificate(interDER)

	// Leaf cert (P-256, like Apple's signing certs).
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "Test Leaf"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, interCert, &leafKey.PublicKey, interKey)
	if err != nil {
		t.Fatalf("create leaf cert: %v", err)
	}
	leafCert, _ := x509.ParseCertificate(leafDER)

	return &testCA{
		rootKey:   rootKey,
		rootCert:  rootCert,
		interKey:  interKey,
		interCert: interCert,
		leafKey:   leafKey,
		leafCert:  leafCert,
		x5c: []string{
			base64.StdEncoding.EncodeToString(leafDER),
			base64.StdEncoding.EncodeToString(interDER),
			base64.StdEncoding.EncodeToString(rootDER),
		},
	}
}

// signJWS creates a JWS token signed by the leaf cert, with the x5c chain in the header.
func (ca *testCA) signJWS(t *testing.T, payload any) string {
	t.Helper()

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	token := gojwt.NewWithClaims(gojwt.SigningMethodES256, gojwt.MapClaims{})
	token.Header["x5c"] = ca.x5c

	// Override the claims with our raw payload.
	// We need to encode payload ourselves since MapClaims doesn't support arbitrary JSON.
	headerBytes, _ := json.Marshal(token.Header)
	headerB64 := base64.RawURLEncoding.EncodeToString(headerBytes)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)

	signingInput := headerB64 + "." + payloadB64
	sigBytes, err := gojwt.SigningMethodES256.Sign(signingInput, ca.leafKey)
	if err != nil {
		t.Fatalf("sign JWS: %v", err)
	}

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sigBytes)
}

// newTestService creates a Service with the test CA as the root instead of Apple's.
func newTestService(t *testing.T, ca *testCA, db ReceiptDB, cfg Config) *Service {
	t.Helper()
	return &Service{
		db:     db,
		cfg:    cfg,
		rootCA: ca.rootCert,
	}
}

// ── Mock DB ─────────────────────────────────────────────────────────────────

type mockReceiptDB struct {
	subscriptions map[string]*storedSub // userID → sub
	transactions  []*VerifiedTransaction
	userByTxn     map[string]string // originalTransactionID → userID
	err           error
}

type storedSub struct {
	productID, originalTxnID, status string
	expiresAt                        *time.Time
	priceCents                       int
	currencyCode                     string
}

func newMockDB() *mockReceiptDB {
	return &mockReceiptDB{
		subscriptions: make(map[string]*storedSub),
		userByTxn:     make(map[string]string),
	}
}

func (m *mockReceiptDB) UpsertSubscription(userID, productID, originalTxnID, status string, expiresAt *time.Time, priceCents int, currency string) error {
	if m.err != nil {
		return m.err
	}
	m.subscriptions[userID] = &storedSub{productID, originalTxnID, status, expiresAt, priceCents, currency}
	m.userByTxn[originalTxnID] = userID
	return nil
}

func (m *mockReceiptDB) UserIDByTransactionID(originalTxnID string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.userByTxn[originalTxnID], nil
}

func (m *mockReceiptDB) StoreTransaction(t *VerifiedTransaction) error {
	m.transactions = append(m.transactions, t)
	return nil
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func authReq(method, path, body string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	ctx := context.WithValue(r.Context(), middleware.CtxUserID, "user-1")
	return r.WithContext(ctx)
}

func decodeJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, w.Body.String())
	}
	return body
}

func sampleTransaction(bundleID, env string) TransactionInfo {
	return TransactionInfo{
		TransactionID:         "txn-123",
		OriginalTransactionID: "orig-txn-123",
		BundleID:              bundleID,
		ProductID:             "com.app.premium.monthly",
		PurchaseDate:          time.Now().Add(-24 * time.Hour).UnixMilli(),
		ExpiresDate:           time.Now().Add(30 * 24 * time.Hour).UnixMilli(),
		Type:                  "Auto-Renewable Subscription",
		InAppOwnershipType:    "PURCHASED",
		Environment:           env,
		Price:                 9990, // $9.99 in milliunits
		Currency:              "USD",
	}
}

// ── JWS Verification Tests ──────────────────────────────────────────────────

func TestVerifyAndParseTransaction_Valid(t *testing.T) {
	ca := newTestCA(t)
	db := newMockDB()
	svc := newTestService(t, ca, db, Config{BundleID: "com.test.app"})

	txn := sampleTransaction("com.test.app", "Production")
	jws := ca.signJWS(t, txn)

	parsed, err := svc.verifyAndParseTransaction(jws)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.TransactionID != "txn-123" {
		t.Fatalf("expected txn-123, got %s", parsed.TransactionID)
	}
	if parsed.ProductID != "com.app.premium.monthly" {
		t.Fatalf("expected product ID, got %s", parsed.ProductID)
	}
}

func TestVerifyAndParseTransaction_InvalidJWS(t *testing.T) {
	ca := newTestCA(t)
	svc := newTestService(t, ca, newMockDB(), Config{})

	_, err := svc.verifyAndParseTransaction("not.a.jws")
	if err == nil {
		t.Fatal("expected error for invalid JWS")
	}
}

func TestVerifyAndParseTransaction_WrongRootCA(t *testing.T) {
	ca := newTestCA(t)
	// Create a service with a DIFFERENT root CA — verification should fail.
	otherCA := newTestCA(t)
	svc := newTestService(t, otherCA, newMockDB(), Config{})

	txn := sampleTransaction("com.test.app", "Production")
	jws := ca.signJWS(t, txn)

	_, err := svc.verifyAndParseTransaction(jws)
	if err == nil {
		t.Fatal("expected error: JWS signed by wrong CA should fail verification")
	}
}

// ── Validation Tests ────────────────────────────────────────────────────────

func validTxn() *TransactionInfo {
	return &TransactionInfo{
		TransactionID:         "txn-1",
		OriginalTransactionID: "orig-1",
		ProductID:             "com.app.pro",
	}
}

func TestValidateTransaction_BundleIDMismatch(t *testing.T) {
	svc := &Service{cfg: Config{BundleID: "com.expected.app"}}
	txn := validTxn()
	txn.BundleID = "com.other.app"

	err := svc.validateTransaction(txn)
	if err == nil || !strings.Contains(err.Error(), "bundle ID mismatch") {
		t.Fatalf("expected bundle ID mismatch error, got %v", err)
	}
}

func TestValidateTransaction_EnvironmentMismatch(t *testing.T) {
	svc := &Service{cfg: Config{Environment: "Production"}}
	txn := validTxn()
	txn.Environment = "Sandbox"

	err := svc.validateTransaction(txn)
	if err == nil || !strings.Contains(err.Error(), "environment mismatch") {
		t.Fatalf("expected environment mismatch error, got %v", err)
	}
}

func TestValidateTransaction_Revoked(t *testing.T) {
	svc := &Service{cfg: Config{}}
	txn := validTxn()
	txn.RevocationDate = time.Now().UnixMilli()

	err := svc.validateTransaction(txn)
	if err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("expected revoked error, got %v", err)
	}
}

func TestValidateTransaction_Valid(t *testing.T) {
	svc := &Service{cfg: Config{BundleID: "com.test.app", Environment: "Production"}}
	txn := validTxn()
	txn.BundleID = "com.test.app"
	txn.Environment = "Production"

	if err := svc.validateTransaction(txn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateTransaction_NoBundleCheck(t *testing.T) {
	svc := &Service{cfg: Config{}}
	txn := validTxn()
	txn.BundleID = "com.any.app"
	txn.Environment = "Sandbox"

	if err := svc.validateTransaction(txn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ── Status Mapping Tests ────────────────────────────────────────────────────

func TestTransactionToStatus(t *testing.T) {
	svc := &Service{}

	tests := []struct {
		name     string
		txn      TransactionInfo
		expected string
	}{
		{
			name:     "active subscription",
			txn:      TransactionInfo{Type: "Auto-Renewable Subscription", ExpiresDate: time.Now().Add(time.Hour).UnixMilli()},
			expected: "active",
		},
		{
			name:     "expired subscription",
			txn:      TransactionInfo{Type: "Auto-Renewable Subscription", ExpiresDate: time.Now().Add(-time.Hour).UnixMilli()},
			expected: "expired",
		},
		{
			name:     "trial subscription",
			txn:      TransactionInfo{Type: "Auto-Renewable Subscription", OfferType: 1, ExpiresDate: time.Now().Add(time.Hour).UnixMilli()},
			expected: "trial",
		},
		{
			name:     "non-consumable (lifetime)",
			txn:      TransactionInfo{Type: "Non-Consumable"},
			expected: "active",
		},
		{
			name:     "consumable",
			txn:      TransactionInfo{Type: "Consumable"},
			expected: "active",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.transactionToStatus(&tt.txn)
			if got != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestNotificationToStatus(t *testing.T) {
	svc := &Service{}
	activeTxn := &TransactionInfo{Type: "Auto-Renewable Subscription", ExpiresDate: time.Now().Add(time.Hour).UnixMilli()}
	trialTxn := &TransactionInfo{Type: "Auto-Renewable Subscription", OfferType: 1, ExpiresDate: time.Now().Add(time.Hour).UnixMilli()}

	tests := []struct {
		name     string
		notif    string
		subtype  string
		txn      *TransactionInfo
		expected string
	}{
		{"subscribed", "SUBSCRIBED", "INITIAL_BUY", activeTxn, "active"},
		{"subscribed trial", "SUBSCRIBED", "INITIAL_BUY", trialTxn, "trial"},
		{"renewed", "DID_RENEW", "", activeTxn, "active"},
		{"expired", "EXPIRED", "VOLUNTARY", activeTxn, "expired"},
		{"refund", "REFUND", "", activeTxn, "expired"},
		{"revoke", "REVOKE", "", activeTxn, "expired"},
		{"auto renew disabled", "DID_CHANGE_RENEWAL_STATUS", "AUTO_RENEW_DISABLED", activeTxn, "cancelled"},
		{"auto renew enabled", "DID_CHANGE_RENEWAL_STATUS", "AUTO_RENEW_ENABLED", activeTxn, "active"},
		{"fail to renew grace", "DID_FAIL_TO_RENEW", "GRACE_PERIOD", activeTxn, "active"},
		{"fail to renew expired", "DID_FAIL_TO_RENEW", "", activeTxn, "expired"},
		{"offer redeemed", "OFFER_REDEEMED", "", activeTxn, "active"},
		{"unknown type", "UNKNOWN_TYPE", "", activeTxn, "active"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.notificationToStatus(tt.notif, tt.subtype, tt.txn)
			if got != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

// ── HandleVerifyReceipt Tests ───────────────────────────────────────────────

func TestHandleVerifyReceipt_Valid(t *testing.T) {
	ca := newTestCA(t)
	db := newMockDB()
	svc := newTestService(t, ca, db, Config{BundleID: "com.test.app"})

	txn := sampleTransaction("com.test.app", "Production")
	jws := ca.signJWS(t, txn)

	body := `{"transaction":"` + jws + `"}`
	req := authReq("POST", "/api/v1/receipt/verify", body)
	w := httptest.NewRecorder()
	svc.HandleVerifyReceipt(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	resp := decodeJSON(t, w)
	if resp["verified"] != true {
		t.Fatal("expected verified=true")
	}
	if resp["status"] != "active" {
		t.Fatalf("expected status=active, got %v", resp["status"])
	}
	if resp["product_id"] != "com.app.premium.monthly" {
		t.Fatalf("expected product_id, got %v", resp["product_id"])
	}

	// Verify subscription was stored.
	sub := db.subscriptions["user-1"]
	if sub == nil {
		t.Fatal("expected subscription to be stored")
	}
	if sub.status != "active" {
		t.Fatalf("expected stored status=active, got %s", sub.status)
	}
	if sub.priceCents != 999 {
		t.Fatalf("expected 999 cents, got %d", sub.priceCents)
	}

	// Verify audit trail.
	if len(db.transactions) != 1 {
		t.Fatalf("expected 1 stored transaction, got %d", len(db.transactions))
	}
}

func TestHandleVerifyReceipt_EmptyTransaction(t *testing.T) {
	ca := newTestCA(t)
	svc := newTestService(t, ca, newMockDB(), Config{})

	req := authReq("POST", "/api/v1/receipt/verify", `{"transaction":""}`)
	w := httptest.NewRecorder()
	svc.HandleVerifyReceipt(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleVerifyReceipt_InvalidBody(t *testing.T) {
	ca := newTestCA(t)
	svc := newTestService(t, ca, newMockDB(), Config{})

	req := authReq("POST", "/api/v1/receipt/verify", "not json")
	w := httptest.NewRecorder()
	svc.HandleVerifyReceipt(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleVerifyReceipt_BundleMismatch(t *testing.T) {
	ca := newTestCA(t)
	svc := newTestService(t, ca, newMockDB(), Config{BundleID: "com.expected.app"})

	txn := sampleTransaction("com.wrong.app", "Production")
	jws := ca.signJWS(t, txn)

	body := `{"transaction":"` + jws + `"}`
	req := authReq("POST", "/api/v1/receipt/verify", body)
	w := httptest.NewRecorder()
	svc.HandleVerifyReceipt(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleVerifyReceipt_ExpiredSubscription(t *testing.T) {
	ca := newTestCA(t)
	db := newMockDB()
	svc := newTestService(t, ca, db, Config{BundleID: "com.test.app"})

	txn := sampleTransaction("com.test.app", "Production")
	txn.ExpiresDate = time.Now().Add(-24 * time.Hour).UnixMilli() // expired
	jws := ca.signJWS(t, txn)

	body := `{"transaction":"` + jws + `"}`
	req := authReq("POST", "/api/v1/receipt/verify", body)
	w := httptest.NewRecorder()
	svc.HandleVerifyReceipt(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	resp := decodeJSON(t, w)
	if resp["status"] != "expired" {
		t.Fatalf("expected status=expired, got %v", resp["status"])
	}
}

func TestHandleVerifyReceipt_TrialSubscription(t *testing.T) {
	ca := newTestCA(t)
	db := newMockDB()
	svc := newTestService(t, ca, db, Config{BundleID: "com.test.app"})

	txn := sampleTransaction("com.test.app", "Production")
	txn.OfferType = 1 // introductory/trial
	jws := ca.signJWS(t, txn)

	body := `{"transaction":"` + jws + `"}`
	req := authReq("POST", "/api/v1/receipt/verify", body)
	w := httptest.NewRecorder()
	svc.HandleVerifyReceipt(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	resp := decodeJSON(t, w)
	if resp["status"] != "trial" {
		t.Fatalf("expected status=trial, got %v", resp["status"])
	}
}

// ── HandleWebhook Tests ─────────────────────────────────────────────────────

func TestHandleWebhook_Renewal(t *testing.T) {
	ca := newTestCA(t)
	db := newMockDB()
	svc := newTestService(t, ca, db, Config{BundleID: "com.test.app"})

	// Pre-register the user-transaction mapping.
	db.userByTxn["orig-txn-123"] = "user-1"

	txn := sampleTransaction("com.test.app", "Production")
	signedTxn := ca.signJWS(t, txn)

	notification := NotificationPayload{
		NotificationType: "DID_RENEW",
		Data: NotificationData{
			SignedTransactionInfo: signedTxn,
			Environment:          "Production",
			BundleID:             "com.test.app",
		},
	}
	signedNotif := ca.signJWS(t, notification)

	body := `{"signedPayload":"` + signedNotif + `"}`
	req := httptest.NewRequest("POST", "/api/v1/receipt/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()
	svc.HandleWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	sub := db.subscriptions["user-1"]
	if sub == nil {
		t.Fatal("expected subscription update")
	}
	if sub.status != "active" {
		t.Fatalf("expected active after renewal, got %s", sub.status)
	}
}

func TestHandleWebhook_Expired(t *testing.T) {
	ca := newTestCA(t)
	db := newMockDB()
	svc := newTestService(t, ca, db, Config{BundleID: "com.test.app"})

	db.userByTxn["orig-txn-123"] = "user-1"

	txn := sampleTransaction("com.test.app", "Production")
	signedTxn := ca.signJWS(t, txn)

	notification := NotificationPayload{
		NotificationType: "EXPIRED",
		Subtype:          "VOLUNTARY",
		Data: NotificationData{
			SignedTransactionInfo: signedTxn,
			Environment:          "Production",
		},
	}
	signedNotif := ca.signJWS(t, notification)

	body := `{"signedPayload":"` + signedNotif + `"}`
	req := httptest.NewRequest("POST", "/api/v1/receipt/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()
	svc.HandleWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if db.subscriptions["user-1"].status != "expired" {
		t.Fatalf("expected expired, got %s", db.subscriptions["user-1"].status)
	}
}

func TestHandleWebhook_Refund(t *testing.T) {
	ca := newTestCA(t)
	db := newMockDB()
	svc := newTestService(t, ca, db, Config{})

	db.userByTxn["orig-txn-123"] = "user-1"

	txn := sampleTransaction("com.test.app", "Production")
	signedTxn := ca.signJWS(t, txn)

	notification := NotificationPayload{
		NotificationType: "REFUND",
		Data:             NotificationData{SignedTransactionInfo: signedTxn},
	}
	signedNotif := ca.signJWS(t, notification)

	body := `{"signedPayload":"` + signedNotif + `"}`
	req := httptest.NewRequest("POST", "/api/v1/receipt/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()
	svc.HandleWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	if db.subscriptions["user-1"].status != "expired" {
		t.Fatalf("expected expired after refund, got %s", db.subscriptions["user-1"].status)
	}
}

func TestHandleWebhook_TestNotification(t *testing.T) {
	ca := newTestCA(t)
	svc := newTestService(t, ca, newMockDB(), Config{})

	notification := NotificationPayload{
		NotificationType: "TEST",
	}
	signedNotif := ca.signJWS(t, notification)

	body := `{"signedPayload":"` + signedNotif + `"}`
	req := httptest.NewRequest("POST", "/api/v1/receipt/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()
	svc.HandleWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for TEST notification, got %d", w.Code)
	}
}

func TestHandleWebhook_UnknownTransaction(t *testing.T) {
	ca := newTestCA(t)
	db := newMockDB()
	// No user-transaction mapping registered.
	svc := newTestService(t, ca, db, Config{})

	txn := sampleTransaction("com.test.app", "Production")
	signedTxn := ca.signJWS(t, txn)

	notification := NotificationPayload{
		NotificationType: "DID_RENEW",
		Data:             NotificationData{SignedTransactionInfo: signedTxn},
	}
	signedNotif := ca.signJWS(t, notification)

	body := `{"signedPayload":"` + signedNotif + `"}`
	req := httptest.NewRequest("POST", "/api/v1/receipt/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()
	svc.HandleWebhook(w, req)

	// Should still return 200 (acknowledge to Apple) but status=unknown_transaction.
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decodeJSON(t, w)
	if resp["status"] != "unknown_transaction" {
		t.Fatalf("expected unknown_transaction, got %v", resp["status"])
	}
}

func TestHandleWebhook_AppAccountTokenFallback(t *testing.T) {
	ca := newTestCA(t)
	db := newMockDB()
	svc := newTestService(t, ca, db, Config{})

	txn := sampleTransaction("com.test.app", "Production")
	txn.AppAccountToken = "user-from-token"
	signedTxn := ca.signJWS(t, txn)

	notification := NotificationPayload{
		NotificationType: "SUBSCRIBED",
		Data:             NotificationData{SignedTransactionInfo: signedTxn},
	}
	signedNotif := ca.signJWS(t, notification)

	body := `{"signedPayload":"` + signedNotif + `"}`
	req := httptest.NewRequest("POST", "/api/v1/receipt/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()
	svc.HandleWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	sub := db.subscriptions["user-from-token"]
	if sub == nil {
		t.Fatal("expected subscription for user-from-token via appAccountToken fallback")
	}
}

func TestHandleWebhook_InvalidPayload(t *testing.T) {
	ca := newTestCA(t)
	svc := newTestService(t, ca, newMockDB(), Config{})

	req := httptest.NewRequest("POST", "/api/v1/receipt/webhook", strings.NewReader(`{"signedPayload":""}`))
	w := httptest.NewRecorder()
	svc.HandleWebhook(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWebhook_InvalidJSON(t *testing.T) {
	ca := newTestCA(t)
	svc := newTestService(t, ca, newMockDB(), Config{})

	req := httptest.NewRequest("POST", "/api/v1/receipt/webhook", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	svc.HandleWebhook(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleWebhook_CancelledRenewal(t *testing.T) {
	ca := newTestCA(t)
	db := newMockDB()
	svc := newTestService(t, ca, db, Config{})

	db.userByTxn["orig-txn-123"] = "user-1"

	txn := sampleTransaction("com.test.app", "Production")
	signedTxn := ca.signJWS(t, txn)

	notification := NotificationPayload{
		NotificationType: "DID_CHANGE_RENEWAL_STATUS",
		Subtype:          "AUTO_RENEW_DISABLED",
		Data:             NotificationData{SignedTransactionInfo: signedTxn},
	}
	signedNotif := ca.signJWS(t, notification)

	body := `{"signedPayload":"` + signedNotif + `"}`
	req := httptest.NewRequest("POST", "/api/v1/receipt/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()
	svc.HandleWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	if db.subscriptions["user-1"].status != "cancelled" {
		t.Fatalf("expected cancelled, got %s", db.subscriptions["user-1"].status)
	}
}

// ── Helper Tests ────────────────────────────────────────────────────────────

func TestMillisToTime(t *testing.T) {
	// 2025-01-01T00:00:00Z in millis
	ms := int64(1735689600000)
	got := millisToTime(ms)
	expected := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if !got.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, got)
	}
}

func TestMillisToTimePtr_Zero(t *testing.T) {
	if millisToTimePtr(0) != nil {
		t.Fatal("expected nil for 0 millis")
	}
}

func TestMillisToTimePtr_NonZero(t *testing.T) {
	ptr := millisToTimePtr(1735689600000)
	if ptr == nil {
		t.Fatal("expected non-nil")
	}
}

func TestParseAppleRootCA(t *testing.T) {
	cert, err := parseAppleRootCA()
	if err != nil {
		t.Fatalf("failed to parse Apple Root CA: %v", err)
	}
	if cert.Subject.CommonName != "Apple Root CA - G3" {
		t.Fatalf("expected Apple Root CA - G3, got %s", cert.Subject.CommonName)
	}
	if !cert.IsCA {
		t.Fatal("expected root CA to be a CA")
	}
}

func TestMigrations(t *testing.T) {
	m := Migrations()
	if len(m) != 3 {
		t.Fatalf("expected 3 migrations, got %d", len(m))
	}
}

// ── Crash Safety Tests ──────────────────────────────────────────────────────

func TestHandleVerifyReceipt_NoAuthContext(t *testing.T) {
	ca := newTestCA(t)
	svc := newTestService(t, ca, newMockDB(), Config{})

	// Request WITHOUT auth context — must not panic.
	req := httptest.NewRequest("POST", "/api/v1/receipt/verify", strings.NewReader(`{"transaction":"x"}`))
	w := httptest.NewRecorder()
	svc.HandleVerifyReceipt(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleVerifyReceipt_DBError(t *testing.T) {
	ca := newTestCA(t)
	db := newMockDB()
	db.err = errors.New("db down")
	svc := newTestService(t, ca, db, Config{BundleID: "com.test.app"})

	txn := sampleTransaction("com.test.app", "Production")
	jws := ca.signJWS(t, txn)

	body := `{"transaction":"` + jws + `"}`
	req := authReq("POST", "/api/v1/receipt/verify", body)
	w := httptest.NewRecorder()
	svc.HandleVerifyReceipt(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on DB error, got %d", w.Code)
	}
}

func TestHandleVerifyReceipt_RevokedTransaction(t *testing.T) {
	ca := newTestCA(t)
	svc := newTestService(t, ca, newMockDB(), Config{BundleID: "com.test.app"})

	txn := sampleTransaction("com.test.app", "Production")
	txn.RevocationDate = time.Now().UnixMilli()
	jws := ca.signJWS(t, txn)

	body := `{"transaction":"` + jws + `"}`
	req := authReq("POST", "/api/v1/receipt/verify", body)
	w := httptest.NewRecorder()
	svc.HandleVerifyReceipt(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for revoked transaction, got %d", w.Code)
	}
}

func TestValidateTransaction_MissingTransactionID(t *testing.T) {
	svc := &Service{cfg: Config{}}
	txn := &TransactionInfo{ProductID: "prod", OriginalTransactionID: ""}

	err := svc.validateTransaction(txn)
	if err == nil || !strings.Contains(err.Error(), "missing transaction ID") {
		t.Fatalf("expected missing transaction ID error, got %v", err)
	}
}

func TestValidateTransaction_MissingProductID(t *testing.T) {
	svc := &Service{cfg: Config{}}
	txn := &TransactionInfo{TransactionID: "t1", OriginalTransactionID: "o1"}

	err := svc.validateTransaction(txn)
	if err == nil || !strings.Contains(err.Error(), "missing product ID") {
		t.Fatalf("expected missing product ID error, got %v", err)
	}
}

func TestHandleWebhook_DBLookupError(t *testing.T) {
	ca := newTestCA(t)
	db := newMockDB()
	db.err = errors.New("db down")
	svc := newTestService(t, ca, db, Config{})

	txn := sampleTransaction("com.test.app", "Production")
	signedTxn := ca.signJWS(t, txn)

	notification := NotificationPayload{
		NotificationType: "DID_RENEW",
		Data:             NotificationData{SignedTransactionInfo: signedTxn},
	}
	signedNotif := ca.signJWS(t, notification)

	body := `{"signedPayload":"` + signedNotif + `"}`
	req := httptest.NewRequest("POST", "/api/v1/receipt/webhook", strings.NewReader(body))
	w := httptest.NewRecorder()
	svc.HandleWebhook(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}
