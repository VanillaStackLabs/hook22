package main

import (
	"bytes"
	"context"
	"testing"
)

func TestMockStorageProvider_Upload(t *testing.T) {
	provider := &MockStorageProvider{}
	data := []byte("hello world sftp stream")
	reader := bytes.NewReader(data)

	// SHA-256 of "hello world sftp stream"
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
