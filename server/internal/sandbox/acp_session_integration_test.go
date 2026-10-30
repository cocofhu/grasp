package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/gorilla/websocket"
)

type acpSessionFrame struct {
	event   string
	payload map[string]any
}

type acpSessionJournal struct {
	mu      sync.Mutex
	frames  []acpSessionFrame
	results map[string]*ChatResult
	calls   []string
}

func (j *acpSessionJournal) publish(event string, payload map[string]any) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.frames = append(j.frames, acpSessionFrame{event, payload})
}

func (j *acpSessionJournal) execute(c *ACPClient, progress func(string, *ChatResult)) func(context.Context, string) (bool, error) {
	return func(ctx context.Context, id string) (bool, error) {
		// A lost transport is repaired before the next accepted FIFO item;
		// this is deliberately not a retry of the already executed prompt.
		if !c.IsConnected() {
			c.redial()
			if err := c.Connect(ctx); err != nil {
				return false, err
			}
		}
		turnCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		res, err := c.ChatStreamResult(turnCtx, id, nil, func(result *ChatResult) {
			if progress != nil {
				progress(id, result)
			}
		})
		j.mu.Lock()
		if j.results == nil {
			j.results = make(map[string]*ChatResult)
		}
		j.results[id] = res
		j.mu.Unlock()
		if res != nil {
			if res.Interrupted {
				return true, err
			}
			// ACP intentionally retains partial output alongside ErrorText.
			// The platform must emit an error terminal rather than retry the
			// prompt or mistake a nil Go error for a successful answer.
			if res.Failed || res.ErrorText != "" {
				return false, errors.New(res.ErrorText)
			}
		}
		return false, err
	}
}

func acpSessionItem(id string) chatsession.ItemView {
	return chatsession.ItemView{ID: id, ClientMessageID: "client-" + id, Text: id}
}

