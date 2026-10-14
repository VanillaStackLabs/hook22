package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/VanillaStackLabs/hook22/pkg/auth"
	"github.com/VanillaStackLabs/hook22/pkg/config"
	"github.com/VanillaStackLabs/hook22/pkg/gateway"
	"github.com/VanillaStackLabs/hook22/pkg/observability"
	"github.com/VanillaStackLabs/hook22/pkg/storage"
	"github.com/VanillaStackLabs/hook22/pkg/webhook"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/crypto/bcrypt"
)

func initStorageProvider(ctx context.Context, cfg *config.Config) (storage.StorageProvider, error) {
	switch cfg.Storage.Driver {
	case "s3":
		return storage.NewS3Provider(ctx, cfg)
	case "gcs":
		return storage.NewGCSProvider(ctx, cfg)
	case "azure":
		return storage.NewAzureProvider(cfg)
	case "disk":
		return storage.NewDiskProvider(cfg), nil
	case "mock":
		slog.Warn("Using mock in-memory storage provider. DATA WILL BE DISCARDED.", "event", "storage.mock_warning")
		return &storage.MockStorageProvider{}, nil
	default:
		return nil, fmt.Errorf("unknown storage driver specified in config: %s", cfg.Storage.Driver)
	}
}

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "hash" {
		hash, err := bcrypt.GenerateFromPassword([]byte(os.Args[2]), bcrypt.DefaultCost)
		if err != nil {
			fmt.Printf("Error generating hash: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Bcrypt hash for config.yaml:\n%s\n", string(hash))
		os.Exit(0)
	}
	
	broadcaster := observability.NewLogBroadcaster()
	logger := slog.New(slog.NewJSONHandler(broadcaster, nil))
	slog.SetDefault(logger)

	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		slog.Error("Failed to load config.yaml", "event", "config.error", "error", err.Error())
		os.Exit(1)
	}

	// Require sessionsecret
	if cfg.Server.SessionSecret == "" {
		slog.Error("CRITICAL: server.session_secret is empty in config.yaml. Refusing to start insecurely.")
		os.Exit(1)
	}

	webhook.InitWebhookDispatcher(cfg)
	storageBackend, err := initStorageProvider(context.Background(), cfg)
	if err != nil {
		slog.Error("Storage initialization failed", "error", err.Error())
		os.Exit(1)
	}

	// SSH Config
	sshConfig, err := auth.BuildSSHConfig(cfg, nil)
	if err != nil {
		slog.Error("Failed to build SSH configuration", "event", "ssh.config_error", "error", err.Error())
		os.Exit(1)
	}

	// Start Gateway Server
	server := gateway.NewGatewayServer(cfg, sshConfig, storageBackend)

	// Clean Stateless HTTP Routes
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/api/v1/login", auth.HandleLogin(cfg, nil))
	http.HandleFunc("/api/v1/logs/stream", auth.RequireCookieAuth(cfg.Server.SessionSecret, handleLogStream(broadcaster)))
	http.HandleFunc("/api/v1/sftp/push", auth.RequireCookieAuth(cfg.Server.SessionSecret, gateway.HandleOutboundPush(storageBackend)))

	// Read-Only Vault Configuration Endpoint
	http.HandleFunc("/api/v1/vaults", auth.RequireCookieAuth(cfg.Server.SessionSecret, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gateway.HandleGetVaults(cfg)(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}))

	go http.ListenAndServe(":8080", nil)

	go func() {
		listenAddr := fmt.Sprintf("0.0.0.0:%d", cfg.Server.Port)
		if err := server.Start(listenAddr); err != nil {
			slog.Error("Server failed to start", "event", "server.start_error", "error", err.Error())
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("Graceful shutdown initiated...", "event", "server.shutdown_start")
	server.Shutdown()
	slog.Info("Graceful shutdown complete. Exiting.", "event", "server.shutdown_complete")
}

func handleLogStream(broadcaster *observability.LogBroadcaster) http.HandlerFunc {
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
			case logBytes, ok := <-clientChan:
				if !ok {
					return // Prevents 100% CPU infinite loop when channel is closed
				}
				fmt.Fprintf(w, "data: %s\n\n", string(logBytes))
				flusher.Flush()
			}
		}
	}
}
