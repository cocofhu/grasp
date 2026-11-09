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
	event, _ := json.Marshal(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "reply to " + text}})
	onEvent(event)
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
	snapshot, err := solo.durable.Snapshot(context.Background(), soloQueueScope(id))
	if err != nil {
		t.Fatal(err)
	}
	total = len(snapshot.Pending)
	if snapshot.Active != nil {
		total++
		started++
	}
	var legacy int64
	if err := solo.db.Model(&models.ChatPendingTurn{}).Where("surface = ? AND session_key = ?", soloPendingSurface, id).Count(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	return total + int(legacy), started
}

func TestSoloQueuedTurnsSurviveRestart(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, sbx, gateway, db := newSoloFixture(t, port, 3)
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
	// Its resource locks are process-local; the old process's persisted lease
	// is expired below to model a crash rather than a second live owner.
	sbx = NewSandboxService(db, sbx.mgr, sbx.skills, sbx.host, SandboxOptions{ModelBinding: sbx.modelBinding, ProfilesRoot: sbx.profilesRoot})
	restarted := NewSoloService(db, sbx, solo.agents, solo.projects)
	after := newRestoreChat(false)
	restarted.SetChatterForTest(after)
	restarted.SetModelAlignerForTest(&recordAligner{})
	if _, err := restarted.Send(id, "alice", "fourth", nil); err != nil {
		t.Fatalf("new durable admission must preserve the existing queue: %v", err)
	}
	session, err := restarted.Require(id, "alice")
	if err != nil {
		t.Fatal(err)
	}
	resident, err := sbx.Get(session.SandboxID)
	if err != nil {
		t.Fatal(err)
	}
	gateway.SetStatus(resident.Name, "stopped")
	if err := db.Model(&models.ChatQueue{}).Where("id = ?", soloQueueScope(id).ID()).Update("owner_expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.Sandbox{}).Where("id = ?", resident.ID).Update("operation_expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	sbx.ReconcileOnStartup(context.Background(), nil)
	gateway.FailStart = true
	sbx.RecoverSoloSandboxes(context.Background())
	restarted.RestoreQueuedTurns()
	if total, _ := pendingTurnCount(t, restarted, id); total < 3 || total > 4 || len(after.seen()) != 0 {
		t.Fatalf("queued turns must stay persisted while recovery fails: total=%d prompts=%q", total, after.seen())
	}
	var accepted int64
	if err := db.Model(&models.ChatQueueTurn{}).Where("queue_id = ?", soloQueueScope(id).ID()).Count(&accepted).Error; err != nil || accepted != 4 {
		t.Fatalf("recovery discarded accepted intent: count=%d err=%v", accepted, err)
	}
	gateway.FailStart = false
	sbx.RecoverSoloSandboxes(context.Background())

	waitFor(t, "restored turns answered", func() bool {
		msgs, _ := restarted.Messages(id, "alice")
		n := 0
		for _, m := range msgs {
			if m.Role == "assistant" && m.Status == "ok" {
				n++
			}
		}
		return n == 3
	})
	if got := after.seen(); len(got) != 3 || !strings.HasSuffix(got[0], "second") || !strings.HasSuffix(got[1], "third") || !strings.HasSuffix(got[2], "fourth") {
		t.Fatalf("restored prompts = %q, want second then third then fourth", got)
	}
	if got := after.seen()[0]; !strings.HasPrefix(got, soloContextPreambleHeader) || !strings.Contains(got, "first") || strings.Contains(got, "未提交的改动已丢失") || strings.Contains(got, soloModelPreambleHeader) {
		t.Fatalf("recovered turn lost history or falsely reported data loss: %q", got)
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
		"user:first:stopped", "assistant:unknown:执行结果待核对：" + soloRestartStopped + "，请核对工作区和已执行操作后继续",
		"user:second:", "assistant:ok:", "user:third:", "assistant:ok:", "user:fourth:", "assistant:ok:",
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

func TestSoloRestoreDoesNotReplayStartedLegacyTurnWithoutTranscript(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, _, db := newSoloFixture(t, port, 3)
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "restore fixture environment ready", func() bool {
		row, err := solo.Require(view.ID, "alice")
		return err == nil && row.Status == models.SoloStatusRunning
	})
	payload, _ := json.Marshal(&soloTurn{ID: "turn-unwritten", Text: "never written"})
	if err := chatsession.NewPendingStore(db).Save(soloPendingSurface, view.ID, []chatsession.PendingTurn{{ID: "turn-unwritten", Started: true, Payload: payload}}); err != nil {
		t.Fatal(err)
	}
	solo = NewSoloService(db, solo.sbx, solo.agents, solo.projects)
	chat := newRestoreChat(false)
	solo.SetChatterForTest(chat)
	solo.SetModelAlignerForTest(&recordAligner{})
	solo.RestoreQueuedTurns()
	solo.RestoreQueuedTurns()
	if _, err := solo.durable.Receipt(context.Background(), soloQueueScope(view.ID), "turn-unwritten"); err == nil {
		t.Fatal("startup imported retained legacy evidence into the executable queue")
	}
	var original models.ChatPendingTurn
	if err := db.First(&original, "id = ?", "turn-unwritten").Error; err != nil || !original.Started || string(original.Payload) != string(payload) {
		t.Fatalf("startup altered retained evidence: %+v %v", original, err)
	}
	if len(chat.seen()) != 0 {
		t.Fatalf("recovery repeated native work without dispatch proof: %q", chat.seen())
	}
}

func TestSoloRestoreRetainsFailedAndOrphanedIntent(t *testing.T) {
	solo, _, _, db := newSoloFixture(t, 0, 3)
	for _, s := range []models.SoloSession{
		{ID: "solo-closed", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusClosed},
		{ID: "solo-error", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusError},
	} {
		if err := db.Create(&s).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"solo-closed", "solo-error", "solo-deleted"} {
		payload, _ := json.Marshal(&soloTurn{ID: id + "-q", Text: "queued"})
		if err := chatsession.NewPendingStore(db).Save(soloPendingSurface, id, []chatsession.PendingTurn{{ID: id + "-q", Payload: payload}}); err != nil {
			t.Fatal(err)
		}
	}
	solo = NewSoloService(db, solo.sbx, solo.agents, solo.projects)
	solo.SetChatterForTest(newRestoreChat(false))
	solo.RestoreQueuedTurns()
	for _, id := range []string{"solo-closed", "solo-error", "solo-deleted"} {
		if solo.lookup(id) != nil {
			t.Fatalf("%s: an ended session must not be reopened", id)
		}
		want := 1
		if total, _ := pendingTurnCount(t, solo, id); total != want {
			t.Fatalf("%s: stored turns left = %d, want %d", id, total, want)
		}
	}
	ready, err := solo.durable.ReadyScopes(context.Background(), soloPendingSurface)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 0 {
		t.Fatal("legacy evidence was scheduled as new durable work")
	}
}

func TestSoloStartupRestoreDoesNotInterruptNewTurns(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, _, db := newSoloFixture(t, port, 3)
	insertSoloModelConfig(t, db)
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "running sandbox", func() bool {
		row, err := solo.Require(view.ID, "alice")
		return err == nil && row.Status == models.SoloStatusRunning
	})
	chat := newRestoreChat(true)
	solo.SetChatterForTest(chat)
	solo.SetModelAlignerForTest(&recordAligner{})
	if _, err := solo.Send(view.ID, "alice", "new after startup", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-chat.started:
	case <-time.After(5 * time.Second):
		t.Fatal("new turn did not start")
	}
	solo.RestoreQueuedTurns()
	messages, err := solo.Messages(view.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if message.Role == "user" && message.Status == "stopped" || message.Role == "assistant" && message.Error == soloRestartStopped {
			t.Fatal("startup restore interrupted a turn accepted by the new process")
		}
	}
	if c := solo.lookup(view.ID); c != nil {
		c.sess.Cancel(true)
	}
}
