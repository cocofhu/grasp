package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func sampleGraph() Graph {
	return Graph{
		Nodes: []Node{
			{ID: "in", Type: "input"},
			{ID: "a", Type: "agent"},
			{ID: "out", Type: "output"},
		},
		Edges: []Edge{
			{ID: "e1", Source: "in", Target: "a"},
			{ID: "e2", Source: "a", Target: "out"},
		},
	}
}

func TestGraphAccessors(t *testing.T) {
	g := sampleGraph()
	if n := g.FindNode("a"); n == nil || n.Type != "agent" {
		t.Fatal("FindNode")
	}
	if g.FindNode("ghost") != nil {
		t.Fatal("FindNode missing")
	}
	if out := g.OutEdges("in"); len(out) != 1 || out[0].Target != "a" {
		t.Fatalf("OutEdges: %+v", out)
	}
	if n := g.StartNode(); n == nil || n.ID != "in" {
		t.Fatalf("StartNode: %+v", n)
	}
}

func TestStartNodeFallbacks(t *testing.T) {
	// No input node -> first node with no incoming edge.
	g := Graph{
		Nodes: []Node{{ID: "x", Type: "agent"}, {ID: "y", Type: "agent"}},
		Edges: []Edge{{Source: "x", Target: "y"}},
	}
	if n := g.StartNode(); n == nil || n.ID != "x" {
		t.Fatalf("no-input start: %+v", n)
	}
	// All nodes have incoming edges (cycle) -> falls back to first node.
	g2 := Graph{
		Nodes: []Node{{ID: "x"}, {ID: "y"}},
		Edges: []Edge{{Source: "x", Target: "y"}, {Source: "y", Target: "x"}},
	}
	if n := g2.StartNode(); n == nil || n.ID != "x" {
		t.Fatalf("cycle start: %+v", n)
	}
	// Empty graph -> nil.
	if (Graph{}).StartNode() != nil {
		t.Fatal("empty start should be nil")
	}
}

func TestGraphValidate(t *testing.T) {
	if err := sampleGraph().Validate(); err != nil {
		t.Fatalf("valid graph: %v", err)
	}
	cases := []Graph{
		{}, // empty
		{Nodes: []Node{{ID: "o", Type: "output"}}},                                                                      // no input
		{Nodes: []Node{{ID: "i", Type: "input"}}},                                                                       // no output
		{Nodes: []Node{{ID: "i", Type: "input"}, {ID: "i2", Type: "input"}, {ID: "o", Type: "output"}}},                 // two inputs
		{Nodes: []Node{{ID: "i", Type: "input"}, {ID: "o", Type: "output"}}, Edges: []Edge{{Source: "o", Target: "i"}}}, // input has incoming
		{Nodes: []Node{{ID: "i", Type: "input"}, {ID: "o", Type: "output"}}, Edges: []Edge{{Source: "o", Target: "x"}}}, // output has outgoing
	}
	for i, g := range cases {
		if err := g.Validate(); err == nil {
			t.Errorf("case %d: expected validate error", i)
		}
	}
}

func TestGraphValidateNodeLimits(t *testing.T) {
	withCfg := func(cfg map[string]any) Graph {
		g := sampleGraph()
		g.Nodes = append(g.Nodes, Node{ID: "a", Type: "agent", Label: "测试", Config: cfg})
		return g
	}
	for _, cfg := range []map[string]any{
		nil,
		{"timeout": float64(30)},
		{"timeout": 0},
		{"nudgeRetries": float64(0)},
		{"nudgeRetries": 10},
	} {
		if err := withCfg(cfg).Validate(); err != nil {
			t.Errorf("%v should be valid: %v", cfg, err)
		}
	}
	for _, cfg := range []map[string]any{
		{"timeout": float64(-1)},
		{"timeout": "30"},
		{"nudgeRetries": float64(11)},
		{"nudgeRetries": float64(-1)},
		{"nudgeRetries": 1.5},
		{"nudgeRetries": "3"},
	} {
		err := withCfg(cfg).Validate()
		if err == nil || !strings.Contains(err.Error(), "节点 测试") {
			t.Errorf("%v should be rejected naming the node, got %v", cfg, err)
		}
	}
}

