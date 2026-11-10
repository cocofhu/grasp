package services

import (
	"context"
	"errors"
	"io"
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

type journalPublicationObjects struct {
	*journalViewObjects
	beforePut func(context.Context) error
	puts      atomic.Int32
}

func (s *journalPublicationObjects) Put(ctx context.Context, key string, body io.ReadSeeker, info objectstore.Info) error {
	s.puts.Add(1)
	if s.beforePut != nil {
		if err := s.beforePut(ctx); err != nil {
			return err
		}
	}
	return s.journalViewObjects.Put(ctx, key, body, info)
}

func journalPublicationFixture(t *testing.T) (*soloChat, *journalPublicationObjects, context.Context, models.Sandbox) {
	t.Helper()
	solo, sbx, _, db := newSoloFixture(t, 0, 3)
	id := "journal-publication-" + uuid.NewString()
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
	t.Cleanup(release)
	objects := &journalPublicationObjects{journalViewObjects: &journalViewObjects{objects: map[string]journalViewObject{}}}
	files, err := filestore.FromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := files.SetObjectStore(objects); err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	return &soloChat{svc: solo, id: id}, objects, ctx, environment
}

func TestSoloJournalPreparesBeforeOuterSandboxFenceAndRejectsLostLease(t *testing.T) {
	chat, objects, ctx, environment := journalPublicationFixture(t)
	db := chat.svc.db
	objects.beforePut = func(ctx context.Context) error {
		probe, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		// This uses the same sole SQL connection while PUT is in progress.
		// Neither queue nor sandbox publication locks may be held yet.
		return db.WithContext(probe).Model(&models.Sandbox{}).Where("id = ?", environment.ID).
			Updates(map[string]any{"generation": gorm.Expr("generation + 1"), "operation_owner": "successor", "error": "successor result"}).Error
	}
	written, err := chat.appendJournal(ctx, environment.ID, "", []chatsession.JournalEntry{{Type: chatsession.FrameSession, Event: chatsession.EventQueueState}}, false)
	if !errors.Is(err, errSandboxOperationLost) || written.Count != 0 || objects.puts.Load() != 1 {
		t.Fatalf("preparation held the outer TX or stale publication committed: %+v %v", written, err)
	}
	if head, err := chat.svc.durable.JournalHead(context.Background(), soloQueueScope(chat.id)); err != nil || head != -1 {
		t.Fatalf("superseded sandbox operation advanced the journal: %d %v", head, err)
	}
	if err := db.First(&environment, environment.ID).Error; err != nil || environment.Error != "successor result" {
		t.Fatalf("old publication changed its successor's state: %+v %v", environment, err)
	}
}

func TestSoloJournalCASRetriesOnlyAfterOuterSandboxTransactionRollsBack(t *testing.T) {
	chat, objects, ctx, environment := journalPublicationFixture(t)
	db, durable := chat.svc.db, chat.svc.durable
	if err := db.Create(&models.Setting{Key: "journal-sandbox-cas", Value: "original"}).Error; err != nil {
		t.Fatal(err)
	}
	competing, err := durable.PrepareJournal(ctx, soloQueueScope(chat.id), nil, "", []chatsession.JournalEntry{{Type: chatsession.FrameSession, Event: "competing"}})
	if err != nil {
		t.Fatal(err)
	}
	var sandboxLocks atomic.Int32
	if err := db.Callback().Query().After("gorm:query").Register("test:journal-outer-rollback", func(tx *gorm.DB) {
		lock, ok := tx.Statement.Clauses["FOR"].Expression.(clause.Locking)
		if tx.Statement.Table == "sandboxes" && ok && lock.Strength == "UPDATE" && sandboxLocks.Add(1) == 1 {
			err := tx.Session(&gorm.Session{NewDB: true}).Model(&models.Setting{}).Where("key = ?", "journal-sandbox-cas").Update("value", "uncommitted attempt").Error
			if err != nil {
				_ = tx.AddError(err)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove("test:journal-outer-rollback") })
	var uploads atomic.Int32
	objects.beforePut = func(ctx context.Context) error {
		probe, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if uploads.Add(1) == 1 {
			// Pure SQL proof checks must not take filestore's PUT mutex.
			return db.WithContext(probe).Transaction(func(tx *gorm.DB) error {
				_, err := durable.AppendPreparedJournalTx(probe, tx, competing, nil)
				return err
			})
		}
		var setting models.Setting
		if err := db.WithContext(probe).First(&setting, "key = ?", "journal-sandbox-cas").Error; err != nil {
			return err
		}
		if setting.Value != "original" || sandboxLocks.Load() != 1 {
			return errors.New("retry retained the previous sandbox transaction")
		}
		return nil
	}
	written, err := chat.appendJournal(ctx, environment.ID, "", []chatsession.JournalEntry{{Type: chatsession.FrameSession, Event: "retry"}}, false)
	if err != nil || written.First != 1 || written.Last != 1 || uploads.Load() != 2 || sandboxLocks.Load() != 2 {
		t.Fatalf("head conflict did not roll back/retry the whole fence: %+v uploads=%d sandboxLocks=%d %v", written, uploads.Load(), sandboxLocks.Load(), err)
	}
	page, err := durable.JournalPage(context.Background(), soloQueueScope(chat.id), chatsession.JournalQuery{After: -1})
	if err != nil || len(page.Items) != 2 || page.Items[0].Event != "competing" || page.Items[1].Event != "retry" {
		t.Fatalf("sandbox CAS introduced a gap or changed ordering: %+v %v", page, err)
	}
}

func TestSoloJournalDeadlineCoversPreparationAndSandboxPublication(t *testing.T) {
	chat, objects, held, environment := journalPublicationFixture(t)
	ctx, cancel := context.WithTimeout(held, 50*time.Millisecond)
	defer cancel()
	objects.beforePut = func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}
	begin := time.Now()
	written, err := chat.appendJournal(ctx, environment.ID, "", []chatsession.JournalEntry{{Type: chatsession.FrameSession, Event: "blocked"}}, false)
	if !errors.Is(err, context.DeadlineExceeded) || written.Count != 0 || time.Since(begin) > time.Second {
		t.Fatalf("preparation lost the caller's earlier deadline: %+v %s %v", written, time.Since(begin), err)
	}
	if head, err := chat.svc.durable.JournalHead(context.Background(), soloQueueScope(chat.id)); err != nil || head != -1 {
		t.Fatalf("timed-out body advanced the cursor: %d %v", head, err)
	}
}
