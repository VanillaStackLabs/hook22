package main

import (
	"crypto/rand"
	"crypto/rsa"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

type mockConnMetadata struct {
	ssh.ConnMetadata
	user string
}

func (m *mockConnMetadata) User() string { return m.user }

func TestBuildSSHConfig_PublicKeyAuth(t *testing.T) {
	tmpDir := t.TempDir()
	hostKeyPath := filepath.Join(tmpDir, "host_rsa")

	generateTestHostKey(t, hostKeyPath)

	userKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	userPubKey, _ := ssh.NewPublicKey(&userKey.PublicKey)
	userPubKeyBytes := ssh.MarshalAuthorizedKey(userPubKey)

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

	sshCfg, err := buildSSHConfig(cfg, nil)
	if err != nil {
		t.Fatalf("buildSSHConfig failed: %v", err)
	}

	validConn := &mockConnMetadata{user: "key_user"}
	perms, err := sshCfg.PublicKeyCallback(validConn, userPubKey)
	if err != nil {
		t.Errorf("Expected public key to be accepted, got error: %v", err)
	}
	if perms == nil || perms.Extensions["auth_method"] != "static_publickey" {
		t.Errorf("Expected static_publickey auth method extension")
	}

	invalidUserConn := &mockConnMetadata{user: "wrong_user"}
	_, err = sshCfg.PublicKeyCallback(invalidUserConn, userPubKey)
	if err == nil {
		t.Errorf("Expected public key to be rejected for wrong user, but it succeeded")
	}

	unknownKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	unknownPubKey, _ := ssh.NewPublicKey(&unknownKey.PublicKey)

	_, err = sshCfg.PublicKeyCallback(validConn, unknownPubKey)
	if err == nil {
		t.Errorf("Expected unknown public key to be rejected, but it succeeded")
	}
}
