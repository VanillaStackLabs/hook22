package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net"
	"testing"
	"time"

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

	cfg := &Config{}
	storage := &MockStorageProvider{}

	server := NewGatewayServer(cfg, sshConfig, storage)

	serverErrChan := make(chan error, 1)

	go func() {
		serverErrChan <- server.Start("127.0.0.1:0")
	}()

	// Polling
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
			t.Errorf("Unexpected error from server.Start(): %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for server goroutine to exit")
	}
}
