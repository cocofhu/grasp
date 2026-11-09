package executionlineage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/objectstore"
	"github.com/cocofhu/grasp/internal/storageguard"
)

type changingObservationReader struct {
	marker Marker
	reads  int
}

func (s *changingObservationReader) Open(context.Context, string) (io.ReadCloser, objectstore.Info, error) {
	s.reads++
	marker := s.marker
	if s.reads > 1 {
		marker.Revision++
	}
	raw, _ := json.Marshal(marker)
	info := markerInfo(raw)
	info.ETag = "diagnostic-observation"
	return io.NopCloser(bytes.NewReader(raw)), info, nil
}

func TestObservationRejectsConcurrentMarkerRotationAndStaleSQL(t *testing.T) {
	db, authority, control, owner := authorityFixture(t)
	_, permit := activateFixture(t, db, authority, control, owner)
	if err := storageguard.Leave(context.Background(), db, owner); err != nil {
		t.Fatal(err)
	}
	reader := &changingObservationReader{marker: permit.data.marker}
	view, err := observe(context.Background(), reader, db, Observation{Version: 1, Status: "unavailable"})
	if !errors.Is(err, ErrUnavailable) || view.Active || view.SQLMatches || reader.reads != 2 {
		t.Fatal("concurrent marker change became active status", view, err, reader.reads)
	}
	if err := db.Table("execution_datasets").Where("id = ?", "primary").Update("control_revision", permit.data.marker.Revision+1).Error; err != nil {
		t.Fatal(err)
	}
	reader.reads = 0
	view, err = observe(context.Background(), reader, db, Observation{Version: 1, Status: "unavailable"})
	if !errors.Is(err, ErrUnbound) || view.Status != "unbound" || view.Active || !view.SQLDatasetPresent || reader.reads != 1 {
		t.Fatal("stale SQL binding admitted", view, err)
	}
}

func TestConfiguredObservationPerformsOnlyGETAndNeverProbes(t *testing.T) {
	var gets, mutations atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gets.Add(1)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		mutations.Add(1)
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer server.Close()
	pathStyle := true
	control := config.StorageConfig{Driver: "s3", S3: config.S3StorageConfig{Endpoint: server.URL, Bucket: "fixture", Prefix: "execution-control", AccessKeyID: "test", SecretAccessKey: "test", PathStyle: &pathStyle}}
	payload := control
	payload.S3.Prefix = "application"
	view, err := ObserveConfigured(context.Background(), control, []config.StorageConfig{payload}, nil, nil)
	if !errors.Is(err, ErrUninitialized) || view.Status != "uninitialized" || gets.Load() != 1 || mutations.Load() != 0 {
		t.Fatal(view, err, gets.Load(), mutations.Load())
	}
	payload.S3.Prefix = ""
	if _, err := ObserveConfigured(context.Background(), control, []config.StorageConfig{payload}, nil, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("read-only status attempted to prove overlapping endpoint aliases", err)
	}
	if gets.Load() != 1 || mutations.Load() != 0 {
		t.Fatal("overlap check touched remote storage")
	}
}

func TestObservationMissingLocalRootDoesNotCreateLockAndRejectsOverlap(t *testing.T) {
	dir := t.TempDir()
	control := config.StorageConfig{Driver: "local", BlobsRoot: filepath.Join(dir, "control")}
	payload := config.StorageConfig{Driver: "local", BlobsRoot: filepath.Join(dir, "payload")}
	view, err := ObserveConfigured(context.Background(), control, []config.StorageConfig{payload}, []string{filepath.Join(dir, "sql")}, nil)
	if !errors.Is(err, ErrUninitialized) || view.Status != "uninitialized" {
		t.Fatal(view, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("read-only diagnostic created directories or lock files", entries, err)
	}
	control.BlobsRoot = filepath.Join(payload.BlobsRoot, "nested-control")
	if _, err := ObserveConfigured(context.Background(), control, []config.StorageConfig{payload}, nil, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("overlapping local control accepted", err)
	}
}
