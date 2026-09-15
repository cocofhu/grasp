package services

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

type fakeAgentLookup map[string]Agent

func (f fakeAgentLookup) Get(name string) (Agent, bool) {
	a, ok := f[name]
	return a, ok
}

func TestValidateAgentProfilesProject(t *testing.T) {
	skills := fakeAgentLookup{
		"ok-agent":      {Name: "ok-agent", ProjectID: "alpha"},
		"other-agent":   {Name: "other-agent", ProjectID: "beta"},
		"unbound-agent": {Name: "unbound-agent", ProjectID: ""},
	}

	t.Run("empty agent_profile skipped", func(t *testing.T) {
		g := models.Graph{Nodes: []models.Node{
			{ID: "n1", Type: "research", Label: "调研", Config: map[string]any{"agent_profile": ""}},
			{ID: "n2", Type: "human_gate", Label: "门禁", Config: map[string]any{}},
		}}
		if err := ValidateAgentProfilesProject(skills, "alpha", g); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
	})

	t.Run("same project ok", func(t *testing.T) {
		g := models.Graph{Nodes: []models.Node{
			{ID: "n1", Type: "implement", Label: "实现", Config: map[string]any{"agent_profile": "ok-agent"}},
			{ID: "n2", Type: "app_preview", Label: "预览", Config: map[string]any{"agent_profile": "ok-agent"}},
		}}
		if err := ValidateAgentProfilesProject(skills, "alpha", g); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
	})

	t.Run("allows cross project and unbound; rejects deleted", func(t *testing.T) {
		g := models.Graph{Nodes: []models.Node{
			{ID: "a", Type: "agent", Label: "执行", Config: map[string]any{"agent_profile": "other-agent"}},
			{ID: "b", Type: "react", Label: "澄清", Config: map[string]any{"agent_profile": "unbound-agent"}},
		}}
		if err := ValidateAgentProfilesProject(skills, "alpha", g); err != nil {
			t.Fatalf("cross/unbound should pass for shared extend: %v", err)
		}
		g2 := models.Graph{Nodes: []models.Node{
			{ID: "c", Type: "plan", Label: "计划", Config: map[string]any{"agent_profile": "ghost"}},
		}}
		err := ValidateAgentProfilesProject(skills, "alpha", g2)
		if err == nil || !strings.Contains(err.Error(), "已删除") || !strings.Contains(err.Error(), "计划 → ghost") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("covers all agent_profile node types not only agent", func(t *testing.T) {
		types := []string{"react", "preflight", "agent", "approve", "plan", "implement", "research", "test", "review", "proposal", "submit_mr", "visual", "app_preview"}
		for _, typ := range types {
			g := models.Graph{Nodes: []models.Node{
				{ID: "n", Type: typ, Label: typ, Config: map[string]any{"agent_profile": "ghost"}},
			}}
			if err := ValidateAgentProfilesProject(skills, "alpha", g); err == nil {
				t.Fatalf("type %s: expected rejection for deleted agent", typ)
			}
		}
	})
}

func TestWorkflowServiceSavePublishAgentProfileGate(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)
	s.SetAgents(fakeAgentLookup{
		"ok-agent":    {Name: "ok-agent", ProjectID: models.DefaultProjectID},
		"other-agent": {Name: "other-agent", ProjectID: "beta"},
	})

	wf := &models.WorkflowDef{
		ID:        "wf-skill-1",
		ProjectID: models.DefaultProjectID,
		Name:      "skill-gate",
		Status:    "draft",
		Version:   1,
		Graph: models.Graph{Nodes: []models.Node{
			{ID: "in", Type: "input", Label: "输入", Position: models.Position{}, Config: map[string]any{}},
			{ID: "ag", Type: "agent", Label: "执行", Position: models.Position{}, Config: map[string]any{"agent_profile": "other-agent"}},
			{ID: "out", Type: "output", Label: "输出", Position: models.Position{}, Config: map[string]any{}},
		}, Edges: []models.Edge{
			{ID: "e1", Source: "in", Target: "ag"},
			{ID: "e2", Source: "ag", Target: "out"},
		}},
	}
	// Cross-project Agent is allowed (extend uses current workflow project).
	if err := s.Save(wf); err != nil {
		t.Fatalf("Save foreign should pass: %v", err)
	}

	wf.Graph.Nodes[1].Config["agent_profile"] = "ghost"
	if err := s.Save(wf); err == nil || !strings.Contains(err.Error(), "已删除") {
		t.Fatalf("Save want deleted reject, got %v", err)
	}

	wf.Graph.Nodes[1].Config["agent_profile"] = "ok-agent"
	if err := s.Save(wf); err != nil {
		t.Fatalf("Save ok: %v", err)
	}

	if _, err := s.Publish(wf.ID); err != nil {
		t.Fatalf("Publish ok: %v", err)
	}
}
