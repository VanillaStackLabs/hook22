package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestHTTPControlPlaneResolver_AuthenticatePassword(t *testing.T) {
	// 1. Mock Control Plane Server
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

	// 2. Test Success
	perms, ok := resolver.AuthenticatePassword("valid_user", "valid_pass")
	if !ok || perms == nil {
		t.Fatalf("Expected successful password authentication")
	}
	if perms.S3PrefixPattern != "tenants/valid_user/" {
		t.Errorf("Expected prefix pattern tenants/valid_user/, got %s", perms.S3PrefixPattern)
	}

	// 3. Test Failure
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
