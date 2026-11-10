package payloadhold

import (
	"context"
	"io"
	"os"
)

// Summary is read-only plan metadata, not a publication or semantic-validation
// proof. The capture service must validate the original runtime manifest and
// archive and bind its private semantic proof to these digests before ready.
type Summary struct {
	ArchiveSHA256, ManifestSHA256 string
	ArchiveBytes, ManifestBytes   int64
	Limits                        Limits
}

func (p Plan) Summary() (Summary, error) {
	if p.data == nil {
		return Summary{}, ErrInvalid
	}
	a, b, limits, err := p.details(p.data.manager)
	if err != nil {
		return Summary{}, err
	}
	return Summary{ArchiveSHA256: a.sha256, ManifestSHA256: b.sha256, ArchiveBytes: a.size, ManifestBytes: b.size, Limits: limits}, nil
}

type planReader struct {
	ctx  context.Context
	file *os.File
}

func (r *planReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.file.Read(p)
}

func (r *planReader) ReadAt(p []byte, offset int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.file.ReadAt(p, offset)
}

func (r *planReader) Seek(offset int64, whence int) (int64, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.file.Seek(offset, whence)
}

func (r *planReader) Close() error { return r.file.Close() }

func (p Plan) open(ctx context.Context, archive bool) (io.ReadSeekCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.data == nil {
		return nil, ErrInvalid
	}
	p.data.mu.Lock()
	defer p.data.mu.Unlock()
	if p.data.closed {
		return nil, ErrInvalid
	}
	path := p.data.manifest.path
	if archive {
		path = p.data.archive.path
	}
	file, err := os.Open(path) // read-only; no paths or mutable handles escape
	if err != nil {
		return nil, err
	}
	return &planReader{ctx: ctx, file: file}, nil
}

func (p Plan) OpenArchive(ctx context.Context) (io.ReadSeekCloser, error) {
	return p.open(ctx, true)
}

func (p Plan) OpenManifest(ctx context.Context) (io.ReadSeekCloser, error) {
	return p.open(ctx, false)
}
