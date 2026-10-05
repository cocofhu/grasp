package crypto

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateKeyFileCreatesOnceAndReuses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "secrets.key")
	first, err := LoadOrCreateKeyFile(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(first)
	if err != nil || len(raw) != 32 {
		t.Fatalf("key is not base64 of 32 bytes: %q", first)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %o, want 600", perm)
	}
	second, err := LoadOrCreateKeyFile(path)
	if err != nil || second != first {
		t.Fatalf("second load = %q, %v; want the same key", second, err)
	}
}

func TestLoadOrCreateKeyFileRejectsInvalidKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.key")
	if err := os.WriteFile(path, []byte("not-a-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateKeyFile(path); !errors.Is(err, ErrInvalidSecretsKey) {
		t.Fatalf("err = %v, want ErrInvalidSecretsKey", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "not-a-key\n" {
		t.Fatal("an existing key file must never be rewritten")
	}
}

func TestKeyFileEncryptsWithSetKeySource(t *testing.T) {
	key, err := LoadOrCreateKeyFile(filepath.Join(t.TempDir(), "secrets.key"))
	if err != nil {
		t.Fatal(err)
	}
	SetKeySource(func() string { return key })
	t.Cleanup(func() { SetKeySource(func() string { return os.Getenv(SecretsKeyEnv) }) })
	enc, err := Encrypt("ghp_token")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if got, err := Decrypt(enc); err != nil || got != "ghp_token" {
		t.Fatalf("decrypt = %q, %v", got, err)
	}
}
