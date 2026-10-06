package main

import (
	"bytes"
	"crypto"
	_ "crypto/sha256"
	_ "crypto/sha512"
	"io"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
	"golang.org/x/crypto/openpgp/packet"
	_ "golang.org/x/crypto/ripemd160"
)

func createTestPGPKey(t *testing.T, dir string) (*openpgp.Entity, string) {
	t.Helper()

	cfg := &packet.Config{
		DefaultHash: crypto.SHA256,
	}

	entity, err := openpgp.NewEntity("Test User", "test comment", "test@example.com", cfg)
	if err != nil {
		t.Fatalf("Failed to create OpenPGP entity: %v", err)
	}

	keyPath := filepath.Join(dir, "private.pgp")
	f, err := os.Create(keyPath)
	if err != nil {
		t.Fatalf("Failed to create key file: %v", err)
	}
	defer f.Close()

	w, err := armor.Encode(f, openpgp.PrivateKeyType, make(map[string]string))
	if err != nil {
		t.Fatalf("Failed to create armor encoder: %v", err)
	}

	if err := entity.SerializePrivate(w, cfg); err != nil {
		t.Fatalf("Failed to serialize private key: %v", err)
	}
	w.Close()

	return entity, keyPath
}

func encryptTestPayload(t *testing.T, entity *openpgp.Entity, plaintext []byte, armored bool) []byte {
	t.Helper()
	var buf bytes.Buffer

	var out io.Writer = &buf
	var armorWriter io.WriteCloser
	var err error

	if armored {
		armorWriter, err = armor.Encode(&buf, "PGP MESSAGE", nil)
		if err != nil {
			t.Fatalf("Failed to create armor encoder: %v", err)
		}
		out = armorWriter
	}

	cfg := &packet.Config{
		DefaultHash: crypto.SHA256,
	}

	encWriter, err := openpgp.Encrypt(out, []*openpgp.Entity{entity}, nil, nil, cfg)
	if err != nil {
		t.Fatalf("Failed to create encryptor: %v", err)
	}

	if _, err := encWriter.Write(plaintext); err != nil {
		t.Fatalf("Failed to write plaintext: %v", err)
	}
	encWriter.Close()

	if armorWriter != nil {
		armorWriter.Close()
	}

	return buf.Bytes()
}

func TestDecryptStreamReader_ArmoredAndBinary(t *testing.T) {
	tmpDir := t.TempDir()
	entity, keyPath := createTestPGPKey(t, tmpDir)

	rawPlaintext := []byte("Sensitive payload to be encrypted via PGP stream")

	tests := []struct {
		name    string
		armored bool
	}{
		{name: "ASCII-Armored PGP Stream", armored: true},
		{name: "Raw Binary PGP Stream", armored: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ciphertext := encryptTestPayload(t, entity, rawPlaintext, tt.armored)
			inR := bytes.NewReader(ciphertext)

			decR, err := DecryptStreamReader(inR, keyPath, "")
			if err != nil {
				t.Fatalf("DecryptStreamReader failed: %v", err)
			}

			decryptedData, err := io.ReadAll(decR)
			if err != nil {
				t.Fatalf("Failed to read decrypted stream: %v", err)
			}

			if !bytes.Equal(decryptedData, rawPlaintext) {
				t.Errorf("Decrypted data mismatch.\nGot:  %s\nWant: %s", decryptedData, rawPlaintext)
			}
		})
	}
}

func TestDecryptStreamReader_MissingKeyPath(t *testing.T) {
	inR := bytes.NewReader([]byte("test"))
	_, err := DecryptStreamReader(inR, "", "")
	if err == nil {
		t.Error("Expected error for empty key path, got nil")
	}
}

func TestDecryptStreamReader_NonExistentKeyFile(t *testing.T) {
	inR := bytes.NewReader([]byte("test"))
	_, err := DecryptStreamReader(inR, "/non/existent/path/key.pgp", "")
	if err == nil {
		t.Error("Expected error for non-existent key file, got nil")
	}
}
