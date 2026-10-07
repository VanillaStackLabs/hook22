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

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to bind ephemeral listener: %v", err)
	}

	server.mu.Lock()
	server.listener = listener
	server.mu.Unlock()

	serverErrChan := make(chan error, 1)

	go func() {
		serverErrChan <- server.Serve(listener)
	}()

	time.Sleep(50 * time.Millisecond)

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
