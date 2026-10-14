package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/VanillaStackLabs/hook22/pkg/config"
)

func TestHandleGetVaults(t *testing.T) {
	tests := []struct {
		name     string
		setupCfg func(cfg *config.Config)
		expected StorageVaultDTO
	}{
		{
			name: "S3 Driver Mapping",
			setupCfg: func(cfg *config.Config) {
				cfg.Storage.Driver = "s3"
				cfg.Storage.S3.Bucket = "s3-test-bucket"
				cfg.Storage.S3.Region = "us-east-1"
				cfg.Storage.S3.Endpoint = "https://s3.custom.net"
			},
			expected: StorageVaultDTO{
				Driver:   "s3",
				Bucket:   "s3-test-bucket",
				Region:   "us-east-1",
				Endpoint: "https://s3.custom.net",
			},
		},
		{
			name: "GCS Driver Mapping",
			setupCfg: func(cfg *config.Config) {
				cfg.Storage.Driver = "gcs"
				cfg.Storage.GCS.Bucket = "gcs-test-bucket"
			},
			expected: StorageVaultDTO{
				Driver: "gcs",
				Bucket: "gcs-test-bucket",
			},
		},
		{
			name: "Azure Driver Mapping",
			setupCfg: func(cfg *config.Config) {
				cfg.Storage.Driver = "azure"
				cfg.Storage.Azure.Container = "azure-test-container"
			},
			expected: StorageVaultDTO{
				Driver: "azure",
				Bucket: "azure-test-container",
			},
		},
		{
			name: "Disk Driver Mapping",
			setupCfg: func(cfg *config.Config) {
				cfg.Storage.Driver = "disk"
				cfg.Storage.Disk.BasePath = "/opt/hook22/storage"
			},
			expected: StorageVaultDTO{
				Driver:   "disk",
				Endpoint: "/opt/hook22/storage",
			},
		},
		{
			name: "Mock Driver Mapping (Default Fallback)",
			setupCfg: func(cfg *config.Config) {
				cfg.Storage.Driver = "mock"
			},
			expected: StorageVaultDTO{
				Driver: "mock",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			tt.setupCfg(cfg)

			handler := HandleGetVaults(cfg)
			req := httptest.NewRequest("GET", "/api/v1/vaults", nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("Expected status code 200 OK, got %d", rec.Code)
			}

			if contentType := rec.Header().Get("Content-Type"); contentType != "application/json" {
				t.Errorf("Expected Content-Type application/json, got %s", contentType)
			}

			var responseDTO StorageVaultDTO
			if err := json.NewDecoder(rec.Body).Decode(&responseDTO); err != nil {
				t.Fatalf("Failed to decode JSON response: %v", err)
			}

			if responseDTO != tt.expected {
				t.Errorf("Expected %+v, got %+v", tt.expected, responseDTO)
			}
		})
	}
}
