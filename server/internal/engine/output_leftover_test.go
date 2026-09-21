package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func leftoverGraph(autoOn bool) models.Graph {
	cfg := map[string]any{}
	if autoOn {
		cfg["auto_leftover_draft"] = true
	}
	return models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "test1", Type: "test", Label: "集成测试"},
			{ID: "review1", Type: "review", Label: "代码评审"},
			{ID: "output", Type: "output", Label: "结束", Config: cfg},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "test1"},
			{ID: "e2", Source: "test1", Target: "review1"},
			{ID: "e3", Source: "review1", Target: "output"},
		},
	}
}

func seedLeftoverRun(t *testing.T, eng *Engine, runID string, g models.Graph, outs map[string]map[string]any) (*execCtx, *models.Node) {
	t.Helper()
	if err := eng.db.Model(&models.WorkflowDef{}).Where("id = ?", "wf").
		Update("project_id", models.DefaultProjectID).Error; err != nil {
		t.Fatalf("bind project: %v", err)
	}
	run := &models.Run{
		ID:              runID,
		WorkflowID:      "wf",
		WorkflowName:    "遗留入库流水线",
		WorkflowVersion: 3,
		Status:          "running",
		Trigger:         models.TriggerManual,
		Inputs:          map[string]any{"feature": "auto leftover draft"},
		Graph:           g,
		StartedAt:       time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC),
	}
	if err := eng.db.Create(run).Error; err != nil {
		t.Fatalf("create run: %v", err)
	}
	for nid, o := range outs {
		if err := eng.db.Create(&models.StateRun{
			RunID: run.ID, NodeID: nid, Status: "completed", Iteration: 1, Outputs: o,
		}).Error; err != nil {
			t.Fatalf("state %s: %v", nid, err)
		}
	}
	c := &execCtx{run: run, graph: g, nodeOutputs: outs, vars: map[string]any{}}
	var outNode *models.Node
	for i := range g.Nodes {
		if g.Nodes[i].ID == "output" {
			outNode = &g.Nodes[i]
			break
		}
	}
	if outNode == nil {
		t.Fatal("missing output node")
	}
	return c, outNode
}

func TestCollectLeftoversFromTestAndReview(t *testing.T) {
	// plan g2.1 / g2.2: merge defects + findings + action_items; skipped is context only.
	c := &execCtx{
		graph: leftoverGraph(true),
		nodeOutputs: map[string]map[string]any{
			"test1": {
				"test_result_json": `{
					"summary":"ok",
					"skipped":1,
					"defects":[{"title":"慢查询","severity":"medium","detail":"N+1"},{"title":"  "}]
				}`,
			},
			"review1": {
				"review_json": `{
					"summary":"approve with comments",
					"verdict":"approve_with_comments",
					"findings":[{"title":"命名","severity":"low","file":"a.go","line":12,"suggestion":"rename"}],
					"action_items":["补单测",""]
				}`,
			},
		},
	}
	b := collectLeftovers(c)
	if len(b.Items) != 3 {
		t.Fatalf("items=%d %#v", len(b.Items), b.Items)
	}
	if b.TestSkipped != 1 {
		t.Fatalf("skipped=%d", b.TestSkipped)
	}
	kinds := map[string]int{}
	for _, it := range b.Items {
		kinds[it.Kind]++
	}
	if kinds["defect"] != 1 || kinds["finding"] != 1 || kinds["action_item"] != 1 {
		t.Fatalf("kinds=%v", kinds)
	}
}

func TestCollectLeftoversSkipsBadJSON(t *testing.T) {
	c := &execCtx{
		graph: leftoverGraph(true),
		nodeOutputs: map[string]map[string]any{
			"test1": {"test_result_json": `{not-json`},
			"review1": {
				"review_json": `{"summary":"s","verdict":"approve","findings":[{"title":"x"}],"action_items":["y"]}`,
			},
		},
	}
	b := collectLeftovers(c)
	if len(b.Items) != 2 {
		t.Fatalf("want review-only leftovers, got %d", len(b.Items))
	}
}

func TestExecOutputLeftoverDraftHappyPath(t *testing.T) {
	// plan g3.1–g3.3 / g4.1: switch on + leftovers → exactly 1 open draft.
	g := leftoverGraph(true)
	eng, db, _ := setupEngineGraphP(t, g)
	outs := map[string]map[string]any{
		"test1": {
			"test_result_json": `{"summary":"s","defects":[{"title":"泄漏","severity":"high","detail":"未关连接"}]}`,
		},
		"review1": {
			"review_json": `{"summary":"s","verdict":"approve_with_comments","findings":[{"title":"日志","severity":"low"}],"action_items":["跟进"]}`,
		},
	}
	c, node := seedLeftoverRun(t, eng, "run-leftover-1", g, outs)
	oc := eng.execOutput(c, node)
	if oc.status != "completed" {
		t.Fatalf("status=%s", oc.status)
	}
	id, _ := oc.outputs[leftoverDraftIDKey].(string)
	if id == "" {
		t.Fatalf("missing leftoverDraftId: %#v", oc.outputs)
	}
	if oc.outputs[leftoverDraftErrKey] != nil {
		t.Fatalf("unexpected error: %v", oc.outputs[leftoverDraftErrKey])
	}
	count, _ := oc.outputs[leftoverItemCountKey].(int)
	if count != 3 {
		t.Fatalf("itemCount=%v", oc.outputs[leftoverItemCountKey])
	}
	title, _ := oc.outputs[leftoverDraftTitleKey].(string)
	if !strings.Contains(title, "遗留入库流水线") || !strings.Contains(title, "run-lef") {
		t.Fatalf("title=%q", title)
	}

	svc := services.NewRequirementDraftService(db)
	draft, err := svc.Get(models.DefaultProjectID, id)
	if err != nil {
		t.Fatalf("get draft: %v", err)
	}
	if draft.Status != models.RequirementDraftStatusOpen || draft.Kind != models.RequirementDraftKindRequirement {
		t.Fatalf("draft meta: %+v", draft)
	}
	if draft.StartAt != "" || draft.DueAt != "" {
		t.Fatalf("must be unscheduled: %+v", draft)
	}
	body := draft.BodyMarkdown
	for _, needle := range []string{
		"背景", "遗留入库流水线", "run-leftover-1", "manual", "泄漏", "日志", "跟进", "集成测试", "代码评审",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("body missing %q:\n%s", needle, body)
		}
	}

	var n int64
	if err := db.Model(&models.RequirementDraft{}).Where("project_id = ?", models.DefaultProjectID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("draft count=%d", n)
	}
}