func waitACPIntegration(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// Exercise the real bounded ACP reader as the executor of a platform FIFO.
// Failure, complete EOF and incomplete EOF all have accepted successors, so
// losing a terminal or retrying an ambiguous partial answer cannot hide behind
// an otherwise idle single-turn client.
func TestACPQueuedSessionMixedOutcomesDrainOnce(t *testing.T) {
	const chunks = 96
	order := []string{"gate", "failed-1", "failed-2", "complete-eof", "partial-eof", "recovered"}
	firstChunk, releaseFirst, idle := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseFirst) }) }
	t.Cleanup(release)
	burstWritten := map[string]chan struct{}{"failed-1": make(chan struct{}), "failed-2": make(chan struct{})}
	j := &acpSessionJournal{}
	h, p := wsServer(t, func(conn *websocket.Conn, op string, msg map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "fifo"})
		case "chat":
			id, opID := fmt.Sprint(msg["content"]), fmt.Sprint(msg["opId"])
			j.mu.Lock()
			j.calls = append(j.calls, id)
			j.mu.Unlock()
			switch id {
			case "gate":
				_ = conn.WriteJSON(tagged(chunkFrame("gate"), opID))
				close(firstChunk)
				<-releaseFirst
				_ = conn.WriteJSON(tagged(doneFrame(), opID))
			case "failed-1", "failed-2":
				for range chunks {
					_ = conn.WriteJSON(tagged(chunkFrame(id+";"), opID))
				}
				_ = conn.WriteJSON(map[string]any{"op": "event", "opId": opID,
					"data": map[string]any{"type": "error_text", "text": "quota-" + id}})
				done := tagged(doneFrame(), opID)
				done["data"].(map[string]any)["stopReason"] = "failed"
				_ = conn.WriteJSON(done)
				marker := 1
				if id == "failed-2" {
					marker = 2
				}
				_ = conn.WriteJSON(queueStateFrame(false, "", marker))
				close(burstWritten[id])
			case "complete-eof":
				_ = conn.WriteJSON(tagged(chunkFrame(id), opID))
				done := tagged(doneFrame(), opID)
				done["data"].(map[string]any)["stopReason"] = "end_turn"
				_ = conn.WriteJSON(done)
				_ = conn.Close()
			case "partial-eof":
				_ = conn.WriteJSON(tagged(chunkFrame(id), opID))
				_ = conn.Close()
			case "recovered":
				_ = conn.WriteJSON(tagged(chunkFrame(id), opID))
				_ = conn.WriteJSON(tagged(doneFrame(), opID))
			}
		case "cancel":
			_ = conn.WriteJSON(map[string]any{"op": "cancel_ack", "opId": msg["opId"], "status": "unknown"})
		}
	})
	c := NewACPClient(h, p).WithPassword(testBridgePassword)
	c.eventCh = make(chan json.RawMessage, 3)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	var progressOnce sync.Once
	previousID := ""
	execute := j.execute(c, func(id string, _ *ChatResult) {
		if id != previousID {
			progressOnce, previousID = sync.Once{}, id
		}
		progressOnce.Do(func() {
			if written, ok := burstWritten[id]; ok {
				select {
				case <-written:
				case <-time.After(time.Second):
					t.Error("bridge did not finish writing the burst")
					return
				}
				// A dropping reader reaches the marker while its consumer is
				// paused; the lossless reader stays at the bounded backlog.
				marker := 1
				if id == "failed-2" {
					marker = 2
				}
				deadline := time.NewTimer(50 * time.Millisecond)
				defer deadline.Stop()
				tick := time.NewTicker(time.Millisecond)
				defer tick.Stop()
				for c.BridgeState().Waiting != marker {
					select {
					case <-tick.C:
					case <-deadline.C:
						return
					}
				}
			} else if id == "complete-eof" {
				select {
				case <-c.done:
				case <-time.After(time.Second):
					t.Error("bridge did not close after writing prompt_done")
				}
			}
		})
	})
	s := chatsession.New(chatsession.Config[string]{View: acpSessionItem, Execute: execute, Publish: j.publish,
		OnIdle: func() { close(idle) }})
	if _, err := s.Enqueue(order[0], nil); err != nil {
		t.Fatal(err)
	}
	waitACPIntegration(t, firstChunk, "first queued turn")
	for _, id := range order[1:] {
		if _, err := s.Enqueue(id, nil); err != nil {
			t.Fatal(err)
		}
	}
	release()
	waitACPIntegration(t, idle, "all accepted turns to drain")
	if !s.Idle() || !s.Ready() || c.readers.Load() != 0 || c.TurnInFlight() {
		t.Fatalf("FIFO or ACP lease did not settle: snapshot=%+v readers=%d inFlight=%v", s.Snapshot(), c.readers.Load(), c.TurnInFlight())
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if !reflect.DeepEqual(j.calls, order) {
		t.Fatalf("accepted prompts must execute once in FIFO order: got %v want %v", j.calls, order)
	}
	wantEvents := map[string]string{"failed-1": chatsession.EventError, "failed-2": chatsession.EventError}
	for index, id := range order {
		res := j.results[id]
		if res == nil {
			t.Errorf("%s lost its partial or complete result", id)
			continue
		}
		if strings.HasPrefix(id, "failed-") {
			if res.Narration != strings.Repeat(id+";", chunks) || !res.Failed || res.ErrorText != "quota-"+id || res.Interrupted {
				t.Errorf("%s: text=%d failed=%v error=%q interrupted=%v", id, len(res.Narration), res.Failed, res.ErrorText, res.Interrupted)
			}
		} else if res.Narration != id || res.Interrupted != (id == "partial-eof") {
			t.Errorf("%s complete/incomplete EOF misclassified: %+v", id, res)
		}
		if id == "partial-eof" && res.ErrorText == "" {
			t.Error("partial EOF needs an explicit interruption reason")
		}
		if id == "complete-eof" && res.StopReason != "end_turn" {
			t.Errorf("complete EOF lost its authoritative prompt_done: %+v", res)
		}
		terminalEvent := wantEvents[id]
		if terminalEvent == "" {
			terminalEvent = chatsession.EventTurnDone
		}
		assertACPQueuedFrames(t, j.frames, id, terminalEvent, len(order)-index-1, id == "partial-eof")
	}
}

