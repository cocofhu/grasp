package engine

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestResolveOutputResults(t *testing.T) {
	got := resolveOutputResults(map[string]any{
		"results": []any{"{{nodes.a.outputs.plan}}", "{{artifact(\"x.md\")}}"},
	})
	if len(got) != 2 {
		t.Fatalf("results len = %d", len(got))
	}
	if got := resolveOutputResults(map[string]any{"result": "{{nodes.old.outputs.content}}"}); len(got) != 0 {
		t.Fatalf("singular result must be ignored: %v", got)
	}
	if len(resolveOutputResults(map[string]any{})) != 0 {
		t.Fatal("empty config should yield no templates")
	}
}

func TestExecOutputMultiSourceAndFallback(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "research", Type: "agent", Caps: capsResearchAuto, Label: "技术调研", Config: map[string]any{"agent_profile": "ResearchAgent"}},
			{ID: "agent", Type: "agent", Caps: capsPlain, Label: "代码实现", Config: map[string]any{"agent_profile": "ImplementAgent"}},
			{ID: "output", Type: "output", Config: map[string]any{
				"results": []any{
					"{{nodes.research.outputs.research}}",
					"{{nodes.agent.outputs.content}}",
					"{{artifact(\"missing.md\")}}",
				},
			}},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "research"},
			{ID: "e2", Source: "research", Target: "agent"},
			{ID: "e3", Source: "agent", Target: "output"},
		},
	}
	eng, db, fp := setupEngineGraphP(t, g)
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	var sr models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "output").First(&sr).Error; err != nil {
		t.Fatal(err)
	}
	if sr.OutputMd != outputCompleteMd {
		t.Errorf("outputMd = %q, want fixed complete message", sr.OutputMd)
	}
	if sr.Status != "completed" {
		t.Errorf("output status = %q", sr.Status)
	}
	cards, ok := sr.Outputs["outputCards"].([]any)
	if !ok || len(cards) != 3 {
		t.Fatalf("outputCards = %T %#v", sr.Outputs["outputCards"], sr.Outputs["outputCards"])
	}
	c0, _ := cards[0].(map[string]any)
	if c0["typeTag"] != "结构化产物" {
		t.Errorf("card0 type = %v", c0["typeTag"])
	}
	c2, _ := cards[2].(map[string]any)
	if c2["status"] != "failed" || c2["typeTag"] != "来源失败" {
		t.Errorf("missing artifact card = %#v", c2)
	}
	_ = fp
}

func TestExecOutputEmptyResults(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "output", Type: "output", Config: map[string]any{"results": []any{}}},
		},
		Edges: []models.Edge{{ID: "e1", Source: "input", Target: "output"}},
	}
	eng, db, _ := setupEngineGraphP(t, g)
	run, _ := eng.StartRun("wf", nil, "test")
	waitRunStatus(t, db, run.ID, "completed")
	var sr models.StateRun
	db.Where("run_id = ? AND node_id = ?", run.ID, "output").First(&sr)
	if sr.OutputMd != outputCompleteMd {
		t.Errorf("empty results outputMd = %q", sr.OutputMd)
	}
	cards, _ := sr.Outputs["outputCards"].([]any)
	if len(cards) != 0 {
		t.Errorf("expected 0 cards, got %d", len(cards))
	}
}

func TestExecOutputHidesUnexecutedAndStaysCompleted(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "agent", Type: "agent", Caps: capsPlain, Label: "代码实现", Config: map[string]any{"agent_profile": "ImplementAgent"}},
			{ID: "visual_l6zc", Type: "agent", Caps: capsPage, Label: ""},
			{ID: "output", Type: "output", Config: map[string]any{
				"results": []any{
					"{{nodes.agent.outputs.content}}",
					"{{nodes.visual_l6zc.outputs.page}}",
				},
			}},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "agent"},
			{ID: "e2", Source: "agent", Target: "output"},
		},
	}
	eng, db, _ := setupEngineGraphP(t, g)
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	var sr models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "output").First(&sr).Error; err != nil {
		t.Fatal(err)
	}
	if sr.Status != "completed" {
		t.Fatalf("output status = %q, hide must not fail the node (g1.3)", sr.Status)
	}
	cards, _ := sr.Outputs["outputCards"].([]any)
	if len(cards) != 1 {
		t.Fatalf("expected only executed source card, got %#v", cards)
	}
	c0, _ := cards[0].(map[string]any)
	if c0["nodeId"] != "agent" || c0["status"] != "ok" {
		t.Fatalf("kept card = %#v", c0)
	}
}

