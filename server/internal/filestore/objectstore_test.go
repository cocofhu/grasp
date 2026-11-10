package filestore_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/objectstore"
)

type objectBackend struct {
	objectstore.Store
	body    []byte
	info    objectstore.Info
	key     string
	err     error
	started chan struct{}
}

func (b *objectBackend) Head(_ context.Context, key string) (objectstore.Info, error) {
	if b.err != nil {
		return objectstore.Info{}, b.err
	}
	if b.key == "" || key != b.key {
		return objectstore.Info{}, os.ErrNotExist
	}
	return b.info, nil
}

func TestArchiveLegacyPayloadKeepsReferencesAndRefusesCorruptSources(t *testing.T) {
	store, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ref, _, err := store.Put(context.Background(), strings.NewReader("old body"), filestore.Text)
	if err != nil {
		t.Fatal(err)
	}
	_, digest, _ := filestore.Parse(ref)
	path := filepath.Join(store.Root, "sha256", digest[:2], digest)
	backend := &objectBackend{err: errors.New("storage outage")}
	if err := store.SetObjectStore(backend); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ArchiveLocal(context.Background()); err == nil {
		t.Fatal("archive acknowledged failed remote copy")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("failed archive removed source")
	}
	backend.err = nil
	report, err := store.ArchiveLocal(context.Background())
	if err != nil || report.Files != 1 {
		t.Fatalf("%+v %v", report, err)
	}
	if _, err := store.ArchiveLocal(context.Background()); err != nil {
		t.Fatal("repeated archive", err)
	}
	cold, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cold.Close()
	if err := cold.SetObjectStore(backend); err != nil {
		t.Fatal(err)
	}
	body, err := cold.Read(context.Background(), ref)
	if err != nil || string(body) != "old body" {
		t.Fatalf("%q %v", body, err)
	}
	if err := os.WriteFile(path, []byte("corrupted local file"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ArchiveLocal(context.Background()); err == nil {
		t.Fatal("corrupt local payload overwrote verified remote data")
	}
	if string(backend.body) != "old body" {
		t.Fatal("remote data was overwritten")
	}
}

func (b *objectBackend) Put(ctx context.Context, key string, body io.ReadSeeker, info objectstore.Info) error {
	if b.started != nil {
		close(b.started)
		<-ctx.Done()
		return ctx.Err()
	}
	if b.err != nil {
		return b.err
	}
	b.key, b.info = key, info
	var err error
	b.body, err = io.ReadAll(body)
	return err
}

func (b *objectBackend) Open(ctx context.Context, key string) (io.ReadCloser, objectstore.Info, error) {
	if b.err != nil {
		return nil, objectstore.Info{}, b.err
	}
	if key != b.key {
		return nil, objectstore.Info{}, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(b.body)), b.info, nil
}

func TestRemotePayloadIsDurableBeforeReferenceAndVerifiedBeforeRead(t *testing.T) {
	store, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	backend := &objectBackend{}
	if err = store.SetObjectStore(backend); err != nil {
		t.Fatal(err)
	}
	body := "持久化正文\x00\xff"
	ref, _, err := store.Put(context.Background(), strings.NewReader(body), filestore.Text)
	if err != nil {
		t.Fatal(err)
	}
	if string(backend.body) != body || !strings.HasPrefix(backend.key, "payloads/sha256/") {
		t.Fatalf("object not durably stored: key=%s body=%q", backend.key, backend.body)
	}
	local, _ := filepath.Glob(filepath.Join(store.Root, "sha256", "*", "*"))
	if len(local) != 0 {
		t.Fatal("remote write silently persisted only to a local directory")
	}
	file, err := store.Open(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	name := file.Name()
	_, _ = file.Seek(3, io.SeekStart)
	partial, _ := io.ReadAll(file)
	if string(partial) != body[3:] {
		t.Fatalf("range body=%q", partial)
	}
	_ = file.Close()
	if _, err = os.Stat(name); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("remote staging file leaked: %v", err)
	}
	backend.body[0] ^= 0xff
	if _, err = store.Read(context.Background(), ref); err == nil {
		t.Fatal("corrupt remote payload was presented as valid history")
	}
}

func TestRemoteFailureCannotCommitSQLReferenceOrFallbackWrite(t *testing.T) {
	db := fileDB(t)
	store, err := filestore.FromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	legacyRef, _, err := store.Put(context.Background(), strings.NewReader("legacy"), filestore.Text)
	if err != nil {
		t.Fatal(err)
	}
	backend := &objectBackend{err: errors.New("remote unavailable")}
	if err = store.SetObjectStore(backend); err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&models.Artifact{ID: "unacknowledged", Kind: "text", Content: "new remote body"}).Error; err == nil {
		t.Fatal("SQL committed a reference after upload failure")
	}
	var count int64
	db.Model(&models.Artifact{}).Where("id = ?", "unacknowledged").Count(&count)
	if count != 0 {
		t.Fatal("failed upload left an acknowledged row")
	}
	if _, err := store.Read(context.Background(), legacyRef); err == nil {
		t.Fatal("remote failure was concealed by retained local history")
	}
}

func TestRemoteReadNeverUsesRetainedLocalBytes(t *testing.T) {
	ctx := context.Background()
	store, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ref, _, err := store.Put(ctx, strings.NewReader("original"), filestore.Text)
	if err != nil {
		t.Fatal(err)
	}
	_, digest, _ := filestore.Parse(ref)
	path := filepath.Join(store.Root, "sha256", digest[:2], digest)
	backend := &objectBackend{err: os.ErrNotExist}
	if err := store.SetObjectStore(backend); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(ctx, ref); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing primary payload was hidden by a local copy", err)
	}
	if original, err := os.ReadFile(path); err != nil || string(original) != "original" {
		t.Fatal("failed primary read altered the retained source", err)
	}
	backend.err, backend.key, backend.body = nil, "payloads/sha256/"+digest[:2]+"/"+digest, []byte("original")
	backend.info = objectstore.Info{Size: int64(len(backend.body))}
	if err := os.WriteFile(path, []byte("stale or corrupt local copy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, err := store.Read(ctx, ref); err != nil || string(data) != "original" {
		t.Fatal("retained local bytes shadowed the verified primary", err)
	}
	backend.body[0] = 'X'
	if _, err := store.Read(ctx, ref); err == nil {
		t.Fatal("primary checksum failure was hidden by a local copy")
	}
}

func TestCloseCancelsRemotePublication(t *testing.T) {
	store, err := filestore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := &objectBackend{started: make(chan struct{})}
	if err = store.SetObjectStore(backend); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, _, err := store.Put(context.Background(), strings.NewReader("pending"), filestore.Text)
		result <- err
	}()
	<-backend.started
	closed := make(chan struct{})
	go func() { store.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close failed to cancel an in-flight object upload")
	}
	if err = <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("pending publication=%v", err)
	}
	if err = store.SetObjectStore(backend); !errors.Is(err, filestore.ErrClosed) {
		t.Fatalf("closed store reconfigured: %v", err)
	}
}
