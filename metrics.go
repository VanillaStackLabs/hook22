package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// ActiveSSHSessions tracks current concurrent SFTP connections
	ActiveSSHSessions = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "hook22_active_ssh_sessions",
		Help: "Current number of active SSH sessions",
	})

	// UploadBytesTotal tracks throughput. (Query with rate(hook22_upload_bytes_total[1m]) for bytes/sec)
	UploadBytesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "hook22_upload_bytes_total",
		Help: "Total bytes successfully uploaded to storage",
	})

	// UploadDuration tracks storage write latencies
	UploadDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "hook22_upload_duration_seconds",
		Help:    "Latency of storage uploads in seconds",
		Buckets: prometheus.DefBuckets,
	})

	// WebhookDeliveriesTotal tracks delivery success and failure rates
	WebhookDeliveriesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hook22_webhook_deliveries_total",
		Help: "Total number of webhook deliveries by final status",
	}, []string{"status"})
)
