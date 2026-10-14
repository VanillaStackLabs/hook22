package gateway

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net"
	"testing"
	"time"

	"github.com/VanillaStackLabs/hook22/pkg/config"
	"github.com/VanillaStackLabs/hook22/pkg/storage"
	"golang.org/x/crypto/ssh"
)

func TestGatewayServer_StartAndShutdown(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate test RSA key: %v", err)
	}
	pemBlock := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	signer, err := ssh.ParsePrivateKey(pemBlock)
	if err != nil {
		t.Fatalf("Failed to parse private key: %v", err)
	}

	sshConfig := &ssh.ServerConfig{}
	sshConfig.AddHostKey(signer)

	cfg := &config.Config{}
	storage := &storage.MockStorageProvider{}

	server := NewGatewayServer(cfg, sshConfig, storage)

	serverErrChan := make(chan error, 1)

	go func() {
		serverErrChan <- server.Start("127.0.0.1:0")
	}()

	for i := 0; i < 50; i++ {
		time.Sleep(10 * time.Millisecond)
		if server.listener != nil {
			break
		}
	}

	if server.listener == nil {
		t.Fatalf("Server listener failed to bind within timeout")
	}

	server.Shutdown()

	select {
	case err := <-serverErrChan:
		if err != nil && err != net.ErrClosed {
			t.Errorf("Unexpected error during shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for server to shut down")
	}
}

func TestGatewayServer_SetAndGetStorage(t *testing.T) {
	cfg := &config.Config{}
	initialStorage := &storage.MockStorageProvider{}
	server := NewGatewayServer(cfg, &ssh.ServerConfig{}, initialStorage)

	if server.GetStorage() != initialStorage {
		t.Errorf("Expected initial storage provider to match")
	}

	diskStorage := storage.NewDiskProvider(cfg)
	server.SetStorage(diskStorage)

	if server.GetStorage() != diskStorage {
		t.Errorf("Expected updated storage provider to match disk provider")
	}
}

func TestGatewayServer_RejectsNonSFTPChannelsAndCommands(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate test RSA key: %v", err)
	}
	pemBlock := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	signer, err := ssh.ParsePrivateKey(pemBlock)
	if err != nil {
		t.Fatalf("Failed to parse private key: %v", err)
	}

	sshConfig := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			return nil, nil // Accept all logins for this unit test
		},
	}
	sshConfig.AddHostKey(signer)

	cfg := &config.Config{}
	server := NewGatewayServer(cfg, sshConfig, &storage.MockStorageProvider{})

	go func() {
		_ = server.Start("127.0.0.1:0")
	}()

	for i := 0; i < 50; i++ {
		time.Sleep(10 * time.Millisecond)
		if server.listener != nil {
			break
		}
	}
	defer server.Shutdown()

	clientConfig := &ssh.ClientConfig{
		User:            "test_user",
		Auth:            []ssh.AuthMethod{ssh.Password("any")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         2 * time.Second,
	}

	client, err := ssh.Dial("tcp", server.listener.Addr().String(), clientConfig)
	if err != nil {
		t.Fatalf("SSH dial failed: %v", err)
	}
	defer client.Close()

	// Verify rejection of non-session channel requests
	_, _, err = client.OpenChannel("direct-tcpip", nil)
	if err == nil {
		t.Error("Expected server to reject direct-tcpip channel, but it was accepted")
	}

	// Open a legit session channel
	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("Failed to open valid session channel: %v", err)
	}
	defer session.Close()

	// Verify rejection of shell / exec execution (non-SFTP subsystems)
	err = session.Run("echo 'hacked'")
	if err == nil {
		t.Error("Expected server to reject arbitrary exec commands, but it succeeded")
	}
}
