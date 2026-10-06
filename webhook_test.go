package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTriggerWebhook_PayloadAndSignature(t *testing.T) {
	secret := "test_webhook_secret_123"
	var receivedBody []byte
	var receivedSig string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSig = r.Header.Get("Hook22-Signature")
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &Config{
		Webhook: struct {
			URL         string `yaml:"url"`
			Secret      string `yaml:"secret"`
			Workers     int    `yaml:"workers"`
			MaxRetries  int    `yaml:"max_retries"`
			BaseBackoff int    `yaml:"base_backoff"`
		}{
			URL:         server.URL,
			Secret:      secret,
			Workers:     1,
			MaxRetries:  1,
			BaseBackoff: 1,
		},
	}

	filepath := "/invoices/2026.csv"
	username := "acme_corp"
	hash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	size := int64(1024)

	// Execute direct dispatch with retry logic
	task := WebhookTask{
		Payload: WebhookPayload{
			Event:     "file.uploaded",
			Username:  username,
			Filepath:  filepath,
			SizeBytes: size,
			SHA256:    hash,
			Status:    "success",
		},
		Attempts: 0,
	}

	dispatchWithRetry(task, cfg, http.DefaultClient)

	// Verify JSON Payload
	var payload WebhookPayload
	if err := json.Unmarshal(receivedBody, &payload); err != nil {
		t.Fatalf("Failed to unmarshal received webhook JSON: %v", err)
	}

	if payload.Filepath != filepath || payload.Username != username || payload.SHA256 != hash || payload.SizeBytes != size {
		t.Errorf("Payload mismatch. Got %+v", payload)
	}

	// Verify HMAC-SHA256 Signature
	if !strings.HasPrefix(receivedSig, "v1=") {
		t.Fatalf("Expected signature header to start with 'v1=', got: %s", receivedSig)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(receivedBody)
	expectedSig := "v1=" + hex.EncodeToString(mac.Sum(nil))

	if receivedSig != expectedSig {
		t.Errorf("HMAC signature verification failed.\nGot:  %s\nWant: %s", receivedSig, expectedSig)
	}
}

func TestTriggerWebhook_RetryMechanismOnFailure(t *testing.T) {
	secret := "test_webhook_secret_123"
	var attempts int32

	// Server fails twice with 500, succeeds on 3rd attempt
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count < 3 {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &Config{
		Webhook: struct {
			URL         string `yaml:"url"`
			Secret      string `yaml:"secret"`
			Workers     int    `yaml:"workers"`
			MaxRetries  int    `yaml:"max_retries"`
			BaseBackoff int    `yaml:"base_backoff"`
		}{
			URL:         server.URL,
			Secret:      secret,
			Workers:     1,
			MaxRetries:  3,
			BaseBackoff: 1,
		},
	}

	task := WebhookTask{
		Payload: WebhookPayload{
			Event:     "file.uploaded",
			Username:  "acme_corp",
			Filepath:  "/test.csv",
			SizeBytes: 100,
			SHA256:    "abc",
			Status:    "success",
		},
		Attempts: 0,
	}

	dispatchWithRetry(task, cfg, http.DefaultClient)

	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("Expected 3 delivery attempts, got %d", attempts)
	}
}
