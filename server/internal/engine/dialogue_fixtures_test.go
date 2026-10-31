package engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

// go test ./internal/engine -run TestDialogueContractFixtures -update-dialogue-fixtures
var updateDialogueFixtures = flag.Bool("update-dialogue-fixtures", false, "rewrite web/src/lib/chat/__fixtures__/dialogue")

const dialogueFixtureDir = "../../../web/src/lib/chat/__fixtures__/dialogue"

// dialogueFixture is one contract scenario: the frames a viewer receives, in
// order, and the state they must add up to.
type dialogueFixture struct {
	Scenario string `json:"scenario"`
	About    string `json:"about"`
	Frames   []any  `json:"frames"`
	Final    any    `json:"final"`
}

// snapRecorder takes dialogue snapshots at chosen points; frames are derived
// from consecutive snapshots, so the fixture does not depend on throttling.
type snapRecorder struct {
	eng           *Engine
	runID, nodeID string
	snaps         []DialogueState
}

func (r *snapRecorder) snap(t *testing.T) {
	t.Helper()
	time.Sleep(10 * time.Millisecond)
	st, ok := r.eng.DialogueSnapshot(r.runID, r.nodeID)
	if !ok {
		t.Fatal("no dialogue to snapshot")
	}
	if n := len(r.snaps); n > 0 && unversioned(r.snaps[n-1]) == unversioned(st) {
		return
	}
	r.snaps = append(r.snaps, st)
}

