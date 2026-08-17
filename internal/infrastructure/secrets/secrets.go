// Package secrets encrypts at-rest secrets stored in the settings database
// (platform credentials such as the NetEase cookie) before they are
// persisted, and decrypts them at the point of use. The key is derived from
// the server's master secret (the JWT secret) via HKDF with a domain label,
// so future platform providers can reuse the same Encryptor for their own
// credentials without inventing per-platform key material.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// storedPrefix marks an encrypted value in server_settings. Values without
// it are treated as legacy plaintext.
const storedPrefix = "enc:v1:"

// defaultPlaceholder is the jwt.secret value shipped in config.example.toml.
// It is publicly known, so a deployment that leaves it in place lets anyone
// derive the at-rest encryption key; it is rejected outright.
const defaultPlaceholder = "change-me-in-production-0123456789abcdef0123456789"

// hkdfInfo is the HKDF domain-separation label so the same master secret can
// serve encryption without reusing key material elsewhere.
const hkdfInfo = "sonicore:settings:v1"

// Encryptor performs AES-256-GCM encrypt/decrypt for at-rest secrets.
type Encryptor struct {
	gcm cipher.AEAD
}

// New derives a 32-byte key from the master secret and returns an Encryptor.
// The master secret must be stable across restarts, or previously stored
// values become undecryptable (rotation requires re-entering credentials). It
// must be a strong random value: a short or default secret would make the
// derived key predictable and the at-rest encryption trivially breakable. The
// shipped config.example.toml placeholder and any value under 32 bytes are
// rejected at construction. The error lets callers fail startup with an
// actionable message instead of crashing the process.
func New(master []byte) (*Encryptor, error) {
	if string(master) == defaultPlaceholder {
		return nil, errors.New("secrets: jwt.secret is the built-in placeholder — replace it with a random value of at least 32 bytes (`openssl rand -hex 32`)")
	}
	if len(master) < 32 {
		return nil, errors.New("secrets: master secret must be at least 32 bytes (use `openssl rand -hex 32`)")
	}
	key, err := hkdf.Key(sha256.New, master, nil, hkdfInfo, 32)
	if err != nil {
		return nil, fmt.Errorf("secrets: derive key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: new gcm: %w", err)
	}
	return &Encryptor{gcm: gcm}, nil
}

// Encrypt returns "enc:v1:" + base64(nonce || ciphertext). An empty
// plaintext stays empty so a cleared/absent credential remains comparable.
func (e *Encryptor) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("secrets: random nonce: %w", err)
	}
	sealed := e.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return storedPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt. Values without the "enc:v1:" prefix are legacy
// plaintext and returned as-is.
func (e *Encryptor) Decrypt(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	if !strings.HasPrefix(stored, storedPrefix) {
		return stored, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, storedPrefix))
	if err != nil {
		return "", fmt.Errorf("secrets: decode stored value: %w", err)
	}
	// Account for the GCM authentication tag (Overhead) as well as the nonce,
	// so a value that decodes to a legal base64 string but is too short is
	// rejected here with a precise message instead of a generic MAC failure.
	if len(raw) < e.gcm.NonceSize()+e.gcm.Overhead() {
		return "", errors.New("secrets: stored value too short")
	}
	nonce, ct := raw[:e.gcm.NonceSize()], raw[e.gcm.NonceSize():]
	plain, err := e.gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("secrets: decrypt stored value: %w", err)
	}
	return string(plain), nil
}
