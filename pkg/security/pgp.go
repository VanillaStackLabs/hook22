package security

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
)

// DecryptStreamReader takes an encrypted reader and decrypts it using the private key at keyPath.
// Supports both raw binary and ASCII-armored OpenPGP encrypted streams.
func DecryptStreamReader(r io.Reader, keyPath string, passphrase string) (io.Reader, error) {
	if keyPath == "" {
		return nil, fmt.Errorf("PGP private key path is not configured")
	}

	keyFile, err := os.Open(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open PGP private key: %w", err)
	}
	defer keyFile.Close()

	// Parse keyring (supports armored or raw binary)
	entityList, err := openpgp.ReadArmoredKeyRing(keyFile)
	if err != nil {
		if _, seekErr := keyFile.Seek(0, 0); seekErr == nil {
			entityList, err = openpgp.ReadKeyRing(keyFile)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to parse PGP key ring: %w", err)
		}
	}

	// Decrypt encrypted private key and subkeys if passphrase is provided
	if passphrase != "" {
		passBytes := []byte(passphrase)
		for _, entity := range entityList {
			if entity.PrivateKey != nil && entity.PrivateKey.Encrypted {
				if err := entity.PrivateKey.Decrypt(passBytes); err != nil {
					return nil, fmt.Errorf("failed to decrypt private key: %w", err)
				}
			}
			for _, subkey := range entity.Subkeys {
				if subkey.PrivateKey != nil && subkey.PrivateKey.Encrypted {
					if err := subkey.PrivateKey.Decrypt(passBytes); err != nil {
						return nil, fmt.Errorf("failed to decrypt subkey: %w", err)
					}
				}
			}
		}
	}

	bufR := bufio.NewReader(r)
	peekBytes, err := bufR.Peek(30)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to peek encrypted stream: %w", err)
	}

	var msgReader io.Reader = bufR
	if strings.HasPrefix(string(peekBytes), "-----BEGIN PGP MESSAGE-----") {
		block, err := armor.Decode(bufR)
		if err != nil {
			return nil, fmt.Errorf("failed to decode armored PGP stream: %w", err)
		}
		msgReader = block.Body
	}

	md, err := openpgp.ReadMessage(msgReader, entityList, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt OpenPGP message: %w", err)
	}

	return md.UnverifiedBody, nil
}
