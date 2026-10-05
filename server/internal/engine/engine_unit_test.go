package engine

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestCoerceVar(t *testing.T) {
	if coerceVar("3.5", "number") != 3.5 {
		t.Error("number string")
	}
	if coerceVar(2, "number") != float64(2) {
		t.Error("number int")
	}
	if coerceVar(float64(4), "number") != float64(4) {
		t.Error("number float")
	}
	if coerceVar("abc", "number") != "abc" {
		t.Error("number bad string keeps raw")
	}
	if coerceVar("TRUE", "bool") != true {
		t.Error("bool string")
	}
	if coerceVar(true, "bool") != true {
		t.Error("bool bool")
	}
	if coerceVar("hi", "string") != "hi" {
		t.Error("string passthrough")
	}
}

func TestInferType(t *testing.T) {
	if inferType(true) != "bool" {
		t.Error("bool")
	}
	if inferType(3) != "int" || inferType(int64(3)) != "int" || inferType(1.5) != "int" {
		t.Error("numeric")
	}
	if inferType("x") != "string" {
		t.Error("string")
	}
}

func TestIsBlank(t *testing.T) {
	if !isBlank(nil) || !isBlank("  ") {
		t.Error("blank")
	}
	if isBlank("x") || isBlank(3) {
		t.Error("non-blank")
	}
	if isBlank(map[string]any{
		"text":   "",
		"images": []any{map[string]any{"data": "x", "mimeType": "image/png"}},
	}) {
		t.Error("images-only composite should not be blank")
	}
}

