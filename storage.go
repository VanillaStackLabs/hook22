package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type StorageProvider interface {
	Upload(ctx context.Context, filepath string, r io.Reader) (hash string, sizeBytes int64, err error)
}

// Mock Storage for local dev testing
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

// Production AWS S3 Provider
type S3Provider struct {
	cfg      *Config
	uploader *manager.Uploader
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

	return &S3Provider{
		cfg:      cfg,
		uploader: manager.NewUploader(client),
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

	_, err := s.uploader.Upload(ctx, &s3.PutObjectInput{
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

type byteCounterWriter struct {
	w            io.Writer
	bytesWritten int64
}

func (b *byteCounterWriter) Write(p []byte) (int, error) {
	n, err := b.w.Write(p)
	b.bytesWritten += int64(n)
	return n, err
}
