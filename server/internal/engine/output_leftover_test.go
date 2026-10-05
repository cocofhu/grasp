package engine

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cocofhu/grasp/internal/mcp"
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
			{ID: "test1", Type: "agent", Caps: capsTest, Label: "集成测试"},
			{ID: "review1", Type: "agent", Caps: capsCodeReview, Label: "代码评审"},
			{ID: "output", Type: "output", Label: "结束", Config: cfg},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "test1"},
			{ID: "e2", Source: "test1", Target: "review1"},
			{ID: "e3", Source: "review1", Target: "output"},
		},
	}
}

func leftoverGraphWithClarify(autoOn bool) models.Graph {
	cfg := map[string]any{}
	if autoOn {
		cfg["auto_leftover_draft"] = true
	}
	return models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "react1", Type: "agent", Caps: capsClarify, Label: "需求澄清"},
			{ID: "grasp1", Type: "agent", Caps: capsClarify, Label: "Grasp"},
			{ID: "test1", Type: "agent", Caps: capsTest, Label: "集成测试"},
			{ID: "review1", Type: "agent", Caps: capsCodeReview, Label: "代码评审"},
			{ID: "plan1", Type: "agent", Caps: capsPlan, Label: "计划"},
			{ID: "impl1", Type: "agent", Caps: capsImplement, Label: "实现"},
			{ID: "output", Type: "output", Label: "结束", Config: cfg},
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
		WorkflowName:    "遗留入库工作流",
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
	// plan g1.2: merge defects + findings + action_items; skipped is context only.
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

