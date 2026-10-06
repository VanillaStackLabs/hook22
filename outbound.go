package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type OutboundPushRequest struct {
	RemoteHost string `json:"remote_host"` // e.g. "sftp.partner.com:22"
	Username   string `json:"username"`
	Password   string `json:"password"`
	SourceKey  string `json:"source_key"`
	TargetPath string `json:"target_remote_path"`
}

func handleOutboundPush(storage StorageProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req OutboundPushRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		go executeOutboundPush(req, storage)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "queued",
			"target":  req.TargetPath,
			"partner": req.RemoteHost,
		})
	}
}

func executeOutboundPush(req OutboundPushRequest, storage StorageProvider) {
	slog.Info("Executing outbound SFTP push", "event", "sftp.outbound_start", "remote_host", req.RemoteHost, "source_key", req.SourceKey)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// 1. Obtain read stream from storage provider
	srcStream, err := storage.Download(ctx, req.SourceKey)
	if err != nil {
		slog.Error("Outbound push failed: unable to fetch source file", "event", "sftp.outbound_source_error", "error", err.Error())
		return
	}
	defer srcStream.Close()

	// 2. Dial remote SSH server
	sshConfig := &ssh.ClientConfig{
		User:            req.Username,
		Auth:            []ssh.AuthMethod{ssh.Password(req.Password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	conn, err := ssh.Dial("tcp", req.RemoteHost, sshConfig)
	if err != nil {
		slog.Error("Outbound push failed: SSH dial error", "event", "sftp.outbound_dial_error", "error", err.Error())
		return
	}
	defer conn.Close()

	client, err := sftp.NewClient(conn)
	if err != nil {
		slog.Error("Outbound push failed: SFTP client init error", "event", "sftp.outbound_client_error", "error", err.Error())
		return
	}
	defer client.Close()

	// 3. Open remote file
	dstFile, err := client.Create(req.TargetPath)
	if err != nil {
		slog.Error("Outbound push failed: target file creation error", "event", "sftp.outbound_create_error", "error", err.Error())
		return
	}
	defer dstFile.Close()

	// 4. Stream directly from storage into remote SFTP file without saving to local disk
	written, err := io.Copy(dstFile, srcStream)
	if err != nil {
		slog.Error("Outbound push failed: streaming copy error", "event", "sftp.outbound_stream_error", "error", err.Error())
		return
	}

	slog.Info("Outbound SFTP push completed successfully", "event", "sftp.outbound_success", "remote_host", req.RemoteHost, "bytes_written", written)
}
