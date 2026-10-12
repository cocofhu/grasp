package engine

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

type projectAgents map[string]services.Agent

func (p projectAgents) Get(name string) (services.Agent, bool) {
	a, ok := p[name]
	return a, ok
}

func TestSnapshotCapsRejectsForeignProjectAgent(t *testing.T) {
	eng := &Engine{}
	eng.SetAgents(projectAgents{
		"mine":    {Name: "mine", ProjectID: "proj-a", Capabilities: capsResearch.Clone()},
		"foreign": {Name: "foreign", ProjectID: "proj-b", Capabilities: capsResearch.Clone()},
	})
	graph := func(profile string) models.Graph {
		return models.Graph{Nodes: []models.Node{
			{ID: "ag", Type: "agent", Config: map[string]any{"agent_profile": profile}},
		}}
	}

	g := graph("mine")
	if err := eng.snapshotCaps(&g, "proj-a"); err != nil {
		t.Fatalf("same project: %v", err)
	}
	g = graph("foreign")
	err := eng.snapshotCaps(&g, "proj-a")
	if err == nil || !strings.Contains(err.Error(), "不属于本项目") {
		t.Fatalf("foreign project: got %v", err)
	}
	g = graph("foreign")
	if err := eng.snapshotCaps(&g, ""); err != nil {
		t.Fatalf("empty project skips ownership check: %v", err)
	}
}
