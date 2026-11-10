package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/objectstore"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type instructionPayloadObjects struct {
	mu      sync.Mutex
	objects map[string][]byte
	puts    atomic.Int32
	gets    atomic.Int32
	onPut   func(context.Context, string) error
	onOpen  func(context.Context, string, int32) error
}

func (s *instructionPayloadObjects) Put(ctx context.Context, key string, body io.ReadSeeker, _ objectstore.Info) error {
	s.puts.Add(1)
	if s.onPut != nil {
		if err := s.onPut(ctx, key); err != nil {
			return err
		}
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.objects[key] = data
	s.mu.Unlock()
	return nil
}

func (s *instructionPayloadObjects) Open(ctx context.Context, key string) (io.ReadCloser, objectstore.Info, error) {
	read := s.gets.Add(1)
	if s.onOpen != nil {
		if err := s.onOpen(ctx, key, read); err != nil {
			return nil, objectstore.Info{}, err
		}
	}
	s.mu.Lock()
	data, ok := s.objects[key]
	s.mu.Unlock()
	if !ok {
		return nil, objectstore.Info{}, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(data)), objectstore.Info{Key: key, Size: int64(len(data))}, nil
}

func (s *instructionPayloadObjects) Head(_ context.Context, key string) (objectstore.Info, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[key]
	if !ok {
		return objectstore.Info{}, os.ErrNotExist
	}
	return objectstore.Info{Key: key, Size: int64(len(data))}, nil
}

func (s *instructionPayloadObjects) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	delete(s.objects, key)
	s.mu.Unlock()
	return nil
}

func (s *instructionPayloadObjects) List(context.Context, string, string) (objectstore.Page, error) {
	return objectstore.Page{}, errors.New("list is not used by instruction storage")
}

func instructionObjects(t *testing.T, db *gorm.DB) *instructionPayloadObjects {
	t.Helper()
	objects := &instructionPayloadObjects{objects: map[string][]byte{}}
	files, err := filestore.FromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := files.SetObjectStore(objects); err != nil {
		t.Fatal(err)
	}
	return objects
}

func instructionSQLProbe(ctx context.Context, db *gorm.DB, key string) error {
	probe, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	return db.WithContext(probe).Model(&models.WorkflowInstructionMailbox{}).Where("id > 0").
		UpdateColumn("revision", gorm.Expr("revision + 1")).Error
}

func TestWorkflowInstructionRawSchemaMatchesLegacyWithoutFileHooks(t *testing.T) {
	e, _ := instructionFixture(t)
	legacy, raw := &gorm.Statement{DB: e.db}, &gorm.Statement{DB: e.db}
	if err := legacy.Parse(&models.WorkflowInstruction{}); err != nil {
		t.Fatal(err)
	}
	if err := raw.Parse(&instructionRecord{}); err != nil {
		t.Fatal(err)
	}
	for _, field := range legacy.Schema.Fields {
		if field.DBName != "" && raw.Schema.LookUpField(field.DBName) == nil {
			t.Fatalf("explicit instruction record loses column %s", field.DBName)
		}
	}
	for _, field := range raw.Schema.Fields {
		if field.DBName != "" && (legacy.Schema.LookUpField(field.DBName) == nil || strings.HasPrefix(field.TagSettings["SERIALIZER"], "file")) {
			t.Fatalf("explicit instruction record adds a column or payload hook: %s", field.DBName)
		}
	}
}

