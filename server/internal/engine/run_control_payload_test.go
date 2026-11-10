package engine

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
)

func controlPayloadFixture(t *testing.T, status string) (*Engine, *instructionPayloadObjects, *execCtx) {
	t.Helper()
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	e := &Engine{db: db, broker: NewBroker(), wake: make(chan struct{}, 1), execRuns: map[string]bool{}, execGen: map[string]uint64{}}
	objects := instructionObjects(t, db)
	missingJSON := filestore.Prefix + filestore.JSON + "/" + strings.Repeat("0", 64)
	if err := db.Table("runs").Create(map[string]any{"id": "control-run", "status": status, "origin_solo_id": "control-parent", "created_by": "alice",
		"attempt": 4, "control_version": 0, "inputs": missingJSON, "first_message": missingJSON, "checkpoints": "{}"}).Error; err != nil {
		t.Fatal(err)
	}
	gen, ok := e.beginExecute("control-run")
	if !ok {
		t.Fatal("could not claim isolated driver")
	}
	c := &execCtx{run: &models.Run{ID: "control-run", Attempt: 4, Checkpoints: map[string]map[string]any{"prior": {"value": "before upload"}}}, execGen: gen}
	return e, objects, c
}

func TestRunControlPauseResumeUsesOnlyMetadataWithUnavailableBodies(t *testing.T) {
	for _, status := range []string{"queued", "running", "waiting_human"} {
		t.Run(status, func(t *testing.T) {
			e, objects, _ := controlPayloadFixture(t, status)
			objects.onOpen = func(context.Context, string, int32) error { return errors.New("offline GET") }
			objects.onPut = func(context.Context, string) error { return errors.New("offline PUT") }
			if err := e.PauseRun("control-run", 0); err != nil {
				t.Fatalf("pause loaded Run payloads under its row lock: %v", err)
			}
			if err := e.ResumePausedRun("control-run", 1); err != nil {
				t.Fatalf("resume loaded Run payloads under its row lock: %v", err)
			}
			if objects.gets.Load() != 0 || objects.puts.Load() != 0 {
				t.Fatalf("control depends on payload backend: GET=%d PUT=%d", objects.gets.Load(), objects.puts.Load())
			}
			var counts []struct{ SourceCount int }
			if err := e.db.Table("workflow_conversation_events").Find(&counts).Error; err != nil || len(counts) == 0 {
				t.Fatalf("control lost its atomic progress event: %+v %v", counts, err)
			}
			for _, event := range counts {
				if event.SourceCount != 1 {
					t.Fatalf("control copied full result sources: %+v", counts)
				}
			}
		})
	}
}

