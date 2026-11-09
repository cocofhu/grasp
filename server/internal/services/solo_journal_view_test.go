package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/objectstore"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type journalViewObject struct {
	body []byte
	info objectstore.Info
}

type journalViewObjects struct {
	objectstore.Store
	mu      sync.Mutex
	objects map[string]journalViewObject
	gets    atomic.Int32
	onGet   func() error
}

func (s *journalViewObjects) Put(_ context.Context, key string, body io.ReadSeeker, info objectstore.Info) error {
	raw, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.objects[key] = journalViewObject{raw, info}
	s.mu.Unlock()
	return nil
}

func (s *journalViewObjects) Open(_ context.Context, key string) (io.ReadCloser, objectstore.Info, error) {
	s.gets.Add(1)
	if s.onGet != nil {
		if err := s.onGet(); err != nil {
			return nil, objectstore.Info{}, err
		}
	}
	s.mu.Lock()
	object, ok := s.objects[key]
	s.mu.Unlock()
	if !ok {
		return nil, objectstore.Info{}, errors.New("journal object unavailable")
	}
	return io.NopCloser(bytes.NewReader(object.body)), object.info, nil
}

func journalViewFixture(t *testing.T) (*SoloService, *journalViewObjects, chatsession.QueueLease, string) {
	t.Helper()
	db := newTestDB(t)
	solo := NewSoloService(db, nil, nil, nil)
	objects := &journalViewObjects{objects: map[string]journalViewObject{}}
	files, err := filestore.FromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := files.SetObjectStore(objects); err != nil {
		t.Fatal(err)
	}
	const id = "journal-view"
	if err := db.Create(&models.SoloSession{ID: id, UserID: "alice", Status: models.SoloStatusRunning}).Error; err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"active", "pending-one", "pending-two", "pending-three"} {
		intent, err := solo.durableSoloIntent(&soloTurn{ID: name, ActorID: "alice", ClientMessageID: "client-" + name, Fingerprint: strings.Repeat("a", 64), Text: strings.Repeat("original body ", 3000)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := solo.durable.Admit(context.Background(), soloQueueScope(id), intent, 0, nil); err != nil {
			t.Fatal(err)
		}
	}
	lease, _, err := solo.durable.Acquire(context.Background(), soloQueueScope(id), uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := solo.durable.Claim(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	return solo, objects, lease, id
}

func appendViewControl(t *testing.T, solo *SoloService, lease chatsession.QueueLease, event string, payload map[string]any) {
	t.Helper()
	if _, err := solo.durable.AppendJournal(context.Background(), lease.Scope, &lease, "active", []chatsession.JournalEntry{{Type: chatsession.FrameSession, Event: event, Payload: payload}}, nil); err != nil {
		t.Fatal(err)
	}
}

func probeViewSQLConnection(t *testing.T, solo *SoloService, objects *journalViewObjects) {
	t.Helper()
	conn, err := solo.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	conn.SetMaxOpenConns(1)
	if err := solo.db.Where("key = ?", "view-get-probe").FirstOrCreate(&models.Setting{Key: "view-get-probe", Value: "0"}).Error; err != nil {
		t.Fatal(err)
	}
	objects.onGet = func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		// With the sole SQLite connection, this cannot succeed while an SQL
		// cursor/transaction is held by the object-read caller.
		return solo.db.WithContext(ctx).Model(&models.Setting{}).Where("key = ?", "view-get-probe").Update("value", "object GET after SQL release").Error
	}
}

func TestSoloJournalViewUsesInlineQueuePreviewsAndLoadsControlsAfterSQLRelease(t *testing.T) {
	solo, objects, lease, id := journalViewFixture(t)
	appendViewControl(t, solo, lease, SoloEventPhase, map[string]any{"phase": SoloPhaseRunning})
	appendViewControl(t, solo, lease, SoloEventExecution, map[string]any{"execution": map[string]any{"modelId": "original-model"}})
	if _, err := solo.durable.AppendJournal(context.Background(), lease.Scope, &lease, "active", []chatsession.JournalEntry{{Type: chatsession.FrameAcp, Data: json.RawMessage(`{"native":"must not be loaded by status"}`)}}, nil); err != nil {
		t.Fatal(err)
	}
	probeViewSQLConnection(t, solo, objects)
	objects.gets.Store(0)
	view, err := solo.journalView(context.Background(), id, "alice")
	if err != nil || objects.gets.Load() != 2 || view.state["phase"] != SoloPhaseRunning || view.state["execution"] == nil || view.state["waiting"] != 3 {
		t.Fatalf("view loaded native/queue bodies or held SQL during GET: gets=%d view=%+v %v", objects.gets.Load(), view, err)
	}
	objects.gets.Store(0)
	for range 20 {
		view, err = solo.journalView(context.Background(), id, "alice", view)
		if err != nil {
			t.Fatal(err)
		}
	}
	if objects.gets.Load() != 0 {
		t.Fatalf("head polling fetched immutable objects again: %d", objects.gets.Load())
	}
	if _, err := solo.journalView(context.Background(), id, "bob"); !errors.Is(err, gorm.ErrRecordNotFound) || objects.gets.Load() != 0 {
		t.Fatalf("unauthorized view touched object storage: gets=%d %v", objects.gets.Load(), err)
	}
}

func TestSoloJournalViewBackfillsLegacyPreviewsOutsideSQLAndCachesPartialControls(t *testing.T) {
	solo, objects, lease, id := journalViewFixture(t)
	if err := solo.db.Model(&models.ChatQueueTurn{}).Where("id IN ?", []string{"active", "pending-two"}).Update("display", "").Error; err != nil {
		t.Fatal(err)
	}
	appendViewControl(t, solo, lease, SoloEventPhase, map[string]any{"phase": SoloPhaseRunning})
	probeViewSQLConnection(t, solo, objects)
	objects.gets.Store(0)
	view, err := solo.journalView(context.Background(), id, "alice")
	if err != nil || objects.gets.Load() != 3 || view.state["phase"] != SoloPhaseRunning {
		t.Fatalf("legacy backfill kept a lock or lost intent: gets=%d view=%+v %v", objects.gets.Load(), view, err)
	}
	objects.gets.Store(0)
	for range 3 {
		view, err = solo.journalView(context.Background(), id, "alice", view)
		if err != nil {
			t.Fatal(err)
		}
	}
	if objects.gets.Load() != 0 {
		t.Fatal("a phase-only view reread its immutable control while waiting for execution metadata")
	}
	objects.onGet = nil // Existing journal writes are outside this read-only test's scope.
	appendViewControl(t, solo, lease, SoloEventExecution, map[string]any{"execution": map[string]any{"modelId": "later-model"}})
	probeViewSQLConnection(t, solo, objects)
	objects.gets.Store(0)
	view, err = solo.journalView(context.Background(), id, "alice", view)
	if err != nil || objects.gets.Load() != 1 || view.state["execution"] == nil {
		t.Fatalf("new control was skipped or old phase reread: gets=%d view=%+v %v", objects.gets.Load(), view, err)
	}
}

func TestSoloJournalViewNeverReusesAnotherExecutionEpochCache(t *testing.T) {
	solo, objects, lease, id := journalViewFixture(t)
	appendViewControl(t, solo, lease, SoloEventPhase, map[string]any{"phase": SoloPhaseRunning})
	appendViewControl(t, solo, lease, SoloEventExecution, map[string]any{"execution": map[string]any{"modelId": "before-takeover"}})
	view, err := solo.journalView(context.Background(), id, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := solo.durable.Abandon(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	next, recovered, err := solo.durable.Acquire(context.Background(), lease.Scope, uuid.NewString())
	if err != nil || recovered == nil || recovered.State != chatsession.TurnQueued {
		t.Fatalf("preparation takeover failed: %+v %v", recovered, err)
	}
	if _, err := solo.durable.Claim(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	appendViewControl(t, solo, next, SoloEventPhase, map[string]any{"phase": SoloPhaseRunning})
	appendViewControl(t, solo, next, SoloEventExecution, map[string]any{"execution": map[string]any{"modelId": "after-takeover"}})
	probeViewSQLConnection(t, solo, objects)
	objects.gets.Store(0)
	updated, err := solo.journalView(context.Background(), id, "alice", view)
	if err != nil || objects.gets.Load() != 2 || updated.generation == view.generation || updated.activeEpoch != next.Generation || updated.state["execution"].(map[string]any)["modelId"] != "after-takeover" {
		t.Fatalf("view reused stale execution metadata: gets=%d view=%+v %v", objects.gets.Load(), updated, err)
	}
}

func TestSoloJournalViewRetainsCapturedHeaderAcrossConcurrentEpochChange(t *testing.T) {
	solo, objects, lease, id := journalViewFixture(t)
	appendViewControl(t, solo, lease, SoloEventPhase, map[string]any{"phase": SoloPhaseRunning})
	appendViewControl(t, solo, lease, SoloEventExecution, map[string]any{"execution": map[string]any{"modelId": "captured-model"}})
	var changed atomic.Bool
	objects.onGet = func() error {
		if !changed.CompareAndSwap(false, true) {
			return nil
		}
		return solo.db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&models.ChatQueue{}).Where("id = ?", lease.Scope.ID()).Update("generation", lease.Generation+1).Error; err != nil {
				return err
			}
			return tx.Model(&models.ChatQueueTurn{}).Where("id = ?", "active").Update("execution_epoch", lease.Generation+1).Error
		})
	}
	view, err := solo.journalView(context.Background(), id, "alice")
	if err != nil || view.generation != lease.Generation || view.activeEpoch != lease.Generation || view.head != 1 || view.state["execution"].(map[string]any)["modelId"] != "captured-model" {
		t.Fatalf("object hydration mixed in a later SQL generation: %+v %v", view, err)
	}
}

func TestSoloJournalViewLoadsExactControlRefsCapturedWithItsHeader(t *testing.T) {
	solo, objects, lease, id := journalViewFixture(t)
	appendViewControl(t, solo, lease, SoloEventPhase, map[string]any{"phase": SoloPhaseRunning})
	appendViewControl(t, solo, lease, SoloEventExecution, map[string]any{"execution": map[string]any{"modelId": "captured-reference"}})
	var changed atomic.Bool
	objects.onGet = func() error {
		if !changed.CompareAndSwap(false, true) {
			return nil
		}
		// Both refs were captured before this first GET. Re-reading the
		// second raw cell here would mix a later edit into the old header.
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		return solo.db.WithContext(ctx).Table("chat_event_chunks").Where("queue_id = ? AND control_event = ?", lease.Scope.ID(), SoloEventExecution).
			Update("payload", filestore.Prefix+"invalid").Error
	}
	view, err := solo.journalView(context.Background(), id, "alice")
	if err != nil || view.head != 1 || view.state["execution"].(map[string]any)["modelId"] != "captured-reference" {
		t.Fatalf("control hydration changed the captured raw reference: %+v %v", view, err)
	}
	if _, err := solo.journalView(context.Background(), id, "alice"); !errors.Is(err, chatsession.ErrJournalGap) {
		t.Fatalf("a fresh snapshot hid the invalid control reference: %v", err)
	}
}

func TestSoloJournalViewRejectsCorruptControlIndexesAndReferences(t *testing.T) {
	for _, corruption := range []string{"wrong_control", "wrong_bytes", "invalid_reference", "unbounded_index"} {
		t.Run(corruption, func(t *testing.T) {
			solo, objects, lease, id := journalViewFixture(t)
			appendViewControl(t, solo, lease, SoloEventPhase, map[string]any{"phase": SoloPhaseRunning})
			query := solo.db.Table("chat_event_chunks").Where("queue_id = ? AND control_event = ?", lease.Scope.ID(), SoloEventPhase)
			var err error
			switch corruption {
			case "wrong_control":
				err = query.Update("control_event", SoloEventExecution).Error
			case "wrong_bytes":
				err = query.Update("bytes", gorm.Expr("bytes + 1")).Error
			case "invalid_reference":
				err = query.Update("payload", filestore.Prefix+"invalid").Error
			case "unbounded_index":
				err = query.Update("bytes", chatsession.JournalChunkBytes+1).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			objects.gets.Store(0)
			view, err := solo.journalView(context.Background(), id, "alice")
			if !errors.Is(err, chatsession.ErrJournalGap) || view.state != nil {
				t.Fatalf("corrupt metadata became a valid view: %+v %v", view, err)
			}
			if (corruption == "invalid_reference" || corruption == "unbounded_index") && objects.gets.Load() != 0 {
				t.Fatal("an invalid reference/index caused an object download")
			}
		})
	}
}

func TestSoloJournalPublicationLocksQueueBeforeSandbox(t *testing.T) {
	solo, sbx, _, db := newSoloFixture(t, 0, 3)
	const id = "journal-lock-order"
	environment := models.Sandbox{Purpose: "solo", ThreadID: id, Status: "running"}
	if err := db.Create(&environment).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SoloSession{ID: id, UserID: "alice", AgentName: "agentA", SandboxID: environment.ID, Status: models.SoloStatusRunning}).Error; err != nil {
		t.Fatal(err)
	}
	ctx, release, err := sbx.acquireSandboxOperation(context.Background(), environment.ID, "inference")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	var queueLocked, sandboxChecked atomic.Bool
	var fenceMu sync.Mutex
	var queueTransaction gorm.ConnPool
	if err := db.Callback().Update().After("gorm:update").Register("test:journal_queue_lock", func(tx *gorm.DB) {
		if tx.Statement.Table == "chat_queues" && tx.Error == nil {
			fenceMu.Lock()
			queueTransaction = tx.Statement.ConnPool
			fenceMu.Unlock()
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().Before("gorm:query").Register("test:journal_lock_order", func(tx *gorm.DB) {
		if tx.Statement.Table != "sandboxes" {
			return
		}
		lock, ok := tx.Statement.Clauses["FOR"].Expression.(clause.Locking)
		if ok && lock.Strength == "UPDATE" {
			sandboxChecked.Store(true)
			fenceMu.Lock()
			matched := queueTransaction == tx.Statement.ConnPool
			fenceMu.Unlock()
			queueLocked.Store(matched)
			if !matched {
				_ = tx.AddError(errors.New("sandbox publication lock taken before the conversation lock"))
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	chat := &soloChat{svc: solo, id: id}
	_, err = chat.appendJournal(ctx, environment.ID, "", []chatsession.JournalEntry{{Type: chatsession.FrameSession, Event: chatsession.EventQueueState}}, false)
	if err != nil || !queueLocked.Load() || !sandboxChecked.Load() {
		t.Fatalf("journal publication violated the stop/admission lock order: queue=%v sandbox=%v %v", queueLocked.Load(), sandboxChecked.Load(), err)
	}
}
