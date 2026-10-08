package storage

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/VanillaStackLabs/hook22/pkg/config"
)

func TestMockStorageProvider_UploadAndDownload(t *testing.T) {
	provider := &MockStorageProvider{}
	data := []byte("hello world sftp stream")
	reader := bytes.NewReader(data)

	expectedHash := "06150febbe4805e0fe816098df1c0ab6cd3cf9f4a2d617f55c8c4722f5386d10"
	expectedSize := int64(len(data))

	hash, size, err := provider.Upload(context.Background(), "/drops/test.txt", reader)
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	if size != expectedSize {
		t.Errorf("Expected size %d, got %d", expectedSize, size)
	}
	if hash != expectedHash {
		t.Errorf("Expected hash %s, got %s", expectedHash, hash)
	}

	rc, err := provider.Download(context.Background(), "/drops/test.txt")
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	defer rc.Close()

	dlData, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("Failed to read downloaded content: %v", err)
	}
	if len(dlData) != 0 {
		t.Errorf("Expected empty mock download body, got %d bytes", len(dlData))
	}
}

func TestDiskStorageProvider_UploadAndDownload(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{}
	cfg.Storage.Disk.BasePath = tmpDir

	provider := NewDiskProvider(cfg)
	data := []byte("disk storage test data")
	reader := bytes.NewReader(data)

	expectedSize := int64(len(data))
	expectedHash := "65464c2fa948a6af0b6b8308445f65354c78c3521cdc646dcee81cb4ace25b19"

	hash, size, err := provider.Upload(context.Background(), "/out/test.txt", reader)
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	if size != expectedSize {
		t.Errorf("Expected size %d, got %d", expectedSize, size)
	}
	if hash != expectedHash {
		t.Errorf("Expected hash %s, got %s", expectedHash, hash)
	}

	fullPath := filepath.Join(tmpDir, "/out/test.txt")
	savedData, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("Failed to read saved file: %v", err)
	}
	if !bytes.Equal(savedData, data) {
		t.Errorf("Saved data mismatch. Got %s", string(savedData))
	}

	rc, err := provider.Download(context.Background(), "/out/test.txt")
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	defer rc.Close()

	downloadedData, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("Failed to read stream: %v", err)
	}
	if !bytes.Equal(downloadedData, data) {
		t.Errorf("Downloaded data mismatch. Got %s", string(downloadedData))
	}
}

func TestByteCounterWriter(t *testing.T) {
	var buf bytes.Buffer
	counter := &byteCounterWriter{w: &buf}

	p1 := []byte("part1_")
	p2 := []byte("part2_data")

	n1, err1 := counter.Write(p1)
	if err1 != nil || n1 != len(p1) {
		t.Fatalf("Write p1 failed: %v", err1)
	}

	n2, err2 := counter.Write(p2)
	if err2 != nil || n2 != len(p2) {
		t.Fatalf("Write p2 failed: %v", err2)
	}

	expectedTotal := int64(len(p1) + len(p2))
	if counter.bytesWritten != expectedTotal {
		t.Errorf("Expected total bytes %d, got %d", expectedTotal, counter.bytesWritten)
	}
	if buf.String() != "part1_part2_data" {
		t.Errorf("Buffer output mismatch: %s", buf.String())
	}
}
