package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"cloud.google.com/go/storage"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"google.golang.org/api/option"
)

type StorageProvider interface {
	Upload(ctx context.Context, filepath string, r io.Reader) (hash string, sizeBytes int64, err error)
}

type MockStorageProvider struct{}

func (m *MockStorageProvider) Upload(ctx context.Context, filepath string, r io.Reader) (string, int64, error) {
	slog.Info("Streaming bytes to mock memory storage", "event", "storage.mock_start", "filepath", filepath)

	hasher := sha256.New()
	written, err := io.Copy(hasher, r)
	if err != nil {
		return "", 0, err
	}
	hashStr := hex.EncodeToString(hasher.Sum(nil))

	slog.Info("Mock upload complete", "event", "storage.mock_complete", "filepath", filepath, "size_bytes", written, "sha256", hashStr)
	return hashStr, written, nil
}

type S3Provider struct {
	cfg      *Config
	uploader *transfermanager.Client
}

func NewS3Provider(ctx context.Context, cfg *Config) (*S3Provider, error) {
	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(cfg.Storage.S3.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.Storage.S3.AccessKey,
			cfg.Storage.S3.SecretKey,
			"",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Storage.S3.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Storage.S3.Endpoint)
			o.UsePathStyle = true
		}
	})

	uploader := transfermanager.New(client, func(o *transfermanager.Options) {
		o.PartSizeBytes = 5 * 1024 * 1024
		o.Concurrency = 3
	})

	return &S3Provider{
		cfg:      cfg,
		uploader: uploader,
	}, nil
}

func (s *S3Provider) Upload(ctx context.Context, filepath string, r io.Reader) (string, int64, error) {
	s3Key := filepath
	if len(s3Key) > 0 && s3Key[0] == '/' {
		s3Key = s3Key[1:]
	}

	hasher := sha256.New()
	cw := &byteCounterWriter{w: hasher}
	tr := io.TeeReader(r, cw)

	_, err := s.uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket: aws.String(s.cfg.Storage.S3.Bucket),
		Key:    aws.String(s3Key),
		Body:   tr,
	})
	if err != nil {
		return "", 0, fmt.Errorf("S3 upload failed: %w", err)
	}

	hashStr := hex.EncodeToString(hasher.Sum(nil))
	return hashStr, cw.bytesWritten, nil
}

type GCSProvider struct {
	cfg    *Config
	client *storage.Client
}

func NewGCSProvider(ctx context.Context, cfg *Config) (*GCSProvider, error) {
	client, err := storage.NewClient(ctx, option.WithCredentialsFile(cfg.Storage.GCS.CredentialsFile))
	if err != nil {
		return nil, fmt.Errorf("failed to create GCS client: %w", err)
	}
	return &GCSProvider{cfg: cfg, client: client}, nil
}

func (g *GCSProvider) Upload(ctx context.Context, filepath string, r io.Reader) (string, int64, error) {
	gcsKey := filepath
	if len(gcsKey) > 0 && gcsKey[0] == '/' {
		gcsKey = gcsKey[1:]
	}

	hasher := sha256.New()
	cw := &byteCounterWriter{w: hasher}
	tr := io.TeeReader(r, cw)

	bucket := g.client.Bucket(g.cfg.Storage.GCS.Bucket)
	obj := bucket.Object(gcsKey)
	writer := obj.NewWriter(ctx)

	if _, err := io.Copy(writer, tr); err != nil {
		writer.Close()
		return "", 0, fmt.Errorf("GCS upload failed: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", 0, fmt.Errorf("GCS close failed: %w", err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), cw.bytesWritten, nil
}

type AzureProvider struct {
	cfg    *Config
	client *azblob.Client
}

func NewAzureProvider(cfg *Config) (*AzureProvider, error) {
	cred, err := azblob.NewSharedKeyCredential(cfg.Storage.Azure.AccountName, cfg.Storage.Azure.AccountKey)
	if err != nil {
		return nil, fmt.Errorf("invalid Azure credentials: %w", err)
	}
	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", cfg.Storage.Azure.AccountName)
	client, err := azblob.NewClientWithSharedKeyCredential(serviceURL, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure client: %w", err)
	}
	return &AzureProvider{cfg: cfg, client: client}, nil
}

func (a *AzureProvider) Upload(ctx context.Context, filepath string, r io.Reader) (string, int64, error) {
	blobName := filepath
	if len(blobName) > 0 && blobName[0] == '/' {
		blobName = blobName[1:]
	}

	hasher := sha256.New()
	cw := &byteCounterWriter{w: hasher}
	tr := io.TeeReader(r, cw)

	_, err := a.client.UploadStream(ctx, a.cfg.Storage.Azure.Container, blobName, tr, nil)
	if err != nil {
		return "", 0, fmt.Errorf("Azure upload failed: %w", err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), cw.bytesWritten, nil
}

type DiskProvider struct {
	cfg *Config
}

func NewDiskProvider(cfg *Config) *DiskProvider {
	return &DiskProvider{cfg: cfg}
}

func (d *DiskProvider) Upload(ctx context.Context, reqPath string, r io.Reader) (string, int64, error) {
	if d.cfg.Storage.Disk.BasePath == "" {
		return "", 0, fmt.Errorf("disk base_path is not configured")
	}

	fullPath := filepath.Join(d.cfg.Storage.Disk.BasePath, reqPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return "", 0, fmt.Errorf("failed to create directory structure: %w", err)
	}

	file, err := os.Create(fullPath)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create file on disk: %w", err)
	}
	defer file.Close()

	hasher := sha256.New()
	cw := &byteCounterWriter{w: hasher}

	mw := io.MultiWriter(file, cw)

	if _, err := io.Copy(mw, r); err != nil {
		return "", 0, fmt.Errorf("disk write failed: %w", err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), cw.bytesWritten, nil
}

type byteCounterWriter struct {
	w            io.Writer
	bytesWritten int64
}

func (b *byteCounterWriter) Write(p []byte) (int, error) {
	n, err := b.w.Write(p)
	b.bytesWritten += int64(n)
	return n, err
}
