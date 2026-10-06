package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
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

type WebhookTask struct {
	Payload  WebhookPayload
	Attempts int
}

type WebhookDispatcher struct {
	cfg       *Config
	client    *http.Client
	taskQueue chan WebhookTask
	rng       *rand.Rand
}

var globalDispatcher *WebhookDispatcher

func InitWebhookDispatcher(cfg *Config) {
	dispatcher := &WebhookDispatcher{
		cfg:       cfg,
		client:    &http.Client{Timeout: 10 * time.Second},
		taskQueue: make(chan WebhookTask, 1000),
		rng:       rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	for i := 0; i < cfg.Webhook.Workers; i++ {
		go dispatcher.worker(i + 1)
	}

	globalDispatcher = dispatcher
}

func triggerWebhook(filepath, username, hash string, size int64, cfg *Config) {
	payload := WebhookPayload{
		Event:     "file.uploaded",
		Username:  username,
		Filepath:  filepath,
		SizeBytes: size,
		SHA256:    hash,
		Status:    "success",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	task := WebhookTask{
		Payload:  payload,
		Attempts: 0,
	}

	if globalDispatcher != nil {
		select {
		case globalDispatcher.taskQueue <- task:
			slog.Info("Enqueued webhook delivery task", "event", "webhook.enqueue", "filepath", filepath)
		default:
			slog.Error("Webhook queue full, dropping event", "event", "webhook.queue_overflow", "filepath", filepath)
			WebhookDeliveriesTotal.WithLabelValues("dropped_queue_full").Inc() // Track dropped events
		}
	} else {
		dispatchWithRetry(task, cfg, &http.Client{Timeout: 10 * time.Second})
	}
}

func (d *WebhookDispatcher) worker(id int) {
	for task := range d.taskQueue {
		d.processTask(task)
	}
}

func (d *WebhookDispatcher) processTask(task WebhookTask) {
	dispatchWithRetry(task, d.cfg, d.client)
}

func dispatchWithRetry(task WebhookTask, cfg *Config, client *http.Client) {
	body, err := json.Marshal(task.Payload)
	if err != nil {
		slog.Error("Failed to marshal webhook payload", "event", "webhook.marshal_error", "error", err.Error())
		return
	}

	mac := hmac.New(sha256.New, []byte(cfg.Webhook.Secret))
	mac.Write(body)
	signature := fmt.Sprintf("v1=%s", hex.EncodeToString(mac.Sum(nil)))

	maxRetries := cfg.Webhook.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 5
	}

	baseBackoff := cfg.Webhook.BaseBackoff
	if baseBackoff <= 0 {
		baseBackoff = 2
	}

	for task.Attempts < maxRetries {
		task.Attempts++

		req, err := http.NewRequest("POST", cfg.Webhook.URL, bytes.NewBuffer(body))
		if err != nil {
			slog.Error("Webhook request creation failed", "event", "webhook.request_error", "error", err.Error())
			return
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Hook22-Signature", signature)

		slog.Info("Attempting webhook delivery", "event", "webhook.attempt", "filepath", task.Payload.Filepath, "attempt", task.Attempts, "max_attempts", maxRetries)

		resp, err := client.Do(req)
		if err == nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			resp.Body.Close()
			slog.Info("Webhook delivered successfully", "event", "webhook.delivered", "filepath", task.Payload.Filepath, "status_code", resp.StatusCode)
			WebhookDeliveriesTotal.WithLabelValues("success").Inc()
			return
		}

		if err != nil {
			slog.Warn("Webhook delivery network error", "event", "webhook.network_error", "error", err.Error(), "attempt", task.Attempts)
		} else {
			resp.Body.Close()
			slog.Warn("Webhook target returned non-2xx status", "event", "webhook.http_error", "status_code", resp.StatusCode, "attempt", task.Attempts)
		}

		if task.Attempts >= maxRetries {
			slog.Error("Webhook delivery exhausted max retries, dropping event", "event", "webhook.exhausted", "filepath", task.Payload.Filepath, "total_attempts", task.Attempts)
			WebhookDeliveriesTotal.WithLabelValues("failure").Inc()
			return
		}

		tempBackoff := float64(baseBackoff) * math.Pow(2, float64(task.Attempts-1))
		maxSleep := math.Min(300.0, tempBackoff)
		jitterSleep := time.Duration(rand.Float64() * maxSleep * float64(time.Second))

		slog.Info("Scheduling webhook retry", "event", "webhook.retry_scheduled", "filepath", task.Payload.Filepath, "backoff", jitterSleep.String())
		time.Sleep(jitterSleep)
	}
}
