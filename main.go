package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func initStorageProvider(ctx context.Context, cfg *Config) StorageProvider {
	switch cfg.Storage.Driver {
	case "s3":
		provider, err := NewS3Provider(ctx, cfg)
		if err != nil {
			slog.Error("S3 initialization failed", "error", err.Error())
			return &MockStorageProvider{}
		}
		return provider
	case "gcs":
		provider, err := NewGCSProvider(ctx, cfg)
		if err != nil {
			slog.Error("GCS initialization failed", "error", err.Error())
			return &MockStorageProvider{}
		}
		return provider
	case "azure":
		provider, err := NewAzureProvider(cfg)
		if err != nil {
			slog.Error("Azure initialization failed", "error", err.Error())
			return &MockStorageProvider{}
		}
		return provider
	case "disk":
		return NewDiskProvider(cfg)
	default:
		slog.Info("Using mock in-memory storage provider", "driver", cfg.Storage.Driver)
		return &MockStorageProvider{}
	}
}

func main() {
	broadcaster := NewLogBroadcaster()
	logger := slog.New(slog.NewJSONHandler(broadcaster, nil))
	slog.SetDefault(logger)

	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/api/v1/logs/stream", handleLogStream(broadcaster))
	go http.ListenAndServe(":8080", nil)

	cfg, err := loadConfig("config.yaml")
	if err != nil {
		slog.Error("Failed to load config.yaml", "event", "config.error", "error", err.Error())
		os.Exit(1)
	}

	InitWebhookDispatcher(cfg)
	storageBackend := initStorageProvider(context.Background(), cfg)

	// Register outbound push endpoint
	http.HandleFunc("/api/v1/sftp/push", handleOutboundPush(storageBackend))

	// Pass nil for resolver if using static YAML auth only
	sshConfig, err := buildSSHConfig(cfg, nil)
	if err != nil {
		slog.Error("Failed to build SSH configuration", "event", "ssh.config_error", "error", err.Error())
		os.Exit(1)
	}

	// Start Server Goroutine
	server := NewGatewayServer(cfg, sshConfig, storageBackend)

	go func() {
		listenAddr := fmt.Sprintf("0.0.0.0:%d", cfg.Server.Port)
		if err := server.Start(listenAddr); err != nil {
			slog.Error("Server failed to start", "event", "server.start_error", "error", err.Error())
			os.Exit(1)
		}
	}()

	// Block on OS Signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("Graceful shutdown initiated...", "event", "server.shutdown_start")

	server.Shutdown()

	if globalDispatcher != nil {
		slog.Info("Draining webhook delivery queue...", "event", "server.shutdown_webhooks")
		globalDispatcher.Shutdown()
	}

	slog.Info("Graceful shutdown complete. Exiting.", "event", "server.shutdown_complete")
}

func handleLogStream(broadcaster *LogBroadcaster) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		clientChan := broadcaster.Subscribe()
		defer broadcaster.Unsubscribe(clientChan)

		for {
			select {
			case <-r.Context().Done():
				return
			case logBytes := <-clientChan:
				fmt.Fprintf(w, "data: %s\n\n", string(logBytes))
				flusher.Flush()
			}
		}
	}
}
