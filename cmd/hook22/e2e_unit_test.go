package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/VanillaStackLabs/hook22/pkg/auth"
	"github.com/VanillaStackLabs/hook22/pkg/config"
	"github.com/VanillaStackLabs/hook22/pkg/gateway"
	"github.com/VanillaStackLabs/hook22/pkg/storage"
	"github.com/VanillaStackLabs/hook22/pkg/webhook"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func TestE2E_FullPipeline(t *testing.T) {
	secret := "whsec_e2e_secret_999"
	webhookChan := make(chan webhook.WebhookPayload, 1)
	sigChan := make(chan string, 1)

	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sigChan <- r.Header.Get("Hook22-Signature")
		body, _ := io.ReadAll(r.Body)
		var payload webhook.WebhookPayload
		_ = json.Unmarshal(body, &payload)
		webhookChan <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookServer.Close()

	tmpDir := t.TempDir()
	hostKeyPath := filepath.Join(tmpDir, "host_rsa")

	// Uses the exported helper from pkg/auth/test_helpers.go
	auth.GenerateTestHostKey(t, hostKeyPath)

	cfg := &config.Config{}
	cfg.Webhook.URL = webhookServer.URL
	cfg.Webhook.Secret = secret
	cfg.Webhook.Workers = 2
	cfg.Webhook.MaxRetries = 3
	cfg.Webhook.BaseBackoff = 1

	webhook.InitWebhookDispatcher(cfg)
	storageBackend := &storage.MockStorageProvider{}

	sshConfig := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "e2e_user" && string(pass) == "e2e_password" {
				return nil, nil
			}
			return nil, ssh.ErrNoAuth
		},
	}
	privateBytes, err := os.ReadFile(hostKeyPath)
	if err != nil {
		t.Fatalf("Failed to read host key: %v", err)
	}
	private, err := ssh.ParsePrivateKey(privateBytes)
	if err != nil {
		t.Fatalf("Failed to parse host key: %v", err)
	}
	sshConfig.AddHostKey(private)

	server := gateway.NewGatewayServer(cfg, sshConfig, storageBackend)
	go func() {
		_ = server.Start("127.0.0.1:0")
	}()

	time.Sleep(100 * time.Millisecond)
	serverAddr := server.Listener().Addr().String()
	defer server.Shutdown()

	clientConfig := &ssh.ClientConfig{
		User: "e2e_user",
		Auth: []ssh.AuthMethod{
			ssh.Password("e2e_password"),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	sshClient, err := ssh.Dial("tcp", serverAddr, clientConfig)
	if err != nil {
		t.Fatalf("SFTP SSH connection failed: %v", err)
	}
	defer sshClient.Close()

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		t.Fatalf("SFTP client initialization failed: %v", err)
	}
	defer sftpClient.Close()

	remotePath := "/e2e_test_file.csv"
	testContent := []byte("header1,header2\nvalue1,value2\n")

	f, err := sftpClient.Create(remotePath)
	if err != nil {
		t.Fatalf("SFTP file create failed: %v", err)
	}

	if _, err := f.Write(testContent); err != nil {
		t.Fatalf("SFTP file write failed: %v", err)
	}

	if err := f.Close(); err != nil {
		t.Fatalf("SFTP file close failed: %v", err)
	}

	select {
	case payload := <-webhookChan:
		sig := <-sigChan

		if payload.Filepath != remotePath {
			t.Errorf("Expected filepath %s, got %s", remotePath, payload.Filepath)
		}
		if payload.Username != "e2e_user" {
			t.Errorf("Expected username e2e_user, got %s", payload.Username)
		}
		if payload.SizeBytes != int64(len(testContent)) {
			t.Errorf("Expected size %d, got %d", len(testContent), payload.SizeBytes)
		}

		hasher := sha256.New()
		hasher.Write(testContent)
		expectedContentHash := hex.EncodeToString(hasher.Sum(nil))
		if payload.SHA256 != expectedContentHash {
			t.Errorf("Expected SHA256 %s, got %s", expectedContentHash, payload.SHA256)
		}

		if sig == "" {
			t.Error("Expected Hook22-Signature header, got empty string")
		}

	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for webhook dispatch after SFTP upload")
	}
}
