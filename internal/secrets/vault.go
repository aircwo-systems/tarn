package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

const (
	vaultPrefix = "vault:v1:"
	vaultKeyLen = 32 // AES-256
)

// Vault provides AES-256-GCM encryption for secret values at rest.
type Vault struct {
	gcm cipher.AEAD
}

// LoadOrCreateVault loads an encryption key from keyPath. If the file does not
// exist, a new random 32-byte key is generated and written to keyPath (mode 0600).
func LoadOrCreateVault(keyPath string) (*Vault, error) {
	key, err := os.ReadFile(keyPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read vault key: %w", err)
		}
		key = make([]byte, vaultKeyLen)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generate vault key: %w", err)
		}
		if err := os.WriteFile(keyPath, key, 0600); err != nil {
			return nil, fmt.Errorf("write vault key: %w", err)
		}
	}

	if len(key) != vaultKeyLen {
		return nil, fmt.Errorf("vault key must be exactly %d bytes, got %d", vaultKeyLen, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("init AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("init GCM: %w", err)
	}

	return &Vault{gcm: gcm}, nil
}

// Seal encrypts plaintext and returns a prefixed base64 string: "vault:v1:<base64>".
func (v *Vault) Seal(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	return v.sealBytes([]byte(plaintext))
}

// SealBytes encrypts binary data and returns a prefixed base64 string.
func (v *Vault) SealBytes(data []byte) (string, error) {
	if len(data) == 0 {
		return "", nil
	}
	return v.sealBytes(data)
}

func (v *Vault) sealBytes(data []byte) (string, error) {
	nonce := make([]byte, v.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext := v.gcm.Seal(nonce, nonce, data, nil)
	return vaultPrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Unseal decrypts a vault-prefixed string back to plaintext.
func (v *Vault) Unseal(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	data, err := v.unsealBytes(sealed)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// UnsealBytes decrypts a vault-prefixed string back to raw bytes.
func (v *Vault) UnsealBytes(sealed string) ([]byte, error) {
	if sealed == "" {
		return nil, nil
	}
	return v.unsealBytes(sealed)
}

func (v *Vault) unsealBytes(sealed string) ([]byte, error) {
	if !strings.HasPrefix(sealed, vaultPrefix) {
		return nil, fmt.Errorf("not a sealed value (missing %s prefix)", vaultPrefix)
	}
	encoded := sealed[len(vaultPrefix):]
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode sealed value: %w", err)
	}

	nonceSize := v.gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("sealed value too short")
	}

	nonce := ciphertext[:nonceSize]
	ciphertext = ciphertext[nonceSize:]

	plaintext, err := v.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}

// IsSealed returns true if the string has the vault prefix.
func IsSealed(s string) bool {
	return strings.HasPrefix(s, vaultPrefix)
}
