package main

import (
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestGatewayServer_StartAndShutdown(t *testing.T) {
	// Create dummy configurations
	cfg := &Config{}
	sshCfg := &ssh.ServerConfig{}
	storage := &MockStorageProvider{}

	server := NewGatewayServer(cfg, sshCfg, storage)

	// Start the server on an ephemeral port in a goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Start("127.0.0.1:0")
	}()

	// Wait a fraction of a second to ensure the listener bound
	time.Sleep(50 * time.Millisecond)

	if server.listener == nil {
		t.Fatal("Expected server listener to be initialized")
	}

	// Verify we can connect to the bound port
	addr := server.listener.Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to connect to running server: %v", err)
	}
	conn.Close()

	// Trigger Graceful Shutdown
	shutdownDone := make(chan struct{})
	go func() {
		server.Shutdown()
		close(shutdownDone)
	}()

	// Ensure shutdown completes within a reasonable timeout
	select {
	case <-shutdownDone:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Server shutdown timed out")
	}

	// Verify the listener is closed by trying to connect again
	_, err = net.Dial("tcp", addr)
	if err == nil {
		t.Fatal("Expected connection to fail after server shutdown")
	}
}

func TestGatewayServer_StartFailsOnBadPort(t *testing.T) {
	server := NewGatewayServer(&Config{}, &ssh.ServerConfig{}, &MockStorageProvider{})

	// Trying to bind to an invalid port should return an error immediately
	err := server.Start("127.0.0.1:9999999")
	if err == nil {
		t.Fatal("Expected error when starting server on invalid port")
	}
}
