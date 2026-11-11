package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/payloadstore"
	"github.com/cocofhu/grasp/internal/storageguard"
)

type memoryObject struct {
	body        []byte
	contentType string
	metadata    map[string]string
}

// memoryS3 speaks the small S3 subset used by replicas to the real SDK.
type memoryS3 struct {
	mu      sync.Mutex
	objects map[string]memoryObject
}

func (m *memoryS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	query := r.URL.Query()
	if r.Method == http.MethodHead && strings.Count(r.URL.Path, "/") == 1 {
		return
	}
	if query.Get("list-type") == "2" {
		bucket := strings.TrimPrefix(r.URL.Path, "/")
		keys := []string{}
		for key := range m.objects {
			if name, ok := strings.CutPrefix(key, "/"+bucket+"/"); ok && strings.HasPrefix(name, query.Get("prefix")) {
				keys = append(keys, name)
			}
		}
		sort.Strings(keys)
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, "<ListBucketResult><IsTruncated>false</IsTruncated>")
		for _, key := range keys {
			_, _ = fmt.Fprintf(w, "<Contents><Key>%s</Key><Size>%d</Size></Contents>", key, len(m.objects["/"+bucket+"/"+key].body))
		}
		_, _ = io.WriteString(w, "</ListBucketResult>")
		return
	}
	switch r.Method {
	case http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		object := memoryObject{body: body, contentType: r.Header.Get("Content-Type"), metadata: map[string]string{}}
		for key := range r.Header {
			if name, ok := strings.CutPrefix(strings.ToLower(key), "x-amz-meta-"); ok {
				object.metadata[name] = r.Header.Get(key)
			}
		}
		m.objects[r.URL.Path] = object
		w.Header().Set("ETag", `"memory"`)
	case http.MethodGet, http.MethodHead:
		object, ok := m.objects[r.URL.Path]
		if !ok {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, "<Error><Code>NoSuchKey</Code></Error>")
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(object.body)))
		w.Header().Set("Content-Type", object.contentType)
		for key, value := range object.metadata {
			w.Header().Set("X-Amz-Meta-"+key, value)
		}
		if r.Method == http.MethodGet {
			_, _ = w.Write(object.body)
		}
	case http.MethodDelete:
		delete(m.objects, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "unexpected operation", http.StatusBadRequest)
	}
}

func TestFreshCopyWithoutResumeCompletesAndReleasesMaintenance(t *testing.T) {
	for _, initialized := range []bool{false, true} {
		t.Run(fmt.Sprintf("executionInitialized=%v", initialized), func(t *testing.T) {
			opts, cfg := executionOptions(t)
			if err := os.MkdirAll(filepath.Dir(cfg.Database.Path), 0o700); err != nil {
				t.Fatal(err)
			}
			db, err := database.OpenWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
			if err != nil {
				t.Fatal(err)
			}
			files, _ := filestore.FromDB(db)
			ref, _, err := files.Put(t.Context(), strings.NewReader("chat body copied by a fresh replica"), filestore.Text)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Table("solo_messages").Create(map[string]any{"id": "history", "session_id": "owner", "content": ref}).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := payloadstore.Open(t.Context(), db, cfg); err != nil {
				t.Fatal(err)
			}
			if err := database.Close(db); err != nil {
				t.Fatal(err)
			}
			if initialized {
				if err := run(t.Context(), opts, io.Discard); err != nil {
					t.Fatal(err)
				}
			}
			wire := &memoryS3{objects: map[string]memoryObject{}}
			server := httptest.NewServer(wire)
			t.Cleanup(server.Close)
			pathStyle := true
			opts.target = filepath.Join(filepath.Dir(opts.config), "replica.yaml")
			writeYAML(t, opts.target, &config.Config{Storage: config.StorageConfig{Driver: "s3", S3: config.S3StorageConfig{
				Endpoint: server.URL, Bucket: "backup", Prefix: "replica", PathStyle: &pathStyle, AccessKeyID: "synthetic", SecretAccessKey: "synthetic"}}})
			opts.mode, opts.resume = "copy", false
			opts.workdir = filepath.Join(filepath.Dir(opts.config), "replica")
			if err := run(t.Context(), opts, io.Discard); err != nil {
				t.Fatal("fresh copy without --resume failed", err)
			}
			var state checkpoint
			if err := readExecutionJSON(filepath.Join(opts.workdir, "checkpoint.json"), &state); err != nil || state.Phase != "complete" {
				t.Fatal("fresh copy did not complete its checkpoint", state.Phase, err)
			}
			if len(wire.objects) == 0 {
				t.Fatal("fresh copy wrote no replica objects")
			}
			db, err = database.ConnectWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = database.Close(db) }()
			status, err := storageguard.Inspect(t.Context(), db)
			if err != nil || status.MaintenanceID != "" {
				t.Fatal("fresh copy left maintenance active", status, err)
			}
		})
	}
}
