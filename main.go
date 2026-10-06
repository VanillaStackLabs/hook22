package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func initStorageProvider(ctx context.Context, cfg *Config) StorageProvider {
	switch cfg.Storage.Driver {
	case "s3":
		provider, err := NewS3Provider(ctx, cfg)
		if err != nil {
			slog.Error("S3 initialization failed, falling back to mock storage",
				"event", "storage.init_failure",
				"driver", cfg.Storage.Driver,
				"error", err.Error(),
			)
			return &MockStorageProvider{}
		}
		slog.Info("Successfully initialized S3 storage provider", "event", "storage.init_success", "bucket", cfg.Storage.S3.Bucket)
		return provider

	default:
		slog.Info("Using mock in-memory storage provider", "event", "storage.init_mock", "driver", cfg.Storage.Driver)
		return &MockStorageProvider{}
	}
}

func main() {
	broadcaster := NewLogBroadcaster()
	logger := slog.New(slog.NewJSONHandler(broadcaster, nil))
	slog.SetDefault(logger)

	http.HandleFunc("/api/v1/logs/stream", handleLogStream(broadcaster))
	go http.ListenAndServe(":8080", nil)

	cfg, err := loadConfig("config.yaml")
	if err != nil {
		slog.Error("Failed to load config.yaml", "event", "config.error", "error", err.Error())
		os.Exit(1)
	}

	storageBackend := initStorageProvider(context.Background(), cfg)

	sshConfig := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			for _, u := range cfg.Users {
				if c.User() == u.Username && string(pass) == u.Password {
					return nil, nil
				}
			}
			return nil, fmt.Errorf("password rejected for %q", c.User())
		},
	}

	privateBytes, err := os.ReadFile(cfg.Server.HostKeyPath)
	if err != nil {
		slog.Error("Failed to load host key", "event", "ssh.host_key_error", "path", cfg.Server.HostKeyPath, "error", err.Error())
		os.Exit(1)
	}
	private, err := ssh.ParsePrivateKey(privateBytes)
	if err != nil {
		slog.Error("Failed to parse host key", "event", "ssh.parse_key_error", "error", err.Error())
		os.Exit(1)
	}
	sshConfig.AddHostKey(private)

	listenAddr := fmt.Sprintf("0.0.0.0:%d", cfg.Server.Port)
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		slog.Error("Failed to start TCP listener", "event", "server.listen_error", "addr", listenAddr, "error", err.Error())
		os.Exit(1)
	}

	slog.Info("Hook22 SFTP Gateway started", "event", "server.started", "addr", listenAddr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			slog.Warn("Failed to accept TCP connection", "event", "server.accept_error", "error", err.Error())
			continue
		}
		go handleConnection(conn, sshConfig, cfg, storageBackend)
	}
}

func handleConnection(conn net.Conn, sshConfig *ssh.ServerConfig, cfg *Config, storage StorageProvider) {
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, sshConfig)
	if err != nil {
		slog.Warn("SSH handshake failed", "event", "ssh.handshake_failed", "remote_addr", conn.RemoteAddr().String(), "error", err.Error())
		return
	}
	defer sshConn.Close()

	slog.Info("Client authenticated", "event", "ssh.auth_success", "username", sshConn.User(), "remote_addr", conn.RemoteAddr().String())

	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			slog.Warn("Could not accept SSH channel", "event", "ssh.channel_error", "error", err.Error())
			continue
		}

		go func(in <-chan *ssh.Request) {
			for req := range in {
				if req.Type == "subsystem" && string(req.Payload[4:]) == "sftp" {
					req.Reply(true, nil)

					handler := &gatewayHandler{
						storage:  storage,
						cfg:      cfg,
						username: sshConn.User(),
					}

					handlers := sftp.Handlers{
						FilePut:  handler,
						FileGet:  handler,
						FileCmd:  handler,
						FileList: handler,
					}

					server := sftp.NewRequestServer(channel, handlers)
					if err := server.Serve(); err != nil && err != io.EOF {
						slog.Error("SFTP server error", "event", "sftp.server_error", "username", sshConn.User(), "error", err.Error())
					}
					return
				}
				req.Reply(false, nil)
			}
		}(requests)
	}
}

func handleLogStream(broadcaster *LogBroadcaster) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Set headers required for Server-Sent Events
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		clientChan := broadcaster.Subscribe()
		defer broadcaster.Unsubscribe(clientChan)

		// Stream logs until client closes the browser tab
		for {
			select {
			case <-r.Context().Done():
				return
			case logBytes := <-clientChan:
				// SSE protocol format: "data: <JSON>\n\n"
				fmt.Fprintf(w, "data: %s\n\n", string(logBytes))
				flusher.Flush()
			}
		}
	}
}
