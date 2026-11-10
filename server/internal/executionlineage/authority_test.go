package executionlineage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/objectstore"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type controlCalls struct {
	objectstore.ConditionalStore
	inSQL      atomic.Bool
	gets, puts atomic.Int32
}

func (s *controlCalls) Open(ctx context.Context, key string) (io.ReadCloser, objectstore.Info, error) {
	s.gets.Add(1)
	if s.inSQL.Load() {
		return nil, objectstore.Info{}, errors.New("external GET while SQL transaction was held")
	}
	return s.ConditionalStore.Open(ctx, key)
}

func (s *controlCalls) PutIfAbsent(ctx context.Context, key string, body io.ReadSeeker, info objectstore.Info) (objectstore.Info, error) {
	s.puts.Add(1)
	if s.inSQL.Load() {
		return objectstore.Info{}, errors.New("external PUT while SQL transaction was held")
	}
	return s.ConditionalStore.PutIfAbsent(ctx, key, body, info)
}

func (s *controlCalls) CompareAndSwap(ctx context.Context, key, expected string, body io.ReadSeeker, info objectstore.Info) (objectstore.Info, error) {
	s.puts.Add(1)
	if s.inSQL.Load() {
		return objectstore.Info{}, errors.New("external CAS while SQL transaction was held")
	}
	return s.ConditionalStore.CompareAndSwap(ctx, key, expected, body, info)
}

