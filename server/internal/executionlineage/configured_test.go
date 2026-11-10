package executionlineage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
)

func TestConfiguredControlCannotOverlapSQLPayloadOrBackupIncludingSymlinks(t *testing.T) {
	dir := t.TempDir()
	payload := filepath.Join(dir, "payload")
	if err := os.Mkdir(payload, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(payload, alias); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{payload, dir, filepath.Join(payload, "control"), filepath.Join(alias, "not-yet-created"), filepath.Join(dir, "sql", "control"), filepath.Join(dir, "backup")} {
		if _, err := OpenConfigured(context.Background(), config.StorageConfig{Driver: "local", BlobsRoot: root}, []config.StorageConfig{{BlobsRoot: payload}}, []string{filepath.Join(dir, "sql"), filepath.Join(dir, "backup")}); err == nil {
			t.Fatalf("overlapping control root accepted: %s", root)
		}
	}
	authority, err := OpenConfigured(context.Background(), config.StorageConfig{Driver: "local", BlobsRoot: filepath.Join(dir, "independent")}, []config.StorageConfig{{BlobsRoot: payload}}, []string{filepath.Join(dir, "sql"), filepath.Join(dir, "backup")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authority.Current(context.Background()); !errors.Is(err, ErrUninitialized) {
		t.Fatal("configured startup initialized a missing marker", err)
	}
}

func TestConfiguredS3ControlRejectsFullBucketAndOverlappingEndpointAliases(t *testing.T) {
	var mu sync.Mutex
	objects := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodHead:
			if !objects[r.URL.Path] {
				w.WriteHeader(http.StatusNotFound)
			}
		case http.MethodPut:
			objects[r.URL.Path] = true
			w.Header().Set("ETag", `"fixture"`)
		case http.MethodDelete:
			delete(objects, r.URL.Path)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	pathStyle := true
	payload := config.StorageConfig{Driver: "s3", S3: config.S3StorageConfig{Endpoint: server.URL, Bucket: "test", AccessKeyID: "test", SecretAccessKey: "test", PathStyle: &pathStyle}}
	control := payload
	control.S3.Prefix = "control"
	if _, err := OpenConfigured(context.Background(), control, []config.StorageConfig{payload}, nil); err == nil {
		t.Fatal("control inside the complete payload bucket was accepted")
	}
	control.S3.Endpoint = strings.Replace(server.URL, "127.0.0.1", "localhost", 1)
	if _, err := OpenConfigured(context.Background(), control, []config.StorageConfig{payload}, nil); err == nil {
		t.Fatal("an endpoint alias hid overlapping physical control storage")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(objects) != 0 {
		t.Fatal("separation probe retained its object")
	}
}
