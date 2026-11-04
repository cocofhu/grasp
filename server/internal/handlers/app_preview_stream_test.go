package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/gin-gonic/gin"
)

func previewHistoryFixture(size int) *previewMainHistory {
	history := &previewMainHistory{Rows: make([]sharedPreviewRow, 0, size+1)}
	for i := 0; i < size; i++ {
		history.Rows = append(history.Rows, sharedPreviewRow{ID: fmt.Sprint(i), Role: "assistant", Text: strings.Repeat("historical content ", 100)})
	}
	history.Rows = append(history.Rows, sharedPreviewRow{ID: "current-user", Role: "user", Text: "Current question"})
	return history
}

func TestPreviewMainTailCarriesActiveTurnChangesAndClearsItWhenIdle(t *testing.T) {
	first, err := newPreviewMainSnapshot(&previewMainHistory{Busy: true, ActiveTurnID: "first"})
	if err != nil {
		t.Fatal(err)
	}
	next, err := newPreviewMainSnapshot(&previewMainHistory{Busy: true, ActiveTurnID: "next"})
	if err != nil {
		t.Fatal(err)
	}
	if next.frameAfter(first)["activeTurnId"] != "next" {
		t.Fatal("active target change missing from tail")
	}
	idle, err := next.withLive(nil, false)
	if err != nil || idle.frameAfter(next)["activeTurnId"] != "" || next.history.ActiveTurnID != "next" {
		t.Fatalf("idle did not clear target immutably: %v", err)
	}
}

func TestPreviewMainTailKeepsHistoryImmutableAndSkipsUnchangedPrefix(t *testing.T) {
	seed, err := newPreviewMainSnapshot(previewHistoryFixture(1000))
	if err != nil {
		t.Fatal(err)
	}
	live := &models.SoloMessage{ID: "live:current-user", Role: "assistant", Content: "First words"}
	first, err := seed.withLive(live, true)
	if err != nil {
		t.Fatal(err)
	}
	live.Content = "Complete streamed reply"
	second, err := first.withLive(live, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(seed.history.Rows) != 1001 || first.history.Rows[1001].Text != "First words" || second.history.Rows[1001].Text != live.Content {
		t.Fatal("live updates mutated an earlier snapshot")
	}
	if &seed.rows[0][0] != &second.rows[0][0] {
		t.Fatal("live updates re-encoded sealed history")
	}
	// A slow viewer may skip the first update and still apply the latest tail.
	frame := second.frameAfter(seed)
	if frame["type"] != "tail" || frame["start"] != 1001 || len(frame["rows"].([]json.RawMessage)) != 1 {
		t.Fatalf("tail: %+v", frame)
	}
	wire, err := json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	if len(wire) > 1000 || strings.Contains(string(wire), "historical content") || !strings.Contains(string(wire), live.Content) {
		t.Fatalf("tail retransmitted history (%d bytes)", len(wire))
	}
	truncated, err := newPreviewMainSnapshot(seed.history)
	if err != nil {
		t.Fatal(err)
	}
	removed := truncated.frameAfter(second)
	if removed["start"] != 1001 || len(removed["rows"].([]json.RawMessage)) != 0 {
		t.Fatalf("removed live row was not truncated: %+v", removed)
	}
	settledHistory := previewHistoryFixture(0)
	settledHistory.Rows = append(settledHistory.Rows,
		sharedPreviewRow{ID: "system", Role: "system"},
		sharedPreviewRow{ID: "saved-reply", Role: "assistant", Text: "Saved"})
	settled, err := newPreviewMainSnapshot(settledHistory)
	if err != nil {
		t.Fatal(err)
	}
	if next, err := settled.withLive(live, true); err != nil || next != settled {
		t.Fatalf("live reply duplicated a saved reply after a system row: %v", err)
	}
}

func TestPreviewMainFanoutSharesSnapshotsAndCoalescesSlowViewers(t *testing.T) {
	h := &Handlers{}
	fast, slow := make(chan previewMainUpdate, 1), make(chan previewMainUpdate, 1)
	stream := &previewMainStream{subscribers: map[chan previewMainUpdate]struct{}{fast: {}, slow: {}}}
	var latest *previewMainSnapshot
	for i := 0; i < 100; i++ {
		latest = &previewMainSnapshot{history: &previewMainHistory{Busy: i%2 == 0}}
		h.publishPreviewMain(stream, previewMainUpdate{snapshot: latest})
		if (<-fast).snapshot != latest {
			t.Fatal("fast subscriber received another snapshot")
		}
	}
	if len(slow) != 1 || (<-slow).snapshot != latest {
		t.Fatal("slow subscriber retained old snapshots")
	}
}

func TestPreviewMainStreamIsReleasedByItsLastViewer(t *testing.T) {
	link := &models.GateShareLink{RunID: "run", NodeID: "node", CreatedBy: "owner", Iteration: 1}
	sandbox := models.Sandbox{ID: 42}
	key := previewMainStreamKey{link.RunID, link.NodeID, link.CreatedBy, sandbox.ID, link.Iteration}
	cancellations := 0
	stream := &previewMainStream{
		cancel: func() { cancellations++ }, subscribers: make(map[chan previewMainUpdate]struct{}),
		latest: previewMainUpdate{snapshot: &previewMainSnapshot{history: &previewMainHistory{Rows: []sharedPreviewRow{}}}},
	}
	h := &Handlers{previewMainStreams: map[previewMainStreamKey]*previewMainStream{key: stream}}
	first, releaseFirst := h.subscribePreviewMain(link, sandbox)
	second, releaseSecond := h.subscribePreviewMain(link, sandbox)
	if (<-first).snapshot != (<-second).snapshot || len(h.previewMainStreams) != 1 {
		t.Fatal("viewers did not share the cached snapshot")
	}
	releaseFirst()
	if cancellations != 0 || len(stream.subscribers) != 1 {
		t.Fatal("one viewer cancelled another viewer's source")
	}
	releaseSecond()
	releaseSecond()
	if cancellations != 1 || len(h.previewMainStreams) != 0 {
		t.Fatal("last viewer did not release the source exactly once")
	}
	if _, open := <-second; open {
		t.Fatal("released subscription stayed open")
	}
}

func TestPreviewMainErrorsKeepTheirHTTPStatus(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{errPreviewMainUnavailable, http.StatusConflict},
		{fmt.Errorf("state changed: %w", errPreviewMainUnavailable), http.StatusConflict},
		{errPreviewMainServiceUnavailable, http.StatusServiceUnavailable},
		{errors.New("database unavailable"), http.StatusInternalServerError},
	} {
		response := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(response)
		writePreviewMainError(ctx, tc.err)
		if response.Code != tc.status {
			t.Fatalf("%v: got %d want %d", tc.err, response.Code, tc.status)
		}
	}
}

