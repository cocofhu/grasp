package services

import (
	"errors"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func graphWithProfiles(profiles map[string]string) models.Graph {
	// Build a linear valid workflow: input → one agent node per key → output.
	nodes := []models.Node{{ID: "in", Type: "input", Label: "Start"}}
	// Stable order for deterministic edges.
	types := make([]string, 0, len(profiles))
	for typ := range profiles {
		types = append(types, typ)
	}
	for i := 0; i < len(types); i++ {
		for j := i + 1; j < len(types); j++ {
			if types[j] < types[i] {
				types[i], types[j] = types[j], types[i]
			}
		}
	}
	for _, typ := range types {
		nodes = append(nodes, models.Node{
			ID:    "n-" + typ,
			Type:  "agent",
			Label: typ,
			Config: map[string]any{
				"agent_profile": profiles[typ],
			},
		})
	}
	nodes = append(nodes, models.Node{ID: "out", Type: "output", Label: "End"})
	edges := make([]models.Edge, 0, len(nodes)-1)
	for i := 0; i < len(nodes)-1; i++ {
		edges = append(edges, models.Edge{
			ID: "e" + nodes[i].ID, Source: nodes[i].ID, Target: nodes[i+1].ID,
		})
	}
	return models.Graph{Nodes: nodes, Edges: edges}
}

func agentProfileOf(g models.Graph, key string) string {
	for _, n := range g.Nodes {
		if n.ID != "n-"+key || n.Config == nil {
			continue
		}
		return models.AgentProfile(n.Config)
	}
	return ""
}

func TestRenameAgentProfileRefs_multiNodeTypesExactReplace(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)

	old, neu := "research-agent", "research-pro"
	wf := &models.WorkflowDef{
		ID: "wf-multi", ProjectID: models.DefaultProjectID, Name: "Multi",
		Graph: graphWithProfiles(map[string]string{
			"research":  old,
			"preview":   old,
			"implement": old,
			"review":    "other-bot",
			"agent":     old + "-extra", // substring must not match
		}),
	}
	if err := s.Save(wf); err != nil {
		t.Fatalf("save: %v", err)
	}
	pub, err := s.Publish("wf-multi")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if pub.Status() != "published" || pub.Version != 1 {
		t.Fatalf("precondition: status=%s version=%d", pub.Status(), pub.Version)
	}

	// Unrelated workflow must stay untouched.
	other := &models.WorkflowDef{
		ID: "wf-other", ProjectID: models.DefaultProjectID, Name: "Other",
		Graph: graphWithProfiles(map[string]string{"plan": "planner"}),
	}
	if err := s.Save(other); err != nil {
		t.Fatal(err)
	}

	n, err := s.RenameAgentProfileRefs(old, neu)
	if err != nil {
		t.Fatalf("rename refs: %v", err)
	}
	if n != 1 {
		t.Fatalf("updatedWorkflowCount want 1, got %d", n)
	}

	got, ok := s.Get("wf-multi")
	if !ok {
		t.Fatal("missing wf-multi")
	}
	for _, typ := range []string{"research", "preview", "implement"} {
		if agentProfileOf(got.Graph, typ) != neu {
			t.Fatalf("%s agent_profile want %q got %q", typ, neu, agentProfileOf(got.Graph, typ))
		}
	}
	if agentProfileOf(got.Graph, "review") != "other-bot" {
		t.Fatalf("unrelated profile rewritten: %q", agentProfileOf(got.Graph, "review"))
	}
	if agentProfileOf(got.Graph, "agent") != old+"-extra" {
		t.Fatalf("substring profile rewritten: %q", agentProfileOf(got.Graph, "agent"))
	}
	// No exact old-name residue.
	for _, node := range got.Graph.Nodes {
		if node.Config == nil {
			continue
		}
		if v, _ := node.Config["agent_profile"].(string); v == old {
			t.Fatalf("old name residue on node %s", node.ID)
		}
	}

	snap, err := s.VersionGraph("wf-multi", got.Version)
	if err != nil {
		t.Fatalf("version graph: %v", err)
	}
	if agentProfileOf(snap, "research") != neu || agentProfileOf(snap, "preview") != neu {
		t.Fatalf("version snapshot not rewritten: research=%q preview=%q",
			agentProfileOf(snap, "research"), agentProfileOf(snap, "preview"))
	}

	restored, err := s.Restore("wf-multi", got.Version)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if agentProfileOf(restored.Graph, "research") != neu {
		t.Fatalf("restore brought back old name: %q", agentProfileOf(restored.Graph, "research"))
	}

	otherGot, _ := s.Get("wf-other")
	if agentProfileOf(otherGot.Graph, "plan") != "planner" {
		t.Fatalf("unrelated workflow changed: %+v", otherGot.Graph)
	}
}

