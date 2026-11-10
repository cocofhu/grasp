package payloadhold

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
)

func TestPlanSemanticInspectionReusesTheSameReadOnlyStagedBytes(t *testing.T) {
	_, m, objects, _ := holdFixture(t, nil)
	plan := holdPlan(t, m, "the immutable archive")
	summary, err := plan.Summary()
	if err != nil || summary.ArchiveBytes != int64(len("the immutable archive")) {
		t.Fatal("inspection metadata lost its original archive", err)
	}
	archive, err := plan.OpenArchive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = archive.Close() }()
	if _, mutable := archive.(io.Writer); mutable {
		t.Fatal("inspection exposed a mutable staging handle")
	}
	body, err := io.ReadAll(archive)
	if err != nil || string(body) != "the immutable archive" {
		t.Fatal("inspection read changed archive bytes", err)
	}
	if _, err := archive.Seek(4, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	manifest, err := plan.OpenManifest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manifest.Close() }()
	cancel()
	if _, err := manifest.Read(make([]byte, 8)); !errors.Is(err, context.Canceled) {
		t.Fatal("inspection lost its own cancellation boundary")
	}
	stages, err := filepath.Glob(filepath.Join(m.options.StagingDir, "snapshot-plan-*"))
	if err != nil || len(stages) != 2 || objects.puts.Load() != 0 || objects.gets.Load() != 0 {
		t.Fatal("semantic inspection copied or uploaded its archive again")
	}
	if err := plan.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.OpenArchive(context.Background()); !errors.Is(err, ErrInvalid) {
		t.Fatal("closed plan allowed a new inspection reader")
	}
	if _, err := plan.Summary(); !errors.Is(err, ErrInvalid) {
		t.Fatal("closed plan continued to offer new capture validation metadata")
	}
}
