// Package payloadhold provides durable upload reservations and verified pins.
// It copies bounded files; it does not promise a volume-atomic snapshot, capture
// process memory, authorize a sandbox task or enable destructive garbage collection.
package payloadhold

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"sync"
)

// Limits are frozen into each intent. Archive/manifest bytes are enforced here;
// the capture service must validate per-file, expanded-byte and entry limits
// against its trusted runtime manifest before publication.
type Limits struct {
	MaxFileBytes         int64 `json:"maxFileBytes"`
	MaxUncompressedBytes int64 `json:"maxUncompressedBytes"`
	MaxArchiveBytes      int64 `json:"maxArchiveBytes"`
	MaxEntries           int64 `json:"maxEntries"`
	MaxManifestBytes     int64 `json:"maxManifestBytes"`
}

func DefaultLimits() Limits {
	return Limits{MaxFileBytes: 512 << 20, MaxUncompressedBytes: 2 << 30, MaxArchiveBytes: 3 << 30, MaxEntries: 100_000, MaxManifestBytes: 64 << 20}
}

func (l Limits) valid(maximum Limits) bool {
	values := []int64{l.MaxFileBytes, l.MaxUncompressedBytes, l.MaxArchiveBytes, l.MaxEntries, l.MaxManifestBytes}
	maxima := []int64{maximum.MaxFileBytes, maximum.MaxUncompressedBytes, maximum.MaxArchiveBytes, maximum.MaxEntries, maximum.MaxManifestBytes}
	for i, n := range values {
		if n < 1 || n == math.MaxInt64 || n > maxima[i] {
			return false
		}
	}
	return l.MaxArchiveBytes <= math.MaxInt64-l.MaxManifestBytes
}

// Opener must supply the same immutable bytes on each call and unblock on Close.
// The server computes its own size/SHA; neither client hashes nor raw refs are
// accepted as proof. The offset supports runtime spool/range implementations.
type Opener func(context.Context, int64) (io.ReadCloser, error)

type stagedPart struct {
	path   string
	sha256 string
	size   int64
}

type planData struct {
	mu                sync.Mutex
	manager           *Manager
	archive, manifest stagedPart
	limits            Limits
	closed            bool
}

// Plan owns bounded local staging files. Always Close it; the hold persists
// independently. Its private state prevents constructing an upload plan from
// unverified caller-provided hashes or reference strings.
type Plan struct{ data *planData }

func (p Plan) Close() error {
	if p.data == nil {
		return nil
	}
	p.data.mu.Lock()
	defer p.data.mu.Unlock()
	if p.data.closed {
		return nil
	}
	p.data.closed = true
	return errors.Join(os.Remove(p.data.archive.path), os.Remove(p.data.manifest.path))
}

func (p Plan) details(m *Manager) (stagedPart, stagedPart, Limits, error) {
	if p.data == nil || p.data.manager != m {
		return stagedPart{}, stagedPart{}, Limits{}, ErrInvalid
	}
	p.data.mu.Lock()
	defer p.data.mu.Unlock()
	if p.data.closed {
		return stagedPart{}, stagedPart{}, Limits{}, ErrInvalid
	}
	return p.data.archive, p.data.manifest, p.data.limits, nil
}

func stagePart(ctx context.Context, dir string, opener Opener, limit int64) (part stagedPart, err error) {
	if opener == nil {
		return part, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return part, err
	}
	source, err := opener(ctx, 0)
	if err != nil {
		return part, err
	}
	if source == nil {
		return part, ErrInvalid
	}
	// No detached reader goroutine: cancellation closes the supplied transport.
	stop := context.AfterFunc(ctx, func() { _ = source.Close() })
	defer func() { stop(); err = errors.Join(err, source.Close()) }()
	file, err := os.CreateTemp(dir, "snapshot-plan-*")
	if err != nil {
		return part, err
	}
	defer func() {
		_ = file.Close()
		if err != nil {
			_ = os.Remove(file.Name())
		}
	}()
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(&contextReader{ctx: ctx, body: source}, limit+1))
	if err != nil {
		return part, errors.Join(err, ctx.Err())
	}
	if size < 1 || size > limit {
		return part, ErrLimit
	}
	if err := ctx.Err(); err != nil {
		return part, err
	}
	if err := file.Sync(); err != nil {
		return part, err
	}
	if err := file.Close(); err != nil {
		return part, err
	}
	return stagedPart{path: file.Name(), sha256: hex.EncodeToString(hash.Sum(nil)), size: size}, nil
}

type contextReader struct {
	ctx  context.Context
	body io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.body.Read(p)
}

// Stage performs no object or SQL I/O. There is at most one archive staging file
// plus one manifest, and memory use is bounded by the configured manifest limit.
func (m *Manager) Stage(ctx context.Context, archive, manifest Opener, limits Limits) (Plan, error) {
	if err := m.outsideTransaction(); err != nil {
		return Plan{}, err
	}
	if !limits.valid(m.options.Limits) {
		return Plan{}, ErrLimit
	}
	a, err := stagePart(ctx, m.options.StagingDir, archive, limits.MaxArchiveBytes)
	if err != nil {
		return Plan{}, err
	}
	manifestPart, err := stagePart(ctx, m.options.StagingDir, manifest, limits.MaxManifestBytes)
	if err != nil {
		_ = os.Remove(a.path)
		return Plan{}, err
	}
	data := &planData{manager: m, archive: a, manifest: manifestPart, limits: limits}
	plan := Plan{data: data}
	body, err := os.ReadFile(manifestPart.path)
	if err != nil || !json.Valid(body) {
		_ = plan.Close()
		return Plan{}, errors.Join(ErrInvalid, err)
	}
	return plan, nil
}
