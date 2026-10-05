package services

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func envelopeJSON(env models.ExportEnvelope) []byte {
	b, err := json.Marshal(env)
	if err != nil {
		panic(err)
	}
	return b
}

func validEnvelope(name string) models.ExportEnvelope {
	return models.ExportEnvelope{
		SchemaVersion: models.ExportSchemaVersion,
		Name:          name,
		Description:   "desc",
		NeedsRepo:     true,
		Graph:         validGraph(),
	}
}

func TestValidateImportRejectsBadSchema(t *testing.T) {
	env := validEnvelope("Demo")
	env.SchemaVersion = 99
	_, err := ValidateImport(envelopeJSON(env))
	if err == nil || !strings.Contains(err.Error(), "schemaVersion") {
		t.Fatalf("want schemaVersion error, got %v", err)
	}
}

func TestValidateImportRejectsMissingName(t *testing.T) {
	env := validEnvelope("")
	_, err := ValidateImport(envelopeJSON(env))
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("want name error, got %v", err)
	}
}

func TestValidateImportRejectsMissingNodes(t *testing.T) {
	raw := `{"schemaVersion":1,"name":"X","graph":{"edges":[]}}`
	_, err := ValidateImport([]byte(raw))
	if err == nil || !strings.Contains(err.Error(), "graph.nodes") {
		t.Fatalf("want nodes error, got %v", err)
	}
}

func TestValidateImportRejectsMissingEdges(t *testing.T) {
	raw := `{"schemaVersion":1,"name":"X","graph":{"nodes":[{"id":"in","type":"input","label":"S"},{"id":"out","type":"output","label":"E"}]}}`
	_, err := ValidateImport([]byte(raw))
	if err == nil || !strings.Contains(err.Error(), "graph.edges") {
		t.Fatalf("want edges error, got %v", err)
	}
}

func TestValidateImportRejectsBadEdgeRef(t *testing.T) {
	env := validEnvelope("Demo")
	env.Graph.Edges = []models.Edge{{ID: "e1", Source: "in", Target: "nope"}}
	_, err := ValidateImport(envelopeJSON(env))
	if err == nil || !strings.Contains(err.Error(), "target") {
		t.Fatalf("want edge target error, got %v", err)
	}
}

func TestValidateImportRejectsUnknownType(t *testing.T) {
	env := validEnvelope("Demo")
	env.Graph.Nodes = append(env.Graph.Nodes, models.Node{ID: "bad", Type: "unknown_node", Label: "Bad"})
	env.Graph.Edges = append(env.Graph.Edges, models.Edge{ID: "e2", Source: "in", Target: "bad"})
	_, err := ValidateImport(envelopeJSON(env))
	if err == nil || !strings.Contains(err.Error(), "unknown_node") {
		t.Fatalf("want unknown type error, got %v", err)
	}
}

func TestValidateImportRejectsGraphValidate(t *testing.T) {
	env := validEnvelope("Demo")
	env.Graph.Nodes = []models.Node{
		{ID: "in1", Type: "input", Label: "A"},
		{ID: "in2", Type: "input", Label: "B"},
		{ID: "out", Type: "output", Label: "E"},
	}
	env.Graph.Edges = []models.Edge{}
	_, err := ValidateImport(envelopeJSON(env))
	if err == nil || !strings.Contains(err.Error(), "输入节点") {
		t.Fatalf("want Graph.Validate error, got %v", err)
	}
}

func TestWorkflowImportCreatesDraft(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)

	existing := &models.WorkflowDef{ID: "wf-x", ProjectID: models.DefaultProjectID, Name: "工作流 A", Graph: validGraph()}
	if err := s.Save(existing); err != nil {
		t.Fatal(err)
	}

	raw := envelopeJSON(validEnvelope("工作流 A"))
	imported, err := s.Import(raw, "")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if imported.Name != "工作流 A 副本" {
		t.Fatalf("name = %q, want 工作流 A 副本", imported.Name)
	}
	if imported.Status != "draft" || imported.Version != 1 {
		t.Fatalf("status/version = %s/%d", imported.Status, imported.Version)
	}
	if imported.ShowOnHome {
		t.Fatal("import ShowOnHome want false (plan g1.3)")
	}
	if imported.ID == "wf-x" {
		t.Fatal("expected new id")
	}

	var verCount int64
	db.Model(&models.WorkflowVersion{}).Where("workflow_id = ?", imported.ID).Count(&verCount)
	if verCount != 0 {
		t.Fatalf("versions copied: %d", verCount)
	}
	var runCount int64
	db.Model(&models.Run{}).Where("workflow_id = ?", imported.ID).Count(&runCount)
	if runCount != 0 {
		t.Fatalf("runs copied: %d", runCount)
	}
}

func TestWorkflowVersionGraph(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)
	wf := &models.WorkflowDef{ID: "wf-v", ProjectID: models.DefaultProjectID, Name: "V", Graph: validGraph()}
	if err := s.Save(wf); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish("wf-v"); err != nil {
		t.Fatal(err)
	}
	g, err := s.VersionGraph("wf-v", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != 2 {
		t.Fatalf("graph nodes = %d", len(g.Nodes))
	}
}