func authorityFixture(t *testing.T) (*gorm.DB, *Authority, *controlCalls, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := database.OpenWithFiles(config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(dir, "history.db")}, filepath.Join(dir, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	local, err := objectstore.NewLocalControl(filepath.Join(dir, "independent-control"))
	if err != nil {
		t.Fatal(err)
	}
	control := &controlCalls{ConditionalStore: local}
	authority, err := Open(context.Background(), control)
	if err != nil {
		t.Fatal(err)
	}
	owner := uuid.NewString()
	if err := storageguard.Enter(context.Background(), db, owner, nil); err != nil {
		t.Fatal(err)
	}
	if err := EnsureRecoverySchema(context.Background(), db, owner); err != nil {
		t.Fatal(err)
	}
	return db, authority, control, owner
}

func commitQuarantine(t *testing.T, db *gorm.DB, a *Authority, control *controlCalls, owner string, ticket Ticket) models.ExecutionDataset {
	t.Helper()
	var binding models.ExecutionDataset
	if err := db.Transaction(func(tx *gorm.DB) error {
		control.inSQL.Store(true)
		defer control.inSQL.Store(false)
		var err error
		binding, err = a.QuarantineTx(tx, owner, ticket)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return binding
}

func activateFixture(t *testing.T, db *gorm.DB, a *Authority, control *controlCalls, owner string) (Ticket, Permit) {
	t.Helper()
	ticket, err := a.Initialize(context.Background(), uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	commitQuarantine(t, db, a, control, owner, ticket)
	permit, err := a.Activate(context.Background(), db, owner, ticket)
	if err != nil {
		t.Fatal(err)
	}
	return ticket, permit
}

func TestExecutionAuthorityStartupNeverInitializesOrActivatesHistory(t *testing.T) {
	db, a, control, owner := authorityFixture(t)
	if _, err := a.Current(context.Background()); !errors.Is(err, ErrUninitialized) {
		t.Fatal("startup implicitly created an active lineage", err)
	}
	ticket, err := a.Initialize(context.Background(), uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Current(context.Background()); !errors.Is(err, ErrInactive) {
		t.Fatal("explicit init granted execution before SQL binding", err)
	}
	if _, err := a.Activate(context.Background(), db, owner, ticket); !errors.Is(err, ErrUnbound) {
		t.Fatal("missing SQL binding activated independent authority", err)
	}
	rollback := errors.New("offline SQL commit failed")
	if err := db.Transaction(func(tx *gorm.DB) error {
		control.inSQL.Store(true)
		defer control.inSQL.Store(false)
		if _, err := a.QuarantineTx(tx, owner, ticket); err != nil {
			return err
		}
		if _, err := a.Activate(context.Background(), tx, owner, ticket); !errors.Is(err, ErrTransaction) {
			return errors.New("uncommitted SQL transaction reached external activation")
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if _, err := a.Activate(context.Background(), db, owner, ticket); !errors.Is(err, ErrUnbound) {
		t.Fatal("rolled-back binding activated a lineage", err)
	}
	commitQuarantine(t, db, a, control, owner, ticket)
	permit, err := a.Activate(context.Background(), db, owner, ticket)
	if err != nil || permit.DatasetID() == "" || permit.Lineage() == "" {
		t.Fatal("committed offline adoption did not activate", err)
	}
	if err := a.CheckBinding(context.Background(), db, permit); err != nil {
		t.Fatal(err)
	}
	for _, forged := range []TicketDocument{{}, {Version: 1, Marker: Marker{State: Restoring}}} {
		if _, err := a.Resume(context.Background(), forged); err == nil {
			t.Fatal("user JSON constructed a private recovery capability")
		}
	}
}

type forbiddenPayloadObjects struct {
	objectstore.Store
	gets, puts atomic.Int32
}

func (s *forbiddenPayloadObjects) Open(context.Context, string) (io.ReadCloser, objectstore.Info, error) {
	s.gets.Add(1)
	return nil, objectstore.Info{}, errors.New("payload backend unavailable")
}

func (s *forbiddenPayloadObjects) Put(context.Context, string, io.ReadSeeker, objectstore.Info) error {
	s.puts.Add(1)
	return errors.New("payload backend unavailable")
}

func TestExecutionQuarantineCoversEveryPendingSourceWithoutPayloadIO(t *testing.T) {
	db, a, control, owner := authorityFixture(t)
	files, _ := filestore.FromDB(db)
	objects := &forbiddenPayloadObjects{}
	if err := files.SetObjectStore(objects); err != nil {
		t.Fatal(err)
	}
	ref := filestore.Prefix + filestore.Text + "/" + strings.Repeat("a", 64)
	rows := []struct {
		table  string
		values map[string]any
	}{
		{"runs", map[string]any{"id": "run", "status": "queued", "title": "history readable", "checkpoints": ref}},
		{"state_runs", map[string]any{"id": 1, "run_id": "run", "status": "running", "output_md": ref}},
		{"chat_queues", map[string]any{"id": "queue", "surface": "solo", "session_key": "solo", "active_turn_id": "turn"}},
		{"chat_queue_turns", map[string]any{"id": "turn", "queue_id": "queue", "state": "unknown", "payload": ref}},
		{"chat_pending_turns", map[string]any{"id": "legacy", "surface": "legacy", "session_key": "solo", "started": true, "payload": ref}},
		{"solo_sessions", map[string]any{"id": "solo", "status": "creating"}},
		{"chat_turn_drafts", map[string]any{"id": "draft", "thread_id": "pm", "status": "streaming", "partial_text": ref}},
		{"agent_cron_jobs", map[string]any{"id": "cron", "enabled": true}},
		{"agent_cron_runs", map[string]any{"id": "cron-run", "job_id": "cron", "status": ""}},
		{"sandboxes", map[string]any{"id": 1, "name": "old-native-environment", "status": "running"}},
	}
	for _, row := range rows {
		if err := db.Table(row.table).Create(row.values).Error; err != nil {
			t.Fatal(row.table, err)
		}
	}
	ticket, err := a.Initialize(context.Background(), uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	beforeGets, beforePuts := control.gets.Load(), control.puts.Load()
	binding := commitQuarantine(t, db, a, control, owner, ticket)
	if binding.QuarantineCount != int64(len(rows)) || control.gets.Load() != beforeGets || control.puts.Load() != beforePuts || objects.gets.Load() != 0 || objects.puts.Load() != 0 {
		t.Fatalf("offline metadata freeze skipped sources or performed locked object I/O: %+v", binding)
	}
	permit, err := a.Activate(context.Background(), db, owner, ticket)
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range quarantineSpecs {
		if !db.Migrator().HasTable(spec.table) {
			continue // Later stack layers add their own source tables and fixtures.
		}
		var review models.ExecutionQuarantine
		if err := db.Where("entity_kind = ?", string(spec.kind)).Take(&review).Error; err != nil {
			t.Fatal("pending source escaped quarantine", spec.kind, err)
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			control.inSQL.Store(true)
			defer control.inSQL.Store(false)
			return a.GuardTx(tx, permit, spec.kind, review.EntityID)
		}); !errors.Is(err, ErrQuarantined) {
			t.Fatal("restored source was executable without review", spec.kind, err)
		}
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return a.GuardTx(tx, permit, EntityRun, "a-new-explicit-request") }); err != nil {
		t.Fatal("fresh explicit identity could not use the activated dataset", err)
	}
	var historical struct{ Title, Status string }
	if err := db.Table("runs").Select("title", "status").Where("id = ?", "run").Take(&historical).Error; err != nil || historical.Status != "queued" || historical.Title != "history readable" {
		t.Fatal("quarantine rewrote execution states or removed readable history", err)
	}
}

func TestExecutionRestoreRotatesBeforeSQLAndRejectsRolledBackDataset(t *testing.T) {
	db, a, control, owner := authorityFixture(t)
	_, oldPermit := activateFixture(t, db, a, control, owner)
	var oldBinding models.ExecutionDataset
	if err := db.Take(&oldBinding, "id = ?", "primary").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("runs").Create(map[string]any{"id": "old-pending-run", "status": "queued"}).Error; err != nil {
		t.Fatal(err)
	}
	restore, err := a.BeginRestore(context.Background(), uuid.NewString(), strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Current(context.Background()); !errors.Is(err, ErrInactive) {
		t.Fatal("restore imported SQL before revoking active execution", err)
	}
	if err := a.Verify(context.Background(), oldPermit.Lineage()); !errors.Is(err, ErrInactive) {
		t.Fatal("old remote job authority survived the recovery barrier", err)
	}
	if _, err := a.Activate(context.Background(), db, owner, restore); !errors.Is(err, ErrUnbound) {
		t.Fatal("restored SQL was activated without a new dataset/quarantine", err)
	}
	commitQuarantine(t, db, a, control, owner, restore)
	fresh, err := a.Activate(context.Background(), db, owner, restore)
	if err != nil || fresh.Lineage() == oldPermit.Lineage() || fresh.DatasetID() == oldPermit.DatasetID() {
		t.Fatal("recovery reused an old execution identity", err)
	}
	if err := a.Verify(context.Background(), oldPermit.Lineage()); !errors.Is(err, ErrConflict) {
		t.Fatal("old-lineage dispatched job became valid after SQL recovery", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return a.GuardTx(tx, fresh, EntityRun, "old-pending-run") }); !errors.Is(err, ErrQuarantined) {
		t.Fatal("restored queued work auto-replayed", err)
	}
	if err := db.Save(&oldBinding).Error; err != nil {
		t.Fatal(err)
	}
	current, err := a.ReadActive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.CheckBinding(context.Background(), db, current); !errors.Is(err, ErrUnbound) {
		t.Fatal("SQL rollback rolled back independent execution authority", err)
	}
	if _, err := a.Resume(context.Background(), restore.Document()); err != nil {
		t.Fatal("completed operation's durable ticket lost its exact activated child", err)
	}
}

func TestExecutionConcurrentRestoreHasOneExternalWinnerAndCannotForgeTicket(t *testing.T) {
	db, a, control, owner := authorityFixture(t)
	activateFixture(t, db, a, control, owner)
	var wg sync.WaitGroup
	tickets := make(chan Ticket, 2)
	for range 2 {
		wg.Go(func() {
			ticket, err := a.BeginRestore(context.Background(), uuid.NewString(), strings.Repeat("c", 64))
			if err == nil {
				tickets <- ticket
			}
		})
	}
	wg.Wait()
	close(tickets)
	var winners []Ticket
	for ticket := range tickets {
		winners = append(winners, ticket)
	}
	if len(winners) != 1 {
		t.Fatal("concurrent restores created multiple lineages", len(winners))
	}
	document := winners[0].Document()
	document.Marker.DatasetID = uuid.NewString()
	if _, err := a.Resume(context.Background(), document); !errors.Is(err, ErrConflict) {
		t.Fatal("caller-supplied document forged a recovery identity", err)
	}
	if _, err := a.Activate(context.Background(), db, owner, Ticket{}); !errors.Is(err, ErrConflict) {
		t.Fatal("zero ticket activated a dataset", err)
	}
	if _, err := a.BeginRestore(context.Background(), uuid.NewString(), strings.Repeat("d", 64)); !errors.Is(err, ErrInactive) {
		t.Fatal("new operation stole the existing inactive recovery barrier", err)
	}
}

func TestExecutionQuarantinePagesNumericAndStringKeysAndDetectsLostReviewRows(t *testing.T) {
	db, a, control, owner := authorityFixture(t)
	for i := range 513 {
		if err := db.Table("runs").Create(map[string]any{"id": fmt.Sprintf("run-%05d", i), "status": "queued"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Table("state_runs").Create(map[string]any{"id": i + 1, "status": "running"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	ticket, err := a.Initialize(context.Background(), uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	binding := commitQuarantine(t, db, a, control, owner, ticket)
	if binding.QuarantineCount != 1026 {
		t.Fatal("paged metadata freeze skipped numeric or string source IDs", binding.QuarantineCount)
	}
	var review models.ExecutionQuarantine
	if err := db.Take(&review).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&review).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := a.Activate(context.Background(), db, owner, ticket); !errors.Is(err, ErrUnbound) {
		t.Fatal("activation ignored a lost committed quarantine row", err)
	}
}

func TestExecutionPermitCannotBeForgedReusedAfterExpiryOrMovedBetweenAuthorities(t *testing.T) {
	db, a, control, owner := authorityFixture(t)
	_, permit := activateFixture(t, db, a, control, owner)
	other := &Authority{store: a.store}
	if err := other.CheckBinding(context.Background(), db, permit); !errors.Is(err, ErrUnavailable) {
		t.Fatal("a foreign authority accepted another process-local permit", err)
	}
	if err := a.CheckBinding(context.Background(), db, Permit{}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("an empty permit authorized SQL execution", err)
	}
	permit.data.expiresAt = time.Now().Add(-time.Second)
	if err := db.Transaction(func(tx *gorm.DB) error { return a.GuardTx(tx, permit, "", "") }); !errors.Is(err, ErrUnavailable) {
		t.Fatal("a long-held permit revived itself after expiry", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.ReadActive(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("authority lost the native caller's cancellation", err)
	}
}
