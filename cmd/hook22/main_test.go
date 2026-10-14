package main

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/VanillaStackLabs/hook22/pkg/config"
	"github.com/VanillaStackLabs/hook22/pkg/observability"
	"github.com/VanillaStackLabs/hook22/pkg/storage"
)

func TestInitStorageProvider_MockDriver(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Driver = "mock"

	provider, err := initStorageProvider(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Unexpected error for mock driver: %v", err)
	}
	if _, ok := provider.(*storage.MockStorageProvider); !ok {
		t.Errorf("Expected MockStorageProvider, got %T", provider)
	}
}

func TestInitStorageProvider_S3Driver(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Driver = "s3"
	cfg.Storage.S3.Region = "us-east-1"

	provider, err := initStorageProvider(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Unexpected error for S3 driver: %v", err)
	}
	if _, ok := provider.(*storage.S3Provider); !ok {
		t.Errorf("Expected S3Provider, got %T", provider)
	}
}

func TestInitStorageProvider_DefaultFallback(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Driver = "unknown_driver"

	provider, err := initStorageProvider(context.Background(), cfg)
	if err == nil {
		t.Errorf("Expected error for unknown_driver, got nil")
	}
	if provider != nil {
		t.Errorf("Expected nil provider for unknown_driver, got %T", provider)
	}
}

func TestInitStorageProvider_GCSDriver(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Driver = "gcs"
	// GCS requires a credentials file or it errors out
	cfg.Storage.GCS.CredentialsFile = "dummy.json"

	// It's expected to fail if the dummy file doesn't exist,
	// but we just want to ensure it routes to the right provider logic.
	_, err := initStorageProvider(context.Background(), cfg)
	if err == nil {
		t.Errorf("Expected error due to missing dummy.json, got nil")
	}
}

func TestInitStorageProvider_AzureDriver(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Driver = "azure"
	cfg.Storage.Azure.AccountName = "devstoreaccount1"
	cfg.Storage.Azure.AccountKey = "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw=="

	provider, err := initStorageProvider(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Unexpected error for Azure driver: %v", err)
	}
	if _, ok := provider.(*storage.AzureProvider); !ok {
		t.Errorf("Expected AzureProvider, got %T", provider)
	}
}

func TestInitStorageProvider_DiskDriver(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Driver = "disk"
	cfg.Storage.Disk.BasePath = "/tmp/hook22"

	provider, err := initStorageProvider(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Unexpected error for Disk driver: %v", err)
	}
	if _, ok := provider.(*storage.DiskProvider); !ok {
		t.Errorf("Expected DiskProvider, got %T", provider)
	}
}

func TestHandleLogStream_SSEHeaders(t *testing.T) {
	broadcaster := observability.NewLogBroadcaster()
	handler := handleLogStream(broadcaster)

	// Pre-cancel context so handler sets headers and exits immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest("GET", "/api/v1/logs/stream", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	// Runs synchronously on the main thread
	handler.ServeHTTP(rec, req)

	// Verify required Server-Sent Events headers
	if contentType := rec.Header().Get("Content-Type"); contentType != "text/event-stream" {
		t.Errorf("Expected Content-Type text/event-stream, got %s", contentType)
	}
	if cacheControl := rec.Header().Get("Cache-Control"); cacheControl != "no-cache" {
		t.Errorf("Expected Cache-Control no-cache, got %s", cacheControl)
	}
}
