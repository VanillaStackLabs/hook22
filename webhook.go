package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

type WebhookPayload struct {
	Event     string `json:"event"`
	Username  string `json:"username"`
	Filepath  string `json:"filepath"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

func triggerWebhook(filepath, username, hash string, size int64, cfg *Config) {
	slog.Info("Dispatching HTTP POST webhook", "event", "webhook.dispatch_start", "filepath", filepath, "username", username)

	payload := WebhookPayload{
		Event:     "file.uploaded",
		Username:  username,
		Filepath:  filepath,
		SizeBytes: size,
		SHA256:    hash,
		Status:    "success",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("Failed to marshal webhook payload", "event", "webhook.marshal_error", "error", err.Error())
		return
	}

	mac := hmac.New(sha256.New, []byte(cfg.Webhook.Secret))
	mac.Write(body)
	signature := hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequest("POST", cfg.Webhook.URL, bytes.NewBuffer(body))
	if err != nil {
		slog.Error("Webhook request creation failed", "event", "webhook.request_error", "error", err.Error())
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Hook22-Signature", fmt.Sprintf("v1=%s", signature))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		slog.Error("Webhook delivery failed", "event", "webhook.delivery_failed", "error", err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Warn("Webhook target returned non-2xx status", "event", "webhook.http_error", "status_code", resp.StatusCode)
		return
	}

	slog.Info("Webhook delivered successfully", "event", "webhook.delivered", "status_code", resp.StatusCode)
}
