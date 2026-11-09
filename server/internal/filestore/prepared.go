package filestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
)

// Prepared proves that this Store completed a content-addressed publication.
// Its private fields prevent inbound references from posing as uploaded data.
type Prepared struct {
	store *Store
	ref   string
	size  int64
}

// Prepare publishes bytes before the caller opens its SQL transaction.
func (s *Store) Prepare(ctx context.Context, body io.Reader, encoding string) (Prepared, error) {
	ref, size, err := s.Put(ctx, body, encoding)
	if err != nil {
		return Prepared{}, err
	}
	return Prepared{store: s, ref: ref, size: size}, nil
}

func (p Prepared) Ref() string { return p.ref }
func (p Prepared) Size() int64 { return p.size }

// PreparedRef is a pure proof check: it never takes the publication mutex or
// performs object I/O while its caller holds a database lock.
func (s *Store) PreparedRef(p Prepared) (string, error) {
	if p.store != s || !IsRef(p.ref) || p.size < 0 {
		return "", errors.New("payload was not prepared by this storage instance")
	}
	return p.ref, nil
}

// Load resolves a raw SQL cell only after its cursor/transaction is released.
// Inline legacy values remain byte-for-byte readable. A reference is resolved
// once; literal reference-looking text inside its body remains ordinary data.
func (s *Store) Load(ctx context.Context, raw, encoding string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !IsRef(raw) {
		if strings.HasPrefix(raw, Prefix) {
			return nil, errors.New("invalid stored file reference")
		}
		return []byte(raw), nil
	}
	actual, digest, err := Parse(raw)
	if err != nil {
		return nil, err
	}
	if actual != encoding {
		return nil, errors.New("stored payload encoding disagrees with its owner")
	}
	body, err := s.Open(ctx, raw)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	data, err := io.ReadAll(contextReader{ctx: ctx, r: body})
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != digest {
		return nil, errors.New("file checksum mismatch")
	}
	return data, nil
}
