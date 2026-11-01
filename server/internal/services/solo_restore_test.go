package services

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
)

// restoreChat blocks every turn until release closes and records prompts.
type restoreChat struct {
	mu      sync.Mutex
	prompts []string
	started chan string
	release chan struct{}
}

func newRestoreChat(blocking bool) *restoreChat {
	c := &restoreChat{started: make(chan string, 16), release: make(chan struct{})}
	if !blocking {
		close(c.release)
	}
	return c
}

func (c *restoreChat) ChatWithTimeout(ctx context.Context, _ uint, text string, _ []models.PromptImage, _ time.Duration, onEvent func(json.RawMessage)) (*models.TokenUsage, models.TokenUsageByModel, error) {
	c.mu.Lock()
	c.prompts = append(c.prompts, text)
	c.mu.Unlock()
	c.started <- text
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case <-c.release:
	}
	onEvent(json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"reply to ` + text + `"}}`))
	return nil, nil, nil
}

func (*restoreChat) Cancel(uint) {}

func (c *restoreChat) seen() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.prompts...)
}

func pendingTurnCount(t *testing.T, solo *SoloService, id string) (total int, started int) {
	t.Helper()
	sessions, _, err := solo.pending.Load(soloPendingSurface)
	if err != nil {
		t.Fatal(err)
	}
	for _, turn := range sessions[id] {
		total++
		if turn.Started {
			started++
		}
	}
	return total, started
}

func TestSoloQueuedTurnsSurviveRestart(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, sbx, _, db := newSoloFixture(t, port, 3)
	insertSoloModelConfig(t, db)
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	id := view.ID
	waitFor(t, "running sandbox", func() bool {
		row, err := solo.Require(id, "alice")
		return err == nil && row.Status == models.SoloStatusRunning
	})
	before := newRestoreChat(true)
	solo.SetChatterForTest(before)
	solo.SetModelAlignerForTest(&recordAligner{})
	if _, err := solo.Send(id, "alice", "first", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-before.started:
	case <-time.After(5 * time.Second):
		t.Fatal("first turn did not start")
	}
	for _, text := range []string{"second", "third"} {
		if _, err := solo.Send(id, "alice", text, nil); err != nil {
			t.Fatal(err)
		}
	}
	if total, started := pendingTurnCount(t, solo, id); total != 3 || started != 1 {
		t.Fatalf("stored turns = %d (started %d), want 3 with the active one started", total, started)
	}

	// A new service on the same database stands in for the next process.
	restarted := NewSoloService(db, sbx, solo.agents, solo.projects)
	after := newRestoreChat(false)
	restarted.SetChatterForTest(after)
	restarted.SetModelAlignerForTest(&recordAligner{})
	restarted.RestoreQueuedTurns()

	waitFor(t, "restored turns answered", func() bool {
		msgs, _ := restarted.Messages(id, "alice")
		n := 0
		for _, m := range msgs {
			if m.Role == "assistant" && m.Status == "ok" {
				n++
			}
		}
		return n == 2
	})
	if got := after.seen(); len(got) != 2 || !strings.HasSuffix(got[0], "second") || !strings.HasSuffix(got[1], "third") {
		t.Fatalf("restored prompts = %q, want second then third", got)
	}
	msgs, _ := restarted.Messages(id, "alice")
	var order []string
	for _, m := range msgs {
		switch m.Role {
		case "user":
			order = append(order, m.Role+":"+m.Content+":"+m.Status)
		case "assistant":
			order = append(order, m.Role+":"+m.Status+":"+m.Error)
		}
	}
	want := []string{
		"user:first:stopped", "assistant:stopped:" + soloRestartStopped,
		"user:second:", "assistant:ok:", "user:third:", "assistant:ok:",
	}
	if strings.Join(order, "|") != strings.Join(want, "|") {
		t.Fatalf("transcript = %q\nwant %q", order, want)
	}
	waitFor(t, "stored turns cleared", func() bool {
		total, _ := pendingTurnCount(t, restarted, id)
		return total == 0
	})

	// The old process's turn is still blocked; settle it like a process exit.
	if c := solo.lookup(id); c != nil {
		c.sess.Cancel(true)
	}
}

func TestSoloRestoreRunsATurnThatNeverReachedTheTranscript(t *testing.T) {
	solo, _, _, db := newSoloFixture(t, 0, 3)
	row := models.SoloSession{ID: "solo-restore-1", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusRunning}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(&soloTurn{ID: "turn-unwritten", Text: "never written"})
	if err := solo.pending.Save(soloPendingSurface, row.ID, []chatsession.PendingTurn{{ID: "turn-unwritten", Started: true, Payload: payload}}); err != nil {
		t.Fatal(err)
	}
	chat := newRestoreChat(true)
	solo.SetChatterForTest(chat)
	solo.RestoreQueuedTurns()
	c := solo.lookup(row.ID)
	if c == nil {
		t.Fatal("restore did not reopen the session")
	}
	defer c.sess.Cancel(true)
	waitFor(t, "unwritten turn runs", func() bool {
		var user models.SoloMessage
		return db.First(&user, "id = ?", "turn-unwritten").Error == nil && user.Content == "never written"
	})
}

func TestSoloRestoreDropsTurnsOfEndedSessions(t *testing.T) {
	solo, _, _, db := newSoloFixture(t, 0, 3)
	for _, s := range []models.SoloSession{
		{ID: "solo-closed", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusClosed},
		{ID: "solo-error", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusError},
	} {
		if err := db.Create(&s).Error; err != nil {
			t.Fatal(err)
		}
	}
	payload, _ := json.Marshal(&soloTurn{ID: "q", Text: "queued"})
	for _, id := range []string{"solo-closed", "solo-error", "solo-deleted"} {
		if err := solo.pending.Save(soloPendingSurface, id, []chatsession.PendingTurn{{ID: id + "-q", Payload: payload}}); err != nil {
			t.Fatal(err)
		}
	}
	solo.SetChatterForTest(newRestoreChat(false))
	solo.RestoreQueuedTurns()
	for _, id := range []string{"solo-closed", "solo-error", "solo-deleted"} {
		if solo.lookup(id) != nil {
			t.Fatalf("%s: an ended session must not be reopened", id)
		}
		if total, _ := pendingTurnCount(t, solo, id); total != 0 {
			t.Fatalf("%s: stored turns left = %d", id, total)
		}
	}
}
