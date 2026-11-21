package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Keyring encrypts small secrets at rest (TOTP seeds) with AES-256-GCM.
type Keyring struct {
	aead cipher.AEAD
}

const keySize = 32

// LoadOrCreateKey reads the 32-byte master key from path, creating it with
// mode 0600 if it doesn't exist yet. Losing this file means every TOTP seed
// must be re-enrolled, so it belongs in backups.
func LoadOrCreateKey(path string) (*Keyring, error) {
	key, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		key = make([]byte, keySize)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, fmt.Errorf("create key dir: %w", err)
		}
		// O_EXCL: fail instead of overwriting if another process created it meanwhile.
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return nil, fmt.Errorf("create secret key %s: %w", path, err)
		}
		if _, err := f.Write(key); err != nil {
			f.Close()
			return nil, fmt.Errorf("write secret key: %w", err)
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, fmt.Errorf("read secret key %s: %w", path, err)
	}
	if len(key) != keySize {
		return nil, fmt.Errorf("secret key %s must be %d bytes, got %d", path, keySize, len(key))
	}
	return NewKeyring(key)
}

func NewKeyring(key []byte) (*Keyring, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Keyring{aead: aead}, nil
}

// Seal encrypts plaintext. aad ("additional data") binds the ciphertext to a
// context (e.g. the username) so it can't be copied to another row.
func (k *Keyring) Seal(plaintext, aad []byte) ([]byte, error) {
	nonce := make([]byte, k.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return k.aead.Seal(nonce, nonce, plaintext, aad), nil
}

// Open decrypts what Seal produced.
func (k *Keyring) Open(ciphertext, aad []byte) ([]byte, error) {
	n := k.aead.NonceSize()
	if len(ciphertext) < n {
		return nil, errors.New("ciphertext too short")
	}
	return k.aead.Open(nil, ciphertext[:n], ciphertext[n:], aad)
}

// NewToken returns a random 32-byte token (hex) and its SHA-256 hash (hex).
// The token goes to the client; only the hash is stored.
func NewToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token = hex.EncodeToString(b)
	return token, HashToken(token), nil
}

// HashToken is the stored form of a token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