func TestMigrateOutputNodes(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "in", Type: "input", Label: "S"},
			{ID: "o1", Type: "output", Label: "E1", Config: map[string]any{"result": "  alpha  "}},
			{ID: "o2", Type: "output", Label: "E2", Config: map[string]any{"results": []any{"keep"}}},
			{ID: "o3", Type: "output", Label: "E3"},
			{ID: "o4", Type: "output", Label: "E4", Config: map[string]any{"result": "  "}},
		},
	}
	MigrateOutputNodes(&g)
	if got, ok := g.Nodes[1].Config["results"].([]any); !ok || len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("o1 results = %#v", g.Nodes[1].Config["results"])
	}
	if got := g.Nodes[2].Config["results"].([]any); len(got) != 1 || got[0] != "keep" {
		t.Fatalf("o2 should keep existing results: %#v", got)
	}
	if g.Nodes[3].Config == nil {
		t.Fatal("o3 config should be initialized")
	}
	// fmt.Sprint(nil) yields "<nil>", which is treated as a legacy result string.
	if got, ok := g.Nodes[3].Config["results"].([]any); !ok || len(got) != 1 || got[0] != "<nil>" {
		t.Fatalf("o3 results from nil result key = %#v", g.Nodes[3].Config["results"])
	}
	if got, ok := g.Nodes[4].Config["results"].([]any); !ok || len(got) != 0 {
		t.Fatalf("o4 blank result → empty results = %#v", got)
	}
}

func TestLiftInputVariables(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{
				ID: "in", Type: "input", Label: "S",
				Config: map[string]any{
					"variables": []any{
						map[string]any{"name": "x", "label": "X", "type": "string"},
					},
					"inputs": "legacy",
				},
			},
			{ID: "out", Type: "output", Label: "E"},
		},
	}
	LiftInputVariables(&g)
	if len(g.Variables) != 1 || g.Variables[0].Name != "x" {
		t.Fatalf("variables = %+v", g.Variables)
	}
	if _, ok := g.Nodes[0].Config["variables"]; ok {
		t.Fatal("variables should be removed from input config")
	}
	if _, ok := g.Nodes[0].Config["inputs"]; ok {
		t.Fatal("inputs should be removed from input config")
	}

	g2 := models.Graph{Nodes: []models.Node{{ID: "out", Type: "output", Label: "E", Config: map[string]any{}}}}
	LiftInputVariables(&g2)
	if len(g2.Variables) != 0 {
		t.Fatalf("no-input should leave Variables empty: %+v", g2.Variables)
	}

	g3 := models.Graph{Nodes: []models.Node{{
		ID: "in", Type: "input", Label: "S",
		Config: map[string]any{"variables": "not-array", "inputs": 1},
	}}}
	LiftInputVariables(&g3)
	if _, ok := g3.Nodes[0].Config["variables"]; ok {
		t.Fatal("bad variables key should still be deleted")
	}
}

func TestValidateImportRejectsInvalidJSON(t *testing.T) {
	_, err := ValidateImport([]byte(`{`))
	if err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("want JSON error, got %v", err)
	}
}

func TestValidateImportNilVariablesDefaults(t *testing.T) {
	raw := `{"schemaVersion":1,"name":"X","graph":{"nodes":[{"id":"in","type":"input","label":"S"},{"id":"out","type":"output","label":"E"}],"edges":[{"id":"e1","source":"in","target":"out"}]}}`
	env, err := ValidateImport([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if env.Graph.Variables == nil {
		t.Fatal("Variables should default to empty slice")
	}
}

func TestWorkflowImportLiftsAndMigrates(t *testing.T) {
	db := newTestDB(t)
	s := NewWorkflowService(db)
	env := models.ExportEnvelope{
		SchemaVersion: models.ExportSchemaVersion,
		Name:          "Mig",
		Graph: models.Graph{
			Nodes: []models.Node{
				{
					ID: "in", Type: "input", Label: "S",
					Config: map[string]any{
						"variables": []any{map[string]any{"name": "n", "label": "N", "type": "string"}},
					},
				},
				{ID: "out", Type: "output", Label: "E", Config: map[string]any{"result": "r1"}},
			},
			Edges: []models.Edge{{ID: "e1", Source: "in", Target: "out"}},
		},
	}
	imported, err := s.Import(envelopeJSON(env), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(imported.Graph.Variables) != 1 || imported.Graph.Variables[0].Name != "n" {
		t.Fatalf("lifted vars: %+v", imported.Graph.Variables)
	}
	var outCfg map[string]any
	for _, n := range imported.Graph.Nodes {
		if n.Type == "output" {
			outCfg = n.Config
		}
		if n.Type == "input" {
			if _, ok := n.Config["variables"]; ok {
				t.Fatal("input variables should be lifted away")
			}
		}
	}
	if got, ok := outCfg["results"].([]any); !ok || len(got) != 1 {
		t.Fatalf("migrated results: %#v", outCfg["results"])
	}
}
