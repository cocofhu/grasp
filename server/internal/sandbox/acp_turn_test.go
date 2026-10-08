package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestMain(m *testing.M) {
	cancelAckWait = 300 * time.Millisecond
	os.Exit(m.Run())
}

func tagged(frame map[string]any, opID string) map[string]any {
	frame["opId"] = opID
	return frame
}

func queueStateFrame(busy bool, runningOpID string, waiting int) map[string]any {
	f := map[string]any{"op": "queue_state", "busy": busy, "queue_length": waiting}
	if runningOpID != "" {
		f["running"] = map[string]any{"opId": runningOpID}
	}
	return f
}

// A tagging bridge still streaming a stale turn: frames of the other opId must
// neither leak into this turn nor end it.
func TestRunTurnIgnoresOtherOpFrames(t *testing.T) {
	h, p := wsServer(t, func(conn *websocket.Conn, op string, msg map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		case "chat":
			mine, _ := msg["opId"].(string)
			_ = conn.WriteJSON(tagged(chunkFrame("stale "), "old-op"))
			_ = conn.WriteJSON(tagged(doneFrame(), "old-op"))
			_ = conn.WriteJSON(tagged(chunkFrame("fresh"), mine))
			_ = conn.WriteJSON(tagged(doneFrame(), mine))
		}
	})
	c := connectAndClient(t, h, p)
	res, err := c.ChatStructured(context.Background(), "hi", nil)
	if err != nil {
		t.Fatalf("ChatStructured: %v", err)
	}
	if res.Narration != "fresh" || res.Interrupted {
		t.Fatalf("narration=%q interrupted=%v", res.Narration, res.Interrupted)
	}
	if !strings.HasPrefix(res.OpID, "g-") {
		t.Fatalf("opId = %q", res.OpID)
	}
}

// Untagged event frames are never part of a turn.
func TestRunTurnIgnoresUntaggedEvents(t *testing.T) {
	h, p := wsServer(t, func(conn *websocket.Conn, op string, msg map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		case "chat":
			_ = conn.WriteJSON(chunkFrame("a"))
			_ = conn.WriteJSON(untagged(chunkFrame("x")))
			_ = conn.WriteJSON(untagged(doneFrame()))
			_ = conn.WriteJSON(chunkFrame("b"))
			_ = conn.WriteJSON(doneFrame())
		}
	})
	c := connectAndClient(t, h, p)
	res, err := c.ChatStructured(context.Background(), "hi", nil)
	if err != nil || res.Narration != "ab" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

// Idle abort sends cancel(opId); the bridge acks with the turn's prompt_done
// and the partial reply comes back marked Interrupted, bridge not desynced.
func TestRunTurnIdleCancelAck(t *testing.T) {
	var mu sync.Mutex
	var cancelled string
	h, p := wsServer(t, func(conn *websocket.Conn, op string, msg map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		case "chat":
			mine, _ := msg["opId"].(string)
			_ = conn.WriteJSON(tagged(chunkFrame("partial"), mine))
		case "cancel":
			id, _ := msg["opId"].(string)
			mu.Lock()
			cancelled = id
			mu.Unlock()
			_ = conn.WriteJSON(map[string]any{"op": "cancel_ack", "opId": id, "status": "cancelling"})
			done := doneFrame()
			done["data"].(map[string]any)["stopReason"] = "cancelled"
			_ = conn.WriteJSON(tagged(done, id))
			_ = conn.WriteJSON(queueStateFrame(false, "", 0))
		}
	})
	c := connectAndClient(t, h, p).WithIdleTimeout(150 * time.Millisecond)
	res, err := c.ChatStructured(context.Background(), "hi", nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !res.Interrupted || res.Narration != "partial" || res.ErrorText == "" {
		t.Fatalf("res = %+v", res)
	}
	mu.Lock()
	defer mu.Unlock()
	if cancelled != res.OpID {
		t.Fatalf("cancel opId = %q, want %q", cancelled, res.OpID)
	}
	if c.BridgeState().Desynced {
		t.Fatal("acked cancel must not mark desynced")
	}
}

