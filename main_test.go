package main

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestInitStorageProvider_MockDriver(t *testing.T) {
	cfg := &Config{}
	cfg.Storage.Driver = "mock"

	provider := initStorageProvider(context.Background(), cfg)
	if _, ok := provider.(*MockStorageProvider); !ok {
		t.Errorf("Expected MockStorageProvider, got %T", provider)
	}
}

func TestInitStorageProvider_S3Driver(t *testing.T) {
	cfg := &Config{}
	cfg.Storage.Driver = "s3"
	cfg.Storage.S3.Region = "us-east-1"

	provider := initStorageProvider(context.Background(), cfg)
	if _, ok := provider.(*S3Provider); !ok {
		t.Errorf("Expected S3Provider, got %T", provider)
	}
}

func TestInitStorageProvider_DefaultFallback(t *testing.T) {
	cfg := &Config{}
	cfg.Storage.Driver = "unknown_driver"

	provider := initStorageProvider(context.Background(), cfg)
	if _, ok := provider.(*MockStorageProvider); !ok {
		t.Errorf("Expected default fallback to MockStorageProvider, got %T", provider)
	}
}

func TestHandleLogStream_SSEHeaders(t *testing.T) {
	broadcaster := NewLogBroadcaster()
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
