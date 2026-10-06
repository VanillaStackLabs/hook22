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
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
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
	Payload  WebhookPayload `json:"payload"`
	Attempts int            `json:"attempts"`
}

type WebhookDispatcher struct {
	cfg        *Config
	client     *http.Client
	taskQueue  chan WebhookTask
	amqpConn   *amqp.Connection
	amqpChan   *amqp.Channel
	wg         sync.WaitGroup
	useRabbit  bool
}

var globalDispatcher *WebhookDispatcher

func InitWebhookDispatcher(cfg *Config) {
	dispatcher := &WebhookDispatcher{
		cfg:       cfg,
		client:    &http.Client{Timeout: 10 * time.Second},
		taskQueue: make(chan WebhookTask, 1000),
	}

	if cfg.RabbitMQ.Enabled && cfg.RabbitMQ.URL != "" {
		queueName := cfg.RabbitMQ.QueueName
		if queueName == "" {
			queueName = "hook22_webhooks"
		}

		conn, err := amqp.Dial(cfg.RabbitMQ.URL)
		if err != nil {
			slog.Error("Failed to connect to RabbitMQ, falling back to memory queue", "event", "rabbitmq.connect_error", "error", err.Error())
		} else {
			ch, err := conn.Channel()
			if err != nil {
				slog.Error("Failed to open RabbitMQ channel, falling back to memory queue", "event", "rabbitmq.channel_error", "error", err.Error())
				conn.Close()
			} else {
				// Declare a durable, persistent queue
				_, err = ch.QueueDeclare(
					queueName, // name
					true,      // durable
					false,     // delete when unused
					false,     // exclusive
					false,     // no-wait
					nil,       // arguments
				)
				if err != nil {
					slog.Error("Failed to declare RabbitMQ queue", "event", "rabbitmq.queue_error", "error", err.Error())
					ch.Close()
					conn.Close()
				} else {
					dispatcher.amqpConn = conn
					dispatcher.amqpChan = ch
					dispatcher.useRabbit = true
					slog.Info("RabbitMQ persistent webhook queue initialized successfully", "event", "rabbitmq.ready", "queue", queueName)
				}
			}
		}
	}

	if dispatcher.useRabbit {
		// Set Prefetch count so workers pull tasks fairly
		_ = dispatcher.amqpChan.Qos(cfg.Webhook.Workers, 0, false)
		for i := 0; i < cfg.Webhook.Workers; i++ {
			dispatcher.wg.Add(1)
			go dispatcher.rabbitWorker(i + 1)
		}
	} else {
		for i := 0; i < cfg.Webhook.Workers; i++ {
			dispatcher.wg.Add(1)
			go dispatcher.memoryWorker(i + 1)
		}
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

	if globalDispatcher == nil {
		dispatchWithRetry(task, cfg, &http.Client{Timeout: 10 * time.Second})
		return
	}

	if globalDispatcher.useRabbit {
		body, err := json.Marshal(task)
		if err != nil {
			slog.Error("Failed to marshal webhook task for RabbitMQ", "event", "webhook.marshal_error", "error", err.Error())
			return
		}

		queueName := cfg.RabbitMQ.QueueName
		if queueName == "" {
			queueName = "hook22_webhooks"
		}

		// Publish persistent message
		err = globalDispatcher.amqpChan.Publish(
			"",        // exchange
			queueName, // routing key
			false,     // mandatory
			false,     // immediate
			amqp.Publishing{
				DeliveryMode: amqp.Persistent,
				ContentType:  "application/json",
				Body:         body,
			},
		)
		if err != nil {
			slog.Error("Failed to publish webhook task to RabbitMQ", "event", "rabbitmq.publish_error", "error", err.Error())
			WebhookDeliveriesTotal.WithLabelValues("dropped_rabbitmq_error").Inc()
			return
		}

		slog.Info("Published webhook task to RabbitMQ persistent queue", "event", "rabbitmq.enqueue", "filepath", filepath)
	} else {
		select {
		case globalDispatcher.taskQueue <- task:
			slog.Info("Enqueued webhook delivery task (memory)", "event", "webhook.enqueue", "filepath", filepath)
		default:
			slog.Error("Webhook queue full, dropping event", "event", "webhook.queue_overflow", "filepath", filepath)
			WebhookDeliveriesTotal.WithLabelValues("dropped_queue_full").Inc()
		}
	}
}

func (d *WebhookDispatcher) memoryWorker(id int) {
	defer d.wg.Done()
	slog.Debug("Starting memory webhook worker", "worker_id", id)
	for task := range d.taskQueue {
		dispatchWithRetry(task, d.cfg, d.client)
	}
}

func (d *WebhookDispatcher) rabbitWorker(id int) {
	defer d.wg.Done()
	slog.Debug("Starting RabbitMQ webhook worker", "worker_id", id)

	queueName := d.cfg.RabbitMQ.QueueName
	if queueName == "" {
		queueName = "hook22_webhooks"
	}

	msgs, err := d.amqpChan.Consume(
		queueName,
		fmt.Sprintf("hook22-worker-%d", id),
		false, // auto-ack = FALSE (manual acknowledgment guarantees no message loss)
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,   // args
	)
	if err != nil {
		slog.Error("RabbitMQ worker failed to start consuming", "worker_id", id, "error", err.Error())
		return
	}

	for msg := range msgs {
		var task WebhookTask
		if err := json.Unmarshal(msg.Body, &task); err != nil {
			slog.Error("Malformed RabbitMQ task body, rejecting", "error", err.Error())
			msg.Nack(false, false) // Reject without requeue
			continue
		}

		success := dispatchWithRetry(task, d.cfg, d.client)
		if success {
			msg.Ack(false) // Confirm message delivery complete
		} else {
			// Delivery exhausted retries, acknowledge to remove from queue
			msg.Ack(false)
		}
	}
}

func (d *WebhookDispatcher) Shutdown() {
	if d.useRabbit {
		if d.amqpChan != nil {
			d.amqpChan.Close()
		}
		if d.amqpConn != nil {
			d.amqpConn.Close()
		}
	} else {
		close(d.taskQueue)
	}
	d.wg.Wait()
}

func dispatchWithRetry(task WebhookTask, cfg *Config, client *http.Client) bool {
	body, err := json.Marshal(task.Payload)
	if err != nil {
		slog.Error("Failed to marshal webhook payload", "event", "webhook.marshal_error", "error", err.Error())
		return false
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
			return false
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Hook22-Signature", signature)

		slog.Info("Attempting webhook delivery", "event", "webhook.attempt", "filepath", task.Payload.Filepath, "attempt", task.Attempts, "max_attempts", maxRetries)

		resp, err := client.Do(req)
		if err == nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			resp.Body.Close()
			slog.Info("Webhook delivered successfully", "event", "webhook.delivered", "filepath", task.Payload.Filepath, "status_code", resp.StatusCode)
			WebhookDeliveriesTotal.WithLabelValues("success").Inc()
			return true
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
			return false
		}

		tempBackoff := float64(baseBackoff) * math.Pow(2, float64(task.Attempts-1))
		maxSleep := math.Min(300.0, tempBackoff)
		jitterSleep := time.Duration(rand.Float64() * maxSleep * float64(time.Second))

		slog.Info("Scheduling webhook retry", "event", "webhook.retry_scheduled", "filepath", task.Payload.Filepath, "backoff", jitterSleep.String())
		time.Sleep(jitterSleep)
	}

	return false
}