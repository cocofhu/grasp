package blob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// LocalFS stores blobs under Root as <aa>/<id>/blob + meta.json.
type LocalFS struct {
	Root string
}

// NewLocalFS creates the root directory if needed.
func NewLocalFS(root string) (*LocalFS, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("blobs root required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &LocalFS{Root: root}, nil
}

func (s *LocalFS) dirFor(id string) string {
	prefix := id
	if len(prefix) >= 2 {
		prefix = id[:2]
	}
	return filepath.Join(s.Root, prefix, id)
}

func (s *LocalFS) Put(ctx context.Context, r io.Reader, meta Meta) (Ref, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	id := strings.ReplaceAll(uuid.NewString(), "-", "")
	dir := s.dirFor(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	blobPath := filepath.Join(dir, "blob")
	f, err := os.OpenFile(blobPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), r)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.RemoveAll(dir)
		return "", copyErr
	}
	if closeErr != nil {
		_ = os.RemoveAll(dir)
		return "", closeErr
	}
	meta.Size = n
	meta.SHA256 = hex.EncodeToString(h.Sum(nil))
	if meta.MimeType == "" {
		meta.MimeType = "application/octet-stream"
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), raw, 0o644); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return MakeRef(id), nil
}

func (s *LocalFS) Open(ctx context.Context, ref Ref) (io.ReadCloser, Meta, error) {
	if err := ctx.Err(); err != nil {
		return nil, Meta{}, err
	}
	parsed, err := ParseRef(string(ref))
	if err != nil {
		return nil, Meta{}, err
	}
	dir := s.dirFor(parsed.ID())
	metaRaw, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return nil, Meta{}, err
	}
	var meta Meta
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		return nil, Meta{}, err
	}
	f, err := os.Open(filepath.Join(dir, "blob"))
	if err != nil {
		return nil, Meta{}, err
	}
	return f, meta, nil
}

func (s *LocalFS) Delete(ctx context.Context, ref Ref) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	parsed, err := ParseRef(string(ref))
	if err != nil {
		return err
	}
	return os.RemoveAll(s.dirFor(parsed.ID()))
}
