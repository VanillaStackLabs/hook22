package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"

	"golang.org/x/crypto/ssh"
)

func buildSSHConfig(cfg *Config) (*ssh.ServerConfig, error) {
	// Load Trusted CA for Certificate Authentication if available
	var trustedCAPubKey ssh.PublicKey
	if cfg.Server.TrustedCAPath != "" {
		caBytes, err := os.ReadFile(cfg.Server.TrustedCAPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read trusted CA file at %s: %w", cfg.Server.TrustedCAPath, err)
		}
		trustedCAPubKey, _, _, _, err = ssh.ParseAuthorizedKey(caBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse trusted CA key: %w", err)
		}
		slog.Info("Loaded Trusted Certificate Authority", "event", "ssh.ca_loaded", "path", cfg.Server.TrustedCAPath)
	}

	// Configure Authentication Callbacks
	sshConfig := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			for _, u := range cfg.Users {
				if c.User() == u.Username && string(pass) == u.Password {
					return nil, nil
				}
			}
			return nil, fmt.Errorf("password rejected for %q", c.User())
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, pubKey ssh.PublicKey) (*ssh.Permissions, error) {
			// OpenSSH Certificate Validation
			if cert, ok := pubKey.(*ssh.Certificate); ok && trustedCAPubKey != nil {
				checker := ssh.CertChecker{
					IsUserAuthority: func(auth ssh.PublicKey) bool {
						return bytes.Equal(auth.Marshal(), trustedCAPubKey.Marshal())
					},
				}
				if err := checker.CheckCert(c.User(), cert); err == nil {
					return &ssh.Permissions{
						Extensions: map[string]string{
							"auth_method": "certificate",
							"cert_id":     cert.KeyId,
						},
					}, nil
				}
				slog.Warn("Certificate validation failed", "event", "ssh.cert_rejected", "user", c.User())
			}

			// Static Authorized Keys Validation
			for _, u := range cfg.Users {
				if c.User() == u.Username {
					for _, keyStr := range u.PublicKeys {
						allowedKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(keyStr))
						if err == nil && bytes.Equal(pubKey.Marshal(), allowedKey.Marshal()) {
							return &ssh.Permissions{
								Extensions: map[string]string{
									"auth_method": "publickey",
									"fingerprint": ssh.FingerprintSHA256(pubKey),
								},
							}, nil
						}
					}
				}
			}
			return nil, fmt.Errorf("public key rejected for %q", c.User())
		},
	}

	// Load Server Host Key
	privateBytes, err := os.ReadFile(cfg.Server.HostKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read host key at %s: %w", cfg.Server.HostKeyPath, err)
	}
	private, err := ssh.ParsePrivateKey(privateBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse host key: %w", err)
	}
	sshConfig.AddHostKey(private)

	return sshConfig, nil
}
