package blob

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/objectstore"
)

type attachmentBackend struct {
	objectstore.Store
	key  string
	body []byte
	info objectstore.Info
	err  error
}

func (b *attachmentBackend) Head(_ context.Context, key string) (objectstore.Info, error) {
	if b.err != nil {
		return objectstore.Info{}, b.err
	}
	if b.key == "" || key != b.key {
		return objectstore.Info{}, os.ErrNotExist
	}
	return b.info, nil
}

func TestLegacyAttachmentArchiveRetainsIDAndLocalSource(t *testing.T) {
	legacy, err := NewLocalFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := legacy.Put(context.Background(), strings.NewReader("old image"), Meta{Name: "截图.png", MimeType: "image/png"})
	if err != nil {
		t.Fatal(err)
	}
	backend := &attachmentBackend{}
	objects, _ := NewObjects(backend, legacy, 1000)
	report, err := objects.ArchiveLegacy(context.Background())
	if err != nil || report.Files != 1 {
		t.Fatalf("%+v %v", report, err)
	}
	if _, err := objects.ArchiveLegacy(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(legacy.dirFor(ref.ID()), "blob")); err != nil {
		t.Fatal("archive removed legacy copy")
	}
	cold, _ := NewObjects(backend, nil, 1000)
	body, meta, err := cold.Open(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(body)
	_ = body.Close()
	if string(data) != "old image" || meta.Name != "截图.png" {
		t.Fatal("archive changed attachment identity or metadata")
	}
	backend.err = errors.New("remote failure")
	if _, err := objects.ArchiveLegacy(context.Background()); err == nil {
		t.Fatal("archive ignored backend error")
	}
}

func (b *attachmentBackend) Put(ctx context.Context, key string, body io.ReadSeeker, info objectstore.Info) error {
	if b.err != nil {
		return b.err
	}
	b.key, b.info = key, info
	var err error
	b.body, err = io.ReadAll(body)
	return err
}

func (b *attachmentBackend) Open(context.Context, string) (io.ReadCloser, objectstore.Info, error) {
	if b.err != nil {
		return nil, objectstore.Info{}, b.err
	}
	return io.NopCloser(bytes.NewReader(b.body)), b.info, nil
}

func (b *attachmentBackend) Delete(context.Context, string) error {
	if b.err != nil {
		return b.err
	}
	b.body = nil
	return nil
}

func TestObjectAttachmentPreservesUnicodeMetadataAndRejectsCorruption(t *testing.T) {
	backend := &attachmentBackend{}
	store, err := NewObjects(backend, nil, 1024)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Put(context.Background(), strings.NewReader("binary\x00\xff"), Meta{Name: "需求 截图.png", MimeType: "image/png"})
	if err != nil || ref == "" {
		t.Fatalf("ref=%s err=%v", ref, err)
	}
	reader, info, err := store.Open(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(reader)
	_ = reader.Close()
	if string(body) != "binary\x00\xff" || info.Name != "需求 截图.png" || info.Size != 8 || info.SHA256 == "" {
		t.Fatalf("attachment metadata or bytes lost: %q %+v", body, info)
	}
	backend.body[0] ^= 0xff
	if _, _, err = store.Open(context.Background(), ref); err == nil {
		t.Fatal("corrupt attachment returned to caller")
	}
}

func TestObjectAttachmentFailuresNeverSilentlyWriteOrReadAnotherBackend(t *testing.T) {
	legacy, err := NewLocalFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	old, err := legacy.Put(context.Background(), strings.NewReader("legacy"), Meta{})
	if err != nil {
		t.Fatal(err)
	}
	backend := &attachmentBackend{err: errors.New("forbidden or unavailable")}
	store, err := NewObjects(backend, legacy, 5)
	if err != nil {
		t.Fatal(err)
	}
	if ref, err := store.Put(context.Background(), strings.NewReader("new"), Meta{}); err == nil || ref != "" {
		t.Fatalf("failed upload acknowledged: %s %v", ref, err)
	}
	if _, _, err = store.Open(context.Background(), old); err == nil {
		t.Fatal("remote authorization failure was treated as missing")
	}
	if err = store.Delete(context.Background(), old); err == nil {
		t.Fatal("unavailable remote delete acknowledged")
	}
	reader, _, err := legacy.Open(context.Background(), old)
	if err != nil {
		t.Fatal("failed remote delete removed the legacy copy")
	}
	_ = reader.Close()
	backend.err = os.ErrNotExist
	reader, _, err = store.Open(context.Background(), old)
	if err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
	backend.err = nil
	if _, err = store.Put(context.Background(), strings.NewReader("too large"), Meta{}); err == nil || backend.key != "" {
		t.Fatal("oversized upload reached the object store")
	}
	if _, _, err = store.Open(context.Background(), Ref("blob:../escape")); err == nil {
		t.Fatal("traversal accepted")
	}
}
