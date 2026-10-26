// Package secretbox encrypts small secrets at rest with AES-256-GCM.
//
// Key material is derived from SESSION_SECRET and separated by purpose, so a
// settings secret and an AI provider key never share a key even though both
// come from the same process secret. Ciphertexts are self-describing:
//
//	enc:v1:<base64(nonce || ciphertext)>
package secretbox

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

const (
	// Prefix marks a secretbox ciphertext and its format version.
	Prefix = "enc:v1:"

	// DomainSettings is the key domain for admin runtime settings.
	DomainSettings = "sodapop-settings-key:v1"
	// DomainAI is the key domain for AI provider credentials.
	DomainAI = "sodapop-ai-key:v1"
)

var (
	// ErrNoSecret is returned when no SESSION_SECRET material is available.
	ErrNoSecret = errors.New("secretbox: session secret is empty")
	// ErrEmptyPlaintext is returned when callers try to encrypt an empty value.
	ErrEmptyPlaintext = errors.New("secretbox: plaintext is empty")
	// ErrEmptyCiphertext is returned when decrypting an empty stored value.
	ErrEmptyCiphertext = errors.New("secretbox: ciphertext is empty")
	// ErrInvalidPrefix reports a value that was not produced by this package.
	ErrInvalidPrefix = errors.New("secretbox: ciphertext has an unknown prefix")
	// ErrInvalidCiphertext reports malformed base64, short nonce, or failed authentication.
	ErrInvalidCiphertext = errors.New("secretbox: ciphertext is invalid")
)

// Box encrypts and decrypts values in one domain. It is safe for concurrent use.
type Box struct {
	aead cipher.AEAD
}

// New derives a 32-byte AES key from secret using domain separation. domain
// must be a stable, versioned identifier such as DomainSettings.
func New(secret, domain string) (*Box, error) {
	if secret == "" {
		return nil, ErrNoSecret
	}
	if strings.TrimSpace(domain) == "" {
		return nil, errors.New("secretbox: domain is empty")
	}
	sum := sha256.Sum256([]byte(domain + ":" + secret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, fmt.Errorf("secretbox: new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretbox: new gcm: %w", err)
	}
	return &Box{aead: aead}, nil
}

// Encrypt returns enc:v1:<base64(nonce||ciphertext)> for plaintext.
func (b *Box) Encrypt(plaintext string) (string, error) {
	if b == nil || b.aead == nil {
		return "", ErrNoSecret
	}
	if plaintext == "" {
		return "", ErrEmptyPlaintext
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("secretbox: read nonce: %w", err)
	}
	sealed := b.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return Prefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt validates and opens a value produced by Encrypt.
func (b *Box) Decrypt(encoded string) (string, error) {
	if b == nil || b.aead == nil {
		return "", ErrNoSecret
	}
	if encoded == "" {
		return "", ErrEmptyCiphertext
	}
	if !strings.HasPrefix(encoded, Prefix) {
		return "", ErrInvalidPrefix
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, Prefix))
	if err != nil {
		return "", ErrInvalidCiphertext
	}
	if len(raw) < b.aead.NonceSize()+b.aead.Overhead() {
		return "", ErrInvalidCiphertext
	}
	nonce, ciphertext := raw[:b.aead.NonceSize()], raw[b.aead.NonceSize():]
	plaintext, err := b.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", ErrInvalidCiphertext
	}
	return string(plaintext), nil
}