func TestExecOutputLeftoverSwitchOff(t *testing.T) {
	g := leftoverGraph(false)
	eng, db, _ := setupEngineGraphP(t, g)
	outs := map[string]map[string]any{
		"test1": {"test_result_json": `{"summary":"s","defects":[{"title":"x"}]}`},
	}
	c, node := seedLeftoverRun(t, eng, "run-leftover-off", g, outs)
	oc := eng.execOutput(c, node)
	if oc.outputs[leftoverDraftIDKey] != nil || oc.outputs[leftoverDraftErrKey] != nil {
		t.Fatalf("switch off must not write leftover outputs: %#v", oc.outputs)
	}
	var n int64
	if err := db.Model(&models.RequirementDraft{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("drafts=%d", n)
	}
}

func TestExecOutputLeftoverNoItems(t *testing.T) {
	g := leftoverGraph(true)
	eng, db, _ := setupEngineGraphP(t, g)
	outs := map[string]map[string]any{
		"test1":  {"test_result_json": `{"summary":"s","defects":[],"cases":[{"name":"a","status":"skipped"}]}`},
		"review1": {"review_json": `{"summary":"s","verdict":"approve","findings":[],"action_items":[]}`},
	}
	c, node := seedLeftoverRun(t, eng, "run-leftover-empty", g, outs)
	oc := eng.execOutput(c, node)
	if oc.status != "completed" {
		t.Fatalf("status=%s", oc.status)
	}
	if oc.outputs[leftoverDraftIDKey] != nil {
		t.Fatalf("no leftovers should not create draft: %#v", oc.outputs)
	}
	var n int64
	if err := db.Model(&models.RequirementDraft{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("drafts=%d", n)
	}
}

func TestExecOutputLeftoverDedupSameRun(t *testing.T) {
	g := leftoverGraph(true)
	eng, db, _ := setupEngineGraphP(t, g)
	outs := map[string]map[string]any{
		"test1": {"test_result_json": `{"summary":"s","defects":[{"title":"a"}]}`},
	}
	c, node := seedLeftoverRun(t, eng, "run-leftover-dedup", g, outs)
	oc1 := eng.execOutput(c, node)
	id1, _ := oc1.outputs[leftoverDraftIDKey].(string)
	if id1 == "" {
		t.Fatalf("first create failed: %#v", oc1.outputs)
	}
	if err := eng.db.Create(&models.StateRun{
		RunID: c.run.ID, NodeID: "output", Status: "completed", Iteration: 1, Outputs: oc1.outputs,
	}).Error; err != nil {
		t.Fatal(err)
	}
	oc2 := eng.execOutput(c, node)
	id2, _ := oc2.outputs[leftoverDraftIDKey].(string)
	if id2 != id1 {
		t.Fatalf("dedup want %s got %s", id1, id2)
	}
	var n int64
	if err := db.Model(&models.RequirementDraft{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("drafts=%d want 1", n)
	}
}

func TestExecOutputLeftoverWriteFailureStillCompleted(t *testing.T) {
	g := leftoverGraph(true)
	eng, _db, _ := setupEngineGraphP(t, g)
	_ = _db
	outs := map[string]map[string]any{
		"test1": {"test_result_json": `{"summary":"s","defects":[{"title":"a"}]}`},
	}
	c, node := seedLeftoverRun(t, eng, "run-leftover-fail", g, outs)
	if err := eng.db.Model(&models.WorkflowDef{}).Where("id = ?", "wf").
		Update("project_id", "").Error; err != nil {
		t.Fatal(err)
	}
	oc := eng.execOutput(c, node)
	if oc.status != "completed" {
		t.Fatalf("status=%s", oc.status)
	}
	errMsg, _ := oc.outputs[leftoverDraftErrKey].(string)
	if errMsg == "" {
		t.Fatalf("expected leftoverDraftError: %#v", oc.outputs)
	}
	if oc.outputs[leftoverDraftIDKey] != nil {
		t.Fatalf("should not have draft id: %#v", oc.outputs)
	}
}

func TestBuildLeftoverDraftTitleRuneCap(t *testing.T) {
	long := strings.Repeat("名", 250)
	title := buildLeftoverDraftTitle(long, "abcdefghij")
	if n := len([]rune(title)); n > services.MaxRequirementDraftTitleRunes {
		t.Fatalf("title runes=%d", n)
	}
	if !strings.Contains(title, "abc") {
		t.Fatalf("title=%q", title)
	}
}

func TestAutoLeftoverDraftEnabled(t *testing.T) {
	if autoLeftoverDraftEnabled(nil) || autoLeftoverDraftEnabled(map[string]any{}) {
		t.Fatal("default off")
	}
	if !autoLeftoverDraftEnabled(map[string]any{"auto_leftover_draft": true}) {
		t.Fatal("true")
	}
	if autoLeftoverDraftEnabled(map[string]any{"auto_leftover_draft": false}) {
		t.Fatal("false")
	}
}
