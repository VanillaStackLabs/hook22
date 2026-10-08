package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/VanillaStackLabs/hook22/pkg/config"
	"gopkg.in/yaml.v3"
)

type StorageVaultDTO struct {
	Driver   string `json:"driver"`   // "s3", "gcs", "azure", "disk", "mock"
	Bucket   string `json:"bucket"`   // Bucket or container name
	Region   string `json:"region"`   // AWS region
	Endpoint string `json:"endpoint"` // Custom endpoint (e.g. MinIO or Disk BasePath)
}

// GET /api/v1/vaults
func HandleGetVaults(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dto := StorageVaultDTO{
			Driver: cfg.Storage.Driver,
		}

		switch cfg.Storage.Driver {
		case "s3":
			dto.Bucket = cfg.Storage.S3.Bucket
			dto.Region = cfg.Storage.S3.Region
			dto.Endpoint = cfg.Storage.S3.Endpoint
		case "gcs":
			dto.Bucket = cfg.Storage.GCS.Bucket
		case "azure":
			dto.Bucket = cfg.Storage.Azure.Container
		case "disk":
			dto.Endpoint = cfg.Storage.Disk.BasePath
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(dto)
	}
}

// POST /api/v1/vaults
func HandleUpdateVault(cfg *config.Config, mu *sync.RWMutex, reloadFn func(*config.Config) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload StorageVaultDTO
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
			return
		}

		mu.Lock()
		defer mu.Unlock()

		// Update root driver selection
		cfg.Storage.Driver = payload.Driver

		// Update the active driver's specific settings block
		switch payload.Driver {
		case "s3":
			cfg.Storage.S3.Bucket = payload.Bucket
			cfg.Storage.S3.Region = payload.Region
			cfg.Storage.S3.Endpoint = payload.Endpoint
		case "gcs":
			cfg.Storage.GCS.Bucket = payload.Bucket
		case "azure":
			cfg.Storage.Azure.Container = payload.Bucket
		case "disk":
			cfg.Storage.Disk.BasePath = payload.Endpoint
		}

		// Persist back to config.yaml on disk
		data, err := yaml.Marshal(cfg)
		if err != nil {
			http.Error(w, "Failed to marshal YAML config", http.StatusInternalServerError)
			return
		}

		if err := os.WriteFile("config.yaml", data, 0644); err != nil {
			http.Error(w, "Failed to write config.yaml", http.StatusInternalServerError)
			return
		}

		// Hot-reload active storage provider instance
		if err := reloadFn(cfg); err != nil {
			http.Error(w, fmt.Sprintf("Config saved but driver reload failed: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
	}
}
