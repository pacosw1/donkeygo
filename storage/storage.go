// Package storage provides S3-compatible object storage with AWS Signature V4 signing.
package storage

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Config holds S3-compatible storage configuration.
type Config struct {
	Bucket    string
	Region    string
	Endpoint  string // e.g. "s3.amazonaws.com"
	AccessKey string
	SecretKey string
}

// Client provides S3-compatible storage operations.
type Client struct {
	cfg Config
}

// New creates a new storage client.
func New(cfg Config) *Client {
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = "s3.amazonaws.com"
	}
	return &Client{cfg: cfg}
}

// Put uploads an object to S3.
func (c *Client) Put(key, contentType string, data []byte) error {
	host, objectURL := c.s3URL(key)

	now := time.Now().UTC()
	dateStamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")
	payloadHash := sha256Hex(data)

	req, err := http.NewRequest("PUT", objectURL, bytes.NewReader(data))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Host", host)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	req.Header.Set("x-amz-date", amzDate)

	signedHeaders := "content-type;host;x-amz-content-sha256;x-amz-date"
	canonicalHeaders := fmt.Sprintf("content-type:%s\nhost:%s\nx-amz-content-sha256:%s\nx-amz-date:%s\n",
		contentType, host, payloadHash, amzDate)
	canonicalRequest := fmt.Sprintf("PUT\n/%s\n\n%s\n%s\n%s", key, canonicalHeaders, signedHeaders, payloadHash)

	c.signRequest(req, canonicalRequest, signedHeaders, dateStamp, amzDate)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("s3 put: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("s3 put status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// Get retrieves an object from S3.
func (c *Client) Get(key string) ([]byte, string, error) {
	host, objectURL := c.s3URL(key)

	now := time.Now().UTC()
	dateStamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")
	payloadHash := sha256Hex([]byte{})

	req, err := http.NewRequest("GET", objectURL, nil)
	if err != nil {
		return nil, "", err
	}

	req.Header.Set("Host", host)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	req.Header.Set("x-amz-date", amzDate)

	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalHeaders := fmt.Sprintf("host:%s\nx-amz-content-sha256:%s\nx-amz-date:%s\n",
		host, payloadHash, amzDate)
	canonicalRequest := fmt.Sprintf("GET\n/%s\n\n%s\n%s\n%s", key, canonicalHeaders, signedHeaders, payloadHash)

	c.signRequest(req, canonicalRequest, signedHeaders, dateStamp, amzDate)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("s3 get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("s3 get status %d: %s", resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = http.DetectContentType(data)
	}
	return data, ct, nil
}

// Configured returns true if the storage client has valid credentials.
func (c *Client) Configured() bool {
	return c.cfg.Bucket != "" && c.cfg.AccessKey != "" && c.cfg.SecretKey != ""
}

func (c *Client) s3URL(key string) (host, objectURL string) {
	host = fmt.Sprintf("%s.%s", c.cfg.Bucket, c.cfg.Endpoint)
	objectURL = fmt.Sprintf("https://%s/%s", host, key)
	return
}

func (c *Client) signRequest(req *http.Request, canonicalRequest, signedHeaders, dateStamp, amzDate string) {
	region := c.cfg.Region
	credentialScope := fmt.Sprintf("%s/%s/s3/aws4_request", dateStamp, region)
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s", amzDate, credentialScope, sha256Hex([]byte(canonicalRequest)))

	signingKey := getSignatureKey(c.cfg.SecretKey, dateStamp, region, "s3")
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	authHeader := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		c.cfg.AccessKey, credentialScope, signedHeaders, signature)
	req.Header.Set("Authorization", authHeader)
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func getSignatureKey(secretKey, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secretKey), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	return kSigning
}
