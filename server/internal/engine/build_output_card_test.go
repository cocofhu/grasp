package engine

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func mustBuildCard(t *testing.T, e *Engine, c *execCtx, tmpl string) map[string]any {
	t.Helper()
	card, include := e.buildOutputCard(c, 1, tmpl)
	if !include || card == nil {
		t.Fatalf("expected card for %s, include=%v card=%v", tmpl, include, card)
	}
	return card
}

func TestBuildOutputCardBranches(t *testing.T) {
	e, db := setupEngine(t)
	run := &models.Run{ID: "run-card", WorkflowID: "wf", Status: "running", Graph: models.Graph{
		Nodes: []models.Node{{ID: "n1", Label: "N1"}, {ID: "n2", Label: "N2"}},
	}}
	if err := db.Create(run).Error; err != nil {
		t.Fatal(err)
	}
	seedArtifactOwner(t, db, run.ID)
	db.Create(&models.StateRun{RunID: run.ID, NodeID: "n1", Status: "failed", Iteration: 1})
	db.Create(&models.StateRun{RunID: run.ID, NodeID: "n2", Status: "succeeded", Iteration: 1})

	c := &execCtx{
		run:   run,
		graph: run.Graph,
		nodeOutputs: map[string]map[string]any{
			"n2": {
				"content":      "hello",
				"page":         "<html/>",
				"plan":         "# plan",
				"plan_json":    `{"goals":[]}`,
				"custom":       "x",
				"empty_custom": "  ",
			},
		},
	}

	// failed upstream — distinguishable failTitle (g1.2)
	card := mustBuildCard(t, e, c, "{{nodes.n1.outputs.content}}")
	if card["status"] != "failed" || card["failTitle"] != failTitleSourceFailed {
		t.Fatalf("failed node: %+v", card)
	}
	if strings.Contains(fmtSprint(card["errorReason"]), "上游节点无输出") {
		t.Fatalf("must not use 上游节点无输出 for real failure: %+v", card)
	}

	// missing outputs on an executed node → still a fail card, missing-product title (g1.2 / g4.1)
	c2 := &execCtx{run: run, graph: run.Graph, nodeOutputs: map[string]map[string]any{}}
	card = mustBuildCard(t, e, c2, "{{nodes.n2.outputs.content}}")
	if card["status"] != "failed" || card["failTitle"] != failTitleMissingOut {
		t.Fatalf("no outs: %+v", card)
	}
	if card["errorReason"] == "上游节点无输出" {
		t.Fatalf("deleted 上游节点无输出 copy: %+v", card)
	}
	// missing key
	card = mustBuildCard(t, e, c, "{{nodes.n2.outputs.missing}}")
	if card["status"] != "failed" || card["failTitle"] != failTitleMissingOut {
		t.Fatalf("missing key: %+v", card)
	}
	// content ok
	card = mustBuildCard(t, e, c, "{{nodes.n2.outputs.content}}")
	if card["status"] != "ok" || card["typeTag"] != "Markdown" {
		t.Fatalf("content: %+v", card)
	}
	// page is a custom HTML product; artifactName is node-scoped (g2.2 / g4.1)
	card = mustBuildCard(t, e, c, "{{nodes.n2.outputs.page}}")
	if card["status"] != "ok" || card["typeTag"] != "自定义产物" || card["artifactName"] != "n2.page.html" {
		t.Fatalf("page: %+v", card)
	}
	if card["structuredArtifactName"] != nil {
		t.Fatalf("page must not set structuredArtifactName: %+v", card)
	}
	if card["jsonSnapshot"] != nil {
		t.Fatalf("page must not set jsonSnapshot: %+v", card)
	}
	if md, _ := card["markdown"].(string); strings.TrimSpace(md) == "" {
		t.Fatalf("page should keep markdown for HtmlPreview fallback: %+v", card)
	}
	card = mustBuildCard(t, e, c, "{{nodes.n2.outputs.plan}}")
	if card["typeTag"] != "结构化产物" {
		t.Fatalf("plan: %+v", card)
	}
	card = mustBuildCard(t, e, c, "{{nodes.n2.outputs.empty_custom}}")
	if card["status"] != "failed" || card["failTitle"] != failTitleMissingOut {
		t.Fatalf("empty custom: %+v", card)
	}
	card = mustBuildCard(t, e, c, "{{nodes.n2.outputs.custom}}")
	if card["status"] != "ok" {
		t.Fatalf("custom: %+v", card)
	}
	card = mustBuildCard(t, e, c, "{{artifact(\"missing.json\")}}")
	if card["status"] != "failed" {
		t.Fatalf("artifact miss: %+v", card)
	}
	// artifact("page.html") remains custom HTML (regression: not structured)
	if _, err := e.store.Save(run.ID, "n2", "page.html", "html", "<html>artifact</html>"); err != nil {
		t.Fatal(err)
	}
	card = mustBuildCard(t, e, c, "{{artifact(\"page.html\")}}")
	if card["status"] != "ok" || card["typeTag"] != "自定义产物" || card["artifactName"] != "page.html" {
		t.Fatalf("artifact page.html: %+v", card)
	}
	if card["structuredArtifactName"] != nil {
		t.Fatalf("artifact page.html must not be structured: %+v", card)
	}
	// empty page content → 缺少可展示产出 (g1.2 / g4.1)
	cEmpty := &execCtx{
		run:   run,
		graph: run.Graph,
		nodeOutputs: map[string]map[string]any{
			"n2": {"page": "  "},
		},
	}
	card = mustBuildCard(t, e, cEmpty, "{{nodes.n2.outputs.page}}")
	if card["status"] != "failed" || card["typeTag"] != "来源失败" || card["failTitle"] != failTitleMissingOut {
		t.Fatalf("empty page: %+v", card)
	}
	card = mustBuildCard(t, e, c, "plain-text")
	if card["status"] != "ok" && card["status"] != "failed" {
		t.Fatalf("unknown tmpl: %+v", card)
	}
}

