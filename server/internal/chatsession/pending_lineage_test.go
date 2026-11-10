package chatsession

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
)

func TestPendingLineageDoesNotHydrateOrOverwriteQuarantinedTerminalRunReview(t *testing.T) {
	db, _, _, _ := durableFixture(t)
	if err := db.Table("runs").Create(map[string]any{"id": "completed-parent", "status": "completed"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("chat_pending_turns").Create(map[string]any{"id": "dispatched-review", "surface": "workflow.review", "session_key": "completed-parent|producer", "started": true, "payload": "fileref:v1/bytes/" + strings.Repeat("f", 64)}).Error; err != nil {
		t.Fatal(err)
	}
	controller, _, _ := queueController(t, db)
	pending := NewPendingStore(db)
	pending.SetExecutionController(controller)
	loaded, keys, err := pending.Load("workflow.review")
	if err != nil || len(loaded) != 0 || len(keys) != 0 {
		t.Fatal("terminal Run review replayed or read an unavailable old dispatch body", err)
	}
	if err := pending.Clear("workflow.review", "completed-parent|producer"); !errors.Is(err, executionlineage.ErrQuarantined) {
		t.Fatal("background cleanup removed restored pending execution evidence", err)
	}
	var kept int64
	if err := db.Model(&models.ChatPendingTurn{}).Where("id = ?", "dispatched-review").Count(&kept).Error; err != nil || kept != 1 {
		t.Fatal("quarantined pending evidence was lost", err)
	}
	if err := db.Table("runs").Create(map[string]any{"id": "explicit-new-parent", "status": "running"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := pending.Save("workflow.review", "explicit-new-parent|producer", []PendingTurn{{ID: "explicit-new-turn", Payload: []byte(`{"text":"new explicit work"}`)}}); err != nil {
		t.Fatal(err)
	}
	loaded, keys, err = pending.Load("workflow.review")
	if err != nil || len(keys) != 1 || len(loaded[keys[0]]) != 1 || string(loaded[keys[0]][0].Payload) != `{"text":"new explicit work"}` {
		t.Fatal("independent explicit new turn could not be persisted and loaded", err)
	}
}

func TestPendingLineagePreparesOutsideSQLAndCannotPublishAfterRestoreBarrier(t *testing.T) {
	db, store, _, _ := durableFixture(t)
	controller, authority, owner := queueController(t, db)
	if err := db.Table("runs").Create(map[string]any{"id": "manual-run", "status": "running"}).Error; err != nil {
		t.Fatal(err)
	}
	objects := queuePayloadBackend(t, store)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	objects.beforePut = func(ctx context.Context) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	pending := NewPendingStore(db)
	pending.SetExecutionController(controller)
	done := make(chan error, 1)
	go func() {
		done <- pending.Save("workflow.clarify", "manual-run|producer", []PendingTurn{{ID: "late-pending", Payload: []byte("a delayed body")}})
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("pending body upload never started")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := storageguard.Enter(ctx, db, owner, nil); err != nil {
		t.Fatal("body publication held SQL or maintenance locks", err)
	}
	if _, err := authority.BeginRestore(ctx, uuid.NewString(), strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })
	if err := <-done; !errors.Is(err, executionlineage.ErrInactive) {
		t.Fatal("prepared pending body published after external restore barrier", err)
	}
	var count int64
	if err := db.Model(&models.ChatPendingTurn{}).Where("id = ?", "late-pending").Count(&count).Error; err != nil || count != 0 {
		t.Fatal("revoked body left a live SQL reference", err)
	}
}