func TestWorkflowInstructionUploadDoesNotHoldMailboxOrSQLConnection(t *testing.T) {
	e, q := instructionFixture(t)
	objects := instructionObjects(t, e.db)
	started, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	objects.onPut = func(ctx context.Context, _ string) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	input := instructionInput(q, "upload before locking the mailbox")
	done := make(chan error, 1)
	go func() {
		_, err := e.EnqueueInstruction(context.Background(), "instruction-run", "impl", "alice", input)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("upload did not start")
	}
	probe, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := q.close(probe, "execution stopped while upload was pending"); err != nil {
		t.Fatalf("PUT held the mailbox/SQL connection: %v", err)
	}
	unblock.Do(func() { close(release) })
	var typed *InstructionError
	if err := <-done; !errors.As(err, &typed) || typed.Code != "instruction_closed" {
		t.Fatalf("late upload was admitted into the closed mailbox: %v", err)
	}
	var count int64
	if err := e.db.Model(&models.WorkflowInstruction{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rejected upload published a SQL receipt: count=%d err=%v", count, err)
	}
	var box models.WorkflowInstructionMailbox
	if err := e.db.First(&box, q.mailboxID).Error; err != nil || box.NextSequence != 0 || box.State != "sealed" {
		t.Fatalf("rejected upload changed admission sequence: %+v %v", box, err)
	}
}

func TestWorkflowInstructionSQLFailureLeavesOnlyVerifiableOrphan(t *testing.T) {
	e, q := instructionFixture(t)
	objects := instructionObjects(t, e.db)
	objects.onPut = func(ctx context.Context, key string) error { return instructionSQLProbe(ctx, e.db, key) }
	injected := errors.New("instruction SQL insert unavailable")
	var fail atomic.Bool
	fail.Store(true)
	const callback = "instruction_test_insert_failure"
	if err := e.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "workflow_instructions" && fail.Load() {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.db.Callback().Create().Remove(callback) })
	input := instructionInput(q, "original intent survives SQL retry")
	if item, err := e.EnqueueInstruction(context.Background(), "instruction-run", "impl", "alice", input); !errors.Is(err, injected) || item != nil {
		t.Fatalf("SQL failure pretended to accept: %+v %v", item, err)
	}
	var count int64
	if err := e.db.Model(&models.WorkflowInstruction{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rollback left a dangling receipt: %d %v", count, err)
	}
	objects.mu.Lock()
	orphans := len(objects.objects)
	objects.mu.Unlock()
	if orphans != 1 {
		t.Fatalf("expected one content-addressed orphan, got %d", orphans)
	}
	fail.Store(false)
	item, err := e.EnqueueInstruction(context.Background(), "instruction-run", "impl", "alice", input)
	if err != nil || item.Text != input.Text || item.Sequence != 1 {
		t.Fatalf("retry did not retain the original intent/sequence: %+v %v", item, err)
	}
	var raw instructionRecord
	if err := e.db.First(&raw, "id = ?", item.ID).Error; err != nil || !filestore.IsRef(raw.Text) {
		t.Fatalf("new receipt is not an explicit raw ref: %+v %v", raw, err)
	}
	objects.onOpen = func(ctx context.Context, key string, _ int32) error { return instructionSQLProbe(ctx, e.db, key) }
	dispatch, err := q.Next(context.Background(), false)
	if err != nil || dispatch == nil || dispatch.Text != input.Text || dispatch.ID != item.ID || objects.gets.Load() != 1 {
		t.Fatalf("claim failed to hydrate exactly once outside SQL: %+v gets=%d %v", dispatch, objects.gets.Load(), err)
	}
}

func TestWorkflowInstructionRetryRecoversReceiptWithoutObjectIO(t *testing.T) {
	e, q := instructionFixture(t)
	objects := instructionObjects(t, e.db)
	input := instructionInput(q, "\n  exact accepted requirement  \n")
	accepted, err := e.EnqueueInstruction(context.Background(), "instruction-run", "impl", "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Next(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	objects.onPut = func(context.Context, string) error { return errors.New("offline PUT") }
	objects.onOpen = func(context.Context, string, int32) error { return errors.New("offline GET") }
	puts, gets := objects.puts.Load(), objects.gets.Load()
	if err := q.RestartAttempt(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := q.close(context.Background(), "closed"); err != nil {
		t.Fatal(err)
	}
	item, err := e.EnqueueInstruction(context.Background(), "instruction-run", "impl", "alice", input)
	if err != nil || item.ID != accepted.ID || item.State != "unknown" || item.Text != strings.TrimSpace(input.Text) {
		t.Fatalf("lost ACK did not recover its original unknown receipt: %+v %v", item, err)
	}
	for _, changed := range []struct {
		actor string
		input InstructionInput
	}{{"bob", input}, {"alice", InstructionInput{Iteration: 2, Generation: input.Generation, ClientMessageID: input.ClientMessageID, Text: input.Text}},
		{"alice", InstructionInput{Iteration: 1, Generation: input.Generation, ClientMessageID: input.ClientMessageID, Text: "different intent"}}} {
		var typed *InstructionError
		if _, err := e.EnqueueInstruction(context.Background(), "instruction-run", "impl", changed.actor, changed.input); !errors.As(err, &typed) || typed.Code != "instruction_idempotency_conflict" {
			t.Fatalf("nonce changed identity or body without a conflict: %v", err)
		}
	}
	legacy := instructionRecord{ID: uuid.NewString(), MailboxID: q.mailboxID, RunID: "instruction-run", NodeID: "impl", Iteration: 1,
		Generation: input.Generation, ClientMessageID: uuid.NewString(), ActorID: "alice", Text: "legacy inline requirement", State: "expired", Sequence: 2}
	if err := e.db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	legacyInput := InstructionInput{Iteration: 1, Generation: legacy.Generation, ClientMessageID: legacy.ClientMessageID, Text: legacy.Text}
	if got, err := e.EnqueueInstruction(context.Background(), "instruction-run", "impl", "alice", legacyInput); err != nil || got.ID != legacy.ID || got.State != "expired" || got.Text != legacy.Text {
		t.Fatalf("legacy receipt did not retain its bytes: %+v %v", got, err)
	}
	if objects.puts.Load() != puts || objects.gets.Load() != gets {
		t.Fatalf("receipt retries require the object backend: PUT %d→%d GET %d→%d", puts, objects.puts.Load(), gets, objects.gets.Load())
	}
}

func TestWorkflowInstructionAdmissionAndClaimReadOnlyRunVisitMetadata(t *testing.T) {
	e, q := instructionFixture(t)
	objects := instructionObjects(t, e.db)
	missingText := filestore.Prefix + filestore.Text + "/" + strings.Repeat("0", 64)
	missingJSON := filestore.Prefix + filestore.JSON + "/" + strings.Repeat("0", 64)
	if err := e.db.Table("runs").Where("id = ?", "instruction-run").Updates(map[string]any{
		"first_message": missingText, "inputs": missingJSON, "checkpoints": missingJSON,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.db.Table("state_runs").Where("id = ?", q.mailboxID).Updates(map[string]any{
		"output_md": missingText, "outputs": missingJSON, "vars_before": missingJSON, "events": missingJSON,
	}).Error; err != nil {
		t.Fatal(err)
	}
	// Literal reference-looking text in the uploaded body is user data. It
	// must be resolved exactly once, never used as another object location.
	input := instructionInput(q, missingText)
	accepted, err := e.EnqueueInstruction(context.Background(), "instruction-run", "impl", "alice", input)
	if err != nil || accepted.Text != input.Text || objects.puts.Load() != 1 || objects.gets.Load() != 0 {
		t.Fatalf("admission loaded unrelated Run/visit bodies: %+v PUT=%d GET=%d %v", accepted, objects.puts.Load(), objects.gets.Load(), err)
	}
	message, err := q.Next(context.Background(), false)
	if err != nil || message == nil || message.ID != accepted.ID || message.Text != input.Text || objects.gets.Load() != 1 {
		t.Fatalf("claim loaded unrelated bodies or recursively interpreted text: %+v GET=%d %v", message, objects.gets.Load(), err)
	}
}

func TestWorkflowInstructionDispatchSQLFailureKeepsOriginalQueuedReceipt(t *testing.T) {
	e, q := instructionFixture(t)
	objects := instructionObjects(t, e.db)
	accepted, err := e.EnqueueInstruction(context.Background(), "instruction-run", "impl", "alice", instructionInput(q, "dispatch only after durable receipt transition"))
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("dispatch SQL transition failed")
	var fail atomic.Bool
	fail.Store(true)
	const callback = "instruction_test_dispatch_failure"
	if err := e.db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "workflow_instructions" && fail.Load() {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.db.Callback().Update().Remove(callback) })
	if message, err := q.Next(context.Background(), false); !errors.Is(err, injected) || message != nil {
		t.Fatalf("uncommitted dispatch returned a native instruction: %+v %v", message, err)
	}
	var raw instructionRecord
	if err := e.db.First(&raw, "id = ?", accepted.ID).Error; err != nil || raw.State != "queued" || !filestore.IsRef(raw.Text) {
		t.Fatalf("failed dispatch rewrote the accepted receipt: %+v %v", raw, err)
	}
	fail.Store(false)
	message, err := q.Next(context.Background(), false)
	if err != nil || message == nil || message.ID != accepted.ID || message.Text != accepted.Text || objects.gets.Load() != 2 {
		t.Fatalf("retry did not use the original queued intent: %+v GET=%d %v", message, objects.gets.Load(), err)
	}
}

type instructionNextResult struct {
	message *runtime.AutoInstruction
	err     error
}

func TestWorkflowInstructionHydrationRechecksStoppedOrChangedExecution(t *testing.T) {
	for _, change := range []string{"cancelled", "manual", "new-visit", "new-generation", "closed"} {
		t.Run(change, func(t *testing.T) {
			e, q := instructionFixture(t)
			objects := instructionObjects(t, e.db)
			accepted, err := e.EnqueueInstruction(context.Background(), "instruction-run", "impl", "alice", instructionInput(q, "only dispatch to the displayed execution"))
			if err != nil {
				t.Fatal(err)
			}
			started, release := make(chan struct{}), make(chan struct{})
			var unblock sync.Once
			t.Cleanup(func() { unblock.Do(func() { close(release) }) })
			objects.onOpen = func(ctx context.Context, _ string, _ int32) error {
				close(started)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			done := make(chan instructionNextResult, 1)
			go func() {
				message, err := q.Next(context.Background(), true)
				done <- instructionNextResult{message, err}
			}()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("GET did not start")
			}
			probe, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			switch change {
			case "cancelled":
				err = e.db.WithContext(probe).Model(&models.Run{}).Where("id = ?", "instruction-run").Update("status", "cancelled").Error
			case "manual":
				err = e.db.WithContext(probe).Model(&models.StateRun{}).Where("id = ?", q.mailboxID).Update("mode", models.NodeModeManual).Error
			case "new-visit":
				err = e.db.WithContext(probe).Create(&models.StateRun{RunID: "instruction-run", NodeID: "impl", Iteration: 2, Status: "running"}).Error
			case "new-generation":
				successor := &instructionQueue{db: e.db, mailboxID: q.mailboxID, generation: q.generation}
				err = successor.RestartAttempt(probe)
			case "closed":
				err = q.close(probe, "executor ended")
			}
			if err != nil {
				t.Fatalf("GET held SQL while execution changed: %v", err)
			}
			unblock.Do(func() { close(release) })
			result := <-done
			if result.message != nil || (change != "closed" && result.err == nil) {
				t.Fatalf("stale body was dispatched after %s: %+v %v", change, result.message, result.err)
			}
			var raw instructionRecord
			if err := e.db.First(&raw, "id = ?", accepted.ID).Error; err != nil || raw.State == "dispatching" {
				t.Fatalf("stale dispatch changed the receipt: %+v %v", raw, err)
			}
		})
	}
}

func TestWorkflowInstructionCompetingClaimNeverReplaysHydratedHead(t *testing.T) {
	e, q := instructionFixture(t)
	objects := instructionObjects(t, e.db)
	first, err := e.EnqueueInstruction(context.Background(), "instruction-run", "impl", "alice", instructionInput(q, "first requirement"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.EnqueueInstruction(context.Background(), "instruction-run", "impl", "alice", instructionInput(q, "second requirement"))
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	objects.onOpen = func(ctx context.Context, _ string, read int32) error {
		if read != 1 {
			return instructionSQLProbe(ctx, e.db, "other claimant")
		}
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	done := make(chan instructionNextResult, 1)
	go func() {
		message, err := q.Next(context.Background(), false)
		done <- instructionNextResult{message, err}
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("GET did not start")
	}
	winner, err := q.Next(context.Background(), false)
	if err != nil || winner == nil || winner.ID != first.ID || winner.Text != first.Text {
		t.Fatalf("competing claimant could not advance during GET: %+v %v", winner, err)
	}
	unblock.Do(func() { close(release) })
	result := <-done
	if result.err != nil || result.message == nil || result.message.ID != second.ID || result.message.Text != second.Text || objects.gets.Load() != 3 {
		t.Fatalf("losing GET replayed the first receipt instead of reloading the head: %+v gets=%d %v", result.message, objects.gets.Load(), result.err)
	}
	if err := q.RestartAttempt(context.Background()); err != nil {
		t.Fatal(err)
	}
	gets := objects.gets.Load()
	if message, err := q.Next(context.Background(), false); err != nil || message != nil || objects.gets.Load() != gets {
		t.Fatalf("unknown dispatched receipts were replayed: %+v gets=%d %v", message, objects.gets.Load(), err)
	}
}

func TestWorkflowInstructionChangedSourceReloadsOutsideMailboxLock(t *testing.T) {
	e, q := instructionFixture(t)
	objects := instructionObjects(t, e.db)
	input := instructionInput(q, "original body")
	item, err := e.EnqueueInstruction(context.Background(), "instruction-run", "impl", "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	repair, err := prepareInstructionText(context.Background(), e.db, "repaired immutable body")
	if err != nil {
		t.Fatal(err)
	}
	objects.onOpen = func(ctx context.Context, _ string, read int32) error {
		if read == 1 {
			return e.db.WithContext(ctx).Table("workflow_instructions").Where("id = ?", item.ID).Update("text", repair.body.Ref()).Error
		}
		return instructionSQLProbe(ctx, e.db, "read repaired body")
	}
	message, err := q.Next(context.Background(), false)
	if err != nil || message == nil || message.ID != item.ID || message.Text != "repaired immutable body" || objects.gets.Load() != 2 {
		t.Fatalf("stale preloaded text survived the raw-ref fence: %+v gets=%d %v", message, objects.gets.Load(), err)
	}
}

func TestWorkflowInstructionMissingBodyNeverDispatchesOrRewritesIntent(t *testing.T) {
	for _, broken := range []string{"missing", "wrong-encoding", "empty", "oversized", "invalid-utf8"} {
		t.Run(broken, func(t *testing.T) {
			e, q := instructionFixture(t)
			objects := instructionObjects(t, e.db)
			item, err := e.EnqueueInstruction(context.Background(), "instruction-run", "impl", "alice", instructionInput(q, "accepted original intent"))
			if err != nil {
				t.Fatal(err)
			}
			var raw instructionRecord
			if err := e.db.First(&raw, "id = ?", item.ID).Error; err != nil {
				t.Fatal(err)
			}
			brokenRaw := raw.Text
			switch broken {
			case "missing":
				objects.mu.Lock()
				clear(objects.objects)
				objects.mu.Unlock()
			case "wrong-encoding":
				brokenRaw = strings.Replace(raw.Text, "/text/", "/json/", 1)
			case "empty":
				brokenRaw = ""
			case "oversized":
				brokenRaw = strings.Repeat("x", maxInstructionTextBytes+1)
			case "invalid-utf8":
				brokenRaw = string([]byte{0xff})
			}
			if err := e.db.Table("workflow_instructions").Where("id = ?", item.ID).Update("text", brokenRaw).Error; err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if message, err := q.Next(context.Background(), true); err == nil || message != nil {
					t.Fatalf("broken accepted payload was dispatched: %+v %v", message, err)
				}
			}
			if err := e.db.First(&raw, "id = ?", item.ID).Error; err != nil || raw.Text != brokenRaw || raw.State != "queued" {
				t.Fatalf("loading failure erased intent/evidence: %+v %v", raw, err)
			}
		})
	}
}

func TestWorkflowInstructionEmptySealRechecksAdmissionUnderLock(t *testing.T) {
	e, q := instructionFixture(t)
	objects := instructionObjects(t, e.db)
	var injected atomic.Bool
	var accepted *models.WorkflowInstruction
	var admissionErr error
	const callback = "instruction_test_admit_after_empty_prefetch"
	if err := e.db.Callback().Query().After("gorm:after_query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.ModelType != reflect.TypeFor[instructionRecord]() || tx.RowsAffected != 0 {
			return
		}
		if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); inside || !injected.CompareAndSwap(false, true) {
			return
		}
		accepted, admissionErr = e.EnqueueInstruction(context.Background(), "instruction-run", "impl", "alice", instructionInput(q, "admitted after the empty prefetch"))
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.db.Callback().Query().Remove(callback) })
	message, err := q.Next(context.Background(), true)
	if err != nil || admissionErr != nil || accepted == nil || message == nil || message.ID != accepted.ID || message.Text != accepted.Text || objects.gets.Load() != 1 {
		t.Fatalf("empty prefetch sealed over an accepted requirement: %+v %+v gets=%d err=%v admission=%v", message, accepted, objects.gets.Load(), err, admissionErr)
	}
}

func TestWorkflowInstructionHistoryLoadsOnlyReturnedPageAfterCursorCloses(t *testing.T) {
	e, q := instructionFixture(t)
	objects := instructionObjects(t, e.db)
	for n := range 51 {
		text := fmt.Sprintf("historical requirement %02d", n+1)
		prepared, err := prepareInstructionText(context.Background(), e.db, text)
		if err != nil {
			t.Fatal(err)
		}
		raw := prepared.body.Ref()
		if n == 0 {
			raw = filestore.Prefix + filestore.Text + "/" + strings.Repeat("0", 64) // the lookahead row is unavailable
		}
		row := instructionRecord{ID: uuid.NewString(), MailboxID: q.mailboxID, Generation: q.generation, ClientMessageID: uuid.NewString(),
			RunID: "instruction-run", NodeID: "impl", Iteration: 1, Attempt: 1, Sequence: uint64(n + 1), ActorID: "alice", Text: raw, State: "applied"}
		if err := e.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	objects.onOpen = func(ctx context.Context, key string, _ int32) error { return instructionSQLProbe(ctx, e.db, key) }
	_, items, more, err := e.InstructionHistory(context.Background(), "instruction-run", "impl", "", 0)
	if err != nil || !more || len(items) != 50 || items[0].Sequence != 2 || items[49].Sequence != 51 || objects.gets.Load() != 50 {
		t.Fatalf("history fetched the lookahead body or held its cursor: count=%d more=%v gets=%d err=%v", len(items), more, objects.gets.Load(), err)
	}
	for _, item := range items {
		if item.Text != fmt.Sprintf("historical requirement %02d", item.Sequence) || item.State != "applied" {
			t.Fatalf("history lost original text/state: %+v", item)
		}
	}
}

func TestWorkflowInstructionRejectsOuterTransactionBeforePayloadIO(t *testing.T) {
	e, q := instructionFixture(t)
	objects := instructionObjects(t, e.db)
	input := instructionInput(q, "must prepare before the outermost transaction")
	if err := e.db.Transaction(func(tx *gorm.DB) error {
		if _, err := prepareInstructionText(context.Background(), tx, input.Text); err == nil {
			t.Error("nested prepare unexpectedly accepted")
		}
		inside := &Engine{db: tx}
		if _, err := inside.EnqueueInstruction(context.Background(), "instruction-run", "impl", "alice", input); err == nil {
			t.Error("enqueue performed object I/O under an outer transaction")
		}
		queue := &instructionQueue{db: tx, mailboxID: q.mailboxID, generation: q.generation}
		if _, err := queue.Next(context.Background(), false); err == nil {
			t.Error("claim performed object I/O under an outer transaction")
		}
		if _, _, _, err := inside.InstructionHistory(context.Background(), "instruction-run", "impl", "", 0); err == nil {
			t.Error("history performed object I/O under an outer transaction")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if objects.puts.Load() != 0 || objects.gets.Load() != 0 {
		t.Fatalf("transaction misuse reached payload backend: PUT=%d GET=%d", objects.puts.Load(), objects.gets.Load())
	}
}
