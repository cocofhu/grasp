package objectstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"maps"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/google/uuid"
)

const MaxConditionalBytes int64 = 64 << 10

var (
	ErrPrecondition           = errors.New("conditional object write precondition failed")
	ErrConditionalUnsupported = errors.New("object backend does not provide verified conditional writes")
)

// ConditionalStore is an optional control-plane interface; the immutable Store
// interface is unchanged. It deliberately does not require unconditional PUT or
// namespace listing, so a local control namespace need not expose either one.
// ETags are opaque compare tokens, never content hashes or authorization.
type ConditionalStore interface {
	Open(context.Context, string) (io.ReadCloser, Info, error)
	PutIfAbsent(context.Context, string, io.ReadSeeker, Info) (Info, error)
	CompareAndSwap(context.Context, string, string, io.ReadSeeker, Info) (Info, error)
	Delete(context.Context, string) error
}

func validCompareToken(token string) bool {
	return token != "" && len(token) <= 256 && strings.TrimSpace(token) == token && !strings.ContainsFunc(token, unicode.IsControl)
}

func conditionalError(err error) error {
	if err == nil {
		return nil
	}
	var response *smithyhttp.ResponseError
	var api smithy.APIError
	if errors.As(err, &response) {
		switch response.HTTPStatusCode() {
		case http.StatusPreconditionFailed, http.StatusConflict:
			return errors.Join(ErrPrecondition, storageError("conditional put", err))
		case http.StatusNotImplemented, http.StatusMethodNotAllowed:
			return errors.Join(ErrConditionalUnsupported, storageError("conditional put", err))
		}
	}
	if errors.As(err, &api) {
		switch api.ErrorCode() {
		case "PreconditionFailed", "ConditionalRequestConflict":
			return errors.Join(ErrPrecondition, storageError("conditional put", err))
		case "NotImplemented", "UnsupportedOperation":
			return errors.Join(ErrConditionalUnsupported, storageError("conditional put", err))
		}
	}
	return storageError("conditional put", err)
}

func (s *S3) PutIfAbsent(ctx context.Context, key string, body io.ReadSeeker, info Info) (Info, error) {
	return s.putConditional(ctx, key, "", body, info)
}

func (s *S3) CompareAndSwap(ctx context.Context, key, expected string, body io.ReadSeeker, info Info) (Info, error) {
	if !validCompareToken(expected) {
		return Info{}, ErrPrecondition
	}
	return s.putConditional(ctx, key, expected, body, info)
}

func (s *S3) putConditional(ctx context.Context, key, expected string, body io.ReadSeeker, info Info) (Info, error) {
	physical, err := s.key(key)
	if err != nil {
		return Info{}, err
	}
	size, err := body.Seek(0, io.SeekEnd)
	if err != nil {
		return Info{}, err
	}
	if size != info.Size || size < 0 || size > MaxConditionalBytes {
		return Info{}, errors.New("conditional object exceeds its declared control-plane size limit")
	}
	if _, err := body.Seek(0, io.SeekStart); err != nil {
		return Info{}, err
	}
	input := &s3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(physical), Body: body, ContentLength: aws.Int64(size), ContentType: aws.String(info.ContentType), Metadata: maps.Clone(info.Metadata)}
	if expected == "" {
		input.IfNoneMatch = aws.String("*")
	} else {
		input.IfMatch = aws.String(expected)
	}
	// An ambiguous response is reconciled by the caller reading its exact
	// immutable operation body. Do not retry a mutation into a new generation.
	result, err := s.client.PutObject(ctx, input, func(o *s3.Options) { o.RetryMaxAttempts = 1 })
	if err != nil {
		return Info{}, conditionalError(err)
	}
	if !validCompareToken(aws.ToString(result.ETag)) {
		return Info{}, ErrConditionalUnsupported
	}
	info.Key, info.ETag, info.VersionID = key, aws.ToString(result.ETag), aws.ToString(result.VersionId)
	info.Metadata = maps.Clone(info.Metadata)
	return info, nil
}

