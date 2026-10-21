package engine

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestResolveStartVarsWorkspaceSeedAndOverride(t *testing.T) {
	wsVars := []models.WorkspaceVariable{
		{Name: "region", Type: "string", Value: "cn"},
		{Name: "shared", Type: "string", Value: "from-workspace"},
		{Name: "only_workspace", Type: "string", Value: "keep"},
	}
	g := models.Graph{
		Variables: []models.Variable{
			{Name: "shared", Type: "string", Value: "from-graph"},
			{Name: "idea", Type: "string", Ask: true, Required: true},
		},
	}
	out, err := resolveStartVars(g, map[string]any{"idea": "hello"}, wsVars)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]any{}
	for _, rv := range out {
		by[rv.Name] = rv.Value
	}
	if by["region"] != "cn" {
		t.Fatalf("region = %v", by["region"])
	}
	if by["shared"] != "from-graph" {
		t.Fatalf("shared override = %v", by["shared"])
	}
	if by["only_workspace"] != "keep" {
		t.Fatalf("only_workspace = %v", by["only_workspace"])
	}
	if by["idea"] != "hello" {
		t.Fatalf("idea = %v", by["idea"])
	}
}

// A secret workspace variable stays Secret on the run (also when a graph
// variable overlays it); its value is unchanged.
func TestResolveStartVarsKeepsSecretFlag(t *testing.T) {
	wsVars := []models.WorkspaceVariable{
		{Name: "tok", Type: "string", Value: "s3cr3t", Secret: true},
		{Name: "over", Type: "string", Value: "ws", Secret: true},
		{Name: "plain", Type: "string", Value: "p"},
	}
	g := models.Graph{Variables: []models.Variable{{Name: "over", Type: "string", Value: "graph"}}}
	out, err := resolveStartVars(g, nil, wsVars)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]models.RunVariable{}
	for _, rv := range out {
		by[rv.Name] = rv
	}
	if !by["tok"].Secret || by["tok"].Value != "s3cr3t" || !by["over"].Secret || by["over"].Value != "graph" || by["plain"].Secret {
		t.Fatalf("secret flags: %+v", by)
	}
}
