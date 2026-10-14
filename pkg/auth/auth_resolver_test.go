package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/VanillaStackLabs/hook22/pkg/config"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/ssh"
)

func TestHTTPControlPlaneResolver_AuthenticatePassword(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload authRequestPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if payload.Username == "valid_user" && payload.Password == "valid_pass" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(UserPermissions{
				AllowedStorageBucket: "tenant-bucket",
				S3PrefixPattern:      "tenants/valid_user/",
				WebhookOverrideURL:   "https://api.tenant.com/webhook",
			})
			return
		}

		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	resolver := NewHTTPControlPlaneResolver(server.URL)

	// Test Success
	perms, ok := resolver.AuthenticatePassword("valid_user", "valid_pass")
	if !ok || perms == nil {
		t.Fatalf("Expected successful password authentication")
	}
	if perms.S3PrefixPattern != "tenants/valid_user/" {
		t.Errorf("Expected prefix pattern tenants/valid_user/, got %s", perms.S3PrefixPattern)
	}

	// Test Failure
	_, ok = resolver.AuthenticatePassword("valid_user", "wrong_pass")
	if ok {
		t.Errorf("Expected failed authentication for wrong password")
	}
}

func TestHTTPControlPlaneResolver_AuthenticatePublicKey(t *testing.T) {
	userKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	userPubKey, _ := ssh.NewPublicKey(&userKey.PublicKey)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload authRequestPayload
		_ = json.NewDecoder(r.Body).Decode(&payload)

		if payload.Username == "key_user" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(UserPermissions{
				AllowedStorageBucket: "key-bucket",
			})
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	resolver := NewHTTPControlPlaneResolver(server.URL)

	perms, ok := resolver.AuthenticatePublicKey("key_user", userPubKey)
	if !ok || perms == nil {
		t.Fatalf("Expected successful public key authentication")
	}
	if perms.AllowedStorageBucket != "key-bucket" {
		t.Errorf("Expected bucket key-bucket, got %s", perms.AllowedStorageBucket)
	}
}

func TestVerifyPasswordAuth_StaticBcrypt(t *testing.T) {
	password := "secure_password_123"
	hashBytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("Failed to generate bcrypt hash: %v", err)
	}

	cfg := &config.Config{}
	cfg.Users = []struct {
		Username   string   `yaml:"username"`
		Password   string   `yaml:"password"`
		PublicKeys []string `yaml:"public_keys"`
	}{
		{Username: "static_user", Password: string(hashBytes)},
	}

	// Test Success
	method, perms := VerifyPasswordAuth(cfg, nil, "static_user", password)
	if method != "static_password" {
		t.Errorf("Expected method 'static_password', got '%s'", method)
	}
	if perms != nil {
		t.Error("Expected nil permissions for static user")
	}

	// Wrong Password
	badMethod, _ := VerifyPasswordAuth(cfg, nil, "static_user", "wrong_password")
	if badMethod != "" {
		t.Errorf("Expected empty method for wrong password, got '%s'", badMethod)
	}

	// Wrong User
	badUserMethod, _ := VerifyPasswordAuth(cfg, nil, "unknown_user", password)
	if badUserMethod != "" {
		t.Errorf("Expected empty method for unknown user, got '%s'", badUserMethod)
	}
}