func TestExecOutputAllHiddenStaysCompleted(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "visual_l6zc", Type: "agent", Caps: capsPage},
			{ID: "output", Type: "output", Config: map[string]any{
				"results": []any{"{{nodes.visual_l6zc.outputs.page}}"},
			}},
		},
		Edges: []models.Edge{{ID: "e1", Source: "input", Target: "output"}},
	}
	eng, db, _ := setupEngineGraphP(t, g)
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")
	var sr models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "output").First(&sr).Error; err != nil {
		t.Fatal(err)
	}
	if sr.Status != "completed" {
		t.Fatalf("all-hidden output status = %q (g1.3)", sr.Status)
	}
	cards, _ := sr.Outputs["outputCards"].([]any)
	if len(cards) != 0 {
		t.Fatalf("all hidden should yield empty cards, got %#v", cards)
	}
}

func TestExecOutputDualVisualIndependentPages(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "visual_a", Type: "agent", Caps: capsPageAuto, Label: "视觉A"},
			{ID: "visual_b", Type: "agent", Caps: capsPageAuto, Label: "视觉B"},
			{ID: "output", Type: "output", Config: map[string]any{
				"results": []any{
					"{{nodes.visual_a.outputs.page}}",
					"{{nodes.visual_b.outputs.page}}",
				},
			}},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "visual_a"},
			{ID: "e2", Source: "visual_a", Target: "visual_b"},
			{ID: "e3", Source: "visual_b", Target: "output"},
		},
	}
	eng, db, fp := setupEngineGraphP(t, g)
	fp.visualBodyByNode = map[string]string{
		"visual_a": "<!doctype html><html><body><h1>page-a</h1></body></html>",
		"visual_b": "<!doctype html><html><body><h1>page-b</h1></body></html>",
	}
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	var sr models.StateRun
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "output").First(&sr).Error; err != nil {
		t.Fatal(err)
	}
	cards, ok := sr.Outputs["outputCards"].([]any)
	if !ok || len(cards) != 2 {
		t.Fatalf("outputCards = %#v", sr.Outputs["outputCards"])
	}
	c0, _ := cards[0].(map[string]any)
	c1, _ := cards[1].(map[string]any)
	if c0["status"] != "ok" || c1["status"] != "ok" {
		t.Fatalf("both visual cards should be ok: %#v %#v", c0, c1)
	}
	if c0["artifactName"] != "visual_a.page.html" || c1["artifactName"] != "visual_b.page.html" {
		t.Fatalf("artifactName a=%v b=%v", c0["artifactName"], c1["artifactName"])
	}
	md0, _ := c0["markdown"].(string)
	md1, _ := c1["markdown"].(string)
	if md0 == md1 || !strings.Contains(md0, "page-a") || !strings.Contains(md1, "page-b") {
		t.Fatalf("markdown snapshots must differ: %q / %q", md0, md1)
	}
	aHTML, okA := eng.store.Get(run.ID, "visual_a.page.html")
	bHTML, okB := eng.store.Get(run.ID, "visual_b.page.html")
	alias, okAlias := eng.store.Get(run.ID, "page.html")
	if !okA || !okB || !okAlias {
		t.Fatalf("expected alias + node copies: a=%v b=%v alias=%v", okA, okB, okAlias)
	}
	if aHTML == bHTML || !strings.Contains(aHTML, "page-a") || !strings.Contains(bHTML, "page-b") {
		t.Fatalf("node-scoped HTML must stay independent: %q / %q", aHTML, bHTML)
	}
	if alias != bHTML {
		t.Fatalf("page.html alias should be the later visual, got %q", alias)
	}
}