func TestBuildOutputCardHidesUnexecutedAndSkipped(t *testing.T) {
	e, db := setupEngine(t)
	run := &models.Run{ID: "run-hide", WorkflowID: "wf", Status: "running", Graph: models.Graph{
		Nodes: []models.Node{
			{ID: "visual_l6zc", Label: ""},
			{ID: "skipped_n", Label: "Skipped"},
			{ID: "running_n", Label: "Running"},
		},
	}}
	if err := db.Create(run).Error; err != nil {
		t.Fatal(err)
	}
	db.Create(&models.StateRun{RunID: run.ID, NodeID: "skipped_n", Status: "skipped", Iteration: 1})
	db.Create(&models.StateRun{RunID: run.ID, NodeID: "running_n", Status: "running", Iteration: 1})

	c := &execCtx{run: run, graph: run.Graph, nodeOutputs: map[string]map[string]any{}}

	// screenshot case: no StateRun + missing nodeOutputs → hide (g1.1)
	if card, include := e.buildOutputCard(c, 1, "{{nodes.visual_l6zc.outputs.page}}"); include || card != nil {
		t.Fatalf("unexecuted visual_l6zc must hide, got include=%v card=%+v", include, card)
	}
	// skipped → hide, not a fail card (g1.1)
	if card, include := e.buildOutputCard(c, 1, "{{nodes.skipped_n.outputs.research}}"); include || card != nil {
		t.Fatalf("skipped must hide, got include=%v card=%+v", include, card)
	}
	// non-terminal + no output → hide (g1.1)
	if card, include := e.buildOutputCard(c, 1, "{{nodes.running_n.outputs.content}}"); include || card != nil {
		t.Fatalf("running without output must hide, got include=%v card=%+v", include, card)
	}
}

func TestBuildOutputCardCancelledAndWaitingWithOutput(t *testing.T) {
	e, db := setupEngine(t)
	run := &models.Run{ID: "run-cancel", WorkflowID: "wf", Status: "running", Graph: models.Graph{
		Nodes: []models.Node{{ID: "cn", Label: "C"}, {ID: "wh", Label: "W"}},
	}}
	if err := db.Create(run).Error; err != nil {
		t.Fatal(err)
	}
	db.Create(&models.StateRun{RunID: run.ID, NodeID: "cn", Status: "cancelled", Iteration: 1})
	db.Create(&models.StateRun{RunID: run.ID, NodeID: "wh", Status: "waiting_human", Iteration: 1})

	c := &execCtx{
		run:   run,
		graph: run.Graph,
		nodeOutputs: map[string]map[string]any{
			"wh": {"page": "<html>live</html>"},
		},
	}
	card := mustBuildCard(t, e, c, "{{nodes.cn.outputs.content}}")
	if card["status"] != "failed" || card["failTitle"] != failTitleCancelled {
		t.Fatalf("cancelled: %+v", card)
	}
	card = mustBuildCard(t, e, c, "{{nodes.wh.outputs.page}}")
	if card["status"] != "ok" || card["artifactName"] != "wh.page.html" {
		t.Fatalf("waiting_human with page should still show: %+v", card)
	}
}

func fmtSprint(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
