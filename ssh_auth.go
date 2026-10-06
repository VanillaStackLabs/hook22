package main

import (
	"bytes"
	"fmt"
	"os"

	"golang.org/x/crypto/ssh"
)

func buildSSHConfig(cfg *Config, resolver DynamicUserResolver) (*ssh.ServerConfig, error) {
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
	}

	sshConfig := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			// 1. Static YAML Lookup
			for _, u := range cfg.Users {
				if c.User() == u.Username && string(pass) == u.Password {
					return &ssh.Permissions{
						Extensions: map[string]string{
							"username":    c.User(),
							"auth_method": "static_password",
						},
					}, nil
				}
			}

			// 2. Dynamic Control Plane Fallback
			if resolver != nil {
				if perms, ok := resolver.AuthenticatePassword(c.User(), string(pass)); ok {
					return &ssh.Permissions{
						Extensions: map[string]string{
							"username":             c.User(),
							"auth_method":          "dynamic_password",
							"prefix_pattern":       perms.S3PrefixPattern,
							"webhook_override_url": perms.WebhookOverrideURL,
						},
					}, nil
				}
			}

			return nil, fmt.Errorf("password rejected for %q", c.User())
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, pubKey ssh.PublicKey) (*ssh.Permissions, error) {
			// 1. OpenSSH Certificate Validation
			if cert, ok := pubKey.(*ssh.Certificate); ok && trustedCAPubKey != nil {
				checker := ssh.CertChecker{
					IsUserAuthority: func(auth ssh.PublicKey) bool {
						return bytes.Equal(auth.Marshal(), trustedCAPubKey.Marshal())
					},
				}
				if err := checker.CheckCert(c.User(), cert); err == nil {
					return &ssh.Permissions{
						Extensions: map[string]string{
							"username":    c.User(),
							"auth_method": "certificate",
							"cert_id":     cert.KeyId,
						},
					}, nil
				}
			}

			// 2. Static Authorized Keys
			for _, u := range cfg.Users {
				if c.User() == u.Username {
					for _, keyStr := range u.PublicKeys {
						allowedKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(keyStr))
						if err == nil && bytes.Equal(pubKey.Marshal(), allowedKey.Marshal()) {
							return &ssh.Permissions{
								Extensions: map[string]string{
									"username":    c.User(),
									"auth_method": "static_publickey",
									"fingerprint": ssh.FingerprintSHA256(pubKey),
								},
							}, nil
						}
					}
				}
			}

			// 3. Dynamic Control Plane Fallback
			if resolver != nil {
				if perms, ok := resolver.AuthenticatePublicKey(c.User(), pubKey); ok {
					return &ssh.Permissions{
						Extensions: map[string]string{
							"username":             c.User(),
							"auth_method":          "dynamic_publickey",
							"prefix_pattern":       perms.S3PrefixPattern,
							"webhook_override_url": perms.WebhookOverrideURL,
						},
					}, nil
				}
			}

			return nil, fmt.Errorf("public key rejected for %q", c.User())
		},
	}

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
