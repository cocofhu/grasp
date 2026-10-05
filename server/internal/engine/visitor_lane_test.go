package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/services"

	"gorm.io/gorm"
)

// visitorFake adds VisitorLaneProvider to fakeProvider, recording what each
// lane's chat was sent.
type visitorFake struct {
	*fakeProvider
	vmu      sync.Mutex
	prompts  map[string][]string // lane -> prompts
	primed   map[string]bool
	retired  []string
	cancelOn map[string]bool
	hold     chan struct{} // when set, visitor turns block until it closes
}

var _ runtime.VisitorLaneProvider = (*visitorFake)(nil)

func (v *visitorFake) VisitorTurn(ctx context.Context, req runtime.NodeReq, lane, prelude, human string, images []models.PromptImage, onProgress func([]models.AcpEvent, bool)) runtime.ReactTurn {
	v.vmu.Lock()
	if v.prompts == nil {
		v.prompts = map[string][]string{}
		v.primed = map[string]bool{}
	}
	prompt := human
	if !v.primed[lane] {
		prompt = prelude + "\n## 用户消息\n" + human
		v.primed[lane] = true
	}
	v.prompts[lane] = append(v.prompts[lane], prompt)
	hold := v.hold
	v.vmu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
		}
	}
	if onProgress != nil {
		onProgress([]models.AcpEvent{{Kind: "message", Text: "visitor-stream"}}, true)
	}
	return runtime.ReactTurn{Msg: "reply to " + human}
}

func (v *visitorFake) CancelVisitorTurn(runID, nodeID, lane string) {}

func (v *visitorFake) RetireVisitorLane(runID, nodeID, lane string) {
	v.vmu.Lock()
	defer v.vmu.Unlock()
	v.retired = append(v.retired, lane)
	delete(v.primed, lane)
}

func (v *visitorFake) promptsFor(lane string) []string {
	v.vmu.Lock()
	defer v.vmu.Unlock()
	return append([]string(nil), v.prompts[lane]...)
}

func (v *visitorFake) retiredLanes() []string {
	v.vmu.Lock()
	defer v.vmu.Unlock()
	return append([]string(nil), v.retired...)
}

func setupVisitorEngine(t *testing.T) (*Engine, *gorm.DB, *visitorFake) {
	t.Helper()
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	arts := services.NewArtifactService(db)
	host := mcp.NewHost(arts)
	p := &visitorFake{fakeProvider: &fakeProvider{host: host}}
	eng := New(db, p, host, arts, 5)
	eng.SetBlobStore(blob.NewMemory())
	cleanupEngineDB(t, eng, db)

	g := models.Graph{Nodes: []models.Node{
		{ID: "p1", Type: "agent", Caps: capsPlain, Label: "产出", Config: map[string]any{"prompt": "写一份设计文档"}},
	}}
	if err := db.Create(&models.Run{ID: "r1", Status: "waiting_human", Graph: g}).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := db.Create(&models.ReactConversation{RunID: "r1", NodeID: "p1", Iteration: 1, Messages: []models.ReactMessage{
		{Role: "agent", Text: "产物已生成"},
		{Role: "human", Text: "owner says hi"},
	}}).Error; err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	return eng, db, p
}

func visitorTarget(lane string) VisitorTarget {
	return VisitorTarget{LinkID: "link1", RunID: "r1", ProducerID: "p1", Lane: lane, Source: "node"}
}

func waitLaneIdle(t *testing.T, eng *Engine, lane string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := eng.VisitorSessionSnapshot("r1", "p1", lane); !ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("lane %s never went idle", lane)
}

func turnTexts(msgs []models.ReactMessage) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.Role+":"+m.Text)
	}
	return out
}

