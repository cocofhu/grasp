package engine

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"gorm.io/gorm"
)

type fakePinner struct {
	mu    sync.Mutex
	users []string
	nodes []string
}

func (p *fakePinner) PinRunReferences(runID, nodeID, userID string, refs []models.MessageReference) []models.MessageReference {
	p.mu.Lock()
	p.users = append(p.users, userID)
	p.nodes = append(p.nodes, nodeID)
	p.mu.Unlock()
	out := append([]models.MessageReference(nil), refs...)
	for i := range out {
		out[i].Anchor = "anchor-" + out[i].ID
		out[i].UserID = userID
	}
	return out
}

func (p *fakePinner) RenderReferenceBlock(refs []models.MessageReference) string {
	ids := make([]string, 0, len(refs))
	for _, r := range refs {
		ids = append(ids, r.ID)
	}
	return "[引用块 " + strings.Join(ids, ",") + "]"
}

func humanTurns(t *testing.T, db *gorm.DB, runID, nodeID string, want int) []models.ReactMessage {
	t.Helper()
	deadline := time.Now().Add(waitPollTimeout)
	for time.Now().Before(deadline) {
		var conv models.ReactConversation
		if err := db.Where("run_id = ? AND node_id = ?", runID, nodeID).Order("iteration desc, id desc").First(&conv).Error; err == nil {
			var out []models.ReactMessage
			for _, m := range conv.Messages {
				if m.Role == "human" {
					out = append(out, m)
				}
			}
			if len(out) >= want {
				return out
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("want %d human turns", want)
	return nil
}

func (f *fakeProvider) humanPrompts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.reactHumans...)
}

func TestFirstMessageReferencesReachTheClarifyTurn(t *testing.T) {
	eng, db, fp := setupEngineGraphP(t, clarifyOnlyGraph())
	pinner := &fakePinner{}
	eng.SetReferencePinner(pinner)
	msg := &models.CompositeText{References: []models.MessageReference{
		{ID: "ref-1", Kind: models.ReferenceKindSolo, Target: "s1", Title: "登录重构", UserID: "alice"},
	}}
	run, err := eng.StartRunWithFirstMessage("wf", nil, "test", "", nil, nil, "", msg, LaunchChoice{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if run.Title != "@登录重构" {
		t.Fatalf("reference-only title = %q", run.Title)
	}
	var stored models.Run
	if err := db.First(&stored, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.FirstMessage == nil || stored.FirstMessage.ReferenceUser != "alice" || len(stored.FirstMessage.References) != 1 {
		t.Fatalf("stored first message: %+v", stored.FirstMessage)
	}

	turn := humanTurns(t, db, run.ID, "predev", 1)[0]
	if len(turn.References) != 1 || turn.References[0].Anchor != "anchor-ref-1" {
		t.Fatalf("first turn references: %+v", turn.References)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")
	if prompts := fp.humanPrompts(); len(prompts) != 1 || prompts[0] != "[引用块 ref-1]" {
		t.Fatalf("first prompt: %q", prompts)
	}
	pinner.mu.Lock()
	if len(pinner.users) != 1 || pinner.users[0] != "alice" || pinner.nodes[0] != "predev" {
		t.Fatalf("pinned for %v on %v", pinner.users, pinner.nodes)
	}
	pinner.mu.Unlock()

	// Redelivery is idempotent: the latch keeps a single human turn.
	c, err := eng.loadCtx(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	eng.fireClarifyFirstMessage(c, c.graph.FindNode("predev"))
	time.Sleep(80 * time.Millisecond)
	if got := humanTurns(t, db, run.ID, "predev", 1); len(got) != 1 {
		t.Fatalf("human turns after re-fire = %d", len(got))
	}

	// The sender never reaches client JSON.
	raw, _ := json.Marshal(turn)
	if strings.Contains(string(raw), "alice") {
		t.Fatalf("sender leaked: %s", raw)
	}
}

func TestReplyAndConfirmCarryReferences(t *testing.T) {
	eng, db, fp := setupEngineGraphP(t, clarifyOnlyGraph())
	eng.SetReferencePinner(&fakePinner{})
	run, err := eng.StartRunWithFirstMessage("wf", nil, "test", "", nil, nil, "", &models.CompositeText{Text: "开始"}, LaunchChoice{})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	humanTurns(t, db, run.ID, "predev", 1)
	waitRunStatus(t, db, run.ID, "waiting_human")
	waitIdle := func() {
		deadline := time.Now().Add(waitPollTimeout)
		for !eng.ReviewSessionReady(run.ID, "predev") && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitIdle()

	refs := []models.MessageReference{{ID: "ref-r", Kind: models.ReferenceKindGitHubPR, Target: "acme/web#7", Title: "PR", UserID: "alice"}}
	if err := eng.ReactReplyLiveCtxWithIDAs("", run.ID, "predev", "", "看这个 PR", nil, nil, nil, refs...); err != nil {
		t.Fatalf("reply: %v", err)
	}
	reply := humanTurns(t, db, run.ID, "predev", 2)[1]
	if reply.Text != "看这个 PR" || len(reply.References) != 1 || reply.References[0].Anchor != "anchor-ref-r" {
		t.Fatalf("reply turn: %+v", reply)
	}
	waitIdle()
	prompts := fp.humanPrompts()
	if got := prompts[len(prompts)-1]; got != "[引用块 ref-r]\n\n看这个 PR" {
		t.Fatalf("reply prompt: %q", got)
	}

	// A reference-only reply is accepted.
	if err := eng.ReactReplyLiveCtxWithIDAs("", run.ID, "predev", "", "", nil, nil, nil, refs...); err != nil {
		t.Fatalf("reference-only reply: %v", err)
	}
	humanTurns(t, db, run.ID, "predev", 3)
	waitIdle()

	confirmRefs := []models.MessageReference{{ID: "ref-c", Kind: models.ReferenceKindSolo, Target: "s1", Title: "会话", UserID: "alice"}}
	if err := eng.ReactConfirmAs("", run.ID, "predev", "确认", nil, nil, false, confirmRefs...); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	confirm := humanTurns(t, db, run.ID, "predev", 4)[3]
	if len(confirm.References) != 1 || confirm.References[0].Anchor != "anchor-ref-c" {
		t.Fatalf("confirm turn: %+v", confirm)
	}
	prompts = fp.humanPrompts()
	if got := prompts[len(prompts)-1]; !strings.HasPrefix(got, "[引用块 ref-c]") {
		t.Fatalf("confirm prompt: %q", got)
	}
}

func TestQueuedItemFrameCarriesReferencesWithoutSender(t *testing.T) {
	item := &reviewQueueItem{ID: "i1", Text: "x", References: []models.MessageReference{{ID: "ref-1", Kind: "solo", Target: "s", UserID: "alice"}}, ReferenceUser: "alice"}
	frame := reviewItemView(item).Frame()
	raw, _ := json.Marshal(frame)
	if !strings.Contains(string(raw), `"references"`) || strings.Contains(string(raw), "alice") {
		t.Fatalf("frame: %s", raw)
	}
	if withReferenceBlock("", "p") != "p" || withReferenceBlock("b", " ") != "b" || withReferenceBlock("b", "p") != "b\n\np" {
		t.Fatal("withReferenceBlock")
	}
	// Without a pinner references stay on the turn and the prompt is unchanged.
	e := &Engine{}
	if got := e.pinTurnReferences("r", "n", &reviewQueueItem{Effective: "p", References: item.References}); got != "p" {
		t.Fatalf("no pinner: %q", got)
	}
	if got := e.renderedReferences(item.References, "p"); got != "p" {
		t.Fatalf("no pinner render: %q", got)
	}
	if got := (&Engine{refs: &fakePinner{}}).renderedReferences(item.References, "p"); got != "[引用块 ref-1]\n\np" {
		t.Fatalf("render: %q", got)
	}
}