func TestResolveStartVars(t *testing.T) {
	g := models.Graph{Variables: []models.Variable{
		{Name: "", Type: "string"}, // skipped
		{Name: "idea", Type: "string", Ask: true, Required: true},
		{Name: "count", Type: "number", Value: "5"},
		{Name: "flag", Type: "bool", Ask: true},
	}}
	// Missing required -> error.
	if _, err := resolveStartVars(g, map[string]any{}, nil); err == nil {
		t.Fatal("missing required should error")
	}
	out, err := resolveStartVars(g, map[string]any{"idea": "build", "flag": "true"}, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	got := map[string]any{}
	for _, v := range out {
		got[v.Name] = v.Value
	}
	if got["idea"] != "build" || got["count"] != float64(5) || got["flag"] != true {
		t.Fatalf("resolved: %+v", got)
	}

	// Required with a Desc label (covers the label branch).
	g2 := models.Graph{Variables: []models.Variable{{Name: "x", Desc: "需求", Ask: true, Required: true}}}
	if _, err := resolveStartVars(g2, map[string]any{"x": "   "}, nil); err == nil {
		t.Fatal("blank required should error")
	}
}

func TestComputeRunTitle(t *testing.T) {
	g := models.Graph{Variables: []models.Variable{
		{Name: "hidden", Type: "string", Ask: false, Value: "skip"},
		{Name: "first", Type: "string", Ask: true, Required: true},
		{Name: "second", Type: "string", Ask: true, Value: "other"},
	}}
	seeded := []models.RunVariable{
		{Name: "first", Type: "string", Value: "alpha"},
		{Name: "second", Type: "string", Value: "beta"},
	}
	if got := computeRunTitle(g, seeded); got != "alpha" {
		t.Fatalf("first ask var: got %q", got)
	}

	if got := computeRunTitle(g, []models.RunVariable{{Name: "first", Type: "string", Value: ""}}); got != "" {
		t.Fatalf("blank first ask: got %q", got)
	}

	if got := computeRunTitle(models.Graph{Variables: []models.Variable{{Name: "n", Type: "number", Ask: true}}}, []models.RunVariable{{Name: "n", Type: "number", Value: float64(0)}}); got != "0" {
		t.Fatalf("number zero: got %q", got)
	}
	if got := computeRunTitle(models.Graph{Variables: []models.Variable{{Name: "b", Type: "bool", Ask: true}}}, []models.RunVariable{{Name: "b", Type: "bool", Value: false}}); got != "false" {
		t.Fatalf("bool false: got %q", got)
	}
	if got := varValueToTitleString(true, "bool"); got != "true" {
		t.Fatalf("bool stringify: got %q", got)
	}
	if got := varValueToTitleString(float64(3.5), "number"); got != "3.5" {
		t.Fatalf("number stringify: got %q", got)
	}
	comp := map[string]any{
		"text":   "看看怎么支持登录",
		"images": []any{map[string]any{"data": "x", "mimeType": "image/png"}},
	}
	if got := varValueToTitleString(comp, "paragraph"); got != "看看怎么支持登录 · 1图" {
		t.Fatalf("composite title: got %q", got)
	}
	if got := varValueToTitleString(map[string]any{
		"text":   "",
		"images": []any{map[string]any{"data": "x", "mimeType": "image/png"}},
	}, "paragraph"); got != "1张图" {
		t.Fatalf("images-only title: got %q", got)
	}
	if got := varValueToTitleString(map[string]any{"foo": "bar"}, "string"); got != "" {
		t.Fatalf("non-composite map should not become a title: got %q", got)
	}
	reposJSON := `[{"branch":"","name":"approving","url":"https://git.example/approving.git"}]`
	if got := varValueToTitleString(reposJSON, "repos"); got != "approving" {
		t.Fatalf("repos json title: got %q", got)
	}
	if got := computeRunTitle(models.Graph{Variables: []models.Variable{{Name: "repos", Type: "repos", Ask: true}}}, []models.RunVariable{{Name: "repos", Type: "repos", Value: reposJSON}}); got != "approving" {
		t.Fatalf("first ask repos: got %q", got)
	}

	gDefault := models.Graph{Variables: []models.Variable{{Name: "env", Type: "select", Ask: true, Value: "prod"}}}
	out, err := resolveStartVars(gDefault, map[string]any{}, nil)
	if err != nil {
		t.Fatalf("resolve default: %v", err)
	}
	if got := computeRunTitle(gDefault, out); got != "prod" {
		t.Fatalf("default value title: got %q", got)
	}

	if got := computeRunTitle(models.Graph{}, nil); got != "" {
		t.Fatalf("no ask vars: got %q", got)
	}
}

func TestApplyRunTitleOverride(t *testing.T) {
	if got := applyRunTitleOverride("alpha", ""); got != "alpha" {
		t.Fatalf("empty override: got %q", got)
	}
	if got := applyRunTitleOverride("alpha", "   "); got != "alpha" {
		t.Fatalf("blank override: got %q", got)
	}
	if got := applyRunTitleOverride("alpha", "  用户第一句话  "); got != "用户第一句话" {
		t.Fatalf("override: got %q", got)
	}
	long := strings.Repeat("啊", 90)
	got := applyRunTitleOverride("alpha", long)
	if n := len([]rune(got)); n != 80 {
		t.Fatalf("rune cap: got %d runes %q", n, got)
	}
}

func TestStartRunTitleOverrideBeatsReposAsk(t *testing.T) {
	reposJSON := `[{"name":"approving","url":"https://git.example/approving.git"}]`
	g := models.Graph{
		Variables: []models.Variable{{Name: "repos", Type: "repos", Ask: true, Value: reposJSON}},
		Nodes:     []models.Node{{ID: "in", Type: "input"}, {ID: "out", Type: "output"}},
		Edges:     []models.Edge{{ID: "e1", Source: "in", Target: "out"}},
	}
	eng, db := setupEngineGraph(t, g)

	run, err := eng.StartRunWithTitle("wf", nil, "test", "normal", nil, nil, "  从一句话开始  ")
	if err != nil {
		t.Fatal(err)
	}
	var got models.Run
	if err := db.First(&got, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Title != "从一句话开始" {
		t.Fatalf("override title=%q", got.Title)
	}

	fallback, err := eng.StartRunWithPriority("wf", nil, "test", "normal", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fallback.Title != "approving" {
		t.Fatalf("fallback title=%q want approving", fallback.Title)
	}
}

// TestFrameworkNodesPipeline drives research → test → review agents end to
// end, exercising each schema's contract enforcement + renderer. Reserved JSON is
// persisted; fixed-name markdown companions are not. Node outputs still carry
// rendered Markdown (outKey) and raw JSON (outKey+"_json").
func TestFrameworkNodesPipeline(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "research", Type: "agent", Caps: capsResearchAuto, Config: map[string]any{"agent_profile": "r", "prompt": "调研"}},
			{ID: "test", Type: "agent", Caps: capsTest, Config: map[string]any{"agent_profile": "t", "prompt": "测试"}},
			{ID: "review", Type: "agent", Caps: capsCodeReview, Config: map[string]any{"agent_profile": "v", "prompt": "评审"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "research"},
			{ID: "e2", Source: "research", Target: "test"},
			{ID: "e3", Source: "test", Target: "review", SourceHandle: handlePass},
			{ID: "e4", Source: "review", Target: "output", SourceHandle: handlePass},
		},
	}
	eng, db := setupEngineGraph(t, g)
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	// Reserved JSON exists; fixed-name markdown companions must not be written.
	for _, name := range []string{"research.json", "test_result.json", "review.json"} {
		var c int64
		db.Model(&models.Artifact{}).Where("run_id = ? AND name = ?", run.ID, name).Count(&c)
		if c == 0 {
			t.Errorf("expected artifact %q", name)
		}
	}
	for _, name := range []string{"research.md", "test_result.md", "review.md"} {
		var c int64
		db.Model(&models.Artifact{}).Where("run_id = ? AND name = ?", run.ID, name).Count(&c)
		if c != 0 {
			t.Errorf("unexpected companion artifact %q", name)
		}
	}

	// f2: outputs still expose rendered Markdown + *_json (not only reserved artifacts).
	for _, tc := range []struct {
		nodeID, outKey string
	}{
		{"research", "research"},
		{"test", "test_result"},
		{"review", "review"},
	} {
		var sr models.StateRun
		if err := db.Where("run_id = ? AND node_id = ?", run.ID, tc.nodeID).
			Order("iteration desc").First(&sr).Error; err != nil {
			t.Fatalf("load %s state run: %v", tc.nodeID, err)
		}
		md, _ := sr.Outputs[tc.outKey].(string)
		if strings.TrimSpace(md) == "" {
			t.Errorf("%s outputs[%q] rendered markdown empty", tc.nodeID, tc.outKey)
		}
		raw, _ := sr.Outputs[tc.outKey+"_json"].(string)
		if strings.TrimSpace(raw) == "" {
			t.Errorf("%s outputs[%q] empty", tc.nodeID, tc.outKey+"_json")
		}
	}
}

func TestEvalContextArtifactClosure(t *testing.T) {
	e, db := setupEngine(t)
	_ = db
	c := &execCtx{
		run:         &models.Run{ID: "rX"},
		vars:        map[string]any{"a": 1},
		nodeOutputs: map[string]map[string]any{"n": {"k": "v"}},
	}
	ec := e.evalContext(c, map[string]any{"extra": true})
	if ec.Vars["a"] != 1 || ec.Extra["extra"] != true {
		t.Fatal("eval context fields")
	}
	// Artifact closure delegates to the store (nothing stored -> not ok).
	if _, ok := ec.Artifact("missing"); ok {
		t.Fatal("artifact should be absent")
	}
}