func TestValidateSuccessFanout(t *testing.T) {
	base := func(edges []Edge) Graph {
		return Graph{
			Nodes: []Node{
				{ID: "in", Type: "input"},
				{ID: "a", Type: "agent"},
				{ID: "b", Type: "agent"},
				{ID: "c", Type: "agent"},
				{ID: "br", Type: "branch"},
				{ID: "out", Type: "output"},
			},
			Edges: edges,
		}
	}
	// Two unconditional success edges from one node -> ambiguous fan-out error.
	amb := base([]Edge{
		{Source: "in", Target: "a"},
		{Source: "a", Target: "b"},
		{Source: "a", Target: "c"},
		{Source: "b", Target: "out"},
		{Source: "c", Target: "out"},
	})
	if err := amb.Validate(); err == nil {
		t.Fatal("expected ambiguous success fan-out error")
	}
	// Guarded success edges (conditional routing) are allowed.
	guarded := base([]Edge{
		{Source: "in", Target: "a"},
		{Source: "a", Target: "b", When: "vars.x == 1"},
		{Source: "a", Target: "c", When: "vars.x == 2"},
		{Source: "b", Target: "out"},
		{Source: "c", Target: "out"},
	})
	if err := guarded.Validate(); err != nil {
		t.Fatalf("guarded fan-out should pass: %v", err)
	}
	// One unconditional + one guarded (else + conditional) is allowed.
	mixed := base([]Edge{
		{Source: "in", Target: "a"},
		{Source: "a", Target: "b", When: "vars.x == 1"},
		{Source: "a", Target: "c"},
		{Source: "b", Target: "out"},
		{Source: "c", Target: "out"},
	})
	if err := mixed.Validate(); err != nil {
		t.Fatalf("mixed guarded+else fan-out should pass: %v", err)
	}
	// A success + a failure edge to two targets is allowed (gate pattern).
	passFail := base([]Edge{
		{Source: "in", Target: "a"},
		{Source: "a", Target: "b"},
		{Source: "a", Target: "c", Kind: EdgeFailure},
		{Source: "b", Target: "out"},
		{Source: "c", Target: "out"},
	})
	if err := passFail.Validate(); err != nil {
		t.Fatalf("success+failure fan-out should pass: %v", err)
	}
	// Distinct outlets (branch cases, gate pass/fail) may each carry an edge.
	branch := base([]Edge{
		{Source: "in", Target: "br"},
		{Source: "br", SourceHandle: "c1", Target: "b"},
		{Source: "br", SourceHandle: "else", Target: "c"},
		{Source: "b", Target: "out"},
		{Source: "c", Target: "out"},
	})
	if err := branch.Validate(); err != nil {
		t.Fatalf("per-outlet fan-out should pass: %v", err)
	}
	sameHandle := base([]Edge{
		{Source: "in", Target: "br"},
		{Source: "br", SourceHandle: "c1", Target: "b"},
		{Source: "br", SourceHandle: "c1", Target: "c"},
		{Source: "b", Target: "out"},
		{Source: "c", Target: "out"},
	})
	if err := sameHandle.Validate(); err == nil {
		t.Fatal("two guardless edges on one outlet must fail")
	}
}

func TestEdgeKindOrDefault(t *testing.T) {
	if (Edge{}).KindOrDefault() != EdgeSuccess {
		t.Error("empty kind default")
	}
	if (Edge{Kind: EdgeFailure}).KindOrDefault() != EdgeFailure {
		t.Error("explicit kind")
	}
}

func TestReactOptionDemoHtmlJSON(t *testing.T) {
	opt := ReactOption{ID: "a", Label: "A", DemoHtml: "<!doctype html><html></html>"}
	b, err := json.Marshal(opt)
	if err != nil {
		t.Fatal(err)
	}
	var back ReactOption
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.DemoHtml != opt.DemoHtml {
		t.Fatalf("DemoHtml roundtrip = %q want %q", back.DemoHtml, opt.DemoHtml)
	}
}

