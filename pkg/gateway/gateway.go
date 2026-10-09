package gateway

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/VanillaStackLabs/hook22/pkg/config"
	"github.com/VanillaStackLabs/hook22/pkg/observability"
	"github.com/VanillaStackLabs/hook22/pkg/security"
	"github.com/VanillaStackLabs/hook22/pkg/storage"
	"github.com/VanillaStackLabs/hook22/pkg/webhook"
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
	cfg      *config.Config

	mu                 sync.Mutex
	spaceCond          *sync.Cond
	maxPendingChunks   int
	nextExpectedOffset int64
	pendingChunks      map[int64][]byte

	fatalErr           error
}

func (w *s3StreamWriter) WriteAt(p []byte, off int64) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Ensure that NO MATTER HOW we exit, we wake up peers.
	defer w.spaceCond.Broadcast()

	// If another worker already failed the stream, abort immediately.
	if w.fatalErr != nil {
		return 0, w.fatalErr
	}

	for len(w.pendingChunks) >= w.maxPendingChunks && off != w.nextExpectedOffset {
		w.spaceCond.Wait()
		
		// When we wake up, we must check if we were woken up because of an error!
		if w.fatalErr != nil {
			return 0, w.fatalErr
		}
	}

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
			// Save the error before returning so peers know to abort
			w.fatalErr = err 
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
		webhook.TriggerWebhook(w.filepath, w.username, res.hash, res.sizeBytes, w.cfg)
	}
	return res.err
}

type streamReaderAt struct {
	rc  io.ReadCloser
	mu  sync.Mutex
	pos int64
}

func (s *streamReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if off != s.pos {
		return 0, fmt.Errorf("random-access read unsupported on zero-disk stream (expected offset %d, got %d)", s.pos, off)
	}

	n, err = io.ReadFull(s.rc, p)
	if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	s.pos += int64(n)
	return n, err
}

type gatewayHandler struct {
	storage  storage.StorageProvider
	cfg      *config.Config
	username string
}

func (h *gatewayHandler) Filewrite(req *sftp.Request) (io.WriterAt, error) {
	slog.Info("Inbound SFTP upload request", "event", "sftp.upload_start", "filepath", req.Filepath, "username", h.username)

	pipeR, pipeW := io.Pipe()
	doneChan := make(chan uploadResult, 1)

	go func() {
		startTime := time.Now()
		var uploadStream io.Reader = pipeR

		ext := filepath.Ext(req.Filepath)
		isPGPExt := ext == ".gpg" || ext == ".pgp"

		if h.cfg.PGP.Enabled || isPGPExt {
			slog.Info("Wrapping inbound upload stream with PGP decryption", "event", "pgp.decrypt_start", "filepath", req.Filepath)
			decryptedR, err := security.DecryptStreamReader(pipeR, h.cfg.PGP.PrivateKeyPath, h.cfg.PGP.Passphrase)
			if err != nil {
				slog.Error("Failed to initialize PGP decryption stream", "event", "pgp.decrypt_error", "filepath", req.Filepath, "error", err.Error())
				pipeR.CloseWithError(err)
				doneChan <- uploadResult{err: fmt.Errorf("PGP decryption stream error: %w", err)}
				return
			}
			uploadStream = decryptedR
		}

		hash, sizeBytes, err := h.storage.Upload(context.Background(), req.Filepath, uploadStream)

		observability.UploadDuration.Observe(time.Since(startTime).Seconds())
		if err == nil {
			observability.UploadBytesTotal.Add(float64(sizeBytes))
		}

		doneChan <- uploadResult{
			hash:      hash,
			sizeBytes: sizeBytes,
			err:       err,
		}
	}()

	writer := &s3StreamWriter{
		filepath:         req.Filepath,
		username:         h.username,
		pipeW:            pipeW,
		doneChan:         doneChan,
		cfg:              h.cfg,
		pendingChunks:    make(map[int64][]byte),
		maxPendingChunks: 1024, // Strict memory bound limit
	}
	// Bind the condition variable to the existing mutex
	writer.spaceCond = sync.NewCond(&writer.mu)

	return writer, nil
}

func (h *gatewayHandler) Fileread(req *sftp.Request) (io.ReaderAt, error) {
	slog.Info("Outbound SFTP download request", "event", "sftp.download_start", "filepath", req.Filepath, "username", h.username)

	rc, err := h.storage.Download(context.Background(), req.Filepath)
	if err != nil {
		slog.Error("Failed to initiate outbound download stream", "event", "sftp.download_error", "filepath", req.Filepath, "error", err.Error())
		return nil, err
	}

	return &streamReaderAt{rc: rc}, nil
}

// Dummy FileInfo to satisfy SFTP client stat/lstat queries without panicking
type virtualFileInfo struct {
	name  string
	isDir bool
}

func (v *virtualFileInfo) Name() string { return v.name }
func (v *virtualFileInfo) Size() int64  { return 0 }
func (v *virtualFileInfo) Mode() os.FileMode {
	if v.isDir {
		return os.ModeDir | 0755
	}
	return 0644
}
func (v *virtualFileInfo) ModTime() time.Time { return time.Now() }
func (v *virtualFileInfo) IsDir() bool        { return v.isDir }
func (v *virtualFileInfo) Sys() interface{}   { return nil }

func (h *gatewayHandler) Filecmd(req *sftp.Request) error {
	switch req.Method {
	case "Stat", "Lstat":
		// Return virtual stat so clients (WinSCP, OpenSSH SFTP) don't crash when probing destination folders
		return nil
	default:
		return nil
	}
}

func (h *gatewayHandler) Filelist(req *sftp.Request) (sftp.ListerAt, error) {
	switch req.Method {
	case "Stat", "Lstat":
		return listerAt([]os.FileInfo{&virtualFileInfo{name: filepath.Base(req.Filepath), isDir: true}}), nil
	default:
		return listerAt([]os.FileInfo{}), nil
	}
}

type listerAt []os.FileInfo

func (l listerAt) ListAt(f []os.FileInfo, offset int64) (int, error) {
	if offset >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(f, l[offset:])
	if n < len(l[offset:]) {
		return n, nil
	}
	return n, io.EOF
}