func TestVisitorLanesKeepSeparateTranscripts(t *testing.T) {
	eng, db, p := setupVisitorEngine(t)
	laneA := gateshare.VisitorLane("link1", strings.Repeat("a", 32))
	laneB := gateshare.VisitorLane("link1", strings.Repeat("b", 32))

	if got := turnTexts(eng.VisitorTurns("link1", laneA, "r1", "p1")); len(got) != 2 {
		t.Fatalf("before speaking a visitor sees the node dialogue, got %v", got)
	}
	if _, err := eng.EnqueueVisitorTurn(visitorTarget(laneA), "from A", nil, nil); err != nil {
		t.Fatalf("enqueue A: %v", err)
	}
	waitLaneIdle(t, eng, laneA)
	if _, err := eng.EnqueueVisitorTurn(visitorTarget(laneB), "from B", nil, nil); err != nil {
		t.Fatalf("enqueue B: %v", err)
	}
	waitLaneIdle(t, eng, laneB)
	if _, err := eng.EnqueueVisitorTurn(visitorTarget(laneA), "A again", nil, nil); err != nil {
		t.Fatalf("enqueue A2: %v", err)
	}
	waitLaneIdle(t, eng, laneA)

	a := strings.Join(turnTexts(eng.VisitorTurns("link1", laneA, "r1", "p1")), "|")
	b := strings.Join(turnTexts(eng.VisitorTurns("link1", laneB, "r1", "p1")), "|")
	if !strings.Contains(a, "human:from A") || !strings.Contains(a, "human:A again") || strings.Contains(a, "from B") {
		t.Fatalf("lane A transcript = %s", a)
	}
	if !strings.Contains(b, "human:from B") || strings.Contains(b, "from A") {
		t.Fatalf("lane B transcript = %s", b)
	}
	if !strings.HasPrefix(a, "agent:产物已生成|human:owner says hi|") {
		t.Fatalf("visitor transcript should open with the node history, got %s", a)
	}

	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", "r1", "p1").First(&conv).Error; err != nil {
		t.Fatal(err)
	}
	if len(conv.Messages) != 2 {
		t.Fatalf("node dialogue must stay untouched, got %v", turnTexts(conv.Messages))
	}

	pa := p.promptsFor(laneA)
	if len(pa) != 2 || !strings.Contains(pa[0], "写一份设计文档") || !strings.Contains(pa[0], "owner says hi") {
		t.Fatalf("first prompt should carry the prelude, got %q", pa)
	}
	if pa[1] != "A again" {
		t.Fatalf("a primed chat gets the bare message, got %q", pa[1])
	}
	if snaps := eng.ReviewSessionsForRun("r1"); len(snaps) != 0 {
		t.Fatalf("visitor lanes must not appear in run snapshots: %+v", snaps)
	}
}

var pageSessionPattern = regexp.MustCompile(`ps_[A-Za-z0-9_-]{43}`)

func TestVisitorPageSessionsPerLane(t *testing.T) {
	eng, _, p := setupVisitorEngine(t)
	hold := make(chan struct{})
	p.hold = hold
	laneA := gateshare.VisitorLane("link1", strings.Repeat("a", 32))
	laneB := gateshare.VisitorLane("link1", strings.Repeat("b", 32))
	for lane, owner := range map[string]string{laneA: "embed:aaaa", laneB: "embed:bbbb"} {
		target := visitorTarget(lane)
		target.Owner = owner
		if _, err := eng.EnqueueVisitorTurn(target, "帮我登录", nil, nil); err != nil {
			t.Fatalf("enqueue %s: %v", lane, err)
		}
	}
	sessionOf := func(lane string) string {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if ps := p.promptsFor(lane); len(ps) > 0 {
				return pageSessionPattern.FindString(ps[len(ps)-1])
			}
			time.Sleep(5 * time.Millisecond)
		}
		return ""
	}
	sidA, sidB := sessionOf(laneA), sessionOf(laneB)
	if sidA == "" || sidB == "" || sidA == sidB {
		t.Fatalf("each lane needs its own page session: %q %q", sidA, sidB)
	}
	if owner, _, ok := eng.PageTurn("r1", "p1", sidA); !ok || owner != "embed:aaaa" {
		t.Fatalf("lane A session -> %q %v", owner, ok)
	}
	if owner, _, ok := eng.PageTurn("r1", "p1", sidB); !ok || owner != "embed:bbbb" {
		t.Fatalf("lane B session -> %q %v", owner, ok)
	}
	close(hold)
	waitLaneIdle(t, eng, laneA)
	waitLaneIdle(t, eng, laneB)
	if _, _, ok := eng.PageTurn("r1", "p1", sidA); ok {
		t.Fatal("finished visitor turn's session still valid")
	}
	for _, m := range eng.VisitorTurns("link1", laneA, "r1", "p1") {
		if m.Role == "human" && strings.Contains(m.Text, "ps_") {
			t.Fatalf("session id persisted in the visitor transcript: %q", m.Text)
		}
	}
}

func TestPageSessionNeedsSender(t *testing.T) {
	eng, _, p := setupVisitorEngine(t)
	lane := gateshare.VisitorLane("link1", strings.Repeat("c", 32))
	if _, err := eng.EnqueueVisitorTurn(visitorTarget(lane), "hi", nil, nil); err != nil {
		t.Fatal(err)
	}
	waitLaneIdle(t, eng, lane)
	if ps := p.promptsFor(lane); len(ps) != 1 || strings.Contains(ps[0], "ps_") {
		t.Fatalf("a turn without a sender must not get a page session: %q", ps)
	}
}

