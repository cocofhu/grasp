// Package filestore keeps persistent payload bytes outside SQL. References are
// relative, content addressed keys; callers must authorize the owning record
// before opening one. A reference is never an authorization credential.
package filestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const Prefix = "fileref:v1/"

// Encoding describes the in-memory representation, not the file bytes. Images
// are stored as their original bytes; legacy readers receive base64 on demand.
const (
	Text   = "text"
	JSON   = "json"
	Image  = "image"
	Binary = "binary"
	Bytes  = "bytes"
)

type Store struct {
	Root   string
	mu     sync.Mutex
	closed bool
}

var ErrClosed = errors.New("file payload store is closed")

// Close prevents further namespace mutations, including a late import's
// publication. It waits for any in-progress rename, not for a blocked source
// reader. An already-open temporary file may finish copying but cannot publish.
func (s *Store) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}

func (s *Store) createTemp() (*os.File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	dir := filepath.Join(s.Root, "tmp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return os.CreateTemp(dir, "upload-*")
}

func New(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("file storage root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(abs, 0o700); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Join(abs, "sha256"), 0o700); err != nil {
		return nil, err
	}
	if err = syncDir(abs); err != nil {
		return nil, err
	}
	if err = syncDir(filepath.Dir(abs)); err != nil {
		return nil, err
	}
	return &Store{Root: abs}, nil
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	err = dir.Sync()
	closeErr := dir.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func Parse(ref string) (encoding, digest string, err error) {
	parts := strings.Split(strings.TrimPrefix(ref, Prefix), "/")
	if !strings.HasPrefix(ref, Prefix) || len(parts) != 2 {
		return "", "", errors.New("invalid file reference")
	}
	switch parts[0] {
	case Text, JSON, Image, Binary, Bytes:
	default:
		return "", "", errors.New("invalid file encoding")
	}
	b, e := hex.DecodeString(parts[1])
	if e != nil || len(b) != sha256.Size || parts[1] != strings.ToLower(parts[1]) {
		return "", "", errors.New("invalid file digest")
	}
	return parts[0], parts[1], nil
}

func IsRef(s string) bool { _, _, err := Parse(s); return err == nil }

func (s *Store) path(digest string) string {
	return filepath.Join(s.Root, "sha256", digest[:2], digest)
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

// Put durably publishes immutable bytes before a caller can commit their SQL
// reference. Failed SQL writes may leave an orphan, never a dangling reference.
func (s *Store) Put(ctx context.Context, r io.Reader, encoding string) (ref string, size int64, err error) {
	return s.put(ctx, r, encoding, 0)
}

func (s *Store) PutBounded(ctx context.Context, r io.Reader, encoding string, limit int64) (string, int64, error) {
	if limit < 1 {
		return "", 0, errors.New("invalid file size limit")
	}
	return s.put(ctx, &io.LimitedReader{R: r, N: limit + 1}, encoding, limit)
}

func (s *Store) put(ctx context.Context, r io.Reader, encoding string, limit int64) (ref string, size int64, err error) {
	if _, _, err = Parse(Prefix + encoding + "/" + strings.Repeat("0", 64)); err != nil {
		return "", 0, err
	}
	if err = ctx.Err(); err != nil {
		return "", 0, err
	}
	f, err := s.createTemp()
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	h := sha256.New()
	size, err = io.Copy(io.MultiWriter(f, h), contextReader{ctx, r})
	if err != nil {
		return "", 0, err
	}
	if limit > 0 && size > limit {
		return "", size, fmt.Errorf("file exceeds %d byte limit", limit)
	}
	if err = f.Sync(); err != nil {
		return "", 0, err
	}
	if err = f.Close(); err != nil {
		return "", 0, err
	}
	digest := hex.EncodeToString(h.Sum(nil))
	dest := s.path(digest)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", 0, ErrClosed
	}
	if err = os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return "", 0, err
	}
	if err = syncDir(filepath.Dir(filepath.Dir(dest))); err != nil {
		return "", 0, err
	}
	if err = ctx.Err(); err != nil {
		return "", 0, err
	}
	if err = os.Rename(f.Name(), dest); err != nil {
		return "", 0, err
	}
	dir, err := os.Open(filepath.Dir(dest))
	if err != nil {
		return "", 0, err
	}
	err = dir.Sync()
	closeErr := dir.Close()
	if err != nil {
		return "", 0, err
	}
	if closeErr != nil {
		return "", 0, closeErr
	}
	return Prefix + encoding + "/" + digest, size, nil
}

func (s *Store) Open(ctx context.Context, ref string) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, digest, err := Parse(ref)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(s.path(digest))
	if err != nil {
		return nil, fmt.Errorf("file unavailable (%s): %w", digest, err)
	}
	return f, nil
}

func (s *Store) Read(ctx context.Context, ref string) ([]byte, error) {
	f, err := s.Open(ctx, ref)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(contextReader{ctx, f})
}

func (s *Store) Verify(ctx context.Context, ref string) error {
	_, digest, err := Parse(ref)
	if err != nil {
		return err
	}
	f, err := s.Open(ctx, ref)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err = io.Copy(h, contextReader{ctx, f}); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != digest {
		return errors.New("file checksum mismatch")
	}
	return nil
}
