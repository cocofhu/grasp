package services

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// A woken guest accepts connections before its bridge serves: the first turn
// after a provider pause must wait for the bridge instead of failing.
func TestSoloFirstTurnAfterProviderPauseE2E(t *testing.T) {
	acp, port := fakeACPServer(t)
	t.Cleanup(acp.Close)
	var booting atomic.Bool
	bridge := acp.Config.Handler
	acp.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if booting.Load() {
			http.Error(w, "bridge starting", http.StatusBadGateway)
			return
		}
		bridge.ServeHTTP(w, r)
	})
	solo, sbx, fg, db := newSoloFixture(t, port, 3)
	sbx.soloBridgeReady = nil
	insertSoloModelConfig(t, db)
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	id := view.ID
	waitFor(t, "running", func() bool { r, _ := solo.load(id); return r.Status == models.SoloStatusRunning })
	replies := func() []models.SoloMessage {
		msgs, _ := solo.Messages(id, "alice")
		var out []models.SoloMessage
		for _, m := range msgs {
			if m.Role == "assistant" {
				out = append(out, m)
			}
		}
		return out
	}
	send := func(text string, n int) models.SoloMessage {
		t.Helper()
		if _, err := solo.SendWithReferences(id, "alice", text, nil, nil); err != nil {
			t.Fatal(err)
		}
		waitFor(t, text+" answered", func() bool {
			r := replies()
			return len(r) >= n && r[n-1].Status != ""
		})
		return replies()[n-1]
	}
	if r := send("before", 1); r.Status != "ok" {
		t.Fatalf("baseline turn: %s %s", r.Status, r.Error)
	}

	row, _ := solo.load(id)
	sb, _ := sbx.Get(row.SandboxID)
	fg.SetStatus(sb.Name, "stopped")
	booting.Store(true)
	time.AfterFunc(2500*time.Millisecond, func() { booting.Store(false) })
	if r := send("after pause", 2); r.Status != "ok" {
		t.Fatalf("first turn after pause: %s %s", r.Status, r.Error)
	}
	if r := send("again", 3); r.Status != "ok" {
		t.Fatalf("second turn after pause: %s %s", r.Status, r.Error)
	}
}
