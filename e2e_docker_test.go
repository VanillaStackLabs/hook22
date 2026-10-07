//go:build e2e

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func TestE2E_DockerComposeStack(t *testing.T) {
	sftpAddr := "127.0.0.1:2222"
	echoLogsURL := "http://127.0.0.1:3000/logs"

	testData := []byte("E2E Blackbox Test Content")
	remotePath := "/drops/blackbox_test.csv"

	hasher := sha256.New()
	hasher.Write(testData)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))

	// Dial Dockerized SFTP Server
	sshConfig := &ssh.ClientConfig{
		User:            "e2e_user",
		Auth:            []ssh.AuthMethod{ssh.Password("e2e_password")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	sshConn, err := ssh.Dial("tcp", sftpAddr, sshConfig)
	if err != nil {
		t.Fatalf("Failed to dial Docker SFTP server (is docker-compose up running?): %v", err)
	}
	defer sshConn.Close()

	client, err := sftp.NewClient(sshConn)
	if err != nil {
		t.Fatalf("Failed to init SFTP client: %v", err)
	}
	defer client.Close()

	// Upload file
	f, err := client.Create(remotePath)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}
	if _, err := io.Copy(f, bytes.NewReader(testData)); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}
	f.Close()

	// Poll Echo Server to confirm RabbitMQ -> Webhook delivery
	var found bool
	for i := 0; i < 10; i++ {
		time.Sleep(1 * time.Second)

		resp, err := http.Get(echoLogsURL)
		if err != nil {
			continue
		}

		var logs []struct {
			Body WebhookPayload `json:"body"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&logs)
		resp.Body.Close()

		for _, log := range logs {
			if log.Body.Filepath == remotePath && log.Body.SHA256 == expectedHash {
				found = true
				break
			}
		}

		if found {
			break
		}
	}

	if !found {
		t.Fatalf("Failed to verify webhook payload delivery on Echo server for %s", remotePath)
	}
}
