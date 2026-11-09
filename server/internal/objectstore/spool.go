package objectstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sync"
)

// TemporaryFile remains seekable for SDK retries and HTTP Range serving. Close
// also removes the staging file, including on systems that cannot unlink an
// open file. Staging files are never the durable copy of an acknowledged body.
type TemporaryFile struct {
	*os.File
	Size   int64
	SHA256 string
	once   sync.Once
	err    error
}

func (f *TemporaryFile) Close() error {
	f.once.Do(func() {
		f.err = f.File.Close()
		if err := os.Remove(f.Name()); err != nil && !errors.Is(err, os.ErrNotExist) && f.err == nil {
			f.err = err
		}
	})
	return f.err
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}

// Spool bounds memory use and calculates immutable-body metadata before upload.
// A positive limit reads at most one extra byte to detect an oversized source.
func Spool(ctx context.Context, reader io.Reader, limit int64) (_ *TemporaryFile, err error) {
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp("", "grasp-object-*")
	if err != nil {
		return nil, err
	}
	result := &TemporaryFile{File: file}
	defer func() {
		if err != nil {
			_ = result.Close()
		}
	}()
	if limit > 0 && limit < math.MaxInt64 {
		reader = io.LimitReader(reader, limit+1)
	}
	hash := sha256.New()
	result.Size, err = io.Copy(io.MultiWriter(file, hash), contextReader{ctx, reader})
	if err != nil {
		return nil, err
	}
	if limit > 0 && result.Size > limit {
		return nil, fmt.Errorf("object exceeds %d byte limit", limit)
	}
	result.SHA256 = hex.EncodeToString(hash.Sum(nil))
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return result, nil
}

// DownloadVerified refuses corrupt or truncated objects before returning bytes
// to a caller. The original hash is a SQL/reference or immutable metadata value.
func DownloadVerified(ctx context.Context, body io.Reader, size int64, digest string) (*TemporaryFile, error) {
	if size < 0 || size >= math.MaxInt64-1 || len(digest) != sha256.Size*2 {
		return nil, errors.New("object integrity metadata is missing")
	}
	file, err := Spool(ctx, body, size+1)
	if err != nil {
		return nil, err
	}
	if file.Size != size || file.SHA256 != digest {
		_ = file.Close()
		return nil, errors.New("object size or checksum mismatch")
	}
	return file, nil
}
