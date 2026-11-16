// Package crypto provides reversible encryption for secrets stored at rest
// (e.g. external channel credentials in the DB). The master key (base64-encoded
// 32 bytes) is supplied by a pluggable key source: by default the
// GRASP_SECRETS_KEY environment variable, but the server wires it to the
// config layer (config: security.secrets_key, env override wins) via
// SetKeySource so it can live in the config file like other sensitive options.
// Treat the key as a fixed salt: keep it stable, since rotating it makes
// existing ciphertext undecryptable.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
)

// SecretsKeyEnv is the environment variable holding the base64 32-byte AES key.
const SecretsKeyEnv = "GRASP_SECRETS_KEY"

// ErrNoSecretsKey is returned when no encryption key is configured.
var ErrNoSecretsKey = errors.New("加密主密钥未配置(config: security.secrets_key 或 GRASP_SECRETS_KEY)")

// ErrInvalidSecretsKey is returned when a key is set but is not valid base64 of
// exactly 32 bytes (AES-256).
var ErrInvalidSecretsKey = errors.New("加密主密钥无效(需 base64 编码的 32 字节密钥；config: security.secrets_key 或 GRASP_SECRETS_KEY)")

// ErrDecrypt is returned when ciphertext cannot be authenticated/decrypted.
var ErrDecrypt = errors.New("密文解密失败(密钥不匹配或数据损坏)")

var (
	keyMu     sync.RWMutex
	keySource = func() string { return os.Getenv(SecretsKeyEnv) }
)

// SetKeySource overrides where the raw base64 key is read from. The server
// points this at the config layer (config file + env override) at boot; tests
// and standalone tools fall back to the GRASP_SECRETS_KEY env var.
func SetKeySource(fn func() string) {
	if fn == nil {
		return
	}
	keyMu.Lock()
	keySource = fn
	keyMu.Unlock()
}

func rawKey() string {
	keyMu.RLock()
	fn := keySource
	keyMu.RUnlock()
	return strings.TrimSpace(fn())
}

func loadKey() ([]byte, error) {
	raw := rawKey()
	if raw == "" {
		return nil, ErrNoSecretsKey
	}
	k, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(k) != 32 {
		return nil, ErrInvalidSecretsKey
	}
	return k, nil
}

// Encrypt returns base64(nonce||ciphertext) using AES-256-GCM.
func Encrypt(plaintext string) (string, error) {
	k, err := loadKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt. Returns ErrDecrypt on tamper/key mismatch.
func Decrypt(enc string) (string, error) {
	k, err := loadKey()
	if err != nil {
		return "", err
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(enc))
	if err != nil {
		return "", ErrDecrypt
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ns := gcm.NonceSize()
	if len(data) < ns {
		return "", ErrDecrypt
	}
	nonce, ct := data[:ns], data[ns:]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", ErrDecrypt
	}
	return string(pt), nil
}
