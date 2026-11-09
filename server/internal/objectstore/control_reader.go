package objectstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
)

// ControlReader deliberately exposes no mutations or CAS capability. An
// observation made with it is not an execution authorization.
type ControlReader interface {
	Open(context.Context, string) (io.ReadCloser, Info, error)
}

type localControlReader struct{ root string }

// NewLocalControlReader neither creates a root nor opens the writer lock. Local
// control publishes a complete envelope by atomic rename, so a reader observes
// one complete inode even when an administrator rotates the marker concurrently.
func NewLocalControlReader(root string) (ControlReader, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("independent local execution control root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &localControlReader{root: abs}, nil
}

func (s *localControlReader) Open(ctx context.Context, key string) (io.ReadCloser, Info, error) {
	if err := ctx.Err(); err != nil {
		return nil, Info{}, err
	}
	path, err := (&LocalControl{root: s.root}).path(key)
	if err != nil {
		return nil, Info{}, err
	}
	body, info, err := readLocalControl(path)
	if err != nil {
		return nil, Info{}, err
	}
	if err := ctx.Err(); err != nil {
		return nil, Info{}, err
	}
	return io.NopCloser(bytes.NewReader(body)), info, nil
}
