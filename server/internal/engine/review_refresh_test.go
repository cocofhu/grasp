package engine

import (
	"sync"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

// switchableAgents is a SkillLookup whose Agents' review switch can be flipped
// while a run is in flight.
type switchableAgents struct {
	mu   sync.Mutex
	caps map[string]*models.AgentCapabilities
}

func (s *switchableAgents) Get(name string) (services.Agent, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.caps[name]
	if !ok {
		return services.Agent{}, false
	}
	return services.Agent{Name: name, Capabilities: c.Clone()}, true
}

func (s *switchableAgents) setReview(name string, on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.caps[name].Review = on
}

func newSwitchableAgents(review bool) *switchableAgents {
	c := capsResearch.Clone()
	c.Review = review
	return &switchableAgents{caps: map[string]*models.AgentCapabilities{"pm-agent": c}}
}

func persistedReview(t *testing.T, eng *Engine, runID, nodeID string) bool {
	t.Helper()
	c, err := eng.loadCtx(runID)
	if err != nil {
		t.Fatalf("loadCtx: %v", err)
	}
	return c.graph.FindNode(nodeID).Caps.ReviewEnabled()
}

// TestRefreshReviewFlagFollowsAgent: a fresh visit picks up the Agent's current
// review switch in both directions and persists it on run.Graph, leaving the
// rest of the snapshot untouched.
func TestRefreshReviewFlagFollowsAgent(t *testing.T) {
	eng, db, _ := setupReviewEngine(t)
	g := reviewGraph()
	for i := range g.Nodes {
		g.Nodes[i].Caps = g.Nodes[i].Caps.Clone()
	}
	if err := db.Create(&models.Run{ID: "run-refresh", WorkflowID: "review-wf", Status: "running", Graph: g}).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}
	agents := newSwitchableAgents(false)
	eng.SetAgents(agents)

	c, err := eng.loadCtx("run-refresh")
	if err != nil {
		t.Fatalf("loadCtx: %v", err)
	}
	node := c.graph.FindNode("prop")
	eng.refreshReviewFlag(c, node)
	if node.Caps.ReviewEnabled() {
		t.Fatal("review should be off after the Agent switch was turned off")
	}
	if persistedReview(t, eng, "run-refresh", "prop") {
		t.Fatal("refreshed review flag not persisted on run.Graph")
	}
	if !node.Caps.WritesSchema(models.SchemaResearch) {
		t.Fatal("refresh must keep the rest of the capability snapshot")
	}
	if !capsResearch.Review {
		t.Fatal("refresh must not mutate the shared fixture")
	}

	agents.setReview("pm-agent", true)
	c, _ = eng.loadCtx("run-refresh")
	eng.refreshReviewFlag(c, c.graph.FindNode("prop"))
	if !persistedReview(t, eng, "run-refresh", "prop") {
		t.Fatal("review should be on again after the Agent switch was turned on")
	}
}

// TestRefreshReviewFlagKeepsParkedReview: turning the switch off while a node is
// already parked in review does not cut the open review short.
func TestRefreshReviewFlagKeepsParkedReview(t *testing.T) {
	eng, db, _ := setupReviewEngine(t)
	agents := newSwitchableAgents(true)
	eng.SetAgents(agents)

	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "prop")
	waitRunStatus(t, db, run.ID, "waiting_human")

	agents.setReview("pm-agent", false)
	if !persistedReview(t, eng, run.ID, "prop") {
		t.Fatal("parked review node must keep its review flag")
	}
	if err := eng.ReactReply(run.ID, "prop", "确认", nil, nil, true); err != nil {
		t.Fatalf("finish reply: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "prop").First(&conv).Error; err != nil {
		t.Fatalf("load review conv: %v", err)
	}
	if !conv.Done {
		t.Fatal("review conversation should be finished through the review path")
	}
}

// TestReviewOffSkipsReviewOnNewRun: with the switch off at run start the node
// completes without parking.
func TestReviewOffSkipsReviewOnNewRun(t *testing.T) {
	eng, db, _ := setupReviewEngine(t)
	eng.SetAgents(newSwitchableAgents(false))

	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")
	var n int64
	db.Model(&models.ReactConversation{}).Where("run_id = ? AND node_id = ?", run.ID, "prop").Count(&n)
	if n != 0 {
		t.Fatalf("no review conversation expected, got %d", n)
	}
}