func TestCollectLeftoversFailedCasesAndOpenQuestions(t *testing.T) {
	// plan g1.2: failed cases + open_questions are leftovers and can trigger alone.
	c := &execCtx{
		graph: leftoverGraphWithClarify(true),
		nodeOutputs: map[string]map[string]any{
			"test1": {
				"test_result_json": `{
					"summary":"s",
					"defects":[],
					"cases":[
						{"name":"登录超时","status":"failed","detail":"500"},
						{"name":"跳过","status":"skipped"},
						{"name":"通过","status":"passed"}
					]
				}`,
			},
			"react1": {
				"clarified_requirement_json": `{
					"title":"T","summary":"s","background":"b","goals":["g"],
					"in_scope":["in"],"out_of_scope":["out"],
					"functional_requirements":[{"title":"f","detail":"d","acceptance_criteria":["ac"]}],
					"assumptions":["a"],"dependencies":["d"],"constraints":["c"],
					"open_questions":["还要短信吗？","  "]
				}`,
			},
		},
	}
	b := collectLeftovers(c)
	if len(b.Items) != 2 {
		t.Fatalf("items=%d %#v", len(b.Items), b.Items)
	}
	kinds := map[string]int{}
	for _, it := range b.Items {
		kinds[it.Kind]++
	}
	if kinds[leftoverKindFailedCase] != 1 || kinds[leftoverKindOpenQuestion] != 1 {
		t.Fatalf("kinds=%v", kinds)
	}
	if b.TestSkipped != 1 {
		t.Fatalf("skipped=%d", b.TestSkipped)
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
	// plan g2.1: switch on + leftovers → exactly 1 open draft; body is self-contained.
	g := leftoverGraph(true)
	eng, db, _ := setupEngineGraphP(t, g)
	outs := map[string]map[string]any{
		"test1": {
			"test_result_json": `{"summary":"s","defects":[{"title":"泄漏","severity":"high","detail":"未关连接"}]}`,
		},
		"review1": {
			"review_json": `{"summary":"s","verdict":"approve_with_comments","findings":[{"title":"日志","severity":"low","detail":"缺字段","file":"log.go","line":9,"suggestion":"补齐"}],"action_items":["跟进"]}`,
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
	if !strings.Contains(title, "遗留入库工作流") || !strings.Contains(title, "run-lef") {
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
		"背景", "自包含", "原始需求输入", "未经结构化澄清", "仍须完成", "来源注记",
		"遗留入库工作流", "run-leftover-1", "manual", "泄漏", "未关连接", "日志", "缺字段", "log.go", "跟进",
		"集成测试", "代码评审", "不是**执行依据",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("body missing %q:\n%s", needle, body)
		}
	}
	if strings.Contains(body, "必须打开原 Run") || strings.Contains(body, "必须打开原工作流") {
		t.Fatalf("body must not require opening original run:\n%s", body)
	}

	var n int64
	if err := db.Model(&models.RequirementDraft{}).Where("project_id = ?", models.DefaultProjectID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("draft count=%d", n)
	}
}

func TestExecOutputLeftoverWithClarifiedSpec(t *testing.T) {
	// plan g1.1 / g2.3: clarified JSON expanded with goals/scope/AC; two clarify nodes both present.
	g := leftoverGraphWithClarify(true)
	eng, db, _ := setupEngineGraphP(t, g)
	longFeature := strings.Repeat("需求字", 50) // >120 runes when used alone
	reqA := `{
		"title":"规格A","summary":"概述A","background":"背景A",
		"goals":["目标A"],"in_scope":["范围内A"],"out_of_scope":["范围外A"],
		"functional_requirements":[{"title":"功能A","detail":"说明A","acceptance_criteria":["验收A1","验收A2"]}],
		"assumptions":["假设A"],"dependencies":["依赖A"],"constraints":["约束A"]
	}`
	reqB := `{
		"title":"规格B","summary":"概述B","background":"背景B",
		"goals":["目标B"],"in_scope":["范围内B"],"out_of_scope":["范围外B"],
		"functional_requirements":[{"title":"功能B","detail":"说明B","acceptance_criteria":["验收B"]}],
		"assumptions":["假设B"],"dependencies":["依赖B"],"constraints":["约束B"]
	}`
	outs := map[string]map[string]any{
		"react1": {
			"clarified_requirement_json": reqA,
			"clarified_requirement":      mcp.RenderClarifiedRequirementMarkdown(reqA),
		},
		"grasp1": {
			"clarified_requirement_json": reqB,
			"clarified_requirement":      mcp.RenderClarifiedRequirementMarkdown(reqB),
		},
		"test1": {
			"test_result_json": `{"summary":"s","defects":[{"title":"缺陷一","severity":"high","detail":"详"}]}`,
		},
		"plan1": {
			"plan_json": `{"title":"历史计划","goals":[{"title":"大目标一","subgoals":[{"title":"小目标一"}]}]}`,
		},
		"impl1": {
			"implementation_result_json": `{"summary":"已交付概述仅此一句","changed_areas":[{"title":"文件清单不应出现","detail":"diff"}]}`,
		},
	}
	c, node := seedLeftoverRun(t, eng, "run-leftover-clarify", g, outs)
	c.run.Inputs = map[string]any{"feature": longFeature}
	oc := eng.execOutput(c, node)
	id, _ := oc.outputs[leftoverDraftIDKey].(string)
	if id == "" {
		t.Fatalf("missing draft: %#v", oc.outputs)
	}
	svc := services.NewRequirementDraftService(db)
	draft, err := svc.Get(models.DefaultProjectID, id)
	if err != nil {
		t.Fatal(err)
	}
	body := draft.BodyMarkdown
	for _, needle := range []string{
		"需求规格", "react1", "grasp1", "规格A", "规格B", "目标A", "目标B",
		"范围内A", "范围外A", "功能A", "验收A1", "验收A2", "假设A", "依赖A", "约束A",
		"原计划要点", "历史计划", "大目标一", "小目标一", "仅为历史对照",
		"已交付说明", "已交付概述仅此一句",
		"仍须完成", "缺陷一", "来源注记",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("body missing %q:\n%s", needle, body)
		}
	}
	if strings.Contains(body, "文件清单不应出现") || strings.Contains(body, "diff") {
		t.Fatalf("implementation must only include summary:\n%s", body)
	}
	if strings.Contains(body, "原始需求输入") {
		t.Fatalf("should use clarified specs, not raw inputs")
	}
}

func TestExecOutputLeftoverRawInputNoClarify(t *testing.T) {
	// plan g1.1 / g2.3: no clarify → full input (>120 runes) preserved.
	g := leftoverGraph(true)
	eng, db, _ := setupEngineGraphP(t, g)
	long := strings.Repeat("长需求内容", 30) // 150 runes
	if utf8.RuneCountInString(long) <= 120 {
		t.Fatalf("fixture too short: %d", utf8.RuneCountInString(long))
	}
	outs := map[string]map[string]any{
		"test1": {"test_result_json": `{"summary":"s","defects":[{"title":"x"}]}`},
	}
	c, node := seedLeftoverRun(t, eng, "run-leftover-raw", g, outs)
	c.run.Inputs = map[string]any{"feature": long + "\n第二行保留"}
	oc := eng.execOutput(c, node)
	id, _ := oc.outputs[leftoverDraftIDKey].(string)
	draft, err := services.NewRequirementDraftService(db).Get(models.DefaultProjectID, id)
	if err != nil {
		t.Fatal(err)
	}
	body := draft.BodyMarkdown
	if !strings.Contains(body, "原始需求输入") || !strings.Contains(body, "未经结构化澄清") {
		t.Fatalf("want raw-input section:\n%s", body)
	}
	if !strings.Contains(body, long) || !strings.Contains(body, "第二行保留") {
		t.Fatalf("full input must be preserved:\n%s", body)
	}
}

func TestExecOutputLeftoverFailedCaseOnly(t *testing.T) {
	// plan g1.2 / g2.3: failed case alone creates draft.
	g := leftoverGraph(true)
	eng, db, _ := setupEngineGraphP(t, g)
	outs := map[string]map[string]any{
		"test1": {
			"test_result_json": `{"summary":"s","defects":[],"cases":[{"name":"仅失败用例","status":"failed","detail":"assert"}]}`,
		},
		"review1": {"review_json": `{"summary":"s","verdict":"approve","findings":[],"action_items":[]}`},
	}
	c, node := seedLeftoverRun(t, eng, "run-leftover-failcase", g, outs)
	oc := eng.execOutput(c, node)
	id, _ := oc.outputs[leftoverDraftIDKey].(string)
	if id == "" {
		t.Fatalf("failed case should create draft: %#v", oc.outputs)
	}
	draft, _ := services.NewRequirementDraftService(db).Get(models.DefaultProjectID, id)
	if !strings.Contains(draft.BodyMarkdown, "仅失败用例") {
		t.Fatalf("body missing case name:\n%s", draft.BodyMarkdown)
	}
}

func TestExecOutputLeftoverOpenQuestionOnly(t *testing.T) {
	// plan g1.2 / g2.3: open_questions alone create draft.
	g := leftoverGraphWithClarify(true)
	eng, db, _ := setupEngineGraphP(t, g)
	outs := map[string]map[string]any{
		"react1": {
			"clarified_requirement_json": `{
				"title":"T","summary":"概述","background":"b","goals":["g"],
				"in_scope":["in"],"out_of_scope":["out"],
				"functional_requirements":[{"title":"f","detail":"d","acceptance_criteria":["ac"]}],
				"assumptions":["a"],"dependencies":["d"],"constraints":["c"],
				"open_questions":["是否支持短信验证？"]
			}`,
		},
	}
	c, node := seedLeftoverRun(t, eng, "run-leftover-oq", g, outs)
	oc := eng.execOutput(c, node)
	id, _ := oc.outputs[leftoverDraftIDKey].(string)
	if id == "" {
		t.Fatalf("open question should create draft: %#v", oc.outputs)
	}
	draft, _ := services.NewRequirementDraftService(db).Get(models.DefaultProjectID, id)
	body := draft.BodyMarkdown
	if !strings.Contains(body, "是否支持短信验证？") || !strings.Contains(body, "需求规格") {
		t.Fatalf("body:\n%s", body)
	}
}

func TestExecOutputLeftoverSurvivesWorkflowDelete(t *testing.T) {
	// plan g2.3: after deleting workflow + runs, draft body unchanged and still executable.
	g := leftoverGraphWithClarify(true)
	eng, db, _ := setupEngineGraphP(t, g)
	outs := map[string]map[string]any{
		"react1": {
			"clarified_requirement_json": `{
				"title":"独立规格","summary":"可独立执行概述","background":"bg",
				"goals":["独立目标"],"in_scope":["范围内"],"out_of_scope":["范围外"],
				"functional_requirements":[{"title":"功能","detail":"说明","acceptance_criteria":["验收OK"]}],
				"assumptions":["假设"],"dependencies":["依赖"],"constraints":["约束"]
			}`,
		},
		"test1": {"test_result_json": `{"summary":"s","defects":[{"title":"遗留缺陷","severity":"medium","detail":"详"}]}`},
	}
	c, node := seedLeftoverRun(t, eng, "run-leftover-survive", g, outs)
	oc := eng.execOutput(c, node)
	id, _ := oc.outputs[leftoverDraftIDKey].(string)
	svc := services.NewRequirementDraftService(db)
	before, err := svc.Get(models.DefaultProjectID, id)
	if err != nil {
		t.Fatal(err)
	}
	beforeBody := before.BodyMarkdown

	if err := db.Where("run_id = ?", c.run.ID).Delete(&models.StateRun{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id = ?", c.run.ID).Delete(&models.Run{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id = ?", "wf").Delete(&models.WorkflowDef{}).Error; err != nil {
		t.Fatal(err)
	}

	after, err := svc.Get(models.DefaultProjectID, id)
	if err != nil {
		t.Fatalf("draft must survive workflow delete: %v", err)
	}
	if after.BodyMarkdown != beforeBody {
		t.Fatalf("body changed after delete")
	}
	for _, needle := range []string{"可独立执行概述", "独立目标", "范围内", "验收OK", "遗留缺陷", "仍须完成"} {
		if !strings.Contains(after.BodyMarkdown, needle) {
			t.Fatalf("missing %q", needle)
		}
	}
}

func TestBuildLeftoverDraftBodyTruncationPriority(t *testing.T) {
	// plan g1.3 / g2.3: over cap → drop source note first; keep overview + a leftover title + trunc note.
	hugeSummary := strings.Repeat("概", services.MaxRequirementDraftBodyRunes) // alone already over cap
	req := `{
		"title":"超长","summary":"` + hugeSummary + `","background":"bg",
		"goals":["目标"],"in_scope":["in"],"out_of_scope":["out"],
		"functional_requirements":[{"title":"f","detail":"d","acceptance_criteria":["ac"]}],
		"assumptions":["a"],"dependencies":["d"],"constraints":["c"]
	}`
	c := &execCtx{
		run: &models.Run{
			ID: "run-trunc", WorkflowID: "wf-trunc", WorkflowName: "截断工作流",
			WorkflowVersion: 1, Trigger: "manual",
			StartedAt: time.Date(2026, 9, 22, 1, 0, 0, 0, time.UTC),
			Inputs:    map[string]any{},
		},
		graph: leftoverGraphWithClarify(true),
		nodeOutputs: map[string]map[string]any{
			"react1": {"clarified_requirement_json": req},
			"plan1": {
				"plan_json": `{"title":"应先被去掉的计划","goals":[{"title":"计划目标X"}]}`,
			},
			"impl1": {
				"implementation_result_json": `{"summary":"应先被去掉的已交付"}`,
			},
		},
	}
	bundle := leftoverBundle{
		Items: []leftoverItem{{
			Kind: leftoverKindDefect, NodeID: "test1", NodeLabel: "集成测试",
			Title: "必须保留的遗留标题", Severity: "high", Detail: "d",
		}},
	}
	body := buildLeftoverDraftBody(c, bundle, nil)
	if n := utf8.RuneCountInString(body); n > services.MaxRequirementDraftBodyRunes {
		t.Fatalf("body runes=%d over cap", n)
	}
	if !strings.Contains(body, "正文已截断") {
		snippet := body
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		t.Fatalf("want trunc note:\n%s", snippet)
	}
	if !strings.Contains(body, "必须保留的遗留标题") {
		t.Fatalf("leftover title must remain; body runes=%d", utf8.RuneCountInString(body))
	}
	if strings.Contains(body, "## 来源注记") {
		t.Fatalf("source note should be dropped before truncating core")
	}
	if !strings.Contains(body, "需求规格") && !strings.Contains(body, "概述") {
		snippet := body
		if len(snippet) > 400 {
			snippet = snippet[:400]
		}
		t.Fatalf("want requirement overview remnant:\n%s", snippet)
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
	// skipped-only is not leftover and must not create a draft (plan g1.2 / g2.1).
	g := leftoverGraph(true)
	eng, db, _ := setupEngineGraphP(t, g)
	outs := map[string]map[string]any{
		"test1":   {"test_result_json": `{"summary":"s","defects":[],"cases":[{"name":"a","status":"skipped"}]}`},
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
	svc := services.NewRequirementDraftService(db)
	first, err := svc.Get(models.DefaultProjectID, id1)
	if err != nil {
		t.Fatal(err)
	}
	firstBody := first.BodyMarkdown

	if err := eng.db.Create(&models.StateRun{
		RunID: c.run.ID, NodeID: "output", Status: "completed", Iteration: 1, Outputs: oc1.outputs,
	}).Error; err != nil {
		t.Fatal(err)
	}
	// Mutate leftovers so a naive rewrite would change the body — dedup must keep first body.
	c.nodeOutputs["test1"] = map[string]any{
		"test_result_json": `{"summary":"s","defects":[{"title":"should-not-overwrite"}]}`,
	}
	oc2 := eng.execOutput(c, node)
	id2, _ := oc2.outputs[leftoverDraftIDKey].(string)
	if id2 != id1 {
		t.Fatalf("dedup want %s got %s", id1, id2)
	}
	second, err := svc.Get(models.DefaultProjectID, id1)
	if err != nil {
		t.Fatal(err)
	}
	if second.BodyMarkdown != firstBody {
		t.Fatalf("dedup must not overwrite body")
	}
	if strings.Contains(second.BodyMarkdown, "should-not-overwrite") {
		t.Fatalf("body was overwritten")
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

// leftoverArtifactFixtures are minimal but renderable structured products for
// plan g1 / g2 coverage (acceptance criteria, root_cause text, plan detail).
const (
	leftoverFixtureClarified = `{
		"title":"产物规格","summary":"产物库规格概述","background":"产物背景",
		"goals":["产物目标"],"in_scope":["范围内"],"out_of_scope":["范围外"],
		"functional_requirements":[{"title":"产物功能","detail":"功能说明","acceptance_criteria":["验收标准甲","验收标准乙"]}],
		"assumptions":["假设"],"dependencies":["依赖"],"constraints":["约束"]
	}`
	leftoverFixtureRootCause = `{
		"title":"根因标题","summary":"根因概述句",
		"symptom":"现象描述","expected":"期望行为","actual":"实际行为",
		"reproduction":["步骤一"],"impact":"影响谁",
		"root_cause":"因为组稿不读产物库所以缺全文",
		"evidence":[{"title":"证据一","detail":"store 未被调用"}],
		"diagrams":[{"kind":"flowchart","title":"因果","source":"flowchart TD\n  A-->B"}]
	}`
	leftoverFixturePlan = `{
		"title":"历史计划全文","architecture":{"summary":"仍在结束节点组装"},
		"data_design":{"summary":"不涉及"},
		"test_design":"补组稿回归",
		"goals":[{"id":"g1","title":"大目标全文","detail":"目标说明应出现在草稿","subgoals":[{"id":"g1.1","title":"小目标","detail":"小目标说明"}]}]
	}`
	leftoverFixtureResearch = `{
		"summary":"调研概述应写入","title":"调研标题",
		"findings":[{"title":"发现一","detail":"详情"}],
		"questions":[{"question":"Q1","answer":"A1"}]
	}`
)

func TestExecOutputLeftoverFromArtifactStoreFullText(t *testing.T) {
	// plan g1.1 / g1.2 / g1.3 / g2.1: three primary files in store, empty node
	// outputs + empty inputs → 产物全文 with AC / root_cause / plan detail, ordered.
	g := leftoverGraph(true)
	eng, db, _ := setupEngineGraphP(t, g)
	outs := map[string]map[string]any{
		"test1": {"test_result_json": `{"summary":"s","defects":[{"title":"遗留缺陷甲","severity":"high","detail":"详"}]}`},
	}
	c, node := seedLeftoverRun(t, eng, "run-leftover-arts", g, outs)
	c.run.Inputs = map[string]any{}
	c.nodeOutputs = outs // no clarified/plan snapshots

	for _, a := range []struct {
		node, name, kind, body string
	}{
		{"react1", mcp.ClarifiedRequirementArtifactName, "json", leftoverFixtureClarified},
		{"grasp1", mcp.RootCauseArtifactName, "json", leftoverFixtureRootCause},
		{"plan1", mcp.PlanArtifactName, "json", leftoverFixturePlan},
		// ledger + image must not appear
		{"output", mcp.NodeOutcomeArtifactName, "json", `{"status":"success"}`},
		{"gate", mcp.FeedbackIndexArtifactName, "json", `{"rounds":[]}`},
		{"shot", "shot.png", "image", "not-a-real-png-binary"},
	} {
		if _, err := eng.store.Save(c.run.ID, a.node, a.name, a.kind, a.body); err != nil {
			t.Fatalf("save %s: %v", a.name, err)
		}
	}

	oc := eng.execOutput(c, node)
	id, _ := oc.outputs[leftoverDraftIDKey].(string)
	if id == "" {
		t.Fatalf("missing draft: %#v", oc.outputs)
	}
	draft, err := services.NewRequirementDraftService(db).Get(models.DefaultProjectID, id)
	if err != nil {
		t.Fatal(err)
	}
	body := draft.BodyMarkdown

	for _, needle := range []string{
		"## 背景", "## 产物全文", "## 仍须完成", "## 来源注记",
		"### `clarified_requirement.json`",
		"### `root_cause.json`",
		"### `plan.json`",
		"验收标准甲", "验收标准乙", "产物功能",
		"因为组稿不读产物库所以缺全文", "根因概述句",
		"目标说明应出现在草稿", "仍在结束节点组装", "历史计划全文",
		"以下为计划全文，仅为历史对照，不是本次验收依据",
		"遗留缺陷甲",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("body missing %q:\n%s", needle, body)
		}
	}
	for _, banned := range []string{
		"本次运行未留下结构化需求或原始输入",
		"## 原始需求输入",
		"## 需求规格",
		"## 原计划要点",
		"node_complete.json",
		"feedback_index.json",
		"shot.png",
		"not-a-real-png-binary",
	} {
		if strings.Contains(body, banned) {
			t.Fatalf("body must not contain %q:\n%s", banned, body)
		}
	}

	// Section order: background → artifacts (clarified, root_cause, plan) → leftovers → source
	pos := func(s string) int {
		i := strings.Index(body, s)
		if i < 0 {
			t.Fatalf("missing %q for order check", s)
		}
		return i
	}
	if !(pos("## 背景") < pos("## 产物全文") &&
		pos("### `clarified_requirement.json`") < pos("### `root_cause.json`") &&
		pos("### `root_cause.json`") < pos("### `plan.json`") &&
		pos("### `plan.json`") < pos("## 仍须完成") &&
		pos("## 仍须完成") < pos("## 来源注记")) {
		t.Fatalf("section order wrong:\n%s", body)
	}
}

func TestExecOutputLeftoverExtraArtifactsAndMissingSkipped(t *testing.T) {
	// plan g1.3 / g2.2: research present → section written; missing root_cause skipped;
	// image + ledgers excluded.
	g := leftoverGraph(true)
	eng, db, _ := setupEngineGraphP(t, g)
	outs := map[string]map[string]any{
		"test1": {"test_result_json": `{"summary":"s","defects":[{"title":"缺陷乙"}]}`},
	}
	c, node := seedLeftoverRun(t, eng, "run-leftover-extra", g, outs)
	c.run.Inputs = map[string]any{}

	for _, a := range []struct {
		node, name, kind, body string
	}{
		{"react1", mcp.ClarifiedRequirementArtifactName, "json", leftoverFixtureClarified},
		{"plan1", mcp.PlanArtifactName, "json", leftoverFixturePlan},
		{"research1", mcp.ResearchArtifactName, "json", leftoverFixtureResearch},
		{"output", mcp.NodeOutcomeArtifactName, "json", `{"status":"success"}`},
		{"gate", "feedback.clarify.approve.i1.json", "json", `{"summary":"feedback round"}`},
		{"shot", "ui.webp", "image", "WEBPBINARY"},
		{"notes", "notes.md", "markdown", "# 自由笔记\n\n自由正文应保留\n"},
	} {
		if _, err := eng.store.Save(c.run.ID, a.node, a.name, a.kind, a.body); err != nil {
			t.Fatalf("save %s: %v", a.name, err)
		}
	}

	oc := eng.execOutput(c, node)
	id, _ := oc.outputs[leftoverDraftIDKey].(string)
	draft, err := services.NewRequirementDraftService(db).Get(models.DefaultProjectID, id)
	if err != nil {
		t.Fatal(err)
	}
	body := draft.BodyMarkdown

	for _, needle := range []string{
		"### `clarified_requirement.json`",
		"### `plan.json`",
		"### `research.json`",
		"调研概述应写入",
		"### `notes.md`",
		"自由正文应保留",
		"验收标准甲",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("body missing %q:\n%s", needle, body)
		}
	}
	for _, banned := range []string{
		"### `root_cause.json`",
		"node_complete.json",
		"feedback.clarify",
		"ui.webp",
		"WEBPBINARY",
	} {
		if strings.Contains(body, banned) {
			t.Fatalf("body must not contain %q:\n%s", banned, body)
		}
	}

	pos := func(s string) int { return strings.Index(body, s) }
	if !(pos("### `clarified_requirement.json`") < pos("### `plan.json`") &&
		pos("### `plan.json`") < pos("### `research.json`") &&
		pos("### `research.json`") < pos("### `notes.md`")) {
		t.Fatalf("artifact order wrong:\n%s", body)
	}
}

func TestExecOutputLeftoverNoArtifactsKeepsSnapshotFallback(t *testing.T) {
	// plan g2.3: no text artifacts → no 「产物全文」; snapshot specs / placeholder / long input.
	t.Run("snapshot_spec", func(t *testing.T) {
		g := leftoverGraphWithClarify(true)
		eng, db, _ := setupEngineGraphP(t, g)
		req := `{
			"title":"快照规格","summary":"快照概述","background":"bg",
			"goals":["快照目标"],"in_scope":["in"],"out_of_scope":["out"],
			"functional_requirements":[{"title":"f","detail":"d","acceptance_criteria":["快照验收"]}],
			"assumptions":["a"],"dependencies":["d"],"constraints":["c"]
		}`
		outs := map[string]map[string]any{
			"react1": {"clarified_requirement_json": req},
			"test1":  {"test_result_json": `{"summary":"s","defects":[{"title":"缺陷丙"}]}`},
		}
		c, node := seedLeftoverRun(t, eng, "run-leftover-snap", g, outs)
		oc := eng.execOutput(c, node)
		id, _ := oc.outputs[leftoverDraftIDKey].(string)
		draft, _ := services.NewRequirementDraftService(db).Get(models.DefaultProjectID, id)
		body := draft.BodyMarkdown
		if strings.Contains(body, "## 产物全文") {
			t.Fatalf("no artifacts should not write 产物全文:\n%s", body)
		}
		if !strings.Contains(body, "## 需求规格") || !strings.Contains(body, "快照验收") {
			t.Fatalf("want snapshot spec:\n%s", body)
		}
	})

	t.Run("empty_placeholder", func(t *testing.T) {
		g := leftoverGraph(true)
		eng, db, _ := setupEngineGraphP(t, g)
		outs := map[string]map[string]any{
			"test1": {"test_result_json": `{"summary":"s","defects":[{"title":"缺陷丁"}]}`},
		}
		c, node := seedLeftoverRun(t, eng, "run-leftover-ph", g, outs)
		c.run.Inputs = map[string]any{}
		oc := eng.execOutput(c, node)
		id, _ := oc.outputs[leftoverDraftIDKey].(string)
		draft, _ := services.NewRequirementDraftService(db).Get(models.DefaultProjectID, id)
		body := draft.BodyMarkdown
		if strings.Contains(body, "## 产物全文") {
			t.Fatal("no 产物全文")
		}
		if !strings.Contains(body, "本次运行未留下结构化需求或原始输入") {
			t.Fatalf("want placeholder:\n%s", body)
		}
	})

	t.Run("long_input_preserved", func(t *testing.T) {
		g := leftoverGraph(true)
		eng, db, _ := setupEngineGraphP(t, g)
		long := strings.Repeat("启动输入全文", 25) // >120 runes
		if utf8.RuneCountInString(long) <= 120 {
			t.Fatalf("fixture too short: %d", utf8.RuneCountInString(long))
		}
		outs := map[string]map[string]any{
			"test1": {"test_result_json": `{"summary":"s","defects":[{"title":"缺陷戊"}]}`},
		}
		c, node := seedLeftoverRun(t, eng, "run-leftover-longin", g, outs)
		c.run.Inputs = map[string]any{"feature": long}
		oc := eng.execOutput(c, node)
		id, _ := oc.outputs[leftoverDraftIDKey].(string)
		draft, _ := services.NewRequirementDraftService(db).Get(models.DefaultProjectID, id)
		body := draft.BodyMarkdown
		if strings.Contains(body, "## 产物全文") {
			t.Fatal("no 产物全文")
		}
		if !strings.Contains(body, long) {
			t.Fatalf("long input must remain:\n%s", body)
		}
	})
}

func TestBuildLeftoverDraftBodyArtifactTruncation(t *testing.T) {
	// plan g1 / F5: over cap with artifacts → drop source note / later sections; keep leftover title.
	huge := strings.Repeat("概", services.MaxRequirementDraftBodyRunes) // alone already over cap
	clarified := `{
		"title":"超长规格","summary":"` + huge + `","background":"bg",
		"goals":["目标"],"in_scope":["in"],"out_of_scope":["out"],
		"functional_requirements":[{"title":"f","detail":"d","acceptance_criteria":["ac"]}],
		"assumptions":["a"],"dependencies":["d"],"constraints":["c"]
	}`
	g := leftoverGraph(true)
	eng, _, _ := setupEngineGraphP(t, g)
	runID := "run-leftover-art-trunc"
	c := &execCtx{
		run: &models.Run{
			ID: runID, WorkflowID: "wf", WorkflowName: "截断",
			WorkflowVersion: 1, Trigger: "manual",
			StartedAt: time.Date(2026, 9, 22, 1, 0, 0, 0, time.UTC),
			Inputs:    map[string]any{},
		},
		graph:       g,
		nodeOutputs: map[string]map[string]any{},
	}
	if _, err := eng.store.Save(runID, "r", mcp.ClarifiedRequirementArtifactName, "json", clarified); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.store.Save(runID, "p", mcp.PlanArtifactName, "json", leftoverFixturePlan); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.store.Save(runID, "x", mcp.ResearchArtifactName, "json", leftoverFixtureResearch); err != nil {
		t.Fatal(err)
	}
	bundle := leftoverBundle{
		Items: []leftoverItem{{
			Kind: leftoverKindDefect, NodeID: "test1", NodeLabel: "集成测试",
			Title: "必须保留的产物遗留标题", Severity: "high",
		}},
	}
	body := buildLeftoverDraftBody(c, bundle, eng.store)
	if n := utf8.RuneCountInString(body); n > services.MaxRequirementDraftBodyRunes {
		t.Fatalf("body runes=%d over cap", n)
	}
	if !strings.Contains(body, "正文已截断") {
		t.Fatalf("want trunc note")
	}
	if !strings.Contains(body, "必须保留的产物遗留标题") {
		t.Fatalf("leftover title must remain")
	}
	if strings.Contains(body, "## 来源注记") {
		t.Fatalf("source note should drop first")
	}
	if !strings.Contains(body, "## 产物全文") && !strings.Contains(body, "概述") {
		t.Fatalf("want clarified remnant in body")
	}
}