func TestRunControlCheckpointPreparesBeforeSQLAndFencesConcurrentChanges(t *testing.T) {
	for _, change := range []string{"none", "attempt", "withdraw", "new-pause", "driver"} {
		t.Run(change, func(t *testing.T) {
			e, objects, c := controlPayloadFixture(t, "running")
			if err := e.db.Table("runs").Where("id = ?", c.run.ID).Updates(map[string]any{"pause_requested": true, "control_version": 1}).Error; err != nil {
				t.Fatal(err)
			}
			started, release := make(chan struct{}), make(chan struct{})
			var unblock sync.Once
			t.Cleanup(func() { unblock.Do(func() { close(release) }) })
			objects.onPut = func(ctx context.Context, _ string) error {
				// Marshal has already frozen these bytes. Later in-memory changes
				// must not alter the raw reference published by this operation.
				c.run.Checkpoints["prior"]["value"] = "changed after prepare"
				close(started)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			type result struct {
				parked bool
				err    error
			}
			done := make(chan result, 1)
			go func() {
				parked, err := e.parkRunBoundary(c, "next")
				done <- result{parked, err}
			}()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("checkpoint upload did not start")
			}
			probe, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var err error
			switch change {
			case "attempt":
				err = e.db.WithContext(probe).Table("runs").Where("id = ?", c.run.ID).UpdateColumn("attempt", 5).Error
			case "withdraw":
				err = e.ResumePausedRun(c.run.ID, 1)
			case "new-pause":
				err = e.db.WithContext(probe).Table("runs").Where("id = ?", c.run.ID).UpdateColumn("control_version", 3).Error
			case "driver":
				e.forceEndExecute(c.run.ID)
				_, _ = e.beginExecute(c.run.ID)
			default:
				err = e.db.WithContext(probe).Create(&models.Setting{Key: "checkpoint-sql-probe", Value: "free during upload"}).Error
			}
			if err != nil {
				t.Fatalf("checkpoint PUT held Run/SQL lock: %v", err)
			}
			unblock.Do(func() { close(release) })
			got := <-done
			if change == "none" {
				if !got.parked || got.err != nil {
					t.Fatalf("unchanged boundary did not publish: %+v", got)
				}
				var raw struct{ Checkpoints string }
				if err := e.db.Table("runs").Select("checkpoints").Where("id = ?", c.run.ID).Take(&raw).Error; err != nil || !filestore.IsRef(raw.Checkpoints) {
					t.Fatalf("checkpoint was not published as an explicit ref: %+v %v", raw, err)
				}
				files, _ := filestore.FromDB(e.db)
				body, err := files.Load(context.Background(), raw.Checkpoints, filestore.JSON)
				if err != nil || !strings.Contains(string(body), "before upload") || strings.Contains(string(body), "changed after prepare") || !strings.Contains(string(body), "next") {
					t.Fatalf("checkpoint did not retain the immutable prepared bytes: %s %v", body, err)
				}
			} else if change == "withdraw" {
				if got.parked || got.err != nil {
					t.Fatalf("proven withdrawal stopped its same-generation owner: %+v", got)
				}
			} else if got.parked || !errors.Is(got.err, errRunBoundaryChanged) || e.requeueFailedRunBoundary(c, "next", got.err) {
				t.Fatalf("stale boundary was replayed or requeued: %+v", got)
			}
		})
	}
}

func TestRunControlBoundaryIOFailureRequeuesOnlyOriginalGeneration(t *testing.T) {
	for _, change := range []string{"none", "attempt", "control", "driver", "deadline"} {
		t.Run(change, func(t *testing.T) {
			e, objects, c := controlPayloadFixture(t, "running")
			if err := e.db.Table("runs").Where("id = ?", c.run.ID).Updates(map[string]any{"pause_requested": true, "control_version": 1}).Error; err != nil {
				t.Fatal(err)
			}
			injected := errors.New("first checkpoint PUT failed")
			var puts atomic.Int32
			objects.onPut = func(context.Context, string) error {
				if puts.Add(1) == 1 {
					return injected
				}
				return nil
			}
			parked, failure := e.parkRunBoundary(c, "next")
			if parked || !errors.Is(failure, injected) {
				t.Fatalf("expected checkpoint publication failure: %v %v", parked, failure)
			}
			switch change {
			case "attempt":
				if err := e.db.Table("runs").Where("id = ?", c.run.ID).UpdateColumn("attempt", 5).Error; err != nil {
					t.Fatal(err)
				}
			case "control":
				if err := e.db.Table("runs").Where("id = ?", c.run.ID).UpdateColumn("control_version", 2).Error; err != nil {
					t.Fatal(err)
				}
			case "driver":
				e.forceEndExecute(c.run.ID)
				_, _ = e.beginExecute(c.run.ID)
			case "deadline":
				var captured *runBoundaryFailure
				if !errors.As(failure, &captured) {
					t.Fatal("failure lost the original deadline")
				}
				captured.deadline = time.Now().Add(-time.Second)
			}
			queued := e.requeueFailedRunBoundary(c, "next", failure)
			if queued != (change == "none") {
				t.Fatalf("I/O fallback adopted a different boundary: change=%s queued=%v", change, queued)
			}
			var current struct{ Status, Checkpoints string }
			if err := e.db.Table("runs").Select("status", "checkpoints").Where("id = ?", c.run.ID).Take(&current).Error; err != nil {
				t.Fatal(err)
			}
			if change == "none" {
				if current.Status != "queued" || !filestore.IsRef(current.Checkpoints) {
					t.Fatalf("safe fallback did not preserve a durable continuation: %+v", current)
				}
			} else if current.Status != "running" || current.Checkpoints != "{}" || puts.Load() != 1 {
				t.Fatalf("stale fallback rewrote the new generation: %+v PUT=%d", current, puts.Load())
			}
		})
	}
}
