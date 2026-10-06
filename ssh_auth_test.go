package main

import (
	"crypto/rand"
	"crypto/rsa"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

// mockConnMetadata stubs the ssh.ConnMetadata interface for testing callbacks
type mockConnMetadata struct {
	ssh.ConnMetadata
	user string
}

func (m *mockConnMetadata) User() string { return m.user }

func TestBuildSSHConfig_PublicKeyAuth(t *testing.T) {
	tmpDir := t.TempDir()
	hostKeyPath := filepath.Join(tmpDir, "host_rsa")

	// Reuse the helper from e2e_test.go to generate a dummy host key
	generateTestHostKey(t, hostKeyPath)

	// Generate a simulated user RSA public key
	userKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	userPubKey, _ := ssh.NewPublicKey(&userKey.PublicKey)
	userPubKeyBytes := ssh.MarshalAuthorizedKey(userPubKey)

	// Setup Config matching the schema
	cfg := &Config{}
	cfg.Server.HostKeyPath = hostKeyPath
	cfg.Users = []struct {
		Username   string   `yaml:"username"`
		Password   string   `yaml:"password"`
		PublicKeys []string `yaml:"public_keys"`
	}{
		{
			Username:   "key_user",
			PublicKeys: []string{string(userPubKeyBytes)},
		},
	}

	sshCfg, err := buildSSHConfig(cfg)
	if err != nil {
		t.Fatalf("buildSSHConfig failed: %v", err)
	}

	// Test Valid Public Key Match
	validConn := &mockConnMetadata{user: "key_user"}
	perms, err := sshCfg.PublicKeyCallback(validConn, userPubKey)
	if err != nil {
		t.Errorf("Expected public key to be accepted, got error: %v", err)
	}
	if perms == nil || perms.Extensions["auth_method"] != "publickey" {
		t.Errorf("Expected publickey auth method extension")
	}

	// Test Invalid User with Valid Key
	invalidUserConn := &mockConnMetadata{user: "wrong_user"}
	_, err = sshCfg.PublicKeyCallback(invalidUserConn, userPubKey)
	if err == nil {
		t.Errorf("Expected public key to be rejected for wrong user, but it succeeded")
	}

	// Test Valid User with Unknown Key
	unknownKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	unknownPubKey, _ := ssh.NewPublicKey(&unknownKey.PublicKey)

	_, err = sshCfg.PublicKeyCallback(validConn, unknownPubKey)
	if err == nil {
		t.Errorf("Expected unknown public key to be rejected, but it succeeded")
	}
}