// No ack at all: the client gives up, reports ErrChatIdle and flags desync
// until the bridge reports idle again.
func TestRunTurnCancelNoAckDesync(t *testing.T) {
	var connMu sync.Mutex
	var server *websocket.Conn
	h, p := wsServer(t, func(conn *websocket.Conn, op string, _ map[string]any) {
		connMu.Lock()
		defer connMu.Unlock()
		server = conn
		if op == "connect" {
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		}
	})
	c := connectAndClient(t, h, p).WithIdleTimeout(100 * time.Millisecond)
	_, err := c.ChatStructured(context.Background(), "hi", nil)
	if !errors.Is(err, ErrChatIdle) {
		t.Fatalf("err = %v, want ErrChatIdle", err)
	}
	if !c.BridgeState().Desynced {
		t.Fatal("unacknowledged cancel should mark desynced")
	}
	connMu.Lock()
	_ = server.WriteJSON(queueStateFrame(false, "", 0))
	connMu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for c.BridgeState().Desynced {
		if time.Now().After(deadline) {
			t.Fatal("idle queue_state should clear desync")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Bridge watchdog timeout with no content surfaces ErrSandboxTurnTimeout with
// the bridge's explanation.
func TestRunTurnBridgeTimeout(t *testing.T) {
	h, p := wsServer(t, func(conn *websocket.Conn, op string, msg map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		case "chat":
			mine, _ := msg["opId"].(string)
			_ = conn.WriteJSON(map[string]any{"op": "event", "opId": mine,
				"data": map[string]any{"type": "error_text", "text": "连续 10m 没有任何输出"}})
			done := doneFrame()
			done["data"].(map[string]any)["stopReason"] = "timeout"
			_ = conn.WriteJSON(tagged(done, mine))
		}
	})
	c := connectAndClient(t, h, p)
	_, err := c.ChatStructured(context.Background(), "hi", nil)
	if !errors.Is(err, ErrSandboxTurnTimeout) || !errors.Is(err, ErrChatIdle) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "没有任何输出") {
		t.Fatalf("err should carry bridge reason: %v", err)
	}
}

func TestBridgeStateFinishedOpIsNotRunning(t *testing.T) {
	c := &ACPClient{}
	c.lastDoneOpID.Store("g-00fccbf7-0f2")
	c.stateMu.Lock()
	c.bridge = BridgeState{
		Known: true, Busy: true, Desynced: true,
		RunningOpID: "g-00fccbf7-0f2", Waiting: 0,
	}
	c.stateMu.Unlock()
	st := c.BridgeState()
	if st.Busy || st.Desynced || st.RunningOpID != "" {
		t.Fatalf("plan g1.1: finished op still live: %+v", st)
	}
	if st.LastDoneOpID != "g-00fccbf7-0f2" {
		t.Fatalf("last done = %q", st.LastDoneOpID)
	}
}

func TestBridgeStateOtherOpStaysOrphan(t *testing.T) {
	c := &ACPClient{}
	c.lastDoneOpID.Store("g-done")
	c.stateMu.Lock()
	c.bridge = BridgeState{
		Known: true, Busy: true, Desynced: true,
		RunningOpID: "g-other", Waiting: 0,
	}
	c.stateMu.Unlock()
	st := c.BridgeState()
	if !st.Busy || !st.Desynced || st.RunningOpID != "g-other" {
		t.Fatalf("plan g2.2: other op cleared: %+v", st)
	}
}

func TestBridgeStateMirror(t *testing.T) {
	var connMu sync.Mutex
	var server *websocket.Conn
	h, p := wsServer(t, func(conn *websocket.Conn, op string, _ map[string]any) {
		connMu.Lock()
		defer connMu.Unlock()
		server = conn
		if op == "connect" {
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		}
	})
	c := connectAndClient(t, h, p)
	if c.BridgeState().Known {
		t.Fatal("state should be unknown before any queue_state")
	}
	connMu.Lock()
	_ = server.WriteJSON(queueStateFrame(true, "op-x", 2))
	connMu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for {
		st := c.BridgeState()
		if st.Known {
			if !st.Busy || st.RunningOpID != "op-x" || st.Waiting != 2 {
				t.Fatalf("state = %+v", st)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("queue_state not mirrored")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A parked client (connected, no chat or cancel in flight) still mirrors
// queue_state but must not buffer the bridge's broadcasts, otherwise eventCh
// fills and every later frame logs a drop.
func TestParkedClientDoesNotBufferBroadcasts(t *testing.T) {
	flood := make(chan struct{})
	flooded := make(chan struct{})
	h, p := wsServer(t, func(conn *websocket.Conn, op string, msg map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "sess-park"})
			go func() {
				<-flood
				for i := 0; i < 2000; i++ {
					_ = conn.WriteJSON(queueStateFrame(i%2 == 0, "", 0))
				}
				_ = conn.WriteJSON(queueStateFrame(true, "op-other", 2))
				close(flooded)
			}()
		case "chat":
			opID := fmt.Sprint(msg["opId"])
			_ = conn.WriteJSON(tagged(chunkFrame("after park"), opID))
			_ = conn.WriteJSON(tagged(doneFrame(), opID))
		}
	})
	c := connectAndClient(t, h, p)
	close(flood)
	<-flooded

	deadline := time.Now().Add(2 * time.Second)
	for c.BridgeState().Waiting != 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if st := c.BridgeState(); !st.Busy || st.Waiting != 2 {
		t.Fatalf("queue_state must still be mirrored while parked: %+v", st)
	}
	if n := len(c.eventCh); n != 0 {
		t.Fatalf("parked client buffered %d frames", n)
	}
	if n := c.dropped.Load(); n != 0 || c.lastDropLog.Load() != 0 {
		t.Fatalf("parked client must not drop/warn, dropped=%d", n)
	}

	res, err := c.ChatStructured(context.Background(), "hi", nil)
	if err != nil || res.Narration != "after park" {
		t.Fatalf("chat after park: res=%+v err=%v", res, err)
	}
	if n := c.readers.Load(); n != 0 {
		t.Fatalf("readers leaked: %d", n)
	}
}

func TestNoteDroppedRateLimits(t *testing.T) {
	c := NewACPClient("127.0.0.1", 1)
	c.noteDropped()
	first := c.lastDropLog.Load()
	if first == 0 || c.dropped.Load() != 0 {
		t.Fatalf("first drop should log and reset the counter: last=%d dropped=%d", first, c.dropped.Load())
	}
	for i := 0; i < 5; i++ {
		c.noteDropped()
	}
	if c.lastDropLog.Load() != first || c.dropped.Load() != 5 {
		t.Fatalf("drops inside the window should only count: last=%d dropped=%d", c.lastDropLog.Load(), c.dropped.Load())
	}
	c.lastDropLog.Store(time.Now().Add(-2 * dropLogEvery).UnixNano())
	c.noteDropped()
	if c.dropped.Load() != 0 {
		t.Fatalf("drop after the window should log the backlog, dropped=%d", c.dropped.Load())
	}
}

func TestAcquireReaderReleaseIsIdempotent(t *testing.T) {
	c := NewACPClient("127.0.0.1", 1)
	r1 := c.acquireReader()
	r2 := c.acquireReader()
	r1()
	r1()
	if n := c.readers.Load(); n != 1 {
		t.Fatalf("readers=%d want 1", n)
	}
	r2()
	if n := c.readers.Load(); n != 0 {
		t.Fatalf("readers=%d want 0", n)
	}
}

func TestRunTurnSendsTurnLimits(t *testing.T) {
	var mu sync.Mutex
	var got map[string]any
	h, p := wsServer(t, func(conn *websocket.Conn, op string, msg map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		case "chat":
			mu.Lock()
			got = msg
			mu.Unlock()
			mine, _ := msg["opId"].(string)
			_ = conn.WriteJSON(tagged(chunkFrame("ok"), mine))
			_ = conn.WriteJSON(tagged(doneFrame(), mine))
		}
	})
	c := connectAndClient(t, h, p).WithIdleTimeoutFunc(func() time.Duration { return 20 * time.Minute })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if _, err := c.ChatStructured(ctx, "hi", nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got["idleSec"] != float64(1200) {
		t.Fatalf("idleSec = %v", got["idleSec"])
	}
	if dl, _ := got["deadlineSec"].(float64); dl < 655 || dl > 660 {
		t.Fatalf("deadlineSec = %v, want ctx remaining + 60", got["deadlineSec"])
	}
}

func TestRunTurnWithoutDeadlineOmitsDeadlineSec(t *testing.T) {
	msg := map[string]any{}
	turnLimitFields(msg, context.Background(), 0)
	if len(msg) != 0 {
		t.Fatalf("msg = %v", msg)
	}
}

// Heartbeats keep a turn alive well past the legacy no-frame window: a quiet
// but busy Agent (long build, long reasoning) is not abandoned.
func TestRunTurnHeartbeatsKeepTurnAlive(t *testing.T) {
	h, p := wsServer(t, func(conn *websocket.Conn, op string, msg map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		case "chat":
			mine, _ := msg["opId"].(string)
			for i := 0; i < 12; i++ {
				_ = conn.WriteJSON(map[string]any{"op": "liveness", "opId": mine, "data": map[string]any{
					"active": true, "cpuMs": 900, "ioBytes": 4096, "idleSec": 0, "limitSec": 1, "lastTool": "go test ./...",
				}})
				time.Sleep(50 * time.Millisecond)
			}
			_ = conn.WriteJSON(tagged(chunkFrame("done"), mine))
			_ = conn.WriteJSON(tagged(doneFrame(), mine))
		}
	})
	old := bridgeLostAfter
	bridgeLostAfter = 300 * time.Millisecond
	defer func() { bridgeLostAfter = old }()
	c := connectAndClient(t, h, p).WithIdleTimeout(100 * time.Millisecond)
	var progressed bool
	res, err := c.ChatStreamResult(context.Background(), "hi", nil, func(r *ChatResult) {
		if r.Liveness != nil {
			progressed = true
		}
	})
	if err != nil || res.Interrupted || res.Narration != "done" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if res.Liveness == nil || !res.Liveness.Active || res.Liveness.CPUMs != 900 || res.Liveness.LastTool != "go test ./..." {
		t.Fatalf("liveness = %+v", res.Liveness)
	}
	if !progressed {
		t.Fatal("heartbeats should be reported through onProgress")
	}
}

// Once the bridge has sent heartbeats, silence past bridgeLostAfter means the
// bridge is gone, however long the Agent no-activity limit is.
func TestRunTurnBridgeLostAfterHeartbeats(t *testing.T) {
	h, p := wsServer(t, func(conn *websocket.Conn, op string, msg map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		case "chat":
			mine, _ := msg["opId"].(string)
			_ = conn.WriteJSON(map[string]any{"op": "liveness", "opId": mine, "data": map[string]any{"active": false}})
		}
	})
	old := bridgeLostAfter
	bridgeLostAfter = 150 * time.Millisecond
	defer func() { bridgeLostAfter = old }()
	c := connectAndClient(t, h, p).WithIdleTimeout(time.Hour)
	start := time.Now()
	_, err := c.ChatStructured(context.Background(), "hi", nil)
	if !errors.Is(err, ErrChatIdle) || !strings.Contains(err.Error(), "no frame from the sandbox") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("took %s; the heartbeat window should apply", time.Since(start))
	}
}