func TestRenameAgentProfileRefs_versionOnlyCountsAndSkipsRun(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)

	old, neu := "legacy-agent", "fresh-agent"
	wf := &models.WorkflowDef{
		ID: "wf-ver", ProjectID: models.DefaultProjectID, Name: "VerOnly",
		Graph: graphWithProfiles(map[string]string{"react": old}),
	}
	if err := s.Save(wf); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish("wf-ver"); err != nil {
		t.Fatal(err)
	}
	// Current Def no longer references old name; Version snapshot still does.
	upd := &models.WorkflowDef{
		ProjectID: models.DefaultProjectID,
		ID:        "wf-ver", Name: "VerOnly",
		Graph: graphWithProfiles(map[string]string{"react": "orchestrator"}),
	}
	if err := s.Save(upd); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("wf-ver")
	if got.Status() != "draft" {
		t.Fatalf("expected draft after graph edit, got %s", got.Status())
	}
	ver := got.Version

	runGraph := graphWithProfiles(map[string]string{"react": old})
	run := models.Run{
		ID: "run-pin", WorkflowID: "wf-ver", WorkflowName: "VerOnly",
		WorkflowVersion: ver, Status: "completed", Trigger: "manual",
		Graph: runGraph, StartedAt: time.Now(), CreatedAt: time.Now(),
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}

	n, err := s.RenameAgentProfileRefs(old, neu)
	if err != nil {
		t.Fatalf("rename refs: %v", err)
	}
	if n != 1 {
		t.Fatalf("version-only hit should count Def once, got %d", n)
	}

	def, _ := s.Get("wf-ver")
	if agentProfileOf(def.Graph, "react") != "orchestrator" {
		t.Fatalf("def current graph should stay orchestrator, got %q", agentProfileOf(def.Graph, "react"))
	}
	snap, err := s.VersionGraph("wf-ver", got.PublishedVersion)
	if err != nil {
		t.Fatal(err)
	}
	if agentProfileOf(snap, "react") != neu {
		t.Fatalf("version snapshot want %q got %q", neu, agentProfileOf(snap, "react"))
	}

	var runGot models.Run
	if err := db.First(&runGot, "id = ?", "run-pin").Error; err != nil {
		t.Fatal(err)
	}
	if agentProfileOf(runGot.Graph, "react") != old {
		t.Fatalf("Run.Graph must not be rewritten, got %q", agentProfileOf(runGot.Graph, "react"))
	}
}

func TestRenameAgentProfileRefs_keepsPublishedAndDraftStatus(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)
	old, neu := "a1", "a2"

	pub := &models.WorkflowDef{
		ID: "wf-pub", ProjectID: models.DefaultProjectID, Name: "Pub",
		Graph: graphWithProfiles(map[string]string{"test": old}),
	}
	if err := s.Save(pub); err != nil {
		t.Fatal(err)
	}
	published, err := s.Publish("wf-pub")
	if err != nil {
		t.Fatal(err)
	}
	pubVer := published.Version

	draft := &models.WorkflowDef{
		ID: "wf-draft2", ProjectID: models.DefaultProjectID, Name: "Draft",
		Graph: graphWithProfiles(map[string]string{"review": old}),
	}
	if err := s.Save(draft); err != nil {
		t.Fatal(err)
	}

	n, err := s.RenameAgentProfileRefs(old, neu)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("want 2 affected defs, got %d", n)
	}

	gotPub, _ := s.Get("wf-pub")
	if gotPub.Status() != "published" {
		t.Fatalf("published downgraded to %s", gotPub.Status())
	}
	if gotPub.Version != pubVer {
		t.Fatalf("version bumped %d → %d", pubVer, gotPub.Version)
	}
	if agentProfileOf(gotPub.Graph, "test") != neu {
		t.Fatalf("pub profile not updated: %q", agentProfileOf(gotPub.Graph, "test"))
	}

	gotDraft, _ := s.Get("wf-draft2")
	if gotDraft.Status() != "draft" {
		t.Fatalf("draft promoted to %s", gotDraft.Status())
	}
	if agentProfileOf(gotDraft.Graph, "review") != neu {
		t.Fatalf("draft profile not updated: %q", agentProfileOf(gotDraft.Graph, "review"))
	}
}

func TestRenameAgentProfileRefs_failHookAbortsTransaction(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)
	old, neu := "x", "y"
	wf := &models.WorkflowDef{
		ID: "wf-fail", ProjectID: models.DefaultProjectID, Name: "Fail",
		Graph: graphWithProfiles(map[string]string{"visual": old}),
	}
	if err := s.Save(wf); err != nil {
		t.Fatal(err)
	}

	renameAgentProfileRefsFailHook = func() error { return errors.New("inject persist fail") }
	t.Cleanup(func() { renameAgentProfileRefsFailHook = nil })

	n, err := s.RenameAgentProfileRefs(old, neu)
	if err == nil {
		t.Fatal("expected injected error")
	}
	if n != 0 {
		t.Fatalf("count on failure want 0, got %d", n)
	}
	got, _ := s.Get("wf-fail")
	if agentProfileOf(got.Graph, "visual") != old {
		t.Fatalf("graph should roll back to old name, got %q", agentProfileOf(got.Graph, "visual"))
	}
}
