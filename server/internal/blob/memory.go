package blob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// Memory is an in-process Store for transient data.
type Memory struct {
	mu   sync.Mutex
	data map[string]memBlob
}

type memBlob struct {
	raw  []byte
	meta Meta
}

// Keep seeking available for HTTP range requests without copying the stored bytes.
type memoryReader struct{ *bytes.Reader }

func (*memoryReader) Close() error { return nil }

// NewMemory returns an empty memory store.
func NewMemory() *Memory {
	return &Memory{data: map[string]memBlob{}}
}

func (m *Memory) Put(ctx context.Context, r io.Reader, meta Meta) (Ref, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	id := strings.ReplaceAll(uuid.NewString(), "-", "")
	sum := sha256.Sum256(raw)
	meta.Size = int64(len(raw))
	meta.SHA256 = hex.EncodeToString(sum[:])
	if meta.MimeType == "" {
		meta.MimeType = "application/octet-stream"
	}
	m.mu.Lock()
	m.data[id] = memBlob{raw: raw, meta: meta}
	m.mu.Unlock()
	return MakeRef(id), nil
}

func (m *Memory) Open(ctx context.Context, ref Ref) (io.ReadCloser, Meta, error) {
	if err := ctx.Err(); err != nil {
		return nil, Meta{}, err
	}
	parsed, err := ParseRef(string(ref))
	if err != nil {
		return nil, Meta{}, err
	}
	m.mu.Lock()
	b, ok := m.data[parsed.ID()]
	m.mu.Unlock()
	if !ok {
		return nil, Meta{}, fmt.Errorf("blob not found")
	}
	return &memoryReader{bytes.NewReader(b.raw)}, b.meta, nil
}

func (m *Memory) Delete(ctx context.Context, ref Ref) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	parsed, err := ParseRef(string(ref))
	if err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.data, parsed.ID())
	m.mu.Unlock()
	return nil
}
