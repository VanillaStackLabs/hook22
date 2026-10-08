package gateway

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"

	"github.com/VanillaStackLabs/hook22/pkg/config"
	"github.com/VanillaStackLabs/hook22/pkg/observability"
	"github.com/VanillaStackLabs/hook22/pkg/storage"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type GatewayServer struct {
	cfg       *config.Config
	sshConfig *ssh.ServerConfig
	storage   storage.StorageProvider
	listener  net.Listener
	connWg    sync.WaitGroup
}

func NewGatewayServer(cfg *config.Config, sshCfg *ssh.ServerConfig, storage storage.StorageProvider) *GatewayServer {
	return &GatewayServer{
		cfg:       cfg,
		sshConfig: sshCfg,
		storage:   storage,
	}
}

func (s *GatewayServer) Listener() net.Listener {
	return s.listener
}

func (s *GatewayServer) Start(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.listener = listener
	slog.Info("Hook22 SFTP Gateway started", "event", "server.started", "addr", addr)

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				break // Expected error when shutting down
			}
			slog.Warn("Failed to accept TCP connection", "event", "server.accept_error", "error", err.Error())
			continue
		}

		s.connWg.Add(1)
		go func() {
			defer s.connWg.Done()
			s.handleConnection(conn)
		}()
	}
	return nil
}

func (s *GatewayServer) Shutdown() {
	if s.listener != nil {
		s.listener.Close()
	}
	slog.Info("Waiting for active client connections to finish...", "event", "server.shutdown_connections")
	s.connWg.Wait()
}

func (s *GatewayServer) handleConnection(conn net.Conn) {
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, s.sshConfig)
	if err != nil {
		slog.Warn("SSH handshake failed", "event", "ssh.handshake_failed", "remote_addr", conn.RemoteAddr().String(), "error", err.Error())
		return
	}

	observability.ActiveSSHSessions.Inc()
	defer observability.ActiveSSHSessions.Dec()
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

		go func(ch ssh.Channel, in <-chan *ssh.Request) {
			defer ch.Close()

			for req := range in {
				if req.Type == "subsystem" && string(req.Payload[4:]) == "sftp" {
					req.Reply(true, nil)

					handler := &gatewayHandler{
						storage:  s.storage,
						cfg:      s.cfg,
						username: sshConn.User(),
					}

					handlers := sftp.Handlers{
						FilePut:  handler,
						FileGet:  handler,
						FileCmd:  handler,
						FileList: handler,
					}

					server := sftp.NewRequestServer(ch, handlers)
					if err := server.Serve(); err != nil && err != io.EOF {
						slog.Error("SFTP server error", "event", "sftp.server_error", "username", sshConn.User(), "error", err.Error())
					}
					return
				}
				req.Reply(false, nil)
			}
		}(channel, requests)
	}
}