var (
	fixtureUUID = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	fixtureTime = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})`)
	fixtureRun  = regexp.MustCompile(`run-[0-9a-f]{8}`)
)

// normalizeSnaps makes snapshots reproducible: turn ids, times and run ids
// are renamed, versions count snapshots, and a row's revision is the first
// snapshot that showed its current content.
func normalizeSnaps(t *testing.T, snaps []DialogueState) []DialogueState {
	t.Helper()
	ids := map[string]string{}
	out := make([]DialogueState, len(snaps))
	revs := map[string]uint64{}
	content := map[string][32]byte{}
	for k, st := range snaps {
		for i := range st.Rows {
			st.Rows[i].Execution = nil
		}
		raw, err := json.Marshal(st)
		if err != nil {
			t.Fatal(err)
		}
		s := fixtureUUID.ReplaceAllStringFunc(string(raw), func(u string) string {
			if ids[u] == "" {
				ids[u] = fmt.Sprintf("turn-%d", len(ids)+1)
			}
			return ids[u]
		})
		s = fixtureTime.ReplaceAllString(s, "2026-01-01T00:00:00Z")
		s = fixtureRun.ReplaceAllString(s, "run-fixture")
		var n DialogueState
		if err := json.Unmarshal([]byte(s), &n); err != nil {
			t.Fatal(err)
		}
		n.Epoch, n.Seq = 1, uint64(k+1)
		for i := range n.Rows {
			n.Rows[i].Rev = 0
			b, _ := json.Marshal(n.Rows[i])
			h := sha256.Sum256(b)
			id := n.Rows[i].ID
			if content[id] != h {
				content[id] = h
				revs[id] = n.Seq
			}
			n.Rows[i].Rev = revs[id]
		}
		out[k] = n
	}
	return out
}

func fixtureFrom(t *testing.T, scenario, about string, snaps []DialogueState, public bool) dialogueFixture {
	t.Helper()
	norm := normalizeSnaps(t, snaps)
	fx := dialogueFixture{Scenario: scenario, About: about}
	for k, st := range norm {
		if public {
			fx.Frames = append(fx.Frames, PublicDialogue(st))
			continue
		}
		if k == 0 {
			fx.Frames = append(fx.Frames, st)
			continue
		}
		fx.Frames = append(fx.Frames, dialogueFrame(&norm[k-1], st))
	}
	last := norm[len(norm)-1]
	if public {
		fx.Final = PublicDialogue(last)
	} else {
		fx.Final = last
	}
	return fx
}

type clarifyFixtureEnv struct {
	eng      *Engine
	provider *fakeProvider
	runID    string
	hold     chan struct{}
	rec      *snapRecorder
}

func newClarifyFixture(t *testing.T) *clarifyFixtureEnv {
	eng, provider, runID := startClarifyDialogue(t)
	return &clarifyFixtureEnv{eng: eng, provider: provider, runID: runID,
		rec: &snapRecorder{eng: eng, runID: runID, nodeID: "clarify"}}
}

func (c *clarifyFixtureEnv) holdTurns() {
	c.hold = make(chan struct{})
	c.provider.mu.Lock()
	c.provider.reactHold = c.hold
	c.provider.mu.Unlock()
}

func (c *clarifyFixtureEnv) script(turns ...runtime.ReactTurn) {
	c.provider.mu.Lock()
	c.provider.reactScript = append(c.provider.reactScript, turns...)
	c.provider.mu.Unlock()
}

func (c *clarifyFixtureEnv) send(t *testing.T, text string, images ...models.PromptImage) {
	t.Helper()
	if _, err := c.eng.EnqueueClarifyTurn(c.runID, "clarify", text, images, nil); err != nil {
		t.Fatal(err)
	}
}

func (c *clarifyFixtureEnv) release(t *testing.T) {
	t.Helper()
	c.provider.mu.Lock()
	c.provider.reactHold = nil
	c.provider.mu.Unlock()
	if c.hold != nil {
		close(c.hold)
		c.hold = nil
	}
	time.Sleep(20 * time.Millisecond)
	if err := c.eng.waitReviewReadyForTest(c.runID, "clarify", 5*time.Second); err != nil {
		t.Fatal(err)
	}
}

func (c *clarifyFixtureEnv) acp(events ...models.AcpEvent) {
	c.eng.publishAcp(c.runID, "clarify", events, true)
}

func buildDialogueFixtures(t *testing.T) []dialogueFixture {
	var out []dialogueFixture
	msg := func(s string) models.AcpEvent { return models.AcpEvent{Kind: "message", Text: s} }

	{
		c := newClarifyFixture(t)
		c.holdTurns()
		c.send(t, "第一条")
		waitRunningTurn(t, c.eng, c.runID, "clarify")
		c.send(t, "第二条")
		c.rec.snap(t)
		c.release(t)
		c.rec.snap(t)
		out = append(out, fixtureFrom(t, "queued", "a message waits behind a running turn, then both finish", c.rec.snaps, false))
	}
	{
		c := newClarifyFixture(t)
		c.holdTurns()
		c.send(t, "做登录")
		waitRunningTurn(t, c.eng, c.runID, "clarify")
		c.rec.snap(t)
		c.acp(models.AcpEvent{Kind: "thought", Text: "先看需求"})
		c.rec.snap(t)
		c.acp(models.AcpEvent{Kind: "thought", Text: "先看需求"}, msg("登录页需要"))
		c.rec.snap(t)
		c.release(t)
		c.rec.snap(t)
		out = append(out, fixtureFrom(t, "running", "a reply streams into one row that is sealed in place", c.rec.snaps, false))
	}
	{
		c := newClarifyFixture(t)
		c.script(runtime.ReactTurn{Msg: ""})
		c.holdTurns()
		c.send(t, "做登录")
		waitRunningTurn(t, c.eng, c.runID, "clarify")
		c.rec.snap(t)
		c.release(t)
		c.rec.snap(t)
		out = append(out, fixtureFrom(t, "empty", "the agent really answered nothing: outcome empty", c.rec.snaps, false))
	}
	{
		c := newClarifyFixture(t)
		c.script(runtime.ReactTurn{Msg: "(澄清回复失败:sandbox gone)", Err: errors.New("sandbox gone")})
		c.holdTurns()
		c.send(t, "做登录")
		waitRunningTurn(t, c.eng, c.runID, "clarify")
		c.rec.snap(t)
		c.release(t)
		c.rec.snap(t)
		out = append(out, fixtureFrom(t, "failed", "the turn failed: outcome failed with its error", c.rec.snaps, false))
	}
	{
		c := newClarifyFixture(t)
		c.holdTurns()
		c.send(t, "做登录")
		waitRunningTurn(t, c.eng, c.runID, "clarify")
		c.acp(msg("说到一半"))
		c.rec.snap(t)
		if err := c.eng.CancelClarifyTurn(c.runID, "clarify"); err != nil {
			t.Fatal(err)
		}
		c.release(t)
		c.rec.snap(t)
		out = append(out, fixtureFrom(t, "interrupted", "the user stopped a streaming turn", c.rec.snaps, false))
	}
	{
		c := newClarifyFixture(t)
		c.provider.mu.Lock()
		c.provider.sandboxBusy = true
		c.provider.sandboxRunningOp = "op-orphan"
		c.provider.mu.Unlock()
		c.acp(msg("沙箱还在说"))
		c.rec.snap(t)
		c.provider.mu.Lock()
		c.provider.sandboxBusy = false
		c.provider.sandboxRunningOp = ""
		c.provider.mu.Unlock()
		c.eng.publishAcp(c.runID, "clarify", nil, false)
		c.rec.snap(t)
		out = append(out, fixtureFrom(t, "orphan", "the sandbox keeps running after the platform went idle", c.rec.snaps, false))
	}
	{
		c := newClarifyFixture(t)
		events := []models.AcpEvent{msg("第一段"), {Kind: "segment"}, msg("第二段")}
		c.script(runtime.ReactTurn{Msg: "第二段", Handoffs: []string{"第一段"}, Events: events})
		c.holdTurns()
		c.send(t, "做登录")
		waitRunningTurn(t, c.eng, c.runID, "clarify")
		c.acp(events[:2]...)
		c.rec.snap(t)
		c.acp(events...)
		c.rec.snap(t)
		c.release(t)
		c.rec.snap(t)
		out = append(out, fixtureFrom(t, "multi-segment", "one message answered by two rows keeps both row ids", c.rec.snaps, false))
	}
	{
		c := newClarifyFixture(t)
		c.script(runtime.ReactTurn{Msg: ""})
		c.send(t, "做登录")
		c.release(t)
		c.rec.snap(t)
		c.holdTurns()
		if err := c.eng.ReactReplyRetryLast(c.runID, "clarify"); err != nil {
			t.Fatal(err)
		}
		waitRunningTurn(t, c.eng, c.runID, "clarify")
		c.rec.snap(t)
		c.release(t)
		c.rec.snap(t)
		out = append(out, fixtureFrom(t, "retry", "retry covers the empty reply; the request row moves to the new turn", c.rec.snaps, false))
	}
	{
		c := newClarifyFixture(t)
		c.holdTurns()
		c.send(t, "按钮改大", models.PromptImage{Ref: "blob:fixture", MimeType: "image/png", Name: "shot.png"})
		waitRunningTurn(t, c.eng, c.runID, "clarify")
		c.acp(msg("好的"))
		c.rec.snap(t)
		c.release(t)
		c.rec.snap(t)
		out = append(out, fixtureFrom(t, "public", "the share-link form: redacted, full frames only", c.rec.snaps, true))
	}
	{
		env := setupTakeoverEngine(t, takeoverPlainGraph(capsPlain))
		run, err := env.eng.StartRun("wf", nil, "test")
		if err != nil {
			t.Fatal(err)
		}
		waitStarted(t, env.provider)
		rec := &snapRecorder{eng: env.eng, runID: run.ID, nodeID: "impl"}
		env.eng.publishAcp(run.ID, "impl", []models.AcpEvent{msg("正在实现")}, true)
		rec.snap(t)
		env.provider.releaseHold("impl")
		waitRunStatus(t, env.db, run.ID, "completed")
		rec.snap(t)
		out = append(out, fixtureFrom(t, "observe", "an automatic visit watched while it runs", rec.snaps, false))
	}
	return out
}

func TestDialogueContractFixtures(t *testing.T) {
	fixtures := buildDialogueFixtures(t)
	for _, fx := range fixtures {
		b, err := json.MarshalIndent(fx, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		b = append(b, '\n')
		path := filepath.Join(dialogueFixtureDir, fx.Scenario+".json")
		if *updateDialogueFixtures {
			if err := os.MkdirAll(dialogueFixtureDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, b, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, b) {
			t.Errorf("%s is stale; rerun with -update-dialogue-fixtures (err=%v)", path, err)
		}
	}
}
