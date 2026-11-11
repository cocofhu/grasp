package solobackup

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Local keeps archives under one directory on the server host.
type Local struct {
	root string
	max  int64
}

// NewLocal creates root when missing.
func NewLocal(root string, max int64) (*Local, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &Local{root: root, max: max}, nil
}

func (l *Local) Kind() string { return KindLocal }

func (l *Local) file(key string) (string, error) {
	if !validKey(key) {
		return "", keyError(key)
	}
	return filepath.Join(l.root, filepath.FromSlash(key)), nil
}

func (l *Local) Put(ctx context.Context, key string, r io.Reader) (Object, error) {
	dst, err := l.file(key)
	if err != nil {
		return Object{}, err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return Object{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".partial-*")
	if err != nil {
		return Object{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	m := newMeter(r, l.max)
	if _, err := io.Copy(tmp, contextReader{ctx: ctx, r: m}); err != nil {
		return Object{}, err
	}
	if err := tmp.Sync(); err != nil {
		return Object{}, err
	}
	if err := tmp.Close(); err != nil {
		return Object{}, err
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return Object{}, err
	}
	committed = true
	return m.object(), nil
}

func (l *Local) Open(_ context.Context, key string) (io.ReadCloser, error) {
	f, err := l.file(key)
	if err != nil {
		return nil, err
	}
	return os.Open(f)
}

func (l *Local) Delete(_ context.Context, key string) error {
	f, err := l.file(key)
	if err != nil {
		return err
	}
	if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if dir := filepath.Dir(f); dir != filepath.Clean(l.root) {
		_ = os.Remove(dir) // drop the per-session directory once empty
	}
	return nil
}

// contextReader stops a local copy when the request is cancelled.
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (c contextReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
