package ai

import (
	"errors"
	"strings"
	"testing"
)

func TestEncryptionRoundtrip(t *testing.T) {
	secret := "test-session-secret"
	plaintext := "sk-live-very-secret-value-1234"

	encrypted, err := encryptAPIKey(secret, plaintext)
	if err != nil {
		t.Fatalf("encryptAPIKey: %v", err)
	}
	if encrypted == "" {
		t.Fatal("encrypted key is empty")
	}
	if strings.Contains(encrypted, plaintext) {
		t.Fatal("ciphertext contains plaintext")
	}

	decrypted, err := decryptAPIKey(secret, encrypted)
	if err != nil {
		t.Fatalf("decryptAPIKey: %v", err)
	}
	if decrypted != plaintext {
		t.Fatalf("roundtrip = %q, want %q", decrypted, plaintext)
	}

	again, err := encryptAPIKey(secret, plaintext)
	if err != nil {
		t.Fatalf("second encryptAPIKey: %v", err)
	}
	if again == encrypted {
		t.Fatal("two encryptions used the same nonce")
	}

	if _, err := decryptAPIKey("another-secret", encrypted); err == nil {
		t.Fatal("decrypt with the wrong secret succeeded")
	}
}

func TestEncryptionEmptyKey(t *testing.T) {
	encrypted, err := encryptAPIKey("secret", "")
	if err != nil {
		t.Fatalf("encrypt empty: %v", err)
	}
	if encrypted != "" {
		t.Fatalf("encrypt empty = %q, want empty", encrypted)
	}
	decrypted, err := decryptAPIKey("secret", "")
	if err != nil {
		t.Fatalf("decrypt empty: %v", err)
	}
	if decrypted != "" {
		t.Fatalf("decrypt empty = %q, want empty", decrypted)
	}
}

func TestEncryptionRequiresSecret(t *testing.T) {
	if _, err := encryptAPIKey("", "sk-secret"); !errors.Is(err, ErrNoEncryptionKey) {
		t.Fatalf("encrypt without secret error = %v, want ErrNoEncryptionKey", err)
	}
	if _, err := decryptAPIKey("", "c29tZXRoaW5n"); !errors.Is(err, ErrNoEncryptionKey) {
		t.Fatalf("decrypt without secret error = %v, want ErrNoEncryptionKey", err)
	}
}

func TestDecryptRejectsShortCiphertext(t *testing.T) {
	if _, err := decryptAPIKey("secret", "YQ=="); err == nil {
		t.Fatal("decrypt accepted a ciphertext shorter than the nonce")
	}
}

func TestMaskAPIKey(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: ""},
		{in: "sk-live-very-secret-value-1234", want: "sk-...1234"},
		{in: "abcdefgh", want: "ab...efgh"},
		{in: "abc", want: "...abc"},
	}
	for _, tc := range tests {
		if got := MaskAPIKey(tc.in); got != tc.want {
			t.Errorf("MaskAPIKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
