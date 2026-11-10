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
)

type messagePayloadObjects struct {
	*journalViewObjects
	puts   atomic.Int32
	onPut  func(context.Context, []byte) error
	onOpen func(context.Context) error
}

type messageTextChat struct{ calls atomic.Int32 }

func (c *messageTextChat) ChatWithTimeout(_ context.Context, _ uint, _ string, _ []models.PromptImage, _ time.Duration, onEvent func(json.RawMessage)) (*models.TokenUsage, models.TokenUsageByModel, error) {
	c.calls.Add(1)
	onEvent(json.RawMessage(`{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"reply to work"}}}`))
	return nil, nil, nil
}

func (*messageTextChat) Cancel(uint) {}

func (s *messagePayloadObjects) Put(ctx context.Context, key string, body io.ReadSeeker, info objectstore.Info) error {
	s.puts.Add(1)
	raw, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	if _, err := body.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if s.onPut != nil {
		if err := s.onPut(ctx, raw); err != nil {
			return err
		}
	}
	return s.journalViewObjects.Put(ctx, key, body, info)
}

func (s *messagePayloadObjects) Open(ctx context.Context, key string) (io.ReadCloser, objectstore.Info, error) {
	if s.onOpen != nil {
		if err := s.onOpen(ctx); err != nil {
			return nil, objectstore.Info{}, err
		}
	}
	return s.journalViewObjects.Open(ctx, key)
}

func messageObjects(t *testing.T, db *gorm.DB) *messagePayloadObjects {
	t.Helper()
	objects := &messagePayloadObjects{journalViewObjects: &journalViewObjects{objects: map[string]journalViewObject{}}}
	files, err := filestore.FromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := files.SetObjectStore(objects); err != nil {
		t.Fatal(err)
	}
	return objects
}

func TestSoloMessageSQLSchemaPreservesEveryLegacyColumnWithoutFileHooks(t *testing.T) {
	db := newTestDB(t)
	legacy, raw := &gorm.Statement{DB: db}, &gorm.Statement{DB: db}
	if err := legacy.Parse(&models.SoloMessage{}); err != nil {
		t.Fatal(err)
	}
	if err := raw.Parse(&soloMessageSQL{}); err != nil {
		t.Fatal(err)
	}
	for _, field := range legacy.Schema.Fields {
		if field.DBName != "" && raw.Schema.LookUpField(field.DBName) == nil {
			t.Fatalf("explicit message record loses column %s", field.DBName)
		}
	}
	for _, field := range raw.Schema.Fields {
		if field.DBName != "" && (legacy.Schema.LookUpField(field.DBName) == nil || strings.HasPrefix(field.TagSettings["SERIALIZER"], "file")) {
			t.Fatalf("raw message record invents a column or file hook: %s", field.DBName)
		}
	}
}