func assertACPQueuedFrames(t *testing.T, frames []acpSessionFrame, id, terminalEvent string, waiting int, interrupted bool) {
	t.Helper()
	var accepted, begin, terminal uint64
	var begins, terminals int
	for _, f := range frames {
		revision, _ := f.payload["revision"].(uint64)
		if f.event == chatsession.EventQueueState {
			for _, item := range f.payload["items"].([]map[string]any) {
				if item["id"] == id && item["clientMessageId"] == "client-"+id && (accepted == 0 || revision < accepted) {
					accepted = revision
				}
			}
		}
		item, _ := f.payload["item"].(map[string]any)
		if item["id"] != id {
			continue
		}
		if item["clientMessageId"] != "client-"+id {
			t.Errorf("%s lost accepted message identity: %v", id, f.payload)
		}
		if f.event == chatsession.EventTurnBegin {
			begins++
			begin = revision
			active, _ := f.payload["activeItem"].(map[string]any)
			if f.payload["busy"] != true || active["id"] != id {
				t.Errorf("%s begin lacks canonical active state: %v", id, f.payload)
			}
		} else if f.event == chatsession.EventTurnDone || f.event == chatsession.EventError {
			terminals++
			terminal = revision
			items, _ := f.payload["items"].([]map[string]any)
			if f.event != terminalEvent || f.payload["busy"] != false || f.payload["waiting"] != waiting || len(items) != waiting || f.payload["interrupted"] != interrupted || f.payload["activeItem"] != nil {
				t.Errorf("%s terminal must settle only its own item and retain %d waiters: event=%s payload=%v", id, waiting, f.event, f.payload)
			}
		}
	}
	if begins != 1 || terminals != 1 || accepted == 0 || accepted >= begin || begin >= terminal {
		t.Errorf("%s accepted/begin/terminal lifecycle: accepted=%d begin=%d (%d calls) terminal=%d (%d calls)", id, accepted, begin, begins, terminal, terminals)
	}
}

