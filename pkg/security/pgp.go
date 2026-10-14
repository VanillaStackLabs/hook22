package security

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
)

var (
	cachedEntityList openpgp.EntityList
	keyMu            sync.RWMutex
)

// verifiedReader wraps the unverified body and checks for signature/MDC errors at EOF
type verifiedReader struct {
	body io.Reader
	md   *openpgp.MessageDetails
}

func (vr *verifiedReader) Read(p []byte) (int, error) {
	n, err := vr.body.Read(p)
	if err == io.EOF {
		if vr.md.SignatureError != nil {
			return n, fmt.Errorf("PGP integrity check failed (tampered data): %w", vr.md.SignatureError)
		}
	}
	return n, err
}

func getEntityList(keyPath, passphrase string) (openpgp.EntityList, error) {
	keyMu.RLock()
	if cachedEntityList != nil {
		defer keyMu.RUnlock()
		return cachedEntityList, nil
	}
	keyMu.RUnlock()

	keyMu.Lock()
	defer keyMu.Unlock()

	// Double-checked locking
	if cachedEntityList != nil {
		return cachedEntityList, nil
	}

	keyFile, err := os.Open(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open PGP private key: %w", err)
	}
	defer keyFile.Close()

	entityList, err := openpgp.ReadArmoredKeyRing(keyFile)
	if err != nil {
		if _, seekErr := keyFile.Seek(0, 0); seekErr == nil {
			entityList, err = openpgp.ReadKeyRing(keyFile)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to parse PGP key ring: %w", err)
		}
	}

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

	cachedEntityList = entityList
	return cachedEntityList, nil
}

// DecryptStreamReader takes an encrypted reader and decrypts it using the private key at keyPath.
func DecryptStreamReader(r io.Reader, keyPath string, passphrase string) (io.Reader, error) {
	if keyPath == "" {
		return nil, fmt.Errorf("PGP private key path is not configured")
	}

	entityList, err := getEntityList(keyPath, passphrase)
	if err != nil {
		return nil, err
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

	return &verifiedReader{
		body: md.UnverifiedBody,
		md:   md,
	}, nil
}
