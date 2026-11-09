package chatsession

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func journalEntry(text string) JournalEntry {
	body, _ := json.Marshal(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": text}})
	return JournalEntry{Type: FrameAcp, Data: body}
}

func TestJournalWireMetadataCannotBeOverriddenByControlPayload(t *testing.T) {
	entry := JournalEntry{Seq: 17, Type: FrameSession, Event: EventQueueState, TurnID: "actual-turn", Generation: 12, At: time.Now().UTC(), Payload: map[string]any{"seq": 999, "type": "wrong", "event": "wrong", "userMsgId": "wrong", "sessionGeneration": 3, "at": "wrong", "busy": true}}
	wire := entry.StreamEvent().Frame()
	if wire["seq"] != 17 || wire["type"] != FrameSession || wire["event"] != EventQueueState || wire["userMsgId"] != "actual-turn" || wire["sessionGeneration"] != uint64(12) || wire["at"] != entry.At || wire["busy"] != true {
		t.Fatalf("control payload forged journal identity: %+v", wire)
	}
	if entry.Payload["seq"] != 999 {
		t.Fatal("wire adapter mutated immutable stored payload")
	}
}

func TestJournalRetainsOverSixThousandFramesAcrossReplicasAndCursorWindows(t *testing.T) {
	_, store, replica, scope := durableFixture(t)
	ctx := context.Background()
	intent := admitQueue(t, store, scope, "long native execution", "one")
	lease := acquireQueue(t, store, scope)
	if _, err := store.Claim(ctx, lease); err != nil {
		t.Fatal(err)
	}
	const total = 7013
	for first := 0; first < total; {
		count := min(JournalBatchEvents, total-first)
		batch := make([]JournalEntry, count)
		for i := range batch {
			batch[i] = journalEntry(fmt.Sprintf("frame-%d", first+i))
		}
		written, err := store.AppendJournal(ctx, scope, &lease, intent.Turn.ID, batch, nil)
		if err != nil || written.First != uint64(first) || written.Last != uint64(first+count-1) {
			t.Fatalf("failed complete batch: %+v %v", written, err)
		}
		first += count
	}
	if head, err := replica.JournalHead(ctx, scope); err != nil || head != total-1 {
		t.Fatalf("journal silently capped: %d %v", head, err)
	}
	cursor := int64(-1)
	var count int
	for {
		page, err := replica.JournalPage(ctx, scope, JournalQuery{After: cursor, Limit: 73})
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range page.Items {
			if entry.Seq != uint64(count) || entry.TurnID != intent.Turn.ID || entry.Generation != lease.Generation || entry.At.IsZero() {
				t.Fatalf("cursor replay changed attribution/order at %d: %+v", count, entry)
			}
			frame := entry.Frame()
			if frame["userMsgId"] != intent.Turn.ID || frame["sessionGeneration"] != lease.Generation || !strings.Contains(string(entry.Data), fmt.Sprintf("frame-%d", count)) {
				t.Fatal("replay changed native frame")
			}
			wire := entry.StreamEvent().Frame()
			if wire["userMsgId"] != intent.Turn.ID || wire["sessionGeneration"] != lease.Generation || wire["at"] != entry.At {
				t.Fatal("stream adapter dropped durable attribution/time")
			}
			count++
		}
		if page.Next < cursor || page.HasMore && page.Next == cursor {
			t.Fatal("cursor did not advance")
		}
		cursor = page.Next
		if !page.HasMore {
			break
		}
	}
	if count != total || cursor != total-1 {
		t.Fatalf("replayed %d/%d frames", count, total)
	}
	through := int64(71)
	page, err := replica.JournalPage(ctx, scope, JournalQuery{After: 69, Through: &through, Limit: 999})
	if err != nil || len(page.Items) != 2 || page.Next != 71 || page.HasMore {
		t.Fatalf("message range escaped: %+v %v", page, err)
	}
}

func TestJournalFailedPublicationLeavesNoCursorAndRejectsOldOwner(t *testing.T) {
	db, store, replica, scope := durableFixture(t)
	ctx := context.Background()
	intent := admitQueue(t, store, scope, "one native effect", "one")
	lease := acquireQueue(t, store, scope)
	if _, err := store.Claim(ctx, lease); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("journal SQL unavailable")
	if err := db.Callback().Create().Before("gorm:create").Register("test:journal_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "chat_event_chunks" {
			_ = tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendJournal(ctx, scope, &lease, intent.Turn.ID, []JournalEntry{journalEntry("must not be shown")}, nil); !errors.Is(err, failure) {
		t.Fatalf("failed chunk reported successful: %v", err)
	}
	if err := db.Callback().Create().Remove("test:journal_failure"); err != nil {
		t.Fatal(err)
	}
	if head, err := replica.JournalHead(ctx, scope); err != nil || head != -1 {
		t.Fatalf("failed save advanced cursor: %d %v", head, err)
	}
	if _, err := store.AppendJournal(ctx, scope, &lease, intent.Turn.ID, []JournalEntry{journalEntry("durable")}, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.Dispatch(ctx, lease, intent.Turn.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.ChatQueue{}).Where("id = ?", scope.ID()).Update("owner_expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := replica.Acquire(ctx, scope, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendJournal(ctx, scope, &lease, intent.Turn.ID, []JournalEntry{journalEntry("late")}, nil); !errors.Is(err, ErrQueueOwnerLost) {
		t.Fatalf("old executor wrote a late frame: %v", err)
	}
	if head, err := replica.JournalHead(ctx, scope); err != nil || head != 0 {
		t.Fatalf("late frame advanced new owner stream: %d %v", head, err)
	}
}

func TestJournalConcurrentControlAppendHasNoOverlapOrGaps(t *testing.T) {
	_, a, b, scope := durableFixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	failures := make(chan error, 24)
	for i := range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store := a
			if i%2 != 0 {
				store = b
			}
			_, err := store.AppendJournal(ctx, scope, nil, "", []JournalEntry{{Type: FrameSession, Event: EventQueueState, Payload: map[string]any{"marker": i}}}, nil)
			if err != nil {
				failures <- err
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	page, err := b.JournalPage(ctx, scope, JournalQuery{After: -1})
	if err != nil || len(page.Items) != 24 || page.HasMore {
		t.Fatalf("concurrent stream lost frames: %+v %v", page, err)
	}
	for i, entry := range page.Items {
		if entry.Seq != uint64(i) || entry.Frame()["event"] != EventQueueState {
			t.Fatal("overlapping control event ranges")
		}
	}
}

func TestJournalMissingChunkIsExplicitAndNeverAdvancesCursor(t *testing.T) {
	db, store, replica, scope := durableFixture(t)
	ctx := context.Background()
	for i := range 3 {
		if _, err := store.AppendJournal(ctx, scope, nil, "", []JournalEntry{{Type: FrameSession, Event: EventQueueState, Payload: map[string]any{"marker": i}}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Where("queue_id = ? AND first_sequence = ?", scope.ID(), 1).Delete(&models.ChatEventChunk{}).Error; err != nil {
		t.Fatal(err)
	}
	page, err := replica.JournalPage(ctx, scope, JournalQuery{After: -1})
	if !errors.Is(err, ErrJournalGap) || page.Next != -1 || len(page.Items) != 0 {
		t.Fatalf("missing frame skipped silently: %+v %v", page, err)
	}
	page, err = replica.JournalPage(ctx, scope, JournalQuery{After: 0})
	if !errors.Is(err, ErrJournalGap) || page.Next != 0 || len(page.Items) != 0 {
		t.Fatalf("cursor advanced across missing history: %+v %v", page, err)
	}
}

func TestJournalLargeFrameAndByteBoundKeepEveryPayload(t *testing.T) {
	_, store, replica, scope := durableFixture(t)
	ctx := context.Background()
	intent := admitQueue(t, store, scope, "large tool output", "one")
	lease := acquireQueue(t, store, scope)
	if _, err := store.Claim(ctx, lease); err != nil {
		t.Fatal(err)
	}
	entries := []JournalEntry{journalEntry(strings.Repeat("a", 700<<10)), journalEntry(strings.Repeat("b", 700<<10)), journalEntry("tail")}
	if _, err := store.AppendJournal(ctx, scope, &lease, intent.Turn.ID, entries, nil); err != nil {
		t.Fatal(err)
	}
	page, err := replica.JournalPage(ctx, scope, JournalQuery{After: -1})
	if err != nil || len(page.Items) != 1 || !page.HasMore || page.Next != 0 {
		t.Fatalf("response byte bound lost/over-read a frame: count=%d next=%d more=%v err=%v", len(page.Items), page.Next, page.HasMore, err)
	}
	next, err := replica.JournalPage(ctx, scope, JournalQuery{After: page.Next})
	if err != nil || len(next.Items) != 2 || next.HasMore || next.Next != 2 {
		t.Fatalf("large remainder was lost: count=%d err=%v", len(next.Items), err)
	}
	if _, err := store.AppendJournal(ctx, scope, &lease, intent.Turn.ID, []JournalEntry{journalEntry(strings.Repeat("x", JournalPageBytes+1024))}, nil); err != nil {
		t.Fatal(err)
	}
	large, err := replica.JournalPage(ctx, scope, JournalQuery{After: 2})
	if err != nil || len(large.Items) != 1 || len(large.Items[0].Data) < JournalPageBytes {
		t.Fatalf("one large event was truncated: %v", err)
	}
}

func TestJournalValidationAndParentDeletionCannotInventHistory(t *testing.T) {
	_, store, _, scope := durableFixture(t)
	ctx := context.Background()
	for _, entries := range [][]JournalEntry{nil, {{Type: "unexpected"}}, {{Type: FrameAcp, Data: json.RawMessage(`{"valid":true}`)}}, {{Type: FrameSession}}, make([]JournalEntry, JournalBatchEvents+1)} {
		if _, err := store.AppendJournal(ctx, scope, nil, "", entries, nil); err == nil {
			t.Fatal("unfenced/invalid event batch was accepted")
		}
	}
	deleted := errors.New("owning conversation deleted")
	if _, err := store.AppendJournal(ctx, scope, nil, "", []JournalEntry{{Type: FrameSession, Event: "status"}}, func(*gorm.DB) error { return deleted }); !errors.Is(err, deleted) {
		t.Fatalf("deleted parent accepted events: %v", err)
	}
	if head, err := store.JournalHead(ctx, scope); err != nil || head != -1 {
		t.Fatalf("failed parent validation left a journal: %d %v", head, err)
	}
	for _, query := range []JournalQuery{{After: -2}, {After: 1}, {After: 0, Through: ptrJournalSequence(-1)}} {
		if _, err := store.JournalPage(ctx, scope, query); !errors.Is(err, ErrJournalCursor) {
			t.Fatalf("invalid cursor accepted: %+v %v", query, err)
		}
	}
}

func ptrJournalSequence(n int64) *int64 { return &n }

func TestJournalCorruptChunkCannotBecomeValidPartialHistory(t *testing.T) {
	db, store, _, scope := durableFixture(t)
	ctx := context.Background()
	if _, err := store.AppendJournal(ctx, scope, nil, "", []JournalEntry{{Type: FrameSession, Event: "status", Payload: map[string]any{"message": "original"}}}, nil); err != nil {
		t.Fatal(err)
	}
	var original models.ChatEventChunk
	if err := db.First(&original, "queue_id = ?", scope.ID()).Error; err != nil {
		t.Fatal(err)
	}
	for name, corrupt := range map[string]func(*models.ChatEventChunk, *journalEnvelope){
		"wrong body size":        func(row *models.ChatEventChunk, _ *journalEnvelope) { row.Bytes++ },
		"wrong count":            func(row *models.ChatEventChunk, _ *journalEnvelope) { row.Count++ },
		"wrong version":          func(_ *models.ChatEventChunk, e *journalEnvelope) { e.Version++ },
		"wrong scope":            func(_ *models.ChatEventChunk, e *journalEnvelope) { e.QueueID = "another-conversation" },
		"wrong sequence":         func(_ *models.ChatEventChunk, e *journalEnvelope) { e.Entries[0].Seq++ },
		"wrong owner generation": func(_ *models.ChatEventChunk, e *journalEnvelope) { e.Entries[0].Generation++ },
		"wrong turn":             func(_ *models.ChatEventChunk, e *journalEnvelope) { e.Entries[0].TurnID = "another-turn" },
		"unknown frame type":     func(_ *models.ChatEventChunk, e *journalEnvelope) { e.Entries[0].Type = "unrecognised" },
		"missing control event":  func(_ *models.ChatEventChunk, e *journalEnvelope) { e.Entries[0].Event = "" },
	} {
		t.Run(name, func(t *testing.T) {
			row := original
			var envelope journalEnvelope
			if err := json.Unmarshal(original.Payload, &envelope); err != nil {
				t.Fatal(err)
			}
			corrupt(&row, &envelope)
			body, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			if row.Bytes == original.Bytes {
				row.Bytes = len(body)
			}
			if err := db.Model(&models.ChatEventChunk{}).Where("id = ?", row.ID).Updates(map[string]any{"payload": body, "bytes": row.Bytes, "count": row.Count}).Error; err != nil {
				t.Fatal(err)
			}
			page, err := store.JournalPage(ctx, scope, JournalQuery{After: -1})
			if !errors.Is(err, ErrJournalGap) || page.Next != -1 || len(page.Items) != 0 {
				t.Fatalf("corruption produced plausible history: %+v %v", page, err)
			}
		})
	}
}
