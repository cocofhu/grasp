package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

type journalBurstChat struct {
	count   int
	emitted chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (c *journalBurstChat) ChatWithTimeout(ctx context.Context, _ uint, _ string, _ []models.PromptImage, _ time.Duration, emit func(json.RawMessage)) (*models.TokenUsage, models.TokenUsageByModel, error) {
	c.calls.Add(1)
	for i := range c.count {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		emit(json.RawMessage(fmt.Sprintf(`{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"%d,"}}}`, i)))
	}
	close(c.emitted)
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case <-c.release:
		return nil, nil, nil
	}
}

func (*journalBurstChat) Cancel(uint) {}

func TestSoloJournalReplaysOverSixThousandFramesAcrossReplicas(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, _, db := newSoloFixture(t, port, 3)
	native := &journalBurstChat{count: 7013, emitted: make(chan struct{}), release: make(chan struct{})}
	solo.SetChatterForTest(native)
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = solo.Close(context.Background(), view.ID, "alice") }()
	receipt, err := solo.SendWithClientMessageID(view.ID, "alice", "journal-burst", "preserve every event", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-native.emitted:
	case <-time.After(10 * time.Second):
		t.Fatal("native burst blocked")
	}
	waitFor(t, "committed entire burst", func() bool {
		var count int
		db.Model(&models.ChatEventChunk{}).Select("COALESCE(SUM(acp_count), 0)").Where("turn_id = ?", receipt.TurnID).Scan(&count)
		return count == native.count
	})
	peer := NewSoloService(db, nil, solo.agents, solo.projects)
	frames, stop, err := peer.Subscribe(view.ID, "alice", -1)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := <-frames
	if snapshot.Payload["journalVersion"] != 1 || snapshot.Payload["activeItem"] == nil {
		t.Fatalf("missing durable subscription head: %+v", snapshot)
	}
	last, seen := snapshot.Seq, 0
	read := func(ch <-chan chatsession.Event, target int) {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for seen < target {
			select {
			case frame, ok := <-ch:
				if !ok {
					t.Fatal("durable replay closed before delivering every frame")
				}
				if frame.Payload["stateSnapshot"] == true {
					continue
				}
				if frame.Seq != last+1 {
					t.Fatalf("sequence gap %d → %d", last, frame.Seq)
				}
				last = frame.Seq
				if frame.Type == chatsession.FrameAcp {
					if frame.TurnID != receipt.TurnID || !strings.Contains(string(frame.Data), fmt.Sprintf(`"text":"%d,"`, seen)) {
						t.Fatalf("frame %d crossed turn or order: %s", seen, frame.Data)
					}
					seen++
				}
			case <-deadline:
				t.Fatalf("only read %d/%d frames", seen, target)
			}
		}
	}
	read(frames, 3500)
	stop()
	frames, stop, err = peer.Subscribe(view.ID, "alice", last)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if head := <-frames; head.Seq != last || head.Payload["cursorReset"] == true {
		t.Fatalf("reconnect cursor changed: %+v", head)
	}
	read(frames, native.count)
	legacy, stopLegacy, err := peer.Subscribe(view.ID, "alice", last, true)
	if err != nil {
		t.Fatal(err)
	}
	legacyHead := <-legacy
	stopLegacy()
	if legacyHead.Payload["cursorReset"] != true || legacyHead.Seq != snapshot.Seq {
		t.Fatal("legacy process cursor was reused as a persistent journal offset")
	}
	if peer.lookup(view.ID) != nil {
		t.Fatal("a passive cross-replica reader constructed an executor")
	}
	close(native.release)
	waitFor(t, "durable journal reply", func() bool {
		var reply models.SoloMessage
		return db.First(&reply, "id = ?", "reply:"+receipt.TurnID).Error == nil && reply.EventLog != nil
	})
	var reply models.SoloMessage
	if err := db.First(&reply, "id = ?", "reply:"+receipt.TurnID).Error; err != nil {
		t.Fatal(err)
	}
	if len(reply.Events) != 0 || reply.EventLog.Count != native.count || native.calls.Load() != 1 {
		t.Fatalf("reply copied/truncated/replayed its event journal: %+v", reply.EventLog)
	}
	messages, err := peer.Messages(view.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || len(messages[1].Events) != native.count {
		t.Fatalf("reload lost durable output: messages=%d", len(messages))
	}
	page, err := peer.MessagePage(context.Background(), view.ID, "alice", "", 50)
	if err != nil || len(page.Items) != 2 || len(page.Items[1].Events) != 0 || page.Items[1].EventLog.Count != native.count {
		t.Fatalf("message page loaded or lost process bodies: %+v %v", page, err)
	}
	count, after := 0, int64(-1)
	for {
		part, err := peer.MessageEvents(context.Background(), view.ID, "alice", reply.ID, after)
		if err != nil {
			t.Fatal(err)
		}
		if len(part.Items) > chatsession.JournalPageEvents || part.Next <= after {
			t.Fatalf("unbounded or stationary process page: %d %d", len(part.Items), part.Next)
		}
		count += len(part.Items)
		after = part.Next
		if !part.HasMore {
			break
		}
	}
	if count != native.count {
		t.Fatalf("paginated process count = %d", count)
	}
	if _, err := peer.MessageEvents(context.Background(), view.ID, "bob", reply.ID, -1); err == nil {
		t.Fatal("another user read journal bodies")
	}
	if _, err := peer.MessageEvents(context.Background(), view.ID, "alice", reply.ID, reply.EventLog.Through+1); !errors.Is(err, chatsession.ErrJournalCursor) {
		t.Fatalf("out-of-range message cursor = %v", err)
	}
	if err := db.Model(&models.ChatEventChunk{}).Where("queue_id = ? AND first_sequence = ?", soloQueueScope(view.ID).ID(), reply.EventLog.From).Update("payload", []byte(`{}`)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := peer.MessageEvents(context.Background(), view.ID, "alice", reply.ID, -1); !errors.Is(err, chatsession.ErrJournalGap) {
		t.Fatalf("corrupt event chunk silently passed: %v", err)
	}
	if _, err := peer.MessagePage(context.Background(), view.ID, "alice", "", 50); err != nil {
		t.Fatalf("corrupt process body hid the persisted reply: %v", err)
	}
}

func TestSoloJournalStorageFailureCancelsNativeAndKeepsCommittedPrefix(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, _, db := newSoloFixture(t, port, 3)
	var chunks atomic.Int32
	if err := db.Callback().Create().Before("gorm:create").Register("journal-disk-failure", func(tx *gorm.DB) {
		if tx.Statement.Table != "chat_event_chunks" {
			return
		}
		if tx.Statement.ReflectValue.FieldByName("AcpCount").Int() > 0 && chunks.Add(1) > 1 {
			tx.AddError(errors.New("fixture object write failed"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Callback().Create().Remove("journal-disk-failure") }()
	native := &journalBurstChat{count: 1024, emitted: make(chan struct{}), release: make(chan struct{})}
	solo.SetChatterForTest(native)
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = solo.Close(context.Background(), view.ID, "alice") }()
	receipt, err := solo.SendWithClientMessageID(view.ID, "alice", "journal-failure", "make partial progress", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "uncertain durable result", func() bool {
		row, err := solo.durable.Receipt(context.Background(), soloQueueScope(view.ID), receipt.TurnID)
		return err == nil && row.State == chatsession.TurnUnknown
	})
	var reply models.SoloMessage
	if err := db.First(&reply, "id = ?", "reply:"+receipt.TurnID).Error; err != nil {
		t.Fatal(err)
	}
	if reply.Status != "unknown" || reply.EventLog == nil || reply.EventLog.Count < 1 || reply.EventLog.Count >= native.count {
		t.Fatalf("failed storage claimed completion or lost committed prefix: %+v", reply.EventLog)
	}
	row, err := solo.Require(view.ID, "alice")
	if err != nil || !row.ConversationContextPending {
		t.Fatal("uncertain native execution lost its context-sync marker")
	}
	if _, err := solo.SendWithClientMessageID(view.ID, "alice", "journal-failure", "make partial progress", nil, nil); err != nil {
		t.Fatal(err)
	}
	if native.calls.Load() != 1 {
		t.Fatal("retry of an uncertain receipt replayed native actions")
	}
	messages, err := solo.Messages(view.ID, "alice")
	if err != nil || len(messages) != 2 || len(messages[1].Events) != reply.EventLog.Count {
		t.Fatalf("committed prefix unavailable after failure: %v", err)
	}
}

func TestSoloJournalRecoveryPreservesArtifactPlaceholderAndPriorEpoch(t *testing.T) {
	db := newTestDB(t)
	solo := NewSoloService(db, nil, nil, nil)
	parent := models.SoloSession{ID: "crash-journal", UserID: "alice", Status: models.SoloStatusStopped}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	scope := soloQueueScope(parent.ID)
	turn := &soloTurn{ID: "native-before-crash", Text: "change a file"}
	intent, err := solo.durableSoloIntent(turn)
	if err != nil {
		t.Fatal(err)
	}
	intent.ActorID, intent.ClientMessageID, intent.Fingerprint = "alice", "before-crash", strings.Repeat("a", 64)
	if _, err := solo.durable.Admit(context.Background(), scope, intent, 16, nil); err != nil {
		t.Fatal(err)
	}
	lease, _, err := solo.durable.Acquire(context.Background(), scope, "11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := solo.durable.Claim(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	if err := solo.durable.Dispatch(context.Background(), lease, turn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := solo.durable.AppendJournal(context.Background(), scope, &lease, turn.ID, []chatsession.JournalEntry{{Type: chatsession.FrameAcp, Data: json.RawMessage(`{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"saved prefix"}}}`)}}, nil); err != nil {
		t.Fatal(err)
	}
	file := models.ArtifactAttachment{ArtifactID: "file-before-crash", Revision: 1, Name: "result.txt"}
	placeholder := models.SoloMessage{ID: "reply:" + turn.ID, SessionID: parent.ID, TurnID: turn.ID, Role: "assistant", Content: "existing partial text", Files: []models.ArtifactAttachment{file}}
	if err := db.Create(&placeholder).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.ChatQueue{}).Where("id = ?", scope.ID()).Update("owner_expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	_, interrupted, err := solo.durable.Acquire(context.Background(), scope, "22222222-2222-4222-8222-222222222222")
	if err != nil || interrupted == nil || interrupted.State != chatsession.TurnUnknown {
		t.Fatalf("unknown receipt missing: %v", err)
	}
	if err := solo.recoverSoloReceipt(parent.ID, *interrupted); err != nil {
		t.Fatal(err)
	}
	if err := solo.recoverSoloReceipt(parent.ID, *interrupted); err != nil {
		t.Fatal(err)
	}
	var saved models.SoloMessage
	if err := db.First(&saved, "id = ?", placeholder.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Status != "unknown" || saved.Content != placeholder.Content || len(saved.Files) != 1 || saved.Files[0] != file || saved.EventLog == nil || saved.EventLog.Generation != lease.Generation || saved.EventLog.Count != 1 {
		t.Fatalf("placeholder lost recovery data: %+v", saved)
	}
	page, err := solo.MessageEvents(context.Background(), parent.ID, "alice", saved.ID, -1)
	if err != nil || len(page.Items) != 1 || page.Items[0].Generation != lease.Generation {
		t.Fatalf("recovered output was relabeled or lost: %+v %v", page, err)
	}
	if err := db.First(&parent, "id = ?", parent.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !parent.ConversationContextPending || parent.WorkflowContextVersion != 1 || parent.Status != models.SoloStatusStopped {
		t.Fatal("recovery replayed a context update or woke a stopped environment")
	}
}
