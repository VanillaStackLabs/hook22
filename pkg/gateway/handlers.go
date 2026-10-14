package gateway

import (
	"encoding/json"
	"net/http"

	"github.com/VanillaStackLabs/hook22/pkg/config"
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
