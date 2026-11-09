package filestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"strings"
)

// Prepared proves that this Store completed a content-addressed publication.
// Its private fields prevent inbound references from posing as uploaded data.
type Prepared struct {
	store *Store
	ref   string
	size  int64
}

// Verified proves a full bounded read-back of a canonical stored reference.
// Recovery can validate an earlier upload without fabricating a Put proof.
type Verified struct {
	store *Store
	ref   string
	size  int64
}

var ErrNonCanonicalPayload = errors.New("stored payload requires a canonical reference; run the migration release offline")

func (v Verified) Ref() string { return v.ref }
func (v Verified) Size() int64 { return v.size }

func (s *Store) VerifiedRef(v Verified) (string, error) {
	if s.closing.Load() {
		return "", ErrClosed
	}
	if v.store != s || !IsRef(v.ref) || v.size < 0 {
		return "", errors.New("payload was not verified by this storage instance")
	}
	return v.ref, nil
}

func (s *Store) VerifyBounded(ctx context.Context, ref, encoding string, limit int64) (Verified, error) {
	actual, digest, err := Parse(ref)
	if err != nil || actual != encoding || limit < 1 || limit == math.MaxInt64 {
		return Verified{}, errors.New("invalid bounded verification reference or limit")
	}
	body, err := s.OpenBounded(ctx, ref, limit)
	if err != nil {
		return Verified{}, err
	}
	defer func() { _ = body.Close() }()
	h := sha256.New()
	size, err := io.Copy(h, io.LimitReader(contextReader{ctx: ctx, r: body}, limit+1))
	if err != nil {
		return Verified{}, err
	}
	if size > limit || hex.EncodeToString(h.Sum(nil)) != digest {
		return Verified{}, errors.New("stored payload size or checksum mismatch")
	}
	if err := ctx.Err(); err != nil {
		return Verified{}, err
	}
	if s.closing.Load() {
		return Verified{}, ErrClosed
	}
	return Verified{store: s, ref: ref, size: size}, nil
}

// Prepare publishes bytes before the caller opens its SQL transaction.
func (s *Store) Prepare(ctx context.Context, body io.Reader, encoding string) (Prepared, error) {
	ref, size, err := s.Put(ctx, body, encoding)
	if err != nil {
		return Prepared{}, err
	}
	return Prepared{store: s, ref: ref, size: size}, nil
}

func (s *Store) PrepareBounded(ctx context.Context, body io.Reader, encoding string, limit int64) (Prepared, error) {
	ref, size, err := s.PutBounded(ctx, body, encoding, limit)
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
	if s.closing.Load() {
		return "", ErrClosed
	}
	if p.store != s || !IsRef(p.ref) || p.size < 0 {
		return "", errors.New("payload was not prepared by this storage instance")
	}
	return p.ref, nil
}

// Load resolves a raw SQL cell only after its cursor/transaction is released.
// Only canonical references and defined empty values are accepted. A reference
// is resolved once; reference-looking text inside its body remains literal.
func (s *Store) Load(ctx context.Context, raw, encoding string) ([]byte, error) {
	return s.load(ctx, raw, encoding, 0)
}

// LoadBounded applies the same explicit loading and checksum contract while
// bounding retained bytes. Old inline SQL bodies require offline conversion.
func (s *Store) LoadBounded(ctx context.Context, raw, encoding string, limit int64) ([]byte, error) {
	if limit < 1 || limit == math.MaxInt64 {
		return nil, errors.New("invalid stored payload size limit")
	}
	return s.load(ctx, raw, encoding, limit)
}

func (s *Store) load(ctx context.Context, raw, encoding string, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch encoding {
	case Text, JSON, Image, Binary, Bytes:
	default:
		return nil, errors.New("invalid payload owner encoding")
	}
	if !IsRef(raw) {
		if strings.HasPrefix(raw, Prefix) {
			return nil, errors.New("invalid stored file reference")
		}
		if raw != "" && !(encoding == JSON && raw == "null") {
			return nil, ErrNonCanonicalPayload
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
	var body File
	if limit > 0 {
		body, err = s.OpenBounded(ctx, raw, limit)
	} else {
		body, err = s.Open(ctx, raw)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	var reader io.Reader = contextReader{ctx: ctx, r: body}
	if limit > 0 {
		reader = io.LimitReader(reader, limit+1)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if limit > 0 && int64(len(data)) > limit {
		return nil, errors.New("stored payload exceeds size limit")
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != digest {
		return nil, errors.New("file checksum mismatch")
	}
	return data, nil
}
