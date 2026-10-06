package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func TestE2E_FullPipeline(t *testing.T) {
	secret := "whsec_e2e_secret_999"
	webhookChan := make(chan WebhookPayload, 1)
	sigChan := make(chan string, 1)

	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sigChan <- r.Header.Get("Hook22-Signature")
		body, _ := io.ReadAll(r.Body)
		var payload WebhookPayload
		_ = json.Unmarshal(body, &payload)
		webhookChan <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookServer.Close()

	tmpDir := t.TempDir()
	hostKeyPath := filepath.Join(tmpDir, "host_rsa")
	generateTestHostKey(t, hostKeyPath)

	cfg := &Config{
		Webhook: struct {
			URL         string `yaml:"url"`
			Secret      string `yaml:"secret"`
			Workers     int    `yaml:"workers"`
			MaxRetries  int    `yaml:"max_retries"`
			BaseBackoff int    `yaml:"base_backoff"`
		}{
			URL:         webhookServer.URL,
			Secret:      secret,
			Workers:     2,
			MaxRetries:  3,
			BaseBackoff: 1,
		},
	}

	// Initialize the worker pool for async test event processing
	InitWebhookDispatcher(cfg)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to bind TCP listener: %v", err)
	}
	defer listener.Close()

	serverAddr := listener.Addr().String()
	storageBackend := &MockStorageProvider{}

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

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleConnection(conn, sshConfig, cfg, storageBackend)
		}
	}()

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

func generateTestHostKey(t *testing.T, path string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate RSA key: %v", err)
	}
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	if err := os.WriteFile(path, privateKeyPEM, 0600); err != nil {
		t.Fatalf("Failed to write host key file: %v", err)
	}
}