func TestPreviewSoloPartsDoNotDuplicateThoughtAndTools(t *testing.T) {
	row := previewSoloRow(models.SoloMessage{ID: "reply", Role: "assistant", Content: "Done", Events: []json.RawMessage{
		json.RawMessage(`{"type":"session_update","update":{"sessionUpdate":"agent_thought_chunk","content":{"text":"Thinking"}}}`),
		json.RawMessage(`{"type":"session_update","update":{"sessionUpdate":"tool_call","toolCallId":"read","title":"read_file","status":"completed"}}`),
		json.RawMessage(`{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"Done"}}}`),
	}})
	if len(row.Parts) != 3 || row.Thought != "" || len(row.Tools) != 0 {
		t.Fatalf("duplicated process fields: %+v", row)
	}
}

func BenchmarkPreviewMainLiveTail(b *testing.B) {
	for _, historySize := range []int{10, 1000} {
		for _, viewers := range []int{1, 10} {
			b.Run(fmt.Sprintf("history=%d/viewers=%d", historySize, viewers), func(b *testing.B) {
				current, err := newPreviewMainSnapshot(previewHistoryFixture(historySize))
				if err != nil {
					b.Fatal(err)
				}
				live := &models.SoloMessage{ID: "live:current-user", Role: "assistant"}
				b.ReportAllocs()
				b.ResetTimer()
				var bytes int
				for i := 0; i < b.N; i++ {
					live.Content = fmt.Sprintf("Increment %d", i)
					next, err := current.withLive(live, true)
					if err != nil {
						b.Fatal(err)
					}
					for n := 0; n < viewers; n++ {
						wire, err := json.Marshal(next.frameAfter(current))
						if err != nil {
							b.Fatal(err)
						}
						bytes = len(wire)
					}
					current = next
				}
				b.ReportMetric(float64(bytes), "tail-bytes/viewer")
			})
		}
	}
}
