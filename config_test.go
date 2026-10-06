package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_ValidYAML(t *testing.T) {
	content := []byte(`
server:
  port: 2222
  host_key_path: "./keys/host_rsa"
  trusted_ca_path: "./keys/ca.pub"
storage:
  driver: "mock"
webhook:
  url: "http://example.com/hook"
  secret: "secret123"
users:
  - username: "testuser"
    password: "pass"
    public_keys:
      - "ssh-rsa AAAAB3NzaC1yc2E..."
`)
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatalf("Failed to write temp config: %v", err)
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if cfg.Server.TrustedCAPath != "./keys/ca.pub" {
		t.Errorf("Expected trusted CA path './keys/ca.pub', got %s", cfg.Server.TrustedCAPath)
	}
	if len(cfg.Users) != 1 || cfg.Users[0].Username != "testuser" {
		t.Errorf("Unexpected users config: %+v", cfg.Users)
	}
	if len(cfg.Users[0].PublicKeys) != 1 || cfg.Users[0].PublicKeys[0] != "ssh-rsa AAAAB3NzaC1yc2E..." {
		t.Errorf("Failed to parse public keys array")
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := loadConfig("non_existent_file.yaml")
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	content := []byte(`server: port: [invalid yaml syntax`)
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "bad_config.yaml")
	os.WriteFile(configPath, content, 0644)

	_, err := loadConfig(configPath)
	if err == nil {
		t.Error("Expected error for malformed YAML, got nil")
	}
}
