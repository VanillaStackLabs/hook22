package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/pkg/sftp"
)

type uploadResult struct {
	hash      string
	sizeBytes int64
	err       error
}

type s3StreamWriter struct {
	filepath string
	username string
	pipeW    *io.PipeWriter
	doneChan chan uploadResult
	cfg      *Config

	mu                 sync.Mutex
	nextExpectedOffset int64
	pendingChunks      map[int64][]byte
}

func (w *s3StreamWriter) WriteAt(p []byte, off int64) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	chunkCopy := make([]byte, len(p))
	copy(chunkCopy, p)

	w.pendingChunks[off] = chunkCopy

	for {
		chunk, exists := w.pendingChunks[w.nextExpectedOffset]
		if !exists {
			break
		}

		_, err := w.pipeW.Write(chunk)
		if err != nil {
			return 0, err
		}

		delete(w.pendingChunks, w.nextExpectedOffset)
		w.nextExpectedOffset += int64(len(chunk))
	}

	return len(p), nil
}

func (w *s3StreamWriter) Close() error {
	slog.Info("SFTP handle closed by client", "event", "sftp.close", "filepath", w.filepath, "username", w.username)
	w.pipeW.Close()

	res := <-w.doneChan
	if res.err == nil {
		triggerWebhook(w.filepath, w.username, res.hash, res.sizeBytes, w.cfg)
	}
	return res.err
}

type gatewayHandler struct {
	storage  StorageProvider
	cfg      *Config
	username string
}

func (h *gatewayHandler) Filewrite(req *sftp.Request) (io.WriterAt, error) {
	slog.Info("Inbound SFTP upload request", "event", "sftp.upload_start", "filepath", req.Filepath, "username", h.username)

	pipeR, pipeW := io.Pipe()
	doneChan := make(chan uploadResult, 1)

	go func() {
		hash, sizeBytes, err := h.storage.Upload(context.Background(), req.Filepath, pipeR)
		doneChan <- uploadResult{
			hash:      hash,
			sizeBytes: sizeBytes,
			err:       err,
		}
	}()

	return &s3StreamWriter{
		filepath:      req.Filepath,
		username:      h.username,
		pipeW:         pipeW,
		doneChan:      doneChan,
		cfg:           h.cfg,
		pendingChunks: make(map[int64][]byte),
	}, nil
}

func (h *gatewayHandler) Fileread(req *sftp.Request) (io.ReaderAt, error) {
	return nil, fmt.Errorf("downloads are disabled on this gateway")
}
func (h *gatewayHandler) Filecmd(req *sftp.Request) error                   { return nil }
func (h *gatewayHandler) Filelist(req *sftp.Request) (sftp.ListerAt, error) { return nil, nil }