func conditionalInfo(body []byte) Info {
	sum := sha256.Sum256(body)
	return Info{Size: int64(len(body)), ContentType: "application/json", Metadata: map[string]string{"grasp-sha256": hex.EncodeToString(sum[:])}}
}

// ProbeConditional verifies actual backend semantics with an isolated random
// key. A server that silently ignores conditional headers is rejected. Only the
// probe's own key is removed; no marker or existing namespace object is changed.
func ProbeConditional(ctx context.Context, store ConditionalStore) (err error) {
	if store == nil {
		return ErrConditionalUnsupported
	}
	key := "_conditional-probe/" + uuid.NewString()
	first, second := []byte(`{"probe":"first"}`), []byte(`{"probe":"second"}`)
	created := false
	defer func() {
		if !created {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if cleanupErr := store.Delete(cleanup, key); cleanupErr != nil {
			err = errors.Join(err, errors.New("conditional-write probe cleanup failed"))
		}
	}()
	put, err := store.PutIfAbsent(ctx, key, bytes.NewReader(first), conditionalInfo(first))
	if err != nil {
		return errors.Join(ErrConditionalUnsupported, err)
	}
	created = true
	if !validCompareToken(put.ETag) {
		return ErrConditionalUnsupported
	}
	if _, err := store.PutIfAbsent(ctx, key, bytes.NewReader(second), conditionalInfo(second)); !errors.Is(err, ErrPrecondition) {
		return errors.Join(ErrConditionalUnsupported, err)
	}
	if _, err := store.CompareAndSwap(ctx, key, `"grasp-stale-`+uuid.NewString()+`"`, bytes.NewReader(second), conditionalInfo(second)); !errors.Is(err, ErrPrecondition) {
		return errors.Join(ErrConditionalUnsupported, err)
	}
	stored, head, err := readConditional(ctx, store, key)
	if err != nil || !bytes.Equal(stored, first) || head.ETag != put.ETag {
		return errors.Join(ErrConditionalUnsupported, err)
	}
	type result struct {
		body []byte
		err  error
	}
	results := make(chan result, 2)
	for _, body := range [][]byte{[]byte(`{"probe":"candidate-a"}`), []byte(`{"probe":"candidate-b"}`)} {
		go func() {
			_, err := store.CompareAndSwap(ctx, key, put.ETag, bytes.NewReader(body), conditionalInfo(body))
			results <- result{body: body, err: err}
		}()
	}
	var winner []byte
	rejected := 0
	for range 2 {
		result := <-results // Both context-bound writes are joined, never detached.
		if result.err == nil && winner == nil {
			winner = result.body
		} else if errors.Is(result.err, ErrPrecondition) {
			rejected++
		} else {
			err = errors.Join(err, ErrConditionalUnsupported, result.err)
		}
	}
	if err != nil || winner == nil || rejected != 1 {
		return errors.Join(ErrConditionalUnsupported, err)
	}
	stored, head, err = readConditional(ctx, store, key)
	if err != nil || !bytes.Equal(stored, winner) || head.ETag == put.ETag || !validCompareToken(head.ETag) {
		return errors.Join(ErrConditionalUnsupported, err)
	}
	return nil
}

func readConditional(ctx context.Context, store ConditionalStore, key string) ([]byte, Info, error) {
	body, info, err := store.Open(ctx, key)
	if err != nil {
		return nil, Info{}, err
	}
	defer func() { _ = body.Close() }()
	if info.Size < 0 || info.Size > MaxConditionalBytes {
		return nil, Info{}, ErrConditionalUnsupported
	}
	data, err := io.ReadAll(io.LimitReader(body, MaxConditionalBytes+1))
	if err != nil || int64(len(data)) != info.Size || int64(len(data)) > MaxConditionalBytes {
		return nil, Info{}, errors.Join(ErrConditionalUnsupported, err)
	}
	return data, info, nil
}