func TestSoloMessagePreparedWritesNeverWaitForOtherUploadsOrCommitUnpublishedBodies(t *testing.T) {
	db := newTestDB(t)
	objects := messageObjects(t, db)
	ctx := context.Background()
	ready, err := prepareSoloMessage(ctx, db, models.SoloMessage{ID: "ready", SessionID: "session", Role: "user", Content: "already durable"})
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	objects.onPut = func(ctx context.Context, _ []byte) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	type result struct {
		prepared preparedSoloMessage
		err      error
	}
	done := make(chan result, 1)
	go func() {
		prepared, err := prepareSoloMessage(ctx, db, models.SoloMessage{ID: "slow", SessionID: "session", Role: "user", Content: "slow durable body"})
		done <- result{prepared, err}
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("upload did not start")
	}
	probe, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := db.WithContext(probe).Transaction(func(tx *gorm.DB) error {
		_, err := insertPreparedSoloMessageTx(tx, ready)
		return err
	}); err != nil {
		t.Fatalf("prepared SQL publication blocked behind another PUT: %v", err)
	}
	if rows, err := readSoloMessages(db.WithContext(probe), 0); err != nil || len(rows) != 1 || rows[0].ID != "ready" {
		t.Fatalf("metadata query depends on payload backend: %+v %v", rows, err)
	}
	if err := db.WithContext(probe).Create(&models.Setting{Key: "message-upload-probe", Value: "connection free"}).Error; err != nil {
		t.Fatalf("upload occupied SQLite's only connection: %v", err)
	}
	unblock.Do(func() { close(release) })
	publication := <-done
	if publication.err != nil {
		t.Fatal(publication.err)
	}
	want := errors.New("rollback after successful PUT")
	if err := db.Transaction(func(tx *gorm.DB) error {
		if _, err := insertPreparedSoloMessageTx(tx, publication.prepared); err != nil {
			return err
		}
		return want
	}); !errors.Is(err, want) {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&models.SoloMessage{}).Where("id = ?", "slow").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("SQL rollback leaked a body reference: %d %v", count, err)
	}
	if objects.puts.Load() != 2 || objects.gets.Load() != 0 {
		t.Fatalf("SQL publication performed hidden I/O: put=%d get=%d", objects.puts.Load(), objects.gets.Load())
	}
	for _, mutation := range []func(*preparedSoloMessage){
		func(p *preparedSoloMessage) {
			p.record.Content = filestore.Prefix + filestore.Text + "/" + strings.Repeat("a", 64)
		},
		func(p *preparedSoloMessage) { p.record.ID = "" },
	} {
		bad := publication.prepared
		mutation(&bad)
		if err := db.Transaction(func(tx *gorm.DB) error { _, err := insertPreparedSoloMessageTx(tx, bad); return err }); err == nil {
			t.Fatal("unprepared or mutated publication accepted")
		}
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if _, err := prepareSoloMessage(ctx, tx, models.SoloMessage{ID: "nested", SessionID: "session"}); err == nil {
			t.Fatal("payload preparation accepted an outer transaction")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSoloMessageHistoryLoadsOnlyRequestedBodiesAfterCursorCloses(t *testing.T) {
	db := newTestDB(t)
	objects := messageObjects(t, db)
	parent := models.SoloSession{ID: "history-explicit", UserID: "alice", Status: models.SoloStatusStopped}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	original := models.SoloMessage{ID: "all-fields", SessionID: parent.ID, Role: "assistant",
		Content: filestore.Prefix + filestore.Text + "/" + strings.Repeat("a", 64), WorkflowContext: "original workflow context",
		Images: []models.PromptImage{{Ref: "blob:attachment", Name: "original"}}, Annotations: []models.ReactAnnotation{{}},
		Events: []json.RawMessage{json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"original event"}}`)},
		Files:  []models.ArtifactAttachment{{ArtifactID: "file", Revision: 1}}, PreviewShared: true}
	prepared, err := prepareSoloMessage(context.Background(), db, original)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { _, err := insertPreparedSoloMessageTx(tx, prepared); return err }); err != nil {
		t.Fatal(err)
	}
	objects.onOpen = func(ctx context.Context) error {
		probe, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		return db.WithContext(probe).Create(&models.Setting{Key: "message-get-" + uuid.NewString(), Value: "cursor released"}).Error
	}
	solo := NewSoloService(db, nil, nil, nil)
	page, err := solo.MessagePage(context.Background(), parent.ID, "alice", "", 20)
	if err != nil || len(page.Items) != 1 || page.Items[0].Content != original.Content || len(page.Items[0].Images) != 1 || objects.gets.Load() != 3 {
		t.Fatalf("chat page loaded unexpected bodies or held a cursor: %+v gets=%d %v", page, objects.gets.Load(), err)
	}
	if page.Items[0].WorkflowContext != "" || len(page.Items[0].Events) != 0 || !page.Items[0].LegacyEvents {
		t.Fatal("chat page included process/context bodies or lost process availability")
	}
	process, err := solo.MessageEvents(context.Background(), parent.ID, "alice", original.ID, -1)
	if err != nil || len(process.Items) != 1 || !bytes.Equal(process.Items[0].Data, original.Events[0]) || objects.gets.Load() != 4 {
		t.Fatalf("process reader loaded other message bodies: %+v gets=%d %v", process, objects.gets.Load(), err)
	}
	before := objects.gets.Load()
	if _, err := solo.MessagePage(context.Background(), parent.ID, "mallory", "", 20); err == nil || objects.gets.Load() != before {
		t.Fatal("unauthorized history request fetched private bodies")
	}
	if !solo.MessageHasAttachment(parent.ID, original.ID, "blob:attachment") || objects.gets.Load() != before+1 {
		t.Fatal("attachment authorization did not limit hydration to images")
	}
	all, err := solo.Messages(parent.ID, "alice")
	if err != nil || len(all) != 1 || all[0].WorkflowContext != original.WorkflowContext || all[0].Content != original.Content || len(all[0].Files) != 1 {
		t.Fatalf("full original message changed: %+v %v", all, err)
	}
}

func TestSoloFinalReplyUploadDoesNotBlockLeaseOrLoseDeliveredFiles(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, _, db := newSoloFixture(t, port, 3)
	insertSoloModelConfig(t, db)
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "environment ready", func() bool {
		row, _ := solo.Require(view.ID, "alice")
		return row != nil && row.Status == models.SoloStatusRunning
	})
	chatter := &messageTextChat{}
	solo.SetChatterForTest(chatter)
	solo.SetModelAlignerForTest(&recordAligner{})
	objects := messageObjects(t, db)
	started, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	objects.onPut = func(ctx context.Context, body []byte) error {
		if !strings.HasPrefix(string(body), "reply to ") {
			return nil
		}
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	receipt, err := solo.SendWithClientMessageID(view.ID, "alice", "final-body", "work", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		row, _ := solo.durable.Receipt(context.Background(), soloQueueScope(view.ID), receipt.TurnID)
		messages, _ := readSoloMessages(db.Where("session_id = ?", view.ID), 0)
		t.Fatalf("final text upload did not begin: receipt=%+v transcript=%+v native calls=%d", row, messages, chatter.calls.Load())
	}
	probe, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var queue models.ChatQueue
	if err := db.WithContext(probe).First(&queue, "id = ?", soloQueueScope(view.ID).ID()).Error; err != nil {
		t.Fatal(err)
	}
	lease := chatsession.QueueLease{Scope: soloQueueScope(view.ID), Owner: queue.Owner, Generation: queue.Generation, ExpiresAt: *queue.OwnerExpiresAt}
	if _, err := solo.durable.RenewUntil(probe, lease); err != nil {
		t.Fatalf("final reply PUT held queue lease lock: %v", err)
	}
	delivery := NewArtifactDeliveryService(db, nil, nil)
	turn := ArtifactTurn{SessionID: view.ID, TurnID: receipt.TurnID, SandboxID: view.SandboxID}
	delivery.CurrentTurn = func(string, string) (ArtifactTurn, error) { return turn, nil }
	if _, err := delivery.deliver(probe, "solo-run", "solo", turn, models.ArtifactAttachment{ArtifactID: "first-file", Revision: 1}, "first"); err != nil {
		t.Fatalf("file publication blocked behind final body PUT: %v", err)
	}
	unblock.Do(func() { close(release) })
	waitFor(t, "final reply receipt", func() bool {
		row, err := solo.durable.Receipt(context.Background(), lease.Scope, receipt.TurnID)
		return err == nil && row.State == chatsession.TurnSucceeded
	})
	objects.onOpen = func(context.Context) error { return errors.New("file metadata must not load reply text") }
	if _, err := delivery.deliver(context.Background(), "solo-run", "solo", turn, models.ArtifactAttachment{ArtifactID: "second-file", Revision: 1}, "second"); err != nil {
		t.Fatalf("metadata-only file update read or rewrote completed text: %v", err)
	}
	objects.onOpen = nil
	message, err := readSoloMessage(db.Where("id = ?", "reply:"+receipt.TurnID), soloMessageContent)
	if err != nil || !strings.HasPrefix(message.Content, "reply to ") || !strings.HasSuffix(message.Content, "work") || len(message.Files) != 2 || message.Status != "ok" {
		t.Fatalf("final body/attachment publication lost data: %+v %v", message, err)
	}
}
