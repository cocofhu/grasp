package objectstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// LocalControl is a small, independent control namespace. Its root MUST be
// outside SQL/payload backup and restore paths. It exposes no unconditional
// PUT, listing, or payload-store migration; startup never creates a marker.
type LocalControl struct{ root string }

const controlLockName = ".control.lock"

func NewLocalControl(root string) (*LocalControl, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("independent local execution control root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, err
	}
	if err := syncControlDir(filepath.Dir(abs)); err != nil {
		return nil, err
	}
	return &LocalControl{root: abs}, nil
}

func syncControlDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}

func (s *LocalControl) path(key string) (string, error) {
	if !validKey(key, false) {
		return "", errors.New("invalid control object key")
	}
	for _, part := range strings.Split(key, "/") {
		if part == controlLockName || strings.HasPrefix(part, ".pending-") {
			return "", errors.New("reserved control object key")
		}
	}
	return filepath.Join(s.root, filepath.FromSlash(key)), nil
}

// lock uses one persistent inode across processes. It is never unlinked after
// release. Every metadata operation is bounded and honors a waiting context.
func (s *LocalControl) lock(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(s.root, controlLockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			if err := ctx.Err(); err != nil {
				_ = file.Close()
				return nil, err
			}
			return func() { _ = file.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = file.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

type controlEnvelope struct {
	Version int    `json:"version"`
	Body    []byte `json:"body"`
	Info    Info   `json:"info"`
}

func readLocalControl(path string) ([]byte, Info, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, Info{}, err
	}
	defer func() { _ = file.Close() }()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() > 3*MaxConditionalBytes {
		return nil, Info{}, errors.Join(ErrConditionalUnsupported, err)
	}
	raw, err := io.ReadAll(io.LimitReader(file, 3*MaxConditionalBytes+1))
	if err != nil {
		return nil, Info{}, err
	}
	var envelope controlEnvelope
	if json.Unmarshal(raw, &envelope) != nil || envelope.Version != 1 || int64(len(envelope.Body)) != envelope.Info.Size || envelope.Info.Size < 0 || envelope.Info.Size > MaxConditionalBytes {
		return nil, Info{}, errors.New("invalid independent control object")
	}
	sum := sha256.Sum256(raw)
	envelope.Info.ETag = `"` + hex.EncodeToString(sum[:]) + `"`
	return envelope.Body, envelope.Info, nil
}

func (s *LocalControl) Open(ctx context.Context, key string) (io.ReadCloser, Info, error) {
	path, err := s.path(key)
	if err != nil {
		return nil, Info{}, err
	}
	unlock, err := s.lock(ctx)
	if err != nil {
		return nil, Info{}, err
	}
	defer unlock()
	body, info, err := readLocalControl(path)
	if err != nil {
		return nil, Info{}, err
	}
	return io.NopCloser(bytes.NewReader(body)), info, nil
}

func (s *LocalControl) PutIfAbsent(ctx context.Context, key string, reader io.ReadSeeker, info Info) (Info, error) {
	return s.swap(ctx, key, "", reader, info)
}

func (s *LocalControl) CompareAndSwap(ctx context.Context, key, expected string, reader io.ReadSeeker, info Info) (Info, error) {
	if !validCompareToken(expected) {
		return Info{}, ErrPrecondition
	}
	return s.swap(ctx, key, expected, reader, info)
}

func (s *LocalControl) swap(ctx context.Context, key, expected string, reader io.ReadSeeker, info Info) (Info, error) {
	path, err := s.path(key)
	if err != nil {
		return Info{}, err
	}
	if info.Size < 0 || info.Size > MaxConditionalBytes {
		return Info{}, ErrConditionalUnsupported
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return Info{}, err
	}
	body, err := io.ReadAll(io.LimitReader(reader, MaxConditionalBytes+1))
	if err != nil || int64(len(body)) != info.Size {
		return Info{}, errors.Join(ErrConditionalUnsupported, err)
	}
	unlock, err := s.lock(ctx)
	if err != nil {
		return Info{}, err
	}
	defer unlock()
	_, current, err := readLocalControl(path)
	if expected == "" {
		if !errors.Is(err, os.ErrNotExist) {
			if err == nil {
				return Info{}, ErrPrecondition
			}
			return Info{}, err
		}
	} else if err != nil || current.ETag != expected {
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return Info{}, err
		}
		return Info{}, ErrPrecondition
	}
	info.Key, info.ETag, info.VersionID = key, "", ""
	info.Metadata = maps.Clone(info.Metadata)
	raw, err := json.Marshal(controlEnvelope{Version: 1, Body: body, Info: info})
	if err != nil || int64(len(raw)) > 3*MaxConditionalBytes {
		return Info{}, errors.Join(ErrConditionalUnsupported, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Info{}, err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".pending-*") // O_EXCL
	if err != nil {
		return Info{}, err
	}
	defer func() { _ = file.Close(); _ = os.Remove(file.Name()) }()
	if _, err := file.Write(raw); err != nil {
		return Info{}, err
	}
	if err := file.Sync(); err != nil {
		return Info{}, err
	}
	if err := file.Close(); err != nil {
		return Info{}, err
	}
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return Info{}, err
	}
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		if err := syncControlDir(dir); err != nil {
			return Info{}, err
		}
		if dir == s.root {
			break
		}
	}
	sum := sha256.Sum256(raw)
	info.ETag = `"` + hex.EncodeToString(sum[:]) + `"`
	return info, nil
}

func (s *LocalControl) Delete(ctx context.Context, key string) error {
	path, err := s.path(key)
	if err != nil {
		return err
	}
	unlock, err := s.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return syncControlDir(filepath.Dir(path))
}
