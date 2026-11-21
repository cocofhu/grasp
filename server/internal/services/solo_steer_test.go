package services

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// steerChat is a gateChat whose running turn answers steers with status. A
// delivered steer ends the turn, as an agent finishing its answer would.
type steerChat struct {
	*gateChat
	status sandbox.SteerStatus
	err    error
	mu     sync.Mutex
	steers []string
}

func (c *steerChat) Steer(_ context.Context, _ uint, text string, _ []models.PromptImage) (sandbox.SteerStatus, error) {
	c.mu.Lock()
	c.steers = append(c.steers, text)
	c.mu.Unlock()
	if c.err == nil && c.status == sandbox.SteerDelivered {
		c.release()
	}
	return c.status, c.err
}

func (c *steerChat) steered() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.steers...)
}

func startSteerSolo(t *testing.T, chat soloChatter) (*SoloService, string) {
	t.Helper()
	acp, port := fakeACPServer(t)
	t.Cleanup(acp.Close)
	solo, _, _, _ := newSoloFixture(t, port, 3)
	v, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "sandbox ready", func() bool {
		row, err := solo.Require(v.ID, "alice")
		return err == nil && row.Status == models.SoloStatusRunning
	})
	solo.SetChatterForTest(chat)
	return solo, v.ID
}

// queueBehindRunningTurn starts "one" and queues "two"; it returns two's id.
func queueBehindRunningTurn(t *testing.T, solo *SoloService, id string, gate *gateChat, picks ...models.ReactAnnotation) string {
	t.Helper()
	if _, err := solo.SendWithReferences(id, "alice", "one", nil, nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "first turn running", func() bool { return gate.started() })
	if _, err := solo.SendWithReferences(id, "alice", "two", nil, nil, picks...); err != nil {
		t.Fatal(err)
	}
	pending := solo.lookup(id).sess.View().Pending
	if len(pending) != 1 {
		t.Fatalf("pending = %d", len(pending))
	}
	return pending[0].ID
}

func TestSoloSteerJoinsRunningTurn(t *testing.T) {
	chat := &steerChat{gateChat: newGateChat(true), status: sandbox.SteerDelivered}
	solo, id := startSteerSolo(t, chat)
	two := queueBehindRunningTurn(t, solo, id, chat.gateChat)

	out, err := solo.SteerQueued(context.Background(), id, "alice", two)
	if err != nil || out != chatsession.SteerOutcomeJoined {
		t.Fatalf("steer = %q %v", out, err)
	}
	if got := chat.steered(); len(got) != 1 || got[0] != "two" {
		t.Fatalf("steered = %v", got)
	}
	waitFor(t, "session idle", func() bool { return solo.lookup(id).sess.Ready() })
	if got := chat.snapshot(); len(got) != 1 {
		t.Fatalf("a joined message must not run as its own turn: %v", got)
	}
}

func TestSoloSteerFallsBackToInterrupt(t *testing.T) {
	for name, chat := range map[string]soloChatter{
		"unsupported": &steerChat{gateChat: newGateChat(true), status: sandbox.SteerUnsupported},
		"failed":      &steerChat{gateChat: newGateChat(true), status: sandbox.SteerFailed, err: errors.New("broken pipe")},
		"no steerer":  newGateChat(true),
	} {
		t.Run(name, func(t *testing.T) {
			gate, _ := chat.(*gateChat)
			if sc, ok := chat.(*steerChat); ok {
				gate = sc.gateChat
			}
			solo, id := startSteerSolo(t, chat)
			two := queueBehindRunningTurn(t, solo, id, gate)
			out, err := solo.SteerQueued(context.Background(), id, "alice", two)
			if err != nil || out != chatsession.SteerOutcomeNext {
				t.Fatalf("steer = %q %v", out, err)
			}
			waitFor(t, "second turn ran", func() bool { return gate.count() == 2 && solo.lookup(id).sess.Ready() })
			if got := gate.snapshot(); got[1] != "two" {
				t.Fatalf("prompts = %v", got)
			}
		})
	}
}

