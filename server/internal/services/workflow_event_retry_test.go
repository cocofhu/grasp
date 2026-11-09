package services

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func frozenRetryEvent(t *testing.T, db *gorm.DB, id string, session models.SoloSession) workflowEventRecord {
	t.Helper()
	body, err := json.Marshal(WorkflowEventPayload{RunID: "run-" + id, Title: id, Status: "completed", Artifacts: []WorkflowArtifactRef{{Name: id + ".md", Version: 1, Content: "immutable " + id}}})
	if err != nil {
		t.Fatal(err)
	}
	event := workflowEventRecord{ID: id, RunID: "run-" + id, Version: 1, SoloID: session.ID, UserID: session.UserID, Kind: "completed", Payload: frozenRetryPayload(t, db, string(body)), Disposition: "pending"}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	return event
}

func frozenRetryPayload(t *testing.T, db *gorm.DB, body string) string {
	t.Helper()
	files, err := filestore.FromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := files.Prepare(t.Context(), strings.NewReader(body), filestore.Text)
	if err != nil {
		t.Fatal(err)
	}
	return prepared.Ref()
}

func dueWorkflowEvent(t *testing.T, db *gorm.DB, id string) {
	t.Helper()
	if err := db.Model(&workflowEventRecord{}).Where("id = ?", id).UpdateColumn("next_attempt_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowDeliveryBadHeadDoesNotBlockOtherUsersAndRetriesAfterRestart(t *testing.T) {
	for _, format := range []string{"legacy-null-metadata", "immutable-source"} {
		t.Run(format, func(t *testing.T) {
			db, objects, alice, run := workflowManifestFixture(t)
			bob := models.SoloSession{ID: "healthy-other-user", UserID: "bob", Status: models.SoloStatusStopped}
			if err := db.Create(&bob).Error; err != nil {
				t.Fatal(err)
			}
			var bad workflowEventRecord
			var repair map[string]any
			stage := "prepare_payload"
			if format == "immutable-source" {
				bad = captureManifest(t, db, run.ID)
				repair = map[string]any{"source_count": bad.SourceCount}
				if err := db.Model(&workflowEventRecord{}).Where("id = ?", bad.ID).UpdateColumn("source_count", bad.SourceCount+1).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				bad = frozenRetryEvent(t, db, "bad-legacy-head", alice)
				repair = map[string]any{"payload": bad.Payload}
				// These nullable source columns represent an older migrated v0
				// row. Its retry CAS still has to match the decoded zero values.
				if err := db.Model(&workflowEventRecord{}).Where("id = ?", bad.ID).Updates(map[string]any{"payload": frozenRetryPayload(t, db, "{private-other-user-result"), "source_version": nil, "source_count": nil, "source_fingerprint": nil}).Error; err != nil {
					t.Fatal(err)
				}
				stage = "decode_payload"
			}
			if err := db.Model(&workflowEventRecord{}).Where("id = ?", bad.ID).UpdateColumn("created_at", time.Now().UTC().Add(-time.Hour)).Error; err != nil {
				t.Fatal(err)
			}
			good := frozenRetryEvent(t, db, "healthy-tail", bob)
			solo := NewSoloService(db, nil, nil, nil)
			before := time.Now().UTC()
			if err := solo.DeliverWorkflowEvents(context.Background()); err != nil {
				t.Fatalf("another user's bad result escaped its isolation: %v", err)
			}
			var pending, delivered workflowEventRecord
			if err := db.First(&pending, "id = ?", bad.ID).Error; err != nil || pending.Disposition != "pending" || pending.Attempts != 1 || pending.LastError != stage || pending.NextAttemptAt == nil || pending.NextAttemptAt.Before(before.Add(2*time.Second)) {
				t.Fatalf("bad event was dropped, disclosed, or hammered: %+v %v", pending, err)
			}
			if err := db.First(&delivered, "id = ?", good.ID).Error; err != nil || delivered.Disposition != "delivered" {
				t.Fatalf("bad head blocked another user: %+v %v", delivered, err)
			}
			if err := db.First(&alice, "id = ?", alice.ID).Error; err != nil || alice.WorkflowContextVersion != 0 {
				t.Fatalf("bad result advanced its context: %+v %v", alice, err)
			}
			if err := db.First(&bob, "id = ?", bob.ID).Error; err != nil || bob.WorkflowContextVersion != 1 || bob.Status != models.SoloStatusStopped {
				t.Fatalf("healthy result failed or started compute: %+v %v", bob, err)
			}
			gets, puts := objects.gets.Load(), objects.puts.Load()
			other := NewSoloService(db, nil, nil, nil)
			if err := other.DeliverWorkflowEvents(context.Background()); err != nil || objects.gets.Load() != gets || objects.puts.Load() != puts {
				t.Fatalf("new worker ignored persisted backoff: GET=%d PUT=%d %v", objects.gets.Load()-gets, objects.puts.Load()-puts, err)
			}
			dueWorkflowEvent(t, db, bad.ID)
			before = time.Now().UTC()
			if err := other.DeliverWorkflowEvents(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := db.First(&pending, "id = ?", bad.ID).Error; err != nil || pending.Attempts != 2 || pending.NextAttemptAt == nil || pending.NextAttemptAt.Before(before.Add(4*time.Second)) {
				t.Fatalf("retry did not advance durable backoff: %+v %v", pending, err)
			}
			if err := db.Model(&workflowEventRecord{}).Where("id = ?", bad.ID).Updates(repair).Error; err != nil {
				t.Fatal(err)
			}
			dueWorkflowEvent(t, db, bad.ID)
			if err := other.DeliverWorkflowEvents(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := other.DeliverWorkflowEvents(context.Background()); err != nil {
				t.Fatal(err)
			}
			delivered = workflowEventRecord{}
			if err := db.First(&delivered, "id = ?", bad.ID).Error; err != nil || delivered.Disposition != "delivered" || delivered.Attempts != 2 || delivered.NextAttemptAt != nil || delivered.LastError != "" {
				t.Fatalf("repair did not preserve and finish its original receipt: %+v %v", delivered, err)
			}
			var messages int64
			if err := db.Model(&models.SoloMessage{}).Where("id = ?", "workflow-event-"+bad.ID).Count(&messages).Error; err != nil || messages != 1 {
				t.Fatalf("retry duplicated the stable message: %d %v", messages, err)
			}
			if err := db.First(&alice, "id = ?", alice.ID).Error; err != nil || alice.WorkflowContextVersion != 1 {
				t.Fatalf("retry duplicated conversation context: %+v %v", alice, err)
			}
		})
	}
}

func TestWorkflowDeliveryOrphansAndWrongOwnersNeverLoadMissingSources(t *testing.T) {
	db, objects, session, run := workflowManifestFixture(t)
	sourceEvent := captureManifest(t, db, run.ID)
	if err := NewSoloService(db, nil, nil, nil).Delete(context.Background(), session.ID, session.UserID); err != nil {
		t.Fatal(err) // No environment ever existed in this metadata-only fixture.
	}
	bob := models.SoloSession{ID: "wrong-owner-chat", UserID: "bob", Status: models.SoloStatusStopped}
	if err := db.Create(&bob).Error; err != nil {
		t.Fatal(err)
	}
	wrongOwner := frozenRetryEvent(t, db, "wrong-owner-event", bob)
	legacy := frozenRetryEvent(t, db, "legacy-orphan-event", session)
	missing := filestore.Prefix + "text/" + strings.Repeat("e", 64)
	if err := db.Model(&workflowEventRecord{}).Where("id IN ?", []string{wrongOwner.ID, legacy.ID}).Updates(map[string]any{"payload": missing, "user_id": "alice"}).Error; err != nil {
		t.Fatal(err)
	}
	objects.mu.Lock()
	clear(objects.objects)
	objects.mu.Unlock()
	var opens, puts, sourceReads atomic.Int32
	objects.onOpen = func(context.Context) error {
		opens.Add(1)
		return errors.New("missing orphan source must not be opened")
	}
	objects.onPut = func(context.Context, []byte) error {
		puts.Add(1)
		return errors.New("orphan body must not be prepared")
	}
	name := "test:orphan_events_do_not_read_sources"
	if err := db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "workflow_event_sources" {
			sourceReads.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(name) })
	if err := NewSoloService(db, nil, nil, nil).DeliverWorkflowEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{sourceEvent.ID, wrongOwner.ID, legacy.ID} {
		var event workflowEventRecord
		if err := db.First(&event, "id = ?", id).Error; err != nil || event.Disposition != "orphaned" || event.Attempts != 0 || event.NextAttemptAt != nil {
			t.Fatalf("orphan retained an unresolvable delivery: %+v %v", event, err)
		}
	}
	if opens.Load() != 0 || puts.Load() != 0 || sourceReads.Load() != 0 {
		t.Fatalf("orphan materialized private/unavailable bodies: GET=%d PUT=%d sourceQueries=%d", opens.Load(), puts.Load(), sourceReads.Load())
	}
	var messages int64
	if err := db.Model(&models.SoloMessage{}).Count(&messages).Error; err != nil || messages != 0 {
		t.Fatalf("orphan delivered to a different owner: %d %v", messages, err)
	}
}

func TestWorkflowDeliveryLateFailureCannotDowngradeConcurrentSuccess(t *testing.T) {
	db, objects, session, run := workflowManifestFixture(t)
	event := captureManifest(t, db, run.ID)
	started, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	var gets atomic.Int32
	objects.onOpen = func(ctx context.Context) error {
		if gets.Add(1) != 1 {
			return nil
		}
		close(started)
		select {
		case <-release:
			return errors.New("late worker's private artifact failure")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	first, winner := NewSoloService(db, nil, nil, nil), NewSoloService(db, nil, nil, nil)
	done := make(chan error, 1)
	go func() { done <- first.DeliverWorkflowEvents(context.Background()) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("late materializer never started")
	}
	if err := winner.DeliverWorkflowEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	unblock.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatalf("late failure escaped its individual retry: %v", err)
	}
	if err := db.First(&event, "id = ?", event.ID).Error; err != nil || event.Disposition != "delivered" || event.Attempts != 0 || event.NextAttemptAt != nil || event.LastError != "" {
		t.Fatalf("late error downgraded the winner: %+v %v", event, err)
	}
	if err := db.First(&session, "id = ?", session.ID).Error; err != nil || session.WorkflowContextVersion != 1 {
		t.Fatalf("late error duplicated context: %+v %v", session, err)
	}
	var count int64
	if err := db.Model(&models.SoloMessage{}).Where("id = ?", "workflow-event-"+event.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("late error changed the stable receipt: %d %v", count, err)
	}
}

func TestWorkflowDeliveryConcurrentFailuresOnlyDeferOneAttempt(t *testing.T) {
	db, _, session, _ := workflowManifestFixture(t)
	event := frozenRetryEvent(t, db, "concurrent-failure", session)
	var group sync.WaitGroup
	start := make(chan struct{})
	for range 12 {
		group.Go(func() {
			<-start
			if err := NewSoloService(db, nil, nil, nil).deferWorkflowEvent(context.Background(), event, "prepare_payload"); err != nil {
				t.Error(err)
			}
		})
	}
	close(start)
	group.Wait()
	if err := db.First(&event, "id = ?", event.ID).Error; err != nil || event.Attempts != 1 || event.Disposition != "pending" || event.NextAttemptAt == nil {
		t.Fatalf("replicas amplified a single retry wave: %+v %v", event, err)
	}
}

func TestWorkflowEventRetryDelayIsBounded(t *testing.T) {
	for attempts, want := range map[int]time.Duration{-1: 2 * time.Second, 0: 2 * time.Second, 1: 2 * time.Second, 2: 4 * time.Second, 8: 256 * time.Second, 9: 5 * time.Minute, 1000000: 5 * time.Minute} {
		if got := workflowEventRetryDelay(attempts); got != want {
			t.Fatalf("attempt %d: got %s want %s", attempts, got, want)
		}
	}
}

func TestWorkflowDeliveryMessageStoreFailureDoesNotBlockAnotherUser(t *testing.T) {
	db := newTestDB(t)
	objects := messageObjects(t, db)
	alice := models.SoloSession{ID: "first-message-store", UserID: "alice", Status: models.SoloStatusStopped}
	bob := models.SoloSession{ID: "second-message-store", UserID: "bob", Status: models.SoloStatusStopped}
	if err := db.Create(&[]models.SoloSession{alice, bob}).Error; err != nil {
		t.Fatal(err)
	}
	first := frozenRetryEvent(t, db, "first-storage-failure", alice)
	second := frozenRetryEvent(t, db, "second-storage-success", bob)
	if err := db.Model(&workflowEventRecord{}).Where("id = ?", first.ID).UpdateColumn("created_at", time.Now().UTC().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	var puts atomic.Int32
	objects.onPut = func(context.Context, []byte) error {
		if puts.Add(1) == 1 {
			return errors.New("synthetic private artifact upload failure")
		}
		return nil
	}
	if err := NewSoloService(db, nil, nil, nil).DeliverWorkflowEvents(context.Background()); err != nil {
		t.Fatalf("one failed message store blocked the result worker: %v", err)
	}
	if err := db.First(&first, "id = ?", first.ID).Error; err != nil || first.Disposition != "pending" || first.LastError != "prepare_message" || first.Attempts != 1 {
		t.Fatalf("failed message was acknowledged or not retried: %+v %v", first, err)
	}
	if err := db.First(&second, "id = ?", second.ID).Error; err != nil || second.Disposition != "delivered" {
		t.Fatalf("storage failure blocked a later user: %+v %v", second, err)
	}
}

func TestWorkflowDeliveryBackoffDoesNotFillGlobalBatchWithDeferredHeads(t *testing.T) {
	db := newTestDB(t)
	objects := messageObjects(t, db)
	alice := models.SoloSession{ID: "deferred-backlog", UserID: "alice", Status: models.SoloStatusStopped}
	bob := models.SoloSession{ID: "eligible-tail", UserID: "bob", Status: models.SoloStatusStopped}
	if err := db.Create(&[]models.SoloSession{alice, bob}).Error; err != nil {
		t.Fatal(err)
	}
	future := time.Now().UTC().Add(time.Hour)
	var old []workflowEventRecord
	for i := range 101 {
		id := "backed-off-" + strconv.Itoa(i)
		old = append(old, workflowEventRecord{ID: id, RunID: "run-" + id, Version: 1, SoloID: alice.ID, UserID: alice.UserID, Kind: "completed", Disposition: "pending",
			Payload: filestore.Prefix + "text/" + strings.Repeat("e", 64), Attempts: 3, LastError: "prepare_payload", NextAttemptAt: &future, CreatedAt: future.Add(-2 * time.Hour)})
	}
	if err := db.CreateInBatches(&old, 25).Error; err != nil {
		t.Fatal(err)
	}
	healthy := frozenRetryEvent(t, db, "eligible-after-101-old-failures", bob)
	var gets atomic.Int32
	objects.onOpen = func(context.Context) error {
		gets.Add(1)
		return nil // A due canonical body requires one GET; deferred ones do not.
	}
	if err := NewSoloService(db, nil, nil, nil).DeliverWorkflowEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&healthy, "id = ?", healthy.ID).Error; err != nil || healthy.Disposition != "delivered" || gets.Load() != 1 {
		t.Fatalf("deferred heads occupied the global batch or fetched missing objects: %+v GET=%d %v", healthy, gets.Load(), err)
	}
	var pending int64
	if err := db.Model(&workflowEventRecord{}).Where("disposition = ? AND attempts = ?", "pending", 3).Count(&pending).Error; err != nil || pending != 101 {
		t.Fatalf("deferred receipts were mutated before due: %d %v", pending, err)
	}
}

func TestWorkflowDeliveryRecipientReappearingIsNotOrphanedByStaleCheck(t *testing.T) {
	db := newTestDB(t)
	messageObjects(t, db)
	session := models.SoloSession{ID: "restored-recipient", UserID: "alice", Status: models.SoloStatusStopped}
	event := frozenRetryEvent(t, db, "receipt-before-recipient-restore", session)
	var restored atomic.Bool
	name := "test:restore_recipient_after_missing_observation"
	if err := db.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table != "solo_sessions" || !errors.Is(tx.Error, gorm.ErrRecordNotFound) || !restored.CompareAndSwap(false, true) {
			return
		}
		if err := db.Create(&session).Error; err != nil {
			t.Error(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(name) })
	solo := NewSoloService(db, nil, nil, nil)
	if err := solo.DeliverWorkflowEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&event, "id = ?", event.ID).Error; err != nil || event.Disposition != "pending" || event.Attempts != 0 {
		t.Fatalf("stale missing observation orphaned a valid restored owner: %+v %v", event, err)
	}
	if err := solo.DeliverWorkflowEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&event, "id = ?", event.ID).Error; err != nil || event.Disposition != "delivered" {
		t.Fatalf("valid recipient never received the preserved receipt: %+v %v", event, err)
	}
}

func TestWorkflowDeliveryShutdownDoesNotBackOffHealthyPendingResult(t *testing.T) {
	db, objects, session, run := workflowManifestFixture(t)
	event := captureManifest(t, db, run.ID)
	started := make(chan struct{})
	var reads atomic.Int32
	objects.onOpen = func(ctx context.Context) error {
		if reads.Add(1) != 1 {
			return nil
		}
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- NewSoloService(db, nil, nil, nil).DeliverWorkflowEvents(ctx) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("delivery did not start its body load")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown was mistaken for an event failure: %v", err)
	}
	if err := db.First(&event, "id = ?", event.ID).Error; err != nil || event.Disposition != "pending" || event.Attempts != 0 || event.NextAttemptAt != nil || event.LastError != "" {
		t.Fatalf("shutdown invented a persistent bad-result retry: %+v %v", event, err)
	}
	if err := NewSoloService(db, nil, nil, nil).DeliverWorkflowEvents(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&session, "id = ?", session.ID).Error; err != nil || session.WorkflowContextVersion != 1 {
		t.Fatalf("healthy interrupted result was lost: %+v %v", session, err)
	}
}