func TestLegacyWatch(t *testing.T) {
	if got := legacyWatch(20 * time.Minute); got != 42*time.Minute {
		t.Fatalf("legacyWatch(20m) = %s", got)
	}
	if got := legacyWatch(time.Minute); got != 3*time.Minute {
		t.Fatalf("legacyWatch(1m) = %s", got)
	}
	if got := legacyWatch(0); got != 0 {
		t.Fatalf("legacyWatch(0) = %s", got)
	}
}

// A turn the bridge stopped as stuck is an error even with partial output; the
// result still comes back so its usage is counted.
func TestRunTurnStuck(t *testing.T) {
	h, p := wsServer(t, func(conn *websocket.Conn, op string, msg map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		case "chat":
			mine, _ := msg["opId"].(string)
			_ = conn.WriteJSON(tagged(chunkFrame("working"), mine))
			_ = conn.WriteJSON(map[string]any{"op": "event", "opId": mine,
				"data": map[string]any{"type": "error_text", "text": "Agent 连续 20 分钟没有任何活动"}})
			done := doneFrame()
			done["data"].(map[string]any)["stopReason"] = "stuck"
			_ = conn.WriteJSON(tagged(done, mine))
		}
	})
	c := connectAndClient(t, h, p)
	res, err := c.ChatStructured(context.Background(), "hi", nil)
	if !errors.Is(err, ErrAgentStuck) || errors.Is(err, ErrChatIdle) {
		t.Fatalf("err = %v", err)
	}
	if res == nil || res.Narration != "working" || !strings.Contains(err.Error(), "没有任何活动") {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestRunTurnStuckDefaultReason(t *testing.T) {
	c := &ACPClient{}
	_, err := c.finishTurn(&ChatResult{OpID: "g-1", StopReason: stopReasonStuck})
	if !errors.Is(err, ErrAgentStuck) || !strings.Contains(err.Error(), "已被终止") {
		t.Fatalf("err = %v", err)
	}
}

type budgetCause struct{}

func (budgetCause) Error() string { return "节点运行超过总时限 1 分钟" }

// A ctx deadline carrying a cause reports that cause, not a generic message.
func TestRunTurnDeadlineCauseReason(t *testing.T) {
	h, p := wsServer(t, func(conn *websocket.Conn, op string, msg map[string]any) {
		switch op {
		case "connect":
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		case "chat":
			mine, _ := msg["opId"].(string)
			_ = conn.WriteJSON(tagged(chunkFrame("partial"), mine))
		case "cancel":
			id, _ := msg["opId"].(string)
			_ = conn.WriteJSON(tagged(doneFrame(), id))
		}
	})
	c := connectAndClient(t, h, p)
	ctx, cancel := context.WithDeadlineCause(context.Background(), time.Now().Add(150*time.Millisecond), budgetCause{})
	defer cancel()
	res, err := c.ChatStructured(ctx, "hi", nil)
	if err != nil || !res.Interrupted || !strings.Contains(res.ErrorText, "节点运行超过总时限") {
		t.Fatalf("res=%+v err=%v", res, err)
	}

	h2, p2 := wsServer(t, func(conn *websocket.Conn, op string, _ map[string]any) {
		if op == "connect" {
			_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
		}
	})
	c2 := connectAndClient(t, h2, p2)
	ctx2, cancel2 := context.WithDeadlineCause(context.Background(), time.Now().Add(100*time.Millisecond), budgetCause{})
	defer cancel2()
	if _, err := c2.ChatStructured(ctx2, "hi", nil); !errors.As(err, new(budgetCause)) {
		t.Fatalf("err = %v, want the deadline cause", err)
	}
}
