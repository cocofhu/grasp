// Package solobackup stores workspace archives of stopped Solo sessions. An
// S3-compatible bucket is used when configured; otherwise a local directory.
package solobackup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"path"
	"strings"
	"unicode"

	"github.com/cocofhu/grasp/internal/config"
)

// Store kinds recorded with each archive.
const (
	KindLocal = "local"
	KindS3    = "s3"
)

// ErrTooLarge rejects an archive above the configured cap.
var ErrTooLarge = errors.New("workspace archive exceeds the backup size limit")

// Object describes a stored archive.
type Object struct {
	Size   int64
	SHA256 string
}

// Store persists one archive per key. Put commits only after the reader ends
// cleanly: a failed or oversized stream leaves no object behind.
type Store interface {
	Kind() string
	Put(ctx context.Context, key string, r io.Reader) (Object, error)
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// New selects S3 when a bucket is configured, else the local directory.
func New(cfg config.SoloBackupConfig) (Store, error) {
	if cfg.S3.Enabled() {
		return NewS3(cfg.S3, cfg.MaxBytes)
	}
	return NewLocal(cfg.LocalDir, cfg.MaxBytes)
}

func validKey(key string) bool {
	if key == "" || path.IsAbs(key) || strings.Contains(key, "\\") || strings.IndexFunc(key, unicode.IsControl) >= 0 {
		return false
	}
	return path.Clean(key) == key && key != "." && key != ".." && !strings.HasPrefix(key, "../")
}

// meter counts and hashes the archive stream and enforces the size cap.
type meter struct {
	r    io.Reader
	max  int64
	n    int64
	hash hash.Hash
}

func newMeter(r io.Reader, max int64) *meter {
	return &meter{r: r, max: max, hash: sha256.New()}
}

func (m *meter) Read(p []byte) (int, error) {
	n, err := m.r.Read(p)
	m.n += int64(n)
	if m.max > 0 && m.n > m.max {
		return 0, ErrTooLarge
	}
	m.hash.Write(p[:n])
	return n, err
}

func (m *meter) object() Object {
	return Object{Size: m.n, SHA256: hex.EncodeToString(m.hash.Sum(nil))}
}

func keyError(key string) error {
	return fmt.Errorf("invalid backup key %q", key)
}
