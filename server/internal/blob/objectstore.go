package blob

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/cocofhu/grasp/internal/objectstore"
	"github.com/google/uuid"
)

// Objects shares the platform object backend with payloads and snapshots.
// Reads and uploads use this configured primary. Retained local source files
// are accessible to explicit archive tools, never a fallback for missing data.
type Objects struct {
	backend objectstore.Store
	legacy  *LocalFS
	limit   int64
}

func NewObjects(backend objectstore.Store, legacy *LocalFS, limit int64) (*Objects, error) {
	if backend == nil {
		return nil, errors.New("object backend is required")
	}
	return &Objects{backend: backend, legacy: legacy, limit: limit}, nil
}

func attachmentKey(ref Ref) string { return "attachments/" + ref.ID() }

func (s *Objects) Put(ctx context.Context, reader io.Reader, meta Meta) (Ref, error) {
	file, err := objectstore.Spool(ctx, reader, s.limit)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	meta.Size, meta.SHA256 = file.Size, file.SHA256
	if meta.MimeType == "" {
		meta.MimeType = "application/octet-stream"
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return "", err
	}
	ref := MakeRef(strings.ReplaceAll(uuid.NewString(), "-", ""))
	if err = s.backend.Put(ctx, attachmentKey(ref), file, objectstore.Info{
		Size: file.Size, ContentType: meta.MimeType,
		Metadata: map[string]string{"grasp-meta": base64.StdEncoding.EncodeToString(raw), "grasp-sha256": file.SHA256},
	}); err != nil {
		return "", err
	}
	return ref, nil
}

func (s *Objects) Open(ctx context.Context, raw Ref) (io.ReadCloser, Meta, error) {
	ref, err := ParseRef(string(raw))
	if err != nil {
		return nil, Meta{}, err
	}
	body, info, err := s.backend.Open(ctx, attachmentKey(ref))
	if err != nil {
		return nil, Meta{}, err
	}
	defer func() { _ = body.Close() }()
	encoded, err := base64.StdEncoding.DecodeString(info.Metadata["grasp-meta"])
	if err != nil {
		return nil, Meta{}, errors.New("attachment metadata is invalid")
	}
	var meta Meta
	if err = json.Unmarshal(encoded, &meta); err != nil || meta.Size != info.Size || meta.SHA256 != info.Metadata["grasp-sha256"] {
		return nil, Meta{}, errors.New("attachment integrity metadata is invalid")
	}
	file, err := objectstore.DownloadVerified(ctx, body, meta.Size, meta.SHA256)
	if err != nil {
		return nil, Meta{}, err
	}
	return file, meta, nil
}

func (s *Objects) Delete(ctx context.Context, raw Ref) error {
	ref, err := ParseRef(string(raw))
	if err != nil {
		return err
	}
	if err = s.backend.Delete(ctx, attachmentKey(ref)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if s.legacy != nil {
		return s.legacy.Delete(ctx, ref)
	}
	return nil
}