func TestRetireRevokesPageSessions(t *testing.T) {
	eng, _, _ := setupVisitorEngine(t)
	lane := "v:0123456789ab"
	done := make(chan struct{})
	id := eng.mintPageSession(&reviewSession{runID: "r1", producerID: "p1", lane: lane}, "embed:x", done)
	eng.mintPageSession(&reviewSession{runID: "r1", producerID: "p1", lane: ""}, "user:o", done)
	eng.revokeLanePageSessions("r1", "p1", lane)
	if _, _, ok := eng.PageTurn("r1", "p1", id); ok {
		t.Fatal("retired lane kept its page session")
	}
	eng.pageMu.Lock()
	n := len(eng.pageSessions)
	eng.pageMu.Unlock()
	if n != 1 {
		t.Fatalf("other lanes' sessions must survive, left %d", n)
	}
}

func TestVisitorLaneCapPerLink(t *testing.T) {
	eng, _, _ := setupVisitorEngine(t)
	for i := 0; i < gateshare.MaxVisitorLanesPerLink; i++ {
		lane := gateshare.VisitorLane("link1", fmt.Sprintf("%032x", i+1))
		if _, err := eng.EnqueueVisitorTurn(visitorTarget(lane), "hi", nil, nil); err != nil {
			t.Fatalf("visitor %d: %v", i, err)
		}
		waitLaneIdle(t, eng, lane)
	}
	extra := gateshare.VisitorLane("link1", fmt.Sprintf("%032x", 99))
	if _, err := eng.EnqueueVisitorTurn(visitorTarget(extra), "hi", nil, nil); !errors.Is(err, ErrVisitorsFull) {
		t.Fatalf("visitor past the cap: err = %v", err)
	}
	again := gateshare.VisitorLane("link1", fmt.Sprintf("%032x", 1))
	if _, err := eng.EnqueueVisitorTurn(visitorTarget(again), "still here", nil, nil); err != nil {
		t.Fatalf("an admitted visitor keeps talking: %v", err)
	}
	waitLaneIdle(t, eng, again)
}

func TestRetireVisitorLanesOnInvalidation(t *testing.T) {
	eng, db, p := setupVisitorEngine(t)
	if err := db.Create(&models.GateShareLink{ID: "link1", TokenHash: "th1", RunID: "r1", NodeID: "p1",
		ExpiresAt: time.Now().Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	lane := gateshare.VisitorLane("link1", strings.Repeat("c", 32))
	if _, err := eng.EnqueueVisitorTurn(visitorTarget(lane), "hello", nil, nil); err != nil {
		t.Fatal(err)
	}
	waitLaneIdle(t, eng, lane)

	eng.RetireVisitorLanesForTokenHashes([]string{"th1"})
	if got := p.retiredLanes(); len(got) != 1 || got[0] != lane {
		t.Fatalf("retired lanes = %v", got)
	}
	eng.visitorMu.Lock()
	left := len(eng.visitorLanes)
	eng.visitorMu.Unlock()
	if left != 0 {
		t.Fatalf("lane bookkeeping not cleared: %d", left)
	}
	// The transcript survives; a later message opens a fresh primed chat.
	if _, err := eng.EnqueueVisitorTurn(visitorTarget(lane), "back", nil, nil); err != nil {
		t.Fatal(err)
	}
	waitLaneIdle(t, eng, lane)
	pr := p.promptsFor(lane)
	if len(pr) != 2 || !strings.Contains(pr[1], "你与该访客此前的对话") || !strings.Contains(pr[1], "hello") {
		t.Fatalf("a re-opened chat should replay the visitor's own turns, got %q", pr)
	}
}

func TestSweepRetiresIdleAndClosedLinks(t *testing.T) {
	eng, db, p := setupVisitorEngine(t)
	if err := db.Create(&models.GateShareLink{ID: "link1", TokenHash: "th1", RunID: "r1", NodeID: "p1",
		ExpiresAt: time.Now().Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	lane := gateshare.VisitorLane("link1", strings.Repeat("d", 32))
	if _, err := eng.EnqueueVisitorTurn(visitorTarget(lane), "hello", nil, nil); err != nil {
		t.Fatal(err)
	}
	waitLaneIdle(t, eng, lane)

	eng.sweepVisitorLanes(time.Now())
	if got := p.retiredLanes(); len(got) != 0 {
		t.Fatalf("a fresh lane on an open link must survive the sweep, retired %v", got)
	}
	eng.sweepVisitorLanes(time.Now().Add(VisitorLaneIdleTTL + time.Minute))
	if got := p.retiredLanes(); len(got) != 1 {
		t.Fatalf("an idle lane should be retired, retired %v", got)
	}
}
