package secretbox

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func mustBox(t *testing.T, secret, domain string) *Box {
	t.Helper()
	b, err := New(secret, domain)
	if err != nil {
		t.Fatalf("New(%q, %q): %v", secret, domain, err)
	}
	return b
}

func TestRoundTrip(t *testing.T) {
	b := mustBox(t, "correct-horse-battery-staple", DomainSettings)
	for _, plaintext := range []string{
		"a",
		"hunter2",
		"multi\nline\tvalue",
		strings.Repeat("x", 4096),
		"unicode-é-🍒",
	} {
		encoded, err := b.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", plaintext, err)
		}
		if !strings.HasPrefix(encoded, Prefix) {
			t.Fatalf("ciphertext %q lacks %q", encoded, Prefix)
		}
		if len(plaintext) >= 4 && strings.Contains(encoded, plaintext) {
			t.Fatalf("ciphertext %q contains plaintext", encoded)
		}
		got, err := b.Decrypt(encoded)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if got != plaintext {
			t.Fatalf("round-trip = %q, want %q", got, plaintext)
		}
	}
}

func TestCiphertextsAreRandomised(t *testing.T) {
	b := mustBox(t, "secret", DomainSettings)
	first, err := b.Encrypt("same")
	if err != nil {
		t.Fatal(err)
	}
	second, err := b.Encrypt("same")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("two encryptions produced identical ciphertext")
	}
}

func TestWrongDomainOrSecretFails(t *testing.T) {
	encoded, err := mustBox(t, "shared-secret", DomainSettings).Encrypt("top-secret")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := mustBox(t, "shared-secret", DomainAI).Decrypt(encoded); !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("wrong domain err = %v, want ErrInvalidCiphertext", err)
	}
	if _, err := mustBox(t, "other-secret", DomainSettings).Decrypt(encoded); !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("wrong secret err = %v, want ErrInvalidCiphertext", err)
	}
}

func TestTamperFails(t *testing.T) {
	b := mustBox(t, "secret", DomainSettings)
	encoded, err := b.Encrypt("do-not-touch")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, Prefix))
	if err != nil {
		t.Fatal(err)
	}

	t.Run("ciphertext", func(t *testing.T) {
		tampered := append([]byte(nil), raw...)
		tampered[len(tampered)-1] ^= 0x01
		if _, err := b.Decrypt(Prefix + base64.StdEncoding.EncodeToString(tampered)); !errors.Is(err, ErrInvalidCiphertext) {
			t.Fatalf("err = %v, want ErrInvalidCiphertext", err)
		}
	})
	t.Run("nonce", func(t *testing.T) {
		tampered := append([]byte(nil), raw...)
		tampered[0] ^= 0x01
		if _, err := b.Decrypt(Prefix + base64.StdEncoding.EncodeToString(tampered)); !errors.Is(err, ErrInvalidCiphertext) {
			t.Fatalf("err = %v, want ErrInvalidCiphertext", err)
		}
	})
	t.Run("tag", func(t *testing.T) {
		tampered := append([]byte(nil), raw...)
		tampered[len(tampered)-1] ^= 0xff
		if _, err := b.Decrypt(Prefix + base64.StdEncoding.EncodeToString(tampered)); !errors.Is(err, ErrInvalidCiphertext) {
			t.Fatalf("err = %v, want ErrInvalidCiphertext", err)
		}
	})
}

func TestMalformedInputs(t *testing.T) {
	b := mustBox(t, "secret", DomainSettings)

	if _, err := b.Encrypt(""); !errors.Is(err, ErrEmptyPlaintext) {
		t.Fatalf("empty encrypt err = %v, want ErrEmptyPlaintext", err)
	}
	if _, err := b.Decrypt(""); !errors.Is(err, ErrEmptyCiphertext) {
		t.Fatalf("empty decrypt err = %v, want ErrEmptyCiphertext", err)
	}
	if _, err := b.Decrypt("plaintext"); !errors.Is(err, ErrInvalidPrefix) {
		t.Fatalf("plaintext decrypt err = %v, want ErrInvalidPrefix", err)
	}
	if _, err := b.Decrypt(Prefix + "not-base64!"); !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("bad base64 err = %v, want ErrInvalidCiphertext", err)
	}
	if _, err := b.Decrypt(Prefix + base64.StdEncoding.EncodeToString([]byte("short"))); !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("short nonce err = %v, want ErrInvalidCiphertext", err)
	}
}

func TestKeySetup(t *testing.T) {
	if _, err := New("", DomainSettings); !errors.Is(err, ErrNoSecret) {
		t.Fatalf("empty secret err = %v, want ErrNoSecret", err)
	}
	if _, err := New("secret", ""); err == nil {
		t.Fatal("empty domain accepted")
	}
	if _, err := New("secret", "   "); err == nil {
		t.Fatal("blank domain accepted")
	}

	// Short secrets are permitted; production config enforces SESSION_SECRET.
	b, err := New("x", DomainSettings)
	if err != nil {
		t.Fatalf("short secret: %v", err)
	}
	encoded, err := b.Encrypt("value")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := b.Decrypt(encoded); err != nil || got != "value" {
		t.Fatalf("short secret round-trip = %q, %v", got, err)
	}

	var nilBox *Box
	if _, err := nilBox.Encrypt("x"); !errors.Is(err, ErrNoSecret) {
		t.Fatalf("nil box encrypt err = %v", err)
	}
	if _, err := nilBox.Decrypt("x"); !errors.Is(err, ErrNoSecret) {
		t.Fatalf("nil box decrypt err = %v", err)
	}
}
