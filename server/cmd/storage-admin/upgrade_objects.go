package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/objectstore"
	"github.com/cocofhu/grasp/internal/payloadstore"
)

func upgradeVerificationObjects(storage *payloadstore.Storage) (objectstore.Store, error) {
	if storage.Objects != nil {
		return storage.Objects, nil // Destination only: never a retained local fallback.
	}
	local, ok := storage.Blobs.(*blob.LocalFS)
	if !ok {
		return nil, errors.New("local upgrade requires a local attachment store")
	}
	return &upgradeLocalObjects{files: storage.Files, blobs: local}, nil
}

// This read-only adapter reuses the production reference-closure verifier for
// the supported local driver. It cannot publish or delete any object.
type upgradeLocalObjects struct {
	files *filestore.Store
	blobs *blob.LocalFS
}

func (s *upgradeLocalObjects) Open(ctx context.Context, key string) (io.ReadCloser, objectstore.Info, error) {
	if strings.HasPrefix(key, "payloads/sha256/") {
		parts := strings.Split(key, "/")
		if len(parts) != 4 || len(parts[2]) != 2 || len(parts[3]) != 64 || parts[2] != parts[3][:2] {
			return nil, objectstore.Info{}, errors.New("invalid local payload key")
		}
		ref := filestore.Prefix + filestore.Binary + "/" + parts[3]
		if !filestore.IsRef(ref) {
			return nil, objectstore.Info{}, errors.New("invalid local payload digest")
		}
		file, err := s.files.Open(ctx, ref)
		if err != nil {
			return nil, objectstore.Info{}, err
		}
		info, err := file.Stat()
		if err != nil {
			_ = file.Close()
			return nil, objectstore.Info{}, err
		}
		return file, objectstore.Info{Key: key, Size: info.Size()}, nil
	}
	if strings.HasPrefix(key, "attachments/") {
		ref, err := blob.ParseRef("blob:" + strings.TrimPrefix(key, "attachments/"))
		if err != nil {
			return nil, objectstore.Info{}, err
		}
		body, meta, err := s.blobs.Open(ctx, ref)
		if err != nil {
			return nil, objectstore.Info{}, err
		}
		raw, err := json.Marshal(meta)
		if err != nil {
			_ = body.Close()
			return nil, objectstore.Info{}, err
		}
		return body, objectstore.Info{Key: key, Size: meta.Size, ContentType: meta.MimeType,
			Metadata: map[string]string{"grasp-meta": base64.StdEncoding.EncodeToString(raw), "grasp-sha256": meta.SHA256}}, nil
	}
	return nil, objectstore.Info{}, errors.New("unsupported local object key")
}

func (*upgradeLocalObjects) Put(context.Context, string, io.ReadSeeker, objectstore.Info) error {
	return errors.New("upgrade verification store is read-only")
}
func (*upgradeLocalObjects) Delete(context.Context, string) error {
	return errors.New("upgrade verification store is read-only")
}
func (*upgradeLocalObjects) List(context.Context, string, string) (objectstore.Page, error) {
	return objectstore.Page{}, errors.New("upgrade verification does not enumerate unknown objects")
}
func (s *upgradeLocalObjects) Head(ctx context.Context, key string) (objectstore.Info, error) {
	body, info, err := s.Open(ctx, key)
	if err == nil {
		err = body.Close()
	}
	return info, err
}
