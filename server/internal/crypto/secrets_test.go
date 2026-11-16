package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func testKey(t *testing.T) {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatalf("rand: %v", err)
	}
	t.Setenv(SecretsKeyEnv, base64.StdEncoding.EncodeToString(k))
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	testKey(t)
	for _, pt := range []string{"", "hello", "app_secret_值-带中文", "a longer secret with spaces"} {
		enc, err := Encrypt(pt)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", pt, err)
		}
		if enc == pt && pt != "" {
			t.Fatalf("ciphertext equals plaintext for %q", pt)
		}
		got, err := Decrypt(enc)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if got != pt {
			t.Fatalf("roundtrip mismatch: got %q want %q", got, pt)
		}
	}
}

func TestEncryptNonceUnique(t *testing.T) {
	testKey(t)
	a, _ := Encrypt("same")
	b, _ := Encrypt("same")
	if a == b {
		t.Fatal("two encryptions of the same plaintext produced identical ciphertext (nonce reuse)")
	}
}

func TestNoKey(t *testing.T) {
	t.Setenv(SecretsKeyEnv, "")
	if _, err := Encrypt("x"); err != ErrNoSecretsKey {
		t.Fatalf("Encrypt without key: got %v want ErrNoSecretsKey", err)
	}
	if _, err := Decrypt("AAAA"); err != ErrNoSecretsKey {
		t.Fatalf("Decrypt without key: got %v want ErrNoSecretsKey", err)
	}
}

func TestBadKey(t *testing.T) {
	t.Setenv(SecretsKeyEnv, "not-base64!!")
	if _, err := Encrypt("x"); err != ErrInvalidSecretsKey {
		t.Fatalf("Encrypt bad base64: got %v want ErrInvalidSecretsKey", err)
	}
	t.Setenv(SecretsKeyEnv, base64.StdEncoding.EncodeToString([]byte("too-short")))
	if _, err := Encrypt("x"); err != ErrInvalidSecretsKey {
		t.Fatalf("Encrypt short key: got %v want ErrInvalidSecretsKey", err)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	testKey(t)
	enc, err := Encrypt("secret")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	// Rotate to a different valid key; the old ciphertext must fail to auth.
	testKey(t)
	if _, err := Decrypt(enc); err != ErrDecrypt {
		t.Fatalf("Decrypt with wrong key: got %v want ErrDecrypt", err)
	}
}

func TestDecryptTampered(t *testing.T) {
	testKey(t)
	if _, err := Decrypt("###not base64###"); err != ErrDecrypt {
		t.Fatalf("Decrypt(non-base64): got %v want ErrDecrypt", err)
	}
	if _, err := Decrypt(base64.StdEncoding.EncodeToString([]byte("short"))); err != ErrDecrypt {
		t.Fatalf("Decrypt(too short): got %v want ErrDecrypt", err)
	}
}
