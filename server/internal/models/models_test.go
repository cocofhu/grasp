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
	// A branch node routes via config, so multiple plain edges are allowed.
	branch := base([]Edge{
		{Source: "in", Target: "br"},
		{Source: "br", Target: "b"},
		{Source: "br", Target: "c"},
		{Source: "b", Target: "out"},
		{Source: "c", Target: "out"},
	})
	if err := branch.Validate(); err != nil {
		t.Fatalf("branch fan-out should pass: %v", err)
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

func TestAgentPromptsNilSafeDefaults(t *testing.T) {
	var p *AgentPrompts
	if p.UpstreamHeader() != DefaultUpstreamArtifactsHeader {
		t.Error("UpstreamHeader nil")
	}
	if p.ReactOpenSuffixText() != DefaultReactOpenSuffix {
		t.Error("ReactOpenSuffixText nil")
	}
	if p.PlanContractText() != DefaultPlanContract {
		t.Error("PlanContractText nil")
	}
	if p.ImplementContractText() != DefaultImplementContract {
		t.Error("ImplementContractText nil")
	}
	if p.ClarifiedRequirementContractText() != DefaultClarifiedRequirementContract {
		t.Error("ClarifiedRequirementContractText nil")
	}
	if p.ImplementResultContractText() != DefaultImplementResultContract {
		t.Error("ImplementResultContractText nil")
	}
	if p.ResearchContractText() != DefaultResearchContract {
		t.Error("ResearchContractText nil")
	}
	if p.TestContractText() != DefaultTestContract {
		t.Error("TestContractText nil")
	}
	if p.ReviewContractText() != DefaultReviewContract {
		t.Error("ReviewContractText nil")
	}
	if p.ProposalContractText() != DefaultProposalContract {
		t.Error("ProposalContractText nil")
	}
	if p.PreflightContractText() != DefaultPreflightContract {
		t.Error("PreflightContractText nil")
	}
	if got := p.PreflightRetryText("need db"); !strings.Contains(got, "need db") {
		t.Errorf("PreflightRetryText nil: %q", got)
	}
	if got := p.PreflightRetryText(""); !strings.Contains(got, "preflight.json 未就绪") {
		t.Errorf("PreflightRetryText empty reason: %q", got)
	}
}

func TestAgentPromptsContractOverrides(t *testing.T) {
	p := &AgentPrompts{
		PlanContract:                 "PLAN",
		ImplementContract:            "IMPL",
		ClarifiedRequirementContract: "CLAR",
		ImplementResultContract:      "IMPLRES",
		ResearchContract:             "RES",
		TestContract:                 "TEST",
		ReviewContract:               "REV",
		ProposalContract:             "PROP",
		PreflightContract:            "PRE",
		PreflightRetry:               "PF {reason}",
		ProducesRetry:                "RETRY {name}",
		PlanIncompleteRetry:          "MISS {items}",
	}
	checks := map[string]string{
		p.PlanContractText():                    "PLAN",
		p.ImplementContractText():               "IMPL",
		p.ClarifiedRequirementContractText():    "CLAR",
		p.ImplementResultContractText():         "IMPLRES",
		p.ResearchContractText():                "RES",
		p.TestContractText():                    "TEST",
		p.ReviewContractText():                  "REV",
		p.ProposalContractText():                "PROP",
		p.PreflightContractText():               "PRE",
		p.PreflightRetryText("gap"):             "PF gap",
		p.ProducesRetryFor("f.md"):              "RETRY f.md",
		p.PlanIncompleteRetryFor([]string{"a"}): "MISS - a",
	}
	for got, want := range checks {
		if got != want {
			t.Errorf("override accessor = %q, want %q", got, want)
		}
	}

	// contractText override branch directly.
	if contractText("X", "def") != "X" {
		t.Error("contractText override")
	}
	if contractText("  ", "def") != "def" {
		t.Error("contractText blank -> default")
	}
}

func TestAgentPromptsTemplatesAndOverrides(t *testing.T) {
	if got := (&AgentPrompts{}).ProducesContractFor("plan.md"); got == "" {
		t.Error("ProducesContractFor default")
	}
	if got := (&AgentPrompts{ProducesContract: "write {name} now"}).ProducesContractFor("x.md"); got != "write x.md now" {
		t.Errorf("ProducesContractFor override: %q", got)
	}
	if got := (&AgentPrompts{}).ProducesRetryFor("x.md"); got == "" {
		t.Error("ProducesRetryFor")
	}
	items := (&AgentPrompts{}).PlanIncompleteRetryFor([]string{"a", "b"})
	if items == "" {
		t.Error("PlanIncompleteRetryFor")
	}
	if got := (&AgentPrompts{PlanIncompleteRetry: "left: {items}"}).PlanIncompleteRetryFor([]string{"a", "b"}); got != "left: - a\n- b" {
		t.Errorf("PlanIncompleteRetryFor override: %q", got)
	}
	if got := (&AgentPrompts{StructuredRetry: "{tool}->{name}"}).StructuredRetryFor("r.json", "set_research"); got != "set_research->r.json" {
		t.Errorf("StructuredRetryFor override: %q", got)
	}
	if (&AgentPrompts{}).StructuredRetryFor("r.json", "set_research") == "" {
		t.Error("StructuredRetryFor default")
	}
	// Override branches for the fixed-contract accessors.
	if (&AgentPrompts{PlanContract: "P"}).PlanContractText() != "P" {
		t.Error("PlanContract override")
	}
	if (&AgentPrompts{ResearchContract: "R"}).ResearchContractText() != "R" {
		t.Error("ResearchContract override")
	}
	if (&AgentPrompts{UpstreamArtifactsHeader: "H"}).UpstreamHeader() != "H" {
		t.Error("UpstreamHeader override")
	}
	if (&AgentPrompts{ReactOpenSuffix: "S"}).ReactOpenSuffixText() != "S" {
		t.Error("ReactOpenSuffix override")
	}
}

func TestSelectRecommendedOptions(t *testing.T) {
	// Single-select: recommended option wins over position.
	q := ReactQuestion{Options: []ReactOption{
		{ID: "a", Label: "A"},
		{ID: "b", Label: "B", Recommended: true},
	}}
	if opts, ok := SelectRecommendedOptions(q); !ok || len(opts) != 1 || opts[0].ID != "b" {
		t.Fatalf("recommended pick = %+v ok=%v", opts, ok)
	}
	// No recommendation -> first option.
	q2 := ReactQuestion{Options: []ReactOption{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}}
	if opts, ok := SelectRecommendedOptions(q2); !ok || len(opts) != 1 || opts[0].ID != "a" {
		t.Fatalf("fallback pick = %+v ok=%v", opts, ok)
	}
	// No options -> not ok.
	if _, ok := SelectRecommendedOptions(ReactQuestion{}); ok {
		t.Fatal("empty options should not be ok")
	}
	// Multi-select: collect every recommended option.
	multi := ReactQuestion{
		AllowMultiple: true,
		Options: []ReactOption{
			{ID: "a", Label: "A", Recommended: true},
			{ID: "b", Label: "B"},
			{ID: "c", Label: "C", Recommended: true},
		},
	}
	got, ok := SelectRecommendedOptions(multi)
	if !ok || len(got) != 2 || got[0].ID != "a" || got[1].ID != "c" {
		t.Fatalf("multi recommended = %+v ok=%v", got, ok)
	}
	// Multi-select with no recommended -> first option only.
	multiFallback := ReactQuestion{
		AllowMultiple: true,
		Options:       []ReactOption{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}},
	}
	got, ok = SelectRecommendedOptions(multiFallback)
	if !ok || len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("multi fallback = %+v ok=%v", got, ok)
	}
	// Single-select still returns at most one recommended.
	single := ReactQuestion{Options: []ReactOption{
		{ID: "a", Label: "A", Recommended: true},
		{ID: "b", Label: "B", Recommended: true},
	}}
	got, ok = SelectRecommendedOptions(single)
	if !ok || len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("single clamp pick = %+v ok=%v", got, ok)
	}
	if _, ok := SelectRecommendedOptions(ReactQuestion{}); ok {
		t.Fatal("empty options should not be ok")
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

func TestIsSkipReply(t *testing.T) {
	if !IsSkipReply("回答已跳过\n用户回复\n先按现有集群，文档这次先不动") {
		t.Fatal("zh skip envelope")
	}
	if !IsSkipReply("Answers skipped\nUser reply\nkeep the cluster") {
		t.Fatal("en skip envelope")
	}
	if !IsSkipReply("回答已跳过\n用户回复\n") {
		t.Fatal("empty body still skip")
	}
	if IsSkipReply("回答已跳过") {
		t.Fatal("label alone without newline is not skip")
	}
	if IsSkipReply("我的选择:\n- q → a") {
		t.Fatal("choice must not match skip")
	}
	if IsSkipReply("提到回答已跳过的自由文本") {
		t.Fatal("mention in body is not skip")
	}
}

func TestFormatChoiceReply(t *testing.T) {
	qs := []ReactQuestion{
		{Prompt: "登录方式?", Options: []ReactOption{{Label: "密码"}, {Label: "验证码", Recommended: true}}},
		{Prompt: "有效期?", Options: []ReactOption{{Label: "5 分钟"}, {Label: "10 分钟"}}},
	}
	got := FormatChoiceReply(qs)
	want := "我的选择:\n- 登录方式? → 验证码\n- 有效期? → 5 分钟"
	if got != want {
		t.Fatalf("FormatChoiceReply =\n%q\nwant\n%q", got, want)
	}
	// Multi-select joins recommended labels with "、".
	multi := []ReactQuestion{{
		Prompt:        "包含哪些?",
		AllowMultiple: true,
		Options: []ReactOption{
			{Label: "A", Recommended: true},
			{Label: "B"},
			{Label: "C", Recommended: true},
		},
	}}
	got = FormatChoiceReply(multi)
	want = "我的选择:\n- 包含哪些? → A、C"
	if got != want {
		t.Fatalf("FormatChoiceReply multi =\n%q\nwant\n%q", got, want)
	}
	// Multi-select with no recommended falls back to first label only.
	multiFallback := []ReactQuestion{{
		Prompt:        "范围?",
		AllowMultiple: true,
		Options:       []ReactOption{{Label: "首项"}, {Label: "次项"}},
	}}
	got = FormatChoiceReply(multiFallback)
	want = "我的选择:\n- 范围? → 首项"
	if got != want {
		t.Fatalf("FormatChoiceReply multi fallback =\n%q\nwant\n%q", got, want)
	}
	// No answerable questions -> empty string.
	if FormatChoiceReply(nil) != "" {
		t.Error("empty questions should format to empty string")
	}
	if FormatChoiceReply([]ReactQuestion{{Prompt: "q"}}) != "" {
		t.Error("optionless question should format to empty string")
	}
}

func TestRenderAnnotations(t *testing.T) {
	if RenderAnnotations(nil) != "" {
		t.Fatal("nil annotations should render empty")
	}
	got := RenderAnnotations([]ReactAnnotation{
		{JSONPath: "proposals[p1].title", Label: "方案 A", Note: "更具体"},
		{Selector: "#hero h1", URL: "http://127.0.0.1:5173/settings?tab=1", Note: ""},
		{JSONPath: "summary", Quote: "划选的一段原文", Label: "概述", Note: "改这句"},
		{Quote: "未绑定摘录", Truncated: true, Note: ""},
	})
	for _, part := range []string{
		"字段路径", "proposals[p1].title", "(方案 A)", "更具体",
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
