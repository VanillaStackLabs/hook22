package main

import (
	"bytes"
	"io"
	"testing"
)

func TestS3StreamWriter_OutOfOrderChunks(t *testing.T) {
	pipeR, pipeW := io.Pipe()
	doneChan := make(chan uploadResult, 1)

	writer := &s3StreamWriter{
		filepath:      "/test.csv",
		username:      "testuser",
		pipeW:         pipeW,
		doneChan:      doneChan,
		pendingChunks: make(map[int64][]byte),
	}

	var readBuffer bytes.Buffer
	readDone := make(chan struct{})

	// Read from the pipe asynchronously
	go func() {
		io.Copy(&readBuffer, pipeR)
		close(readDone)
	}()

	// Simulate SFTP client writing Chunk 2 (offset 10) BEFORE Chunk 1 (offset 0)
	chunk2 := []byte("WORLD_12345") // 11 bytes at offset 10
	chunk1 := []byte("0123456789")  // 10 bytes at offset 0

	n, err := writer.WriteAt(chunk2, 10)
	if err != nil || n != len(chunk2) {
		t.Fatalf("WriteAt chunk2 failed: %v", err)
	}

	// Stream should NOT have emitted chunk2 yet because offset 0 is missing
	if readBuffer.Len() > 0 {
		t.Fatalf("Expected buffer to be empty before offset 0, got %d bytes", readBuffer.Len())
	}

	// Write Chunk 1 (offset 0)
	n, err = writer.WriteAt(chunk1, 0)
	if err != nil || n != len(chunk1) {
		t.Fatalf("WriteAt chunk1 failed: %v", err)
	}

	// Close pipe to flush reader
	pipeW.Close()
	<-readDone

	expected := "0123456789WORLD_12345"
	if readBuffer.String() != expected {
		t.Errorf("Stream reordering failed.\nGot:  %s\nWant: %s", readBuffer.String(), expected)
	}
}

func TestStreamReaderAt_SequentialReads(t *testing.T) {
	data := []byte("0123456789abcdefghij")
	rc := io.NopCloser(bytes.NewReader(data))
	sra := &streamReaderAt{rc: rc}

	buf1 := make([]byte, 10)
	n, err := sra.ReadAt(buf1, 0)
	if err != nil || n != 10 {
		t.Fatalf("ReadAt chunk 1 failed: %v (n=%d)", err, n)
	}
	if string(buf1) != "0123456789" {
		t.Errorf("Got %s, want 0123456789", string(buf1))
	}

	buf2 := make([]byte, 10)
	n, err = sra.ReadAt(buf2, 10)
	if err != nil || n != 10 {
		t.Fatalf("ReadAt chunk 2 failed: %v (n=%d)", err, n)
	}
	if string(buf2) != "abcdefghij" {
		t.Errorf("Got %s, want abcdefghij", string(buf2))
	}
}

func TestStreamReaderAt_NonSequentialReadFails(t *testing.T) {
	data := []byte("0123456789")
	rc := io.NopCloser(bytes.NewReader(data))
	sra := &streamReaderAt{rc: rc}

	buf := make([]byte, 5)
	_, err := sra.ReadAt(buf, 5) // Expected offset 0, passed offset 5
	if err == nil {
		t.Error("Expected error on non-sequential offset read, got nil")
	}
}