func TestIsChoiceReply(t *testing.T) {
	if !IsChoiceReply("我的选择:\n- q → a") {
		t.Fatal("zh prefix")
	}
	if !IsChoiceReply("My choices:\n- q → a") {
		t.Fatal("en prefix")
	}
	if IsChoiceReply("第一条自由文本") {
		t.Fatal("free text must not match")
	}
	if IsChoiceReply(" 我的选择:\n- q → a") {
		t.Fatal("leading space is not a choice prefix")
	}
	if IsChoiceReply("回答已跳过\n用户回复\n先按现有集群") {
		t.Fatal("skip envelope must not be treated as choice")
	}
}

func TestRenderAnnotations(t *testing.T) {
	if RenderAnnotations(nil) != "" {
		t.Fatal("nil annotations should render empty")
	}
	got := RenderAnnotations([]ReactAnnotation{
		{JSONPath: "findings[r1].title", Label: "发现 A", Note: "更具体"},
		{Selector: "#hero h1", URL: "http://127.0.0.1:5173/settings?tab=1", Note: ""},
		{JSONPath: "summary", Quote: "划选的一段原文", Label: "概述", Note: "改这句"},
		{Quote: "未绑定摘录", Truncated: true, Note: ""},
	})
	for _, part := range []string{
		"字段路径", "findings[r1].title", "(发现 A)", "更具体",
		"页面元素", "#hero h1", "(见下方文字说明)",
		"页面 URL: http://127.0.0.1:5173/settings?tab=1",
		"引用原文", "划选的一段原文", "未绑定摘录", "已截断", "段落摘录",
	} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing %q in:\n%s", part, got)
		}
	}
}

func TestRenderAnnotationsPickContext(t *testing.T) {
	got := RenderAnnotations([]ReactAnnotation{
		{
			Selector:  "main > section:nth-of-type(2) > h2",
			URL:       "http://10.0.0.5:5173/pricing",
			TagName:   "h2",
			Text:      "  Choose\n your   plan ",
			OuterHTML: "<h2 class=\"title\">\n  Choose your plan\n</h2>",
		},
		{Selector: "#only-tag", TagName: "button"},
	})
	for _, part := range []string{
		"页面 URL: http://10.0.0.5:5173/pricing",
		"标签: h2 · 可见文本: 「Choose your plan」",
		"HTML: <h2 class=\"title\"> Choose your plan </h2>",
		"`#only-tag`",
		"标签: button\n",
	} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing %q in:\n%s", part, got)
		}
	}
	if strings.Contains(got, "可见文本: 「」") || strings.Count(got, "HTML:") != 1 {
		t.Fatalf("empty pick fields should be omitted:\n%s", got)
	}
}

func TestRenderAnnotationsClipsPickContext(t *testing.T) {
	longText := strings.Repeat("字", annotationTextMaxRunes+50)
	longHTML := "<div>" + strings.Repeat("x", annotationHTMLMaxRunes*2) + "</div>"
	got := RenderAnnotations([]ReactAnnotation{{Selector: "#big", Text: longText, OuterHTML: longHTML}})
	if !strings.Contains(got, "「"+strings.Repeat("字", annotationTextMaxRunes)+"…」") {
		t.Fatalf("text not clipped to %d runes:\n%s", annotationTextMaxRunes, got)
	}
	if strings.Contains(got, strings.Repeat("字", annotationTextMaxRunes+1)) {
		t.Fatal("text longer than the clamp leaked into the prompt")
	}
	htmlLine := got[strings.Index(got, "HTML: ")+len("HTML: "):]
	htmlLine = strings.TrimSuffix(htmlLine, "\n")
	if n := len([]rune(htmlLine)); n != annotationHTMLMaxRunes+1 {
		t.Fatalf("html runes=%d want %d", n, annotationHTMLMaxRunes+1)
	}
}
