package handlers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestRenameAgent_cascadesWorkflowAndReturnsCount(t *testing.T) {
	hn := newHarness(t)
	seedAgent(t, hn, "research-agent")

	g := models.Graph{
		Nodes: []models.Node{
			{ID: "in", Type: "input", Label: "Start"},
			{ID: "r", Type: "agent", Caps: testReviewCaps, Label: "R", Config: map[string]any{"agent_profile": "research-agent"}},
			{ID: "p", Type: "agent", Caps: testPreviewCaps, Label: "P", Config: map[string]any{"agent_profile": "research-agent"}},
			{ID: "out", Type: "output", Label: "End"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "in", Target: "r"},
			{ID: "e2", Source: "r", Target: "p"},
			{ID: "e3", Source: "p", Target: "out"},
		},
	}
	wf := &models.WorkflowDef{
		ID: "wf-cascade", ProjectID: models.DefaultProjectID, Name: "Cascade", Graph: g,
	}
	if err := hn.h.WF.Save(wf); err != nil {
		t.Fatal(err)
	}
	if _, err := hn.h.WF.Publish("wf-cascade"); err != nil {
		t.Fatal(err)
	}

	w := hn.do(http.MethodPost, "/api/agents/research-agent/rename", map[string]any{"name": "research-pro"})
	if w.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Name                 string `json:"name"`
		UpdatedWorkflowCount int    `json:"updatedWorkflowCount"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Name != "research-pro" {
		t.Fatalf("name=%q", resp.Name)
	}
	if resp.UpdatedWorkflowCount != 1 {
		t.Fatalf("updatedWorkflowCount=%d", resp.UpdatedWorkflowCount)
	}
	if !hn.h.Agents.Exists("research-pro") || hn.h.Agents.Exists("research-agent") {
		t.Fatal("directory rename incomplete")
	}
	got, ok := hn.h.WF.Get("wf-cascade")
	if !ok {
		t.Fatal("missing workflow")
	}
	if got.Status != "published" {
		t.Fatalf("status=%s", got.Status)
	}
	for _, n := range got.Graph.Nodes {
		if n.Config == nil {
			continue
		}
		if v, _ := n.Config["agent_profile"].(string); v == "research-agent" {
			t.Fatalf("old agent_profile residue on %s", n.ID)
		}
	}
}

func TestRenameAgent_workflowCascadeFailureRollsBackSkillPmOrg(t *testing.T) {
	hn := newHarness(t)
	skills := hn.h.Agents
	hn.h.Org = services.NewOrgService(t.TempDir(), skills)
	hn.h.Pm = services.NewPmService(hn.db, skills)

	seedAgent(t, hn, "old-agent")
	a, _ := skills.Get("old-agent")
	a.ProjectID = models.DefaultProjectID
	if err := skills.Save(a); err != nil {
		t.Fatal(err)
	}
	if _, err := hn.h.Pm.UpsertMemory(models.DefaultProjectID, "old-agent", "T", "c", "admin", "u"); err != nil {
		t.Fatal(err)
	}
	if _, err := hn.h.Org.Put(services.AgentOrg{
		Revision: 0,
		Groups:   []services.OrgGroup{{ID: "g1", Name: "Group"}},
		Agents:   map[string]services.OrgAgentMembership{"old-agent": {GroupIDs: []string{"g1"}}},
	}, 0); err != nil {
		t.Fatal(err)
	}

	clear := services.SetRenameAgentProfileRefsFailHookForTest(func() error {
		return errors.New("inject cascade fail")
	})
	t.Cleanup(clear)

	w := hn.do(http.MethodPost, "/api/agents/old-agent/rename", map[string]any{"name": "new-agent"})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 on cascade fail, got %d %s", w.Code, w.Body.String())
	}
	if !hn.h.Agents.Exists("old-agent") {
		t.Fatal("skill directory should roll back to old-agent")
	}
	if hn.h.Agents.Exists("new-agent") {
		t.Fatal("new-agent directory should not remain after rollback")
	}
	mem, _ := hn.h.Pm.ListMemories(models.DefaultProjectID, "old-agent")
	if len(mem) != 1 {
		t.Fatalf("pm memories should be back under old-agent, got %d", len(mem))
	}
	memNew, _ := hn.h.Pm.ListMemories(models.DefaultProjectID, "new-agent")
	if len(memNew) != 0 {
		t.Fatalf("pm memories leaked under new-agent: %d", len(memNew))
	}
	orgAfter, err := hn.h.Org.Get()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := orgAfter.Agents["old-agent"]; !ok {
		t.Fatalf("org membership should roll back to old-agent: %+v", orgAfter.Agents)
	}
	if _, ok := orgAfter.Agents["new-agent"]; ok {
		t.Fatalf("org membership leaked under new-agent: %+v", orgAfter.Agents)
	}
}