func TestSoloSteerKeepsPreparedInputAsOwnTurn(t *testing.T) {
	chat := &steerChat{gateChat: newGateChat(true), status: sandbox.SteerDelivered}
	solo, id := startSteerSolo(t, chat)
	two := queueBehindRunningTurn(t, solo, id, chat.gateChat, models.ReactAnnotation{Selector: "#a", Label: "button", URL: "http://localhost/"})
	if out, err := solo.SteerQueued(context.Background(), id, "alice", two); err != nil || out != chatsession.SteerOutcomeNext {
		t.Fatalf("steer = %q %v", out, err)
	}
	if got := chat.steered(); len(got) != 0 {
		t.Fatalf("an annotated message must not be steered: %v", got)
	}
	waitFor(t, "second turn ran", func() bool { return chat.count() == 2 && solo.lookup(id).sess.Ready() })
}

func TestSoloSteerWaitsForAPreparingTurn(t *testing.T) {
	chat := &steerChat{gateChat: newGateChat(true), status: sandbox.SteerDelivered}
	solo, id := startSteerSolo(t, chat)
	two := queueBehindRunningTurn(t, solo, id, chat.gateChat)
	c := solo.lookup(id)
	c.mu.Lock()
	c.phase = SoloPhasePreparing
	c.mu.Unlock()

	if out, err := solo.SteerQueued(context.Background(), id, "alice", two); err != nil || out != chatsession.SteerOutcomeNext {
		t.Fatalf("steer = %q %v", out, err)
	}
	if got := chat.steered(); len(got) != 0 {
		t.Fatalf("a turn that has not reached the agent cannot take a steer: %v", got)
	}
	chat.release()
	waitFor(t, "second turn ran", func() bool { return chat.count() == 2 && c.sess.Ready() })
	var stopped int64
	solo.db.Model(&models.SoloMessage{}).Where("session_id = ? AND status = ?", id, "stopped").Count(&stopped)
	if stopped != 0 {
		t.Fatalf("steering must not interrupt a preparing turn: %d stopped", stopped)
	}
}

// partialChat streams some reply, then ends cleanly once cancelled, as the
// bridge client does for an interrupted turn that already has output.
type partialChat struct{ started chan struct{} }

func (c *partialChat) ChatWithTimeout(ctx context.Context, _ uint, _ string, _ []models.PromptImage, _ time.Duration, _ *models.ChatExecution, onEvent func(json.RawMessage)) (*models.TokenUsage, models.TokenUsageByModel, error) {
	onEvent(json.RawMessage(`{"op":"event","data":{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"working on it"}}}}`))
	close(c.started)
	<-ctx.Done()
	return &models.TokenUsage{}, nil, nil
}

func (c *partialChat) Cancel(uint) {}

func TestSoloStoppedTurnKeepsPartialReplyAsStopped(t *testing.T) {
	chat := &partialChat{started: make(chan struct{})}
	solo, id := startSteerSolo(t, chat)
	if _, err := solo.SendWithReferences(id, "alice", "long task", nil, nil); err != nil {
		t.Fatal(err)
	}
	<-chat.started
	if err := solo.Cancel(id, "alice"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "session idle", func() bool { return solo.lookup(id).sess.Ready() })
	var reply models.SoloMessage
	if err := solo.db.Where("session_id = ? AND role = ?", id, "assistant").First(&reply).Error; err != nil {
		t.Fatal(err)
	}
	if reply.Status != "stopped" || reply.Content != "working on it" {
		t.Fatalf("reply = %q %q", reply.Status, reply.Content)
	}
}

func TestSoloSendAndSteer(t *testing.T) {
	chat := &steerChat{gateChat: newGateChat(true), status: sandbox.SteerDelivered}
	solo, id := startSteerSolo(t, chat)
	if _, err := solo.SendWithReferences(id, "alice", "one", nil, nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "first turn running", func() bool { return chat.started() })
	if out, err := solo.SendAndSteer(context.Background(), id, "alice", "now", nil, nil); err != nil || out != chatsession.SteerOutcomeJoined {
		t.Fatalf("send and steer = %q %v", out, err)
	}
	if _, err := solo.SendAndSteer(context.Background(), id, "bob", "x", nil, nil); err == nil {
		t.Fatal("another user's session")
	}
	if _, err := solo.SteerQueued(context.Background(), id, "alice", "missing"); err == nil {
		t.Fatal("unknown turn")
	}
	waitFor(t, "session idle", func() bool { return solo.lookup(id).sess.Ready() })
	// An idle session has nothing to join: the message simply runs.
	if out, err := solo.SendAndSteer(context.Background(), id, "alice", "later", nil, nil); err != nil || out == "" {
		t.Fatalf("idle send and steer = %q %v", out, err)
	}
	waitFor(t, "idle turn ran", func() bool { return chat.count() == 2 && solo.lookup(id).sess.Ready() })
}
