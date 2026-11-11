package solobackup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
)

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type failingReader struct{ err error }

func (f failingReader) Read([]byte) (int, error) { return 0, f.err }

func exercise(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()
	data := bytes.Repeat([]byte("workspace"), 1000)
	obj, err := s.Put(ctx, "sess/a.tar.gz", bytes.NewReader(data))
	if err != nil || obj.Size != int64(len(data)) || obj.SHA256 != sum(data) {
		t.Fatalf("put: %+v %v", obj, err)
	}
	rc, err := s.Open(ctx, "sess/a.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(got, data) {
		t.Fatal("round trip mismatch")
	}
	if _, err := s.Put(ctx, "sess/b.tar.gz", io.MultiReader(bytes.NewReader(data[:10]), failingReader{errors.New("tar failed")})); err == nil {
		t.Fatal("failed stream committed")
	}
	if rc, err := s.Open(ctx, "sess/b.tar.gz"); err == nil {
		_ = rc.Close()
		t.Fatal("failed stream left an object")
	}
	if _, err := s.Put(ctx, "sess/big.tar.gz", bytes.NewReader(make([]byte, 20000))); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("size cap: %v", err)
	}
	if err := s.Delete(ctx, "sess/a.tar.gz"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "sess/a.tar.gz"); err != nil {
		t.Fatalf("delete is not idempotent: %v", err)
	}
	if rc, err := s.Open(ctx, "sess/a.tar.gz"); err == nil {
		_ = rc.Close()
		t.Fatal("deleted object still readable")
	}
	for _, bad := range []string{"", "/abs", "../up", "a/../b", "a\\b", "a\nb"} {
		if _, err := s.Put(ctx, bad, strings.NewReader("x")); err == nil {
			t.Fatalf("key %q accepted", bad)
		}
		if _, err := s.Open(ctx, bad); err == nil {
			t.Fatalf("open %q accepted", bad)
		}
		if err := s.Delete(ctx, bad); err == nil {
			t.Fatalf("delete %q accepted", bad)
		}
	}
}

func TestLocalStore(t *testing.T) {
	root := filepath.Join(t.TempDir(), "backups")
	s, err := New(config.SoloBackupConfig{LocalDir: root, MaxBytes: 10000})
	if err != nil || s.Kind() != KindLocal {
		t.Fatalf("new: %v", err)
	}
	exercise(t, s)
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatalf("partial files or empty session dirs left: %v", entries)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Put(ctx, "c.tar.gz", strings.NewReader("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled put: %v", err)
	}
	if _, err := NewLocal(filepath.Join(root, "c.tar.gz", "x"), 0); err != nil {
		t.Fatalf("nested dir: %v", err)
	}
}

func TestLocalStoreRejectsUnwritableRoot(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewLocal(filepath.Join(file, "sub"), 0); err == nil {
		t.Fatal("root under a file accepted")
	}
}

// fakeS3 implements the path-style PUT/GET/DELETE subset single-part uploads use.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
	deny    bool
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deny {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `<Error><Code>AccessDenied</Code><Message>secret detail</Message></Error>`)
		return
	}
	if r.Header.Get("Authorization") == "" {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodPut:
		b, _ := io.ReadAll(r.Body)
		f.objects[r.URL.Path] = b
		w.Header().Set("ETag", `"x"`)
	case http.MethodGet:
		b, ok := f.objects[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `<Error><Code>NoSuchKey</Code></Error>`)
			return
		}
		_, _ = w.Write(b)
	case http.MethodDelete:
		delete(f.objects, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotImplemented)
	}
}

func TestS3Store(t *testing.T) {
	fake := &fakeS3{objects: map[string][]byte{}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	cfg := config.SoloBackupConfig{MaxBytes: 10000, S3: config.SoloBackupS3{
		Endpoint: srv.URL, Bucket: "bkt", Prefix: "/grasp/", AccessKeyID: "ak", SecretAccessKey: "sk", PathStyle: true,
	}}
	s, err := New(cfg)
	if err != nil || s.Kind() != KindS3 {
		t.Fatalf("new: %v", err)
	}
	exercise(t, s)
	if _, err := s.Put(context.Background(), "sess/c.tar.gz", strings.NewReader("abc")); err != nil {
		t.Fatal(err)
	}
	if _, ok := fake.objects["/bkt/grasp/sess/c.tar.gz"]; !ok {
		t.Fatalf("prefix not applied: %v", fake.objects)
	}
	fake.deny = true
	_, err = s.Put(context.Background(), "sess/d.tar.gz", strings.NewReader("abc"))
	if err == nil || strings.Contains(err.Error(), "secret detail") || !strings.Contains(err.Error(), "403") {
		t.Fatalf("denied error: %v", err)
	}
	if _, err := s.Open(context.Background(), "sess/c.tar.gz"); err == nil {
		t.Fatal("denied open succeeded")
	}
	if err := s.Delete(context.Background(), "sess/c.tar.gz"); err == nil {
		t.Fatal("denied delete succeeded")
	}
	if _, err := NewS3(config.SoloBackupS3{Bucket: "b", Prefix: "../x"}, 0); err == nil {
		t.Fatal("bad prefix accepted")
	}
	if s, err := NewS3(config.SoloBackupS3{Bucket: "b", AccessKeyID: "a", SecretAccessKey: "s"}, 0); err != nil || s.prefix != "" {
		t.Fatalf("default endpoint: %v", err)
	}
}

func TestStorageErrorHidesDetails(t *testing.T) {
	if storageError("x", nil) != nil {
		t.Fatal("nil error wrapped")
	}
	err := storageError("upload", errors.New("https://signed?X-Amz-Signature=secret"))
	if strings.Contains(err.Error(), "secret") || err.Error() != "backup storage upload failed" {
		t.Fatalf("error: %v", err)
	}
}
