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

func TestValidateAgentProfiles(t *testing.T) {
	skills := fakeAgentLookup{
		"ok-agent": {Name: "ok-agent"},
	}

	t.Run("empty agent_profile skipped", func(t *testing.T) {
		g := models.Graph{Nodes: []models.Node{
			{ID: "n1", Type: "agent", Label: "调研", Config: map[string]any{"agent_profile": ""}},
			{ID: "n2", Type: "human_gate", Label: "门禁", Config: map[string]any{}},
		}}
		if err := ValidateAgentProfiles(skills, g); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
	})

	t.Run("existing agent ok", func(t *testing.T) {
		g := models.Graph{Nodes: []models.Node{
			{ID: "n1", Type: "agent", Label: "实现", Config: map[string]any{"agent_profile": "ok-agent"}},
			{ID: "n2", Type: "agent", Label: "预览", Config: map[string]any{"agent_profile": "ok-agent"}},
		}}
		if err := ValidateAgentProfiles(skills, g); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
	})

	t.Run("rejects deleted", func(t *testing.T) {
		g2 := models.Graph{Nodes: []models.Node{
			{ID: "c", Type: "agent", Label: "计划", Config: map[string]any{"agent_profile": "ghost"}},
		}}
		err := ValidateAgentProfiles(skills, g2)
		if err == nil || !strings.Contains(err.Error(), "已删除") || !strings.Contains(err.Error(), "计划 → ghost") {
			t.Fatalf("got %v", err)
		}
	})

}

func TestWorkflowServiceSavePublishAgentProfileGate(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)
	s.SetAgents(fakeAgentLookup{
		"ok-agent": {Name: "ok-agent"},
	})

	wf := &models.WorkflowDef{
		ID:      "wf-skill-1",
		Name:    "skill-gate",
		Version: 1,
		Graph: models.Graph{Nodes: []models.Node{
			{ID: "in", Type: "input", Label: "输入", Position: models.Position{}, Config: map[string]any{}},
			{ID: "ag", Type: "agent", Label: "执行", Position: models.Position{}, Config: map[string]any{"agent_profile": "ghost"}},
			{ID: "out", Type: "output", Label: "输出", Position: models.Position{}, Config: map[string]any{}},
		}, Edges: []models.Edge{
			{ID: "e1", Source: "in", Target: "ag"},
			{ID: "e2", Source: "ag", Target: "out"},
		}},
	}
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