// Pending-message persistence, a slow cancellation publisher and admission
// leases meet here around a real provider cancellation. A newly accepted item
// must survive clearing the old queue, ignore old-op acknowledgements and run
// once while the stale cancellation frame is still waiting for its viewer.
func TestACPQueuedSessionCancelClearThenAdmitAcrossSlowPublisher(t *testing.T) {
	firstProgress, dropStarted, releaseDrop := make(chan struct{}), make(chan struct{}), make(chan struct{})
	delayedPublish, releasePublish, idle := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var dropOnce, publishOnce sync.Once
	releaseDropped := func() { dropOnce.Do(func() { close(releaseDrop) }) }
	releasePublisher := func() { publishOnce.Do(func() { close(releasePublish) }) }
	t.Cleanup(releaseDropped)
	t.Cleanup(releasePublisher)
	cancelTargets := make(chan string, 8)
	var firstOp string
	j := &acpSessionJournal{}
	h, p := wsServer(t, func(conn *websocket.Conn, op string, msg map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "cancel-fifo"})
		case "chat":
			id, opID := fmt.Sprint(msg["content"]), fmt.Sprint(msg["opId"])
			j.mu.Lock()
			j.calls = append(j.calls, id)
			j.mu.Unlock()
			if id == "first" {
				firstOp = opID
				_ = conn.WriteJSON(tagged(chunkFrame("partial first"), opID))
				return
			}
			// Trailing frames are deliberately delivered after the new chat
			// request, beyond runTurn's initial drainEvents boundary.
			_ = conn.WriteJSON(tagged(chunkFrame("stale first"), firstOp))
			_ = conn.WriteJSON(tagged(doneFrame(), firstOp))
			_ = conn.WriteJSON(tagged(chunkFrame("survivor"), opID))
			_ = conn.WriteJSON(tagged(doneFrame(), opID))
		case "cancel":
			cancelTargets <- fmt.Sprint(msg["opId"])
			done := tagged(doneFrame(), firstOp)
			done["data"].(map[string]any)["stopReason"] = "cancelled"
			_ = conn.WriteJSON(done)
		}
	})
	c := connectAndClient(t, h, p)
	var progressOnce sync.Once
	execute := j.execute(c, func(id string, _ *ChatResult) {
		if id == "first" {
			progressOnce.Do(func() { close(firstProgress) })
		}
	})
	var registry chatsession.Registry[*chatsession.Session[string]]
	var s *chatsession.Session[string]
	var cancelling atomic.Bool
	var droppedPersisted atomic.Bool
	var firstCtx context.Context
	var nativeTarget string
	var dropped []string
	create := func() *chatsession.Session[string] {
		return chatsession.New(chatsession.Config[string]{
			View: acpSessionItem,
			Execute: func(ctx context.Context, id string) (bool, error) {
				if id == "first" {
					firstCtx = ctx
				}
				interrupted, err := execute(ctx, id)
				if id == "first" && !droppedPersisted.Load() {
					t.Error("provider turn finalized before pending-message persistence")
				}
				return interrupted, err
			},
			OnDropped: func(items []string) {
				dropped = append([]string(nil), items...)
				if firstCtx.Err() != nil {
					t.Error("active provider context cancelled before pending-message persistence")
				}
				close(dropStarted)
				<-releaseDrop
				droppedPersisted.Store(true)
			},
			CancelTurn: func() {
				nativeTarget, _ = s.Active()
				if !c.AbortRunning(time.Second) {
					t.Error("native provider cancellation failed")
				}
			},
			Publish: func(event string, payload map[string]any) {
				j.publish(event, payload)
				active, _ := payload["activeItem"].(map[string]any)
				if cancelling.Load() && event == chatsession.EventQueueState && active["id"] == "first" && payload["waiting"] == 0 {
					close(delayedPublish)
					<-releasePublish
				}
			},
			OnIdle: func() {
				registry.DropIfIdle("thread")
				close(idle)
			},
		})
	}
	var releaseFirstLease func()
	s, releaseFirstLease = registry.AcquireOrCreate("thread", create)
	if _, err := s.Enqueue("first", nil); err != nil {
		t.Fatal(err)
	}
	releaseFirstLease()
	waitACPIntegration(t, firstProgress, "active provider content")
	for _, id := range []string{"drop-1", "drop-2"} {
		if _, err := s.Enqueue(id, nil); err != nil {
			t.Fatal(err)
		}
	}
	cancelling.Store(true)
	cancelDone := make(chan struct{})
	go func() { s.Cancel(true); close(cancelDone) }()
	waitACPIntegration(t, dropStarted, "pending-message persistence")
	same, releaseAdmission := registry.AcquireOrCreate("thread", create)
	t.Cleanup(releaseAdmission)
	if same != s {
		t.Fatal("concurrent admission detached the active FIFO")
	}
	if _, err := same.Enqueue("survivor", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case target := <-cancelTargets:
		t.Errorf("provider cancellation escaped pending persistence: %q", target)
	default:
	}
	releaseDropped()
	waitACPIntegration(t, delayedPublish, "slow cancellation viewer")
	waitACPIntegration(t, idle, "surviving accepted message despite slow cancellation viewer")
	if nativeTarget != "first" || !reflect.DeepEqual(dropped, []string{"drop-1", "drop-2"}) {
		t.Errorf("cancel boundary: native target=%q dropped=%v", nativeTarget, dropped)
	}
	if got, ok := registry.Get("thread"); !ok || got != s {
		t.Error("idle cleanup discarded a FIFO still pinned by admission")
	}
	if c.readers.Load() != 0 || c.TurnInFlight() || !s.Idle() {
		t.Errorf("cancel leaked ACP consumer or FIFO: readers=%d inFlight=%v snapshot=%+v", c.readers.Load(), c.TurnInFlight(), s.Snapshot())
	}
	final := s.Snapshot()
	releasePublisher()
	waitACPIntegration(t, cancelDone, "late cancellation publication")
	if got := s.Snapshot(); got.Revision != final.Revision || got.Busy || got.Waiting != 0 {
		t.Errorf("late cancellation regressed canonical FIFO: before=%+v after=%+v", final, got)
	}
	releaseAdmission()
	if _, ok := registry.Get("thread"); ok {
		t.Error("final admission release retained an idle FIFO")
	}
	if replacement, releaseReplacement := registry.AcquireOrCreate("thread", create); replacement.Snapshot().SessionGeneration <= final.SessionGeneration {
		releaseReplacement()
		t.Error("replacement FIFO did not advance the authoritative generation")
	} else {
		releaseReplacement()
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if !reflect.DeepEqual(j.calls, []string{"first", "survivor"}) {
		t.Errorf("dropped or accepted prompt executed twice: %v", j.calls)
	}
	first, survivor := j.results["first"], j.results["survivor"]
	if first == nil || first.Narration != "partial first" || !first.Interrupted || (first.ErrorText == "" && first.StopReason != "cancelled") {
		t.Errorf("cancelled partial result was lost: %+v", first)
	}
	if survivor == nil || survivor.Narration != "survivor" || survivor.Interrupted || survivor.ErrorText != "" {
		t.Errorf("old cancellation contaminated the next accepted turn: %+v", survivor)
	}
	assertACPQueuedFrames(t, j.frames, "first", chatsession.EventTurnDone, 1, true)
	assertACPQueuedFrames(t, j.frames, "survivor", chatsession.EventTurnDone, 0, false)
}
