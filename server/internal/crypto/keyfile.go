package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadOrCreateKeyFile returns the base64 key stored at path, generating a new
// random 32-byte key (file mode 0600) when the file does not exist yet. The
// file must keep the same key across restarts, so an existing file is never
// rewritten; a file holding an invalid key is an error.
func LoadOrCreateKeyFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err == nil {
		key := strings.TrimSpace(string(b))
		if k, decErr := base64.StdEncoding.DecodeString(key); decErr != nil || len(k) != 32 {
			return "", fmt.Errorf("%w: %s", ErrInvalidSecretsKey, path)
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	key := base64.StdEncoding.EncodeToString(raw)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return LoadOrCreateKeyFile(path)
		}
		return "", err
	}
	if _, err := f.WriteString(key + "\n"); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return key, nil
}
