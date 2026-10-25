package ai

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

// keyDerivationPrefix separates Sodapop's AI key material from other uses of
// SESSION_SECRET.
const keyDerivationPrefix = "sodapop-ai-key:"

// ErrNoEncryptionKey is returned when an API key must be encrypted but no
// session secret is available.
var ErrNoEncryptionKey = errors.New("ai encryption key unavailable: SESSION_SECRET is not set")

// deriveKey turns the session secret into a 32-byte AES key. Deriving instead
// of using the secret directly keeps the same secret reusable for cookies.
func deriveKey(secret string) []byte {
	sum := sha256.Sum256([]byte(keyDerivationPrefix + secret))
	return sum[:]
}

// encryptAPIKey encrypts plaintext with AES-256-GCM and a fresh random nonce.
// The nonce is prepended to the ciphertext and the result is base64 encoded.
func encryptAPIKey(secret, plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if secret == "" {
		return "", ErrNoEncryptionKey
	}
	block, err := aes.NewCipher(deriveKey(secret))
	if err != nil {
		return "", fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("new gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("read nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// decryptAPIKey reverses encryptAPIKey. An empty value is a valid "no key"
// endpoint (for example a local Ollama server).
func decryptAPIKey(secret, encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	if secret == "" {
		return "", ErrNoEncryptionKey
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode api key: %w", err)
	}
	block, err := aes.NewCipher(deriveKey(secret))
	if err != nil {
		return "", fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("new gcm: %w", err)
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("api key ciphertext too short")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", errors.New("api key could not be decrypted")
	}
	return string(plaintext), nil
}

// MaskAPIKey returns the only representation of a key that may leave the
// process: a short prefix plus the last four characters, for example
// "sk-...a1b2".
func MaskAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) <= 4 {
		return "..." + key
	}
	prefix := key[:2]
	if strings.HasPrefix(key, "sk-") {
		prefix = "sk-"
	}
	return prefix + "..." + key[len(key)-4:]
}
