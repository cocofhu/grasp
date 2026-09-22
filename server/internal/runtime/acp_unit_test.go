package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/nodereg"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/textutil"

	"github.com/gorilla/websocket"
)

func TestPureHelpers(t *testing.T) {
	if got := artifactKind("a.json"); got != "json" {
		t.Errorf("artifactKind json = %q", got)
	}
	if got := artifactKind("a.yaml"); got != "yaml" {
		t.Errorf("artifactKind yaml = %q", got)
	}
	if got := artifactKind("a.md"); got != "markdown" {
		t.Errorf("artifactKind md = %q", got)
	}
	if got := gitBaseURL("https://gitlab.com/g/p.git"); got != "https://gitlab.com" {
		t.Errorf("gitBaseURL = %q", got)
	}
	if got := gitBaseURL("git@host:g/p.git"); got != "" {
		t.Errorf("gitBaseURL ssh should be empty, got %q", got)
	}
	if got := gitRepoScheme("https://github.com/o/r.git"); got != "https" {
		t.Errorf("gitRepoScheme https = %q", got)
	}
	if got := gitRepoScheme("git@github.com:o/r.git"); got != "ssh" {
		t.Errorf("gitRepoScheme scp = %q", got)
	}
	if got := gitRepoScheme("ssh://git@gitlab.com/o/r.git"); got != "ssh" {
		t.Errorf("gitRepoScheme ssh:// = %q", got)
	}
	if got := gitRepoHost("https://github.com/o/r.git"); got != "github.com" {
		t.Errorf("gitRepoHost https = %q", got)
	}
	if got := gitRepoHost("git@git.example.com:o/r.git"); got != "git.example.com" {
		t.Errorf("gitRepoHost scp = %q", got)
	}
	if got := gitRepoHost("ssh://git@gitlab.com/o/r.git"); got != "gitlab.com" {
		t.Errorf("gitRepoHost ssh:// = %q", got)
	}
	if !isGitLabRepo("https://gitlab.com/o/r.git", "") {
		t.Error("gitlab.com should be GitLab")
	}
	if !isGitLabRepo("https://git.example.com/o/r.git", "https://git.example.com") {
		t.Error("self-hosted GitLab should match GITLAB_URL")
	}
	if isGitLabRepo("https://github.com/o/r.git", "") {
		t.Error("github.com should not be GitLab")
	}
	if isGitLabRepo("https://github.com/o/r.git", "glpat-xxx") {
		t.Error("GitHub with GITLAB_TOKEN env should not be GitLab repo")
	}
	if got := str2(nil); got != "" {
		t.Errorf("str2(nil) = %q", got)
	}
	if got := str2(42); got != "42" {
		t.Errorf("str2(int) = %q", got)
	}
	if got := firstLine("a\nb"); got != "a" {
		t.Errorf("firstLine = %q", got)
	}
	if got := textutil.TruncateBytes("abcdef", 3, "…(truncated)"); got != "abc…(truncated)" {
		t.Errorf("TruncateBytes = %q", got)
	}
	if _, ok := toInt("nope"); ok {
		t.Error("toInt(string) should be false")
	}
	if n, ok := toInt(float64(7)); !ok || n != 7 {
		t.Errorf("toInt(float64) = %d,%v", n, ok)
	}
	if got := shellArg("a'b"); got != `'a'\''b'` {
		t.Errorf("shellArg = %q", got)
	}
}

func TestStructuredProduct(t *testing.T) {
	cases := map[string]string{"plan": mcp.PlanArtifactName, "implement": mcp.ImplementationResultArtifactName,
		"research": mcp.ResearchArtifactName, "test": mcp.TestResultArtifactName, "review": mcp.ReviewArtifactName,
		"proposal": mcp.ProposalsArtifactName, "preflight": mcp.PreflightArtifactName, "agent": ""}
	for nt, want := range cases {
		name, _ := nodereg.StructuredProduct(nt)
		if name != want {
			t.Errorf("StructuredProduct(%q) = %q want %q", nt, name, want)
		}
	}
}

func TestArtifactOwnedByNode(t *testing.T) {
	store := newMemStore()
	host := mcp.NewHost(store)
	tok := host.RegisterRun("r")
	t.Cleanup(func() { host.UnregisterRun("r") })

	if artifactOwnedByNode(host, "r", tok, "n", mcp.PlanArtifactName) {
		t.Fatal("missing artifact must not be owned")
	}
	if _, err := host.WriteArtifact("r", tok, "upstream", mcp.PlanArtifactName, `{"goals":[]}`, "json"); err != nil {
		t.Fatal(err)
	}
	if artifactOwnedByNode(host, "r", tok, "n", mcp.PlanArtifactName) {
		t.Fatal("upstream writer must not satisfy current node")
	}
	if !artifactOwnedByNode(host, "r", tok, "upstream", mcp.PlanArtifactName) {
		t.Fatal("actual writer should own the artifact")
	}
	if _, err := host.WriteArtifact("r", tok, "n", mcp.PlanArtifactName, `{"goals":[{"title":"G"}]}`, "json"); err != nil {
		t.Fatal(err)
	}
	if !artifactOwnedByNode(host, "r", tok, "n", mcp.PlanArtifactName) {
		t.Fatal("rewrite by current node should own the artifact")
	}
	if artifactOwnedByNode(host, "r", "bad-token", "n", mcp.PlanArtifactName) {
		t.Fatal("unauthorized token must not report ownership")
	}
}

func TestApproveProductsSettled(t *testing.T) {
	store := newMemStore()
	host := mcp.NewHost(store)
	tok := host.RegisterRun("r")
	t.Cleanup(func() { host.UnregisterRun("r") })
	p := newACPProvider(host, Options{}).(*acpProvider)
	req := NodeReq{RunID: "r", NodeID: "n", NodeType: "approve", Token: tok}

	if p.approveProductsSettled(req) {
		t.Fatal("missing artifacts must not be settled")
	}
	if _, err := host.WriteArtifact("r", tok, "n", mcp.ClarifiedRequirementArtifactName,
		mcp.MinimalValidClarifiedRequirementJSON, "json"); err != nil {
		t.Fatal(err)
	}
	if p.approveProductsSettled(req) {
		t.Fatal("clarified requirement alone must not be settled")
	}
	if _, err := host.WriteArtifact("r", tok, "n", mcp.PlanArtifactName,
		`{"goals":[{"id":"g1","title":"目标"}]}`, "json"); err != nil {
		t.Fatal(err)
	}
	if !p.approveProductsSettled(req) {
		t.Fatal("both products with work_kind=feature and empty open_questions must be settled")
	}

	openReq := strings.Replace(mcp.MinimalValidClarifiedRequirementJSON,
		`"constraints": ["仅邮箱登录"]`,
		`"constraints": ["仅邮箱登录"], "open_questions": ["还要不要短信?"]`, 1)
	if _, err := host.WriteArtifact("r", tok, "n", mcp.ClarifiedRequirementArtifactName, openReq, "json"); err != nil {
		t.Fatal(err)
	}
	if p.approveProductsSettled(req) {
		t.Fatal("non-empty open_questions must not be settled")
	}

	if _, err := host.WriteArtifact("r", tok, "n", mcp.ClarifiedRequirementArtifactName,
		`{not-json`, "json"); err != nil {
		t.Fatal(err)
	}
	if p.approveProductsSettled(req) {
		t.Fatal("unparseable clarified requirement must not be settled")
	}
}

func TestConditionalInjection(t *testing.T) {
	req := NodeReq{Config: map[string]any{"conditional_prompt": map[string]any{"when_var": "flag", "text": "EXTRA"}}, Vars: map[string]any{}}
	if got := conditionalInjection(req); got != "" {
		t.Errorf("no var => empty, got %q", got)
	}
	req.Vars["flag"] = "yes"
	if got := conditionalInjection(req); got != "EXTRA" {
		t.Errorf("var set => EXTRA, got %q", got)
	}
	req.Vars["flag"] = "false"
	if got := conditionalInjection(req); got != "" {
		t.Errorf("false => empty, got %q", got)
	}
	req.NodeType = "approve"
	req.Vars["flag"] = "yes"
	if got := conditionalInjection(req); got != "" {
		t.Errorf("approve leftover conditional_prompt => empty, got %q", got)
	}
}

func TestConditionalInjectionComposite(t *testing.T) {
	req := NodeReq{
		Config: map[string]any{"conditional_prompt": map[string]any{"when_var": "flag", "text": "EXTRA"}},
		Vars: map[string]any{
			"flag": map[string]any{
				"text":   "",
				"images": []any{map[string]any{"data": "x", "mimeType": "image/png"}},
			},
		},
	}
	if got := conditionalInjection(req); got != "EXTRA" {
		t.Errorf("images-only when_var should inject, got %q", got)
	}
}

func TestSubstHelpers(t *testing.T) {
	vars := map[string]string{"A": "x", "B": "y"}
	if got := substVars("${A}-${B}", vars); got != "x-y" {
		t.Errorf("substVars = %q", got)
	}
	if got := substMap(map[string]string{"k": "${A}"}, vars); got["k"] != "x" {
		t.Errorf("substMap = %v", got)
	}
	if got := substSlice([]string{"${B}"}, vars); got[0] != "y" {
		t.Errorf("substSlice = %v", got)
	}
	if substMap(nil, vars) != nil || substSlice(nil, vars) != nil {
		t.Error("nil inputs should return nil")
	}
}

func TestBuildAgentPromptPerType(t *testing.T) {
	host := mcp.NewHost(newMemStore())
	p := newACPProvider(host, Options{}).(*acpProvider)
	for _, nt := range []string{"plan", "implement", "react", "research", "test", "review", "proposal", "agent"} {
		req := NodeReq{NodeType: nt, Config: map[string]any{"prompt": "P", "produces": "out.md"}}
		got := p.buildAgentPrompt(req, []string{"up.md"})
		if !strings.Contains(got, "P") {
			t.Errorf("%s: prompt body missing", nt)
		}
		if !strings.Contains(got, "up.md") {
			t.Errorf("%s: upstream seed missing", nt)
		}
	}
}

func TestApprovePromptIgnoresTemplateAndInjectsVars(t *testing.T) {
	host := mcp.NewHost(newMemStore())
	p := newACPProvider(host, Options{}).(*acpProvider)
	req := NodeReq{
		NodeType: "approve",
		Config:   map[string]any{"prompt": "UNIQUE_USER_PROMPT_XYZ"},
		Vars:     map[string]any{"feature": "邮箱验证码登录", "empty": ""},
	}
	got := p.buildAgentPrompt(req, []string{"up.md"})
	if strings.Contains(got, "UNIQUE_USER_PROMPT_XYZ") {
		t.Fatal("approve must ignore inspector prompt template")
	}
	if !strings.Contains(got, "邮箱验证码登录") {
		t.Fatalf("approve should inject feature var:\n%s", got)
	}
	if strings.Contains(got, "用 ask_question 开始") {
		t.Fatal("approve seed must not start with ask_question")
	}
	if !strings.Contains(got, "以下是本次运行输入") {
		t.Fatalf("approve seed should present run inputs:\n%s", got)
	}
	open := p.buildReactOpenPrompt(req, nil)
	if strings.Contains(open, "第一回合必须调用 ask_question") {
		t.Fatalf("approve must not force first-turn ask_question:\n%s", open)
	}
	if !strings.Contains(open, "真实分歧") {
		t.Fatalf("approve open suffix missing:\n%s", open)
	}
	leftover := req
	leftover.Config = map[string]any{
		"prompt":             "UNIQUE_USER_PROMPT_XYZ",
		"conditional_prompt": map[string]any{"when_var": "feature", "text": "CONDITIONAL_INJECT_XYZ"},
	}
	got = p.buildAgentPrompt(leftover, nil)
	if strings.Contains(got, "CONDITIONAL_INJECT_XYZ") {
		t.Fatal("approve must ignore leftover conditional_prompt")
	}
	if strings.Contains(got, "node_complete") {
		t.Fatalf("Grasp Phase1 system prompt must not mention node_complete:\n%s", got)
	}
	if strings.Contains(got, "完成标记契约") {
		t.Fatalf("Grasp Phase1 must not append OutcomeContract:\n%s", got)
	}
	impl := p.buildAgentPrompt(NodeReq{NodeType: "implement", Config: map[string]any{"prompt": "P"}}, nil)
	if !strings.Contains(impl, "node_complete") {
		t.Fatal("implement prompt must still include OutcomeContract")
	}
}

// stubHistory feeds the host the two run-scoped reads FeedbackBrief needs.
type stubHistory struct {
	run      models.Run
	feedback []models.FeedbackEvent
}

func (s *stubHistory) States(string) []models.StateRun { return nil }
func (s *stubHistory) Get(string) (models.Run, bool)   { return s.run, true }
func (s *stubHistory) FeedbackEvents(string) []models.FeedbackEvent {
	return s.feedback
}

// Stored feedback an agent is never told to read is the same as no feedback, so
// the clause must appear when — and only when — the node has rounds in scope.
func TestBuildAgentPromptInjectsFeedbackOnlyWhenInScope(t *testing.T) {
	host := mcp.NewHost(newMemStore())
	host.SetHistoryProvider(&stubHistory{
		run: models.Run{ID: "r1", Graph: models.Graph{
			Nodes: []models.Node{{ID: "impl", Type: "implement"}, {ID: "other", Type: "implement"}},
		}},
		feedback: []models.FeedbackEvent{{
			RunID: "r1", Kind: models.FeedbackKindReview, NodeID: "impl", Iteration: 2, Round: 3,
			Text:         "第 3 条证据不足," + strings.Repeat("请补上原始链接。", 40),
			ArtifactName: "feedback.review.impl.i2r3.json",
		}},
	})
	p := newACPProvider(host, Options{}).(*acpProvider)

	got := p.buildAgentPrompt(NodeReq{
		RunID: "r1", NodeID: "impl", NodeType: "implement",
		Config: map[string]any{"prompt": "P"},
	}, nil)
	if !strings.Contains(got, "历史人工反馈") || !strings.Contains(got, "list_run_history") {
		t.Fatalf("node with feedback must be told to read it:\n%s", got)
	}
	if !strings.Contains(got, "feedback.review.impl.i2r3.json") {
		t.Fatalf("the round's product must be cited by name:\n%s", got)
	}
	// One line per round, not the body — a long push-back must not push the
	// node's own instructions out of the context window.
	if !strings.Contains(got, "第 3 条证据不足") {
		t.Fatalf("the round needs a readable gist:\n%s", got)
	}
	if strings.Contains(got, strings.Repeat("请补上原始链接。", 40)) {
		t.Fatalf("full opinion text must not be inlined into the prompt:\n%s", got)
	}

	clean := p.buildAgentPrompt(NodeReq{
		RunID: "r1", NodeID: "other", NodeType: "implement",
		Config: map[string]any{"prompt": "P"},
	}, nil)
	if strings.Contains(clean, "历史人工反馈") {
		t.Fatalf("a node with no feedback in scope must not carry the clause:\n%s", clean)
	}
}

func TestTestNodePromptExtras(t *testing.T) {
	host := mcp.NewHost(newMemStore())
	p := newACPProvider(host, Options{}).(*acpProvider)

	t.Run("no repos no extras", func(t *testing.T) {
		got := p.buildAgentPrompt(NodeReq{
			NodeType: "test",
			Config:   map[string]any{"prompt": "P"},
			Vars:     map[string]any{},
		}, nil)
		if strings.Contains(got, "多仓测试范围") || strings.Contains(got, "工作区仓库布局") {
			t.Error("no repos should not inject repo layout/scope section")
		}
	})

	t.Run("multi repo all scope", func(t *testing.T) {
		got := p.buildAgentPrompt(NodeReq{
			NodeType: "test",
			Config:   map[string]any{"prompt": "P", "repoScope": "all"},
			Vars: map[string]any{
				"repos": `[{"name":"primary"},{"name":"frontend"}]`,
			},
		}, nil)
		if !strings.Contains(got, "多仓测试范围") || !strings.Contains(got, "repoScope=all") {
			t.Errorf("expected multi-repo injection: %q", got)
		}
	})

	t.Run("scoped repo", func(t *testing.T) {
		got := p.buildAgentPrompt(NodeReq{
			NodeType: "test",
			Config:   map[string]any{"prompt": "P", "repoScope": "frontend"},
			Vars: map[string]any{
				"repos": `[{"name":"primary"},{"name":"frontend"}]`,
			},
		}, nil)
		if !strings.Contains(got, "repoScope=frontend") || !strings.Contains(got, "/root/workspace/frontend/") {
			t.Errorf("expected scoped repo injection: %q", got)
		}
	})

	t.Run("block_on_skipped", func(t *testing.T) {
		got := p.buildAgentPrompt(NodeReq{
			NodeType: "test",
			Config:   map[string]any{"prompt": "P", "block_on_skipped": true},
			Vars:     map[string]any{},
		}, nil)
		if !strings.Contains(got, "block_on_skipped=true") {
			t.Errorf("expected block_on_skipped injection: %q", got)
		}
	})

	t.Run("direct_preview extras", func(t *testing.T) {
		got := p.buildAgentPrompt(NodeReq{
			NodeType: "app_preview",
			Config:   map[string]any{"prompt": "P", "direct_preview": true},
		}, nil)
		if !strings.Contains(got, "direct_preview") || !strings.Contains(got, "PREVIEW_PORT") || !strings.Contains(got, "PREVIEW_PICK_SCRIPT_URL") {
			t.Errorf("expected direct_preview injection: %q", got)
		}
	})

	t.Run("approve direct_preview extras", func(t *testing.T) {
		got := p.buildAgentPrompt(NodeReq{
			NodeType: "approve",
			Config:   map[string]any{"direct_preview": true},
		}, nil)
		if !strings.Contains(got, "direct_preview") || !strings.Contains(got, "PREVIEW_PORT") {
			t.Errorf("expected approve direct_preview injection: %q", got)
		}
		if !strings.Contains(got, "set_preview") {
			t.Errorf("approve contract should mention set_preview: %q", got)
		}
	})

	t.Run("direct_preview off no extras", func(t *testing.T) {
		got := p.buildAgentPrompt(NodeReq{
			NodeType: "app_preview",
			Config:   map[string]any{"prompt": "P"},
		}, nil)
		if strings.Contains(got, "节点配置:direct_preview") {
			t.Errorf("default must not inject direct_preview: %q", got)
		}
	})
}

func TestPreviewNodePromptExtras(t *testing.T) {
	host := mcp.NewHost(newMemStore())
	p := newACPProvider(host, Options{}).(*acpProvider)

	got := p.buildAgentPrompt(NodeReq{
		NodeType: "app_preview",
		Config:   map[string]any{"prompt": "P", "direct_preview": true},
	}, nil)
	if !strings.Contains(got, "direct_preview") || !strings.Contains(got, "PREVIEW_PORT") || !strings.Contains(got, "PREVIEW_PICK_SCRIPT_URL") {
		t.Errorf("expected direct_preview injection: %q", got)
	}
	if !strings.Contains(got, `<script src="$PREVIEW_PICK_SCRIPT_URL"></script>`) {
		t.Errorf("expected pick script contract: %q", got)
	}
	if !strings.Contains(got, "自动向 HTML 注入") {
		t.Errorf("expected platform auto-inject: %q", got)
	}
	if !strings.Contains(got, "旧沙箱镜像") {
		t.Errorf("expected old-image fallback: %q", got)
	}

	manual := p.buildAgentPrompt(NodeReq{
		NodeType: "app_preview",
		Config:   map[string]any{"prompt": "P", "direct_preview": true, "auto_inject": false},
	}, nil)
	if !strings.Contains(manual, "已关闭自动注入") {
		t.Errorf("expected manual script contract: %q", manual)
	}
	if strings.Contains(manual, "自动向 HTML 注入") {
		t.Errorf("manual must not claim auto-inject: %q", manual)
	}

	off := p.buildAgentPrompt(NodeReq{
		NodeType: "app_preview",
		Config:   map[string]any{"prompt": "P"},
	}, nil)
	if strings.Contains(off, "节点配置:direct_preview") {
		t.Errorf("default must not inject direct_preview: %q", off)
	}
	for _, want := range []string{
		"port 与 url 必须恰好提供其一",
		`set_preview(url=`,
		"不要为了走 port 而在沙箱里反代外部站点",
	} {
		if !strings.Contains(off, want) {
			t.Errorf("preview contract missing %q:\n%s", want, off)
		}
	}
	if strings.Contains(off, "port 必填") {
		t.Errorf("preview contract must not require port: %q", off)
	}
}

func TestApplyAppPreviewEnv(t *testing.T) {
	vnc := map[string]string{}
	applyAppPreviewEnv(vnc, "app_preview", nil, "http://app.example")
	if vnc["VNC_PREVIEW"] != "1" || vnc["PREVIEW_DIRECT"] != "" {
		t.Fatalf("default vnc: %v", vnc)
	}
	direct := map[string]string{}
	applyAppPreviewEnv(direct, "app_preview", map[string]any{"direct_preview": true}, "http://app.example/")
	if direct["PREVIEW_DIRECT"] != "1" || direct["VNC_PREVIEW"] != "" {
		t.Fatalf("direct: %v", direct)
	}
	if direct["PREVIEW_PICK_SCRIPT_URL"] != "/__grasp/preview-pick.js" {
		t.Fatalf("pick script: %v", direct)
	}
	if direct["PREVIEW_AUTO_INJECT"] != "1" {
		t.Fatalf("auto inject default on: %v", direct)
	}
	manualEnv := map[string]string{}
	applyAppPreviewEnv(manualEnv, "app_preview", map[string]any{"direct_preview": true, "auto_inject": false}, "http://app.example")
	if manualEnv["PREVIEW_DIRECT"] != "1" || manualEnv["PREVIEW_AUTO_INJECT"] != "0" {
		t.Fatalf("auto inject off: %v", manualEnv)
	}
	if manualEnv["PREVIEW_PICK_SCRIPT_URL"] != "http://app.example/preview-pick.js" {
		t.Fatalf("manual pick script: %v", manualEnv)
	}
	approve := map[string]string{}
	applyAppPreviewEnv(approve, "approve", nil, "http://app.example")
	if approve["VNC_PREVIEW"] != "1" || approve["PREVIEW_DIRECT"] != "" {
		t.Fatalf("approve default vnc: %v", approve)
	}
	off := map[string]string{"VNC_PREVIEW": "0"}
	applyAppPreviewEnv(off, "approve", nil, "http://app.example")
	if off["VNC_PREVIEW"] != "0" || off["GRASP_VNC_PREVIEW"] != "" {
		t.Fatalf("explicit off must stick: %v", off)
	}
	other := map[string]string{}
	applyAppPreviewEnv(other, "implement", map[string]any{"direct_preview": true}, "http://app.example")
	if other["PREVIEW_DIRECT"] != "" || other["VNC_PREVIEW"] != "" || other["PREVIEW_PICK_SCRIPT_URL"] != "" {
		t.Fatalf("other node: %v", other)
	}
	empty := map[string]string{}
	applyAppPreviewEnv(empty, "app_preview", map[string]any{"direct_preview": true}, "")
	if empty["PREVIEW_PICK_SCRIPT_URL"] != "/__grasp/preview-pick.js" {
		t.Fatalf("auto-inject uses same-origin path without advertise: %v", empty)
	}
}

func TestProviderWiringAndAbort(t *testing.T) {
	host := mcp.NewHost(newMemStore())
	p := NewProvider("cursor", host, Options{})
	if p.Name() != "registry" {
		t.Errorf("Name = %q", p.Name())
	}
	if NewProvider("weird", host, Options{}) == nil {
		t.Error("NewProvider fallback nil")
	}
	reg := p.(*ProviderRegistry)
	cp := reg.providers[BackendCursor].(*acpProvider)
	cp.SetEventSink(func(string, string, []models.AcpEvent, bool) {})
	cp.SetSandboxRegistry(&countingRegistry{})

	// AbortRun tears down a live session (fake sandbox, nil mgr => Destroy no-op).
	key := "runX|nodeY"
	cp.mu.Lock()
	cp.sessions[key] = &reactSession{sb: &sandbox.Sandbox{Name: "fake"}}
	cp.mu.Unlock()
	reg.AbortRun("runX")
	cp.mu.Lock()
	_, still := cp.sessions[key]
	cp.mu.Unlock()
	if still {
		t.Error("AbortRun should remove the session")
	}
}

// TestAbortRunRetiresWhenPreviewKeepalivePID ensures Cancel of the production
// agent turn consults ListPreviewKeepalivePIDs and retires (keeps container)
// instead of Destroy — so setsid-detached preview survives session end.
func TestAbortRunRetiresWhenPreviewKeepalivePID(t *testing.T) {
	host := mcp.NewHost(newMemStore())
	host.PutPreviewPortForTest("runKeep", "nodeP", 8080, "web")
	if pids := host.ListPreviewKeepalivePIDs("runKeep", "nodeP"); len(pids) == 0 {
		t.Fatal("PutPreviewPortForTest must register keepalive pid for whitelist")
	}

	cp := newACPProvider(host, Options{}).(*acpProvider)
	reg := &retiringRegistry{}
	cp.registry = reg

	key := "runKeep|nodeP"
	sb := &sandbox.Sandbox{Name: "preview-sb"}
	cp.mu.Lock()
	cp.inflightACP[key] = nil
	cp.live[key] = sb
	cp.mu.Unlock()

	cp.AbortRun("runKeep")

	cp.mu.Lock()
	_, stillACP := cp.inflightACP[key]
	_, stillLive := cp.live[key]
	cp.mu.Unlock()
	if stillACP || stillLive {
		t.Fatal("AbortRun should clear inflight agent maps")
	}
	reg.mu.Lock()
	retired := reg.retired
	reg.mu.Unlock()
	if retired != 1 {
		t.Fatalf("expected retireRunSandbox (keep preview container), retired=%d", retired)
	}
}

func TestLiveNodeEvents(t *testing.T) {
	host := mcp.NewHost(newMemStore())
	p := newACPProvider(host, Options{}).(*acpProvider)
	// Not live => ok=false, err=nil (fall back to snapshot).
	if _, ok, err := p.LiveNodeEvents(context.Background(), "r", "n"); ok || err != nil {
		t.Errorf("expected ok=false err=nil when node not live, got ok=%v err=%v", ok, err)
	}
	// Live but its /api/events is unreachable (dead port) => ok=false with error
	// so callers can surface a rehydrate failure (not a fake empty live page).
	p.registerLive(NodeReq{RunID: "r", NodeID: "n"}, &sandbox.Sandbox{Host: "127.0.0.1", Port: 1}, nil)
	if _, ok, err := p.LiveNodeEvents(context.Background(), "r", "n"); ok || err == nil {
		t.Errorf("expected ok=false with err when event log fetch fails, got ok=%v err=%v", ok, err)
	}
	if _, _, more, ok, err := p.LiveNodeEventsPage(context.Background(), "r", "n", "", 10); ok || more || err == nil {
		t.Errorf("expected page ok=false with err on fetch failure, got ok=%v more=%v err=%v", ok, more, err)
	}
}

// TestLiveNodeEventsPageSuccessPath confirms sticky/same-process REST re-read:
// when the live sandbox bridge answers, LiveNodeEventsPage returns ok=true with
// the timeline (supports the ~2s refresh recovery SLA under single-instance).
func TestLiveNodeEventsPageSuccessPath(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" {
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var m map[string]any
		if json.Unmarshal(msg, &m) != nil || fmt.Sprint(m["op"]) != "connect" {
			return
		}
		frame, _ := json.Marshal(map[string]any{
			"type": "session_update",
			"update": map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content":       map[string]any{"text": "hello-rehydrate"},
			},
		})
		_ = conn.WriteJSON(map[string]any{
			"op": "connected", "sessionId": "s1",
			"eventLog":   []json.RawMessage{frame},
			"totalTurns": 1,
		})
	}))
	t.Cleanup(srv.Close)
	host, port := "127.0.0.1", srv.Listener.Addr().(*net.TCPAddr).Port

	p := newACPProvider(mcp.NewHost(newMemStore()), Options{}).(*acpProvider)
	p.registerLive(NodeReq{RunID: "run1", NodeID: "node1"}, &sandbox.Sandbox{Host: host, Port: port}, nil)

	ev, next, more, ok, err := p.LiveNodeEventsPage(context.Background(), "run1", "node1", "", 20)
	if err != nil || !ok {
		t.Fatalf("expected live page ok, got ok=%v next=%q more=%v err=%v", ok, next, more, err)
	}
	if more {
		t.Fatalf("expected hasMore=false, got true")
	}
	if len(ev) == 0 {
		t.Fatal("expected re-read events from live bridge")
	}
	found := false
	for _, e := range ev {
		if strings.Contains(e.Text, "hello-rehydrate") || strings.Contains(e.Title, "hello-rehydrate") {
			found = true
		}
	}
	if !found {
		t.Fatalf("re-read events missing payload: %+v", ev)
	}
}

// TestLiveNodeEventsPageSnapshotFirst confirms platform timeline snapshots are
// returned even when the live bridge is unreachable (cold dial is fallback).
func TestLiveNodeEventsPageSnapshotFirst(t *testing.T) {
	p := newACPProvider(mcp.NewHost(newMemStore()), Options{}).(*acpProvider)
	p.registerLive(NodeReq{RunID: "run1", NodeID: "node1"}, &sandbox.Sandbox{Host: "127.0.0.1", Port: 1}, nil)
	p.timeline.upsert("run1", "node1", []models.AcpEvent{{T: 1, Kind: "message", Text: "from-snapshot"}})

	ev, _, more, ok, err := p.LiveNodeEventsPage(context.Background(), "run1", "node1", "", 20)
	if err != nil || !ok {
		t.Fatalf("snapshot page want ok err=nil, got ok=%v err=%v", ok, err)
	}
	if more || len(ev) != 1 || ev[0].Text != "from-snapshot" {
		t.Fatalf("unexpected snapshot page: more=%v ev=%+v", more, ev)
	}
}

// profilesRoot writes a minimal agent.json so agentConfig/resolvedMCPSpecs/etc.
// have something to read.
func writeAgent(t *testing.T, profile, agentJSON string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, profile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(agentJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestAgentConfigAndMCP(t *testing.T) {
	root := writeAgent(t, "dev", `{"mcp":[{"name":"artifact-store","url":"${GRASP_ARTIFACT_URL}","headers":{"Authorization":"Bearer ${GRASP_ARTIFACT_TOKEN}"}}],"env":{"GITLAB_TOKEN":"tok-${GRASP_RUN_ID}","GRASP_CURSOR_API_KEY":"test-key"}}`)
	host := mcp.NewHost(newMemStore())
	p := newACPProvider(host, Options{ProfilesRoot: root, MCPEndpoint: "http://host.docker.internal:9099"}).(*acpProvider)
	req := NodeReq{RunID: "run9", NodeID: "n", Token: "tkn", NodeType: "agent",
		Config: map[string]any{"agent_profile": "dev"}, Vars: map[string]any{"repos": `[{"name":"p","url":"https://gitlab.com/g/p.git"}]`}}

	cfg := p.agentConfig("dev")
	if len(cfg.MCP) != 1 {
		t.Fatalf("agentConfig MCP = %d", len(cfg.MCP))
	}
	specs := p.resolvedMCPSpecs(req)
	if len(specs) != 1 || !strings.Contains(specs[0].URL, "9099") {
		t.Errorf("resolvedMCPSpecs = %+v", specs)
	}
	if raw := p.mcpServers(req); len(raw) == 0 {
		t.Error("mcpServers should be non-empty when artifact-store present")
	}
	if tok := p.gitToken(req); tok != "tok-run9" {
		t.Errorf("gitToken = %q", tok)
	}
	if wd := p.workDir("dev"); wd != "" { // no cursor/ dir exists
		t.Errorf("workDir = %q, want empty", wd)
	}
	// spec + buildCursorHome exercise the filesystem layout builder.
	sp, err := p.spec(req)
	if err != nil {
		t.Fatalf("spec: %v", err)
	}
	if sp.Env["GITLAB_URL"] != "https://gitlab.com" {
		t.Errorf("derived GITLAB_URL = %q", sp.Env["GITLAB_URL"])
	}
	removeHome(sp.ConfigHome)
}

func TestSpecDoesNotDeriveGitLabURLFromGitHubRepo(t *testing.T) {
	root := writeAgent(t, "dev", `{"env":{"GITHUB_TOKEN":"gh","GITLAB_TOKEN":"gl","GRASP_CURSOR_API_KEY":"test-key"}}`)
	host := mcp.NewHost(newMemStore())
	p := newACPProvider(host, Options{ProfilesRoot: root}).(*acpProvider)
	req := NodeReq{RunID: "run-gh", NodeID: "n", Token: "tkn", NodeType: "agent",
		Config: map[string]any{"agent_profile": "dev"}, Vars: map[string]any{"repos": `[{"name":"app","url":"https://github.com/acme/app.git"}]`}}

	sp, err := p.spec(req)
	if err != nil {
		t.Fatalf("spec: %v", err)
	}
	if got := sp.Env["GITLAB_URL"]; got != "" {
		t.Errorf("GITLAB_URL should not be derived from a GitHub repo, got %q", got)
	}
	removeHome(sp.ConfigHome)
}

func TestWorkspaceWorkDirSrc(t *testing.T) {
	root := t.TempDir()
	profile := "dev"
	agentDir := filepath.Join(root, profile)
	workDir := filepath.Join(agentDir, "workspace")
	if err := os.MkdirAll(filepath.Join(workDir, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "rules", "custom.md"), []byte("custom rule"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	host := mcp.NewHost(newMemStore())
	p := newACPProvider(host, Options{ProfilesRoot: root}).(*acpProvider)
	req := NodeReq{NodeID: "n", Config: map[string]any{"agent_profile": profile}}

	if wd := p.workDir(profile); wd != workDir {
		t.Fatalf("workDir = %q, want %q", wd, workDir)
	}

	home := p.buildConfigHome(req, nil)
	defer removeHome(home)
	if home == "" {
		t.Fatal("buildConfigHome returned empty home")
	}
	if b, err := os.ReadFile(filepath.Join(home, "rules", "custom.md")); err != nil || string(b) != "custom rule" {
		t.Fatalf("workspace rules not copied to config home: err=%v b=%q", err, b)
	}
}

func TestUpstreamArtifacts(t *testing.T) {
	store := newMemStore()
	host := mcp.NewHost(store)
	tok := host.RegisterRun("r")
	defer host.UnregisterRun("r")
	host.WriteArtifact("r", tok, "n", "a.md", "x", "markdown")
	p := newACPProvider(host, Options{}).(*acpProvider)
	names := p.upstreamArtifacts(NodeReq{RunID: "r", Token: tok})
	if len(names) != 1 || names[0] != "a.md" {
		t.Errorf("upstreamArtifacts = %v", names)
	}
}

// --- docker-stubbed sandbox paths -----------------------------------------

func TestHarvestReadsFileAndWrites(t *testing.T) {
	restore := sandbox.SetExecHook(func(_ context.Context, _ string, _ int, command string, _ io.Reader) ([]byte, error) {
		if strings.Contains(command, "cat") {
			return []byte("harvested body"), nil
		}
		return nil, nil
	})
	defer restore()

	store := newMemStore()
	host := mcp.NewHost(store)
	tok := host.RegisterRun("r")
	defer host.UnregisterRun("r")
	p := newACPProvider(host, Options{}).(*acpProvider)
	sb := &sandbox.Sandbox{Name: "sb", Host: "127.0.0.1", Port: 1, WorkspaceDir: "/root/workspace"}
	out := map[string]any{}
	var events []models.AcpEvent
	if err := p.harvest(context.Background(), sb, NodeReq{RunID: "r", NodeID: "n", Token: tok}, "report.md", out, &events); err != nil {
		t.Fatalf("harvest: %v", err)
	}
	if c, ok := store.Get("r", "report.md"); !ok || c != "harvested body" {
		t.Errorf("harvest stored %q ok=%v", c, ok)
	}
	if len(events) != 1 || events[0].Kind != "tool_call" {
		t.Errorf("harvest event = %+v", events)
	}
}

// execHookBody joins SSH argv with ExecScript stdin so tests can match
// script content after the safeCmd migration (command is only `bash -s`).
func execHookBody(command string, stdin io.Reader) string {
	if stdin == nil {
		return command
	}
	b, _ := io.ReadAll(stdin)
	if len(b) == 0 {
		return command
	}
	return command + "\n" + string(b)
}

func TestEnsurePushedAndDetectPush(t *testing.T) {
	restore := sandbox.SetExecHook(func(_ context.Context, _ string, _ int, command string, stdin io.Reader) ([]byte, error) {
		body := execHookBody(command, stdin)
		switch {
		case strings.Contains(body, "rev-parse --abbrev-ref"):
			return []byte("feature-x\n"), nil
		case strings.Contains(body, "rev-parse HEAD"):
			return []byte("abc123\n"), nil
		case strings.Contains(body, "ls-remote"):
			return []byte("abc123\trefs/heads/feature-x\n"), nil
		case strings.Contains(body, "glab mr list"):
			return []byte(`[{"web_url":"https://gitlab.com/g/p/-/merge_requests/1"}]`), nil
		}
		return []byte(""), nil
	})
	defer restore()

	host := mcp.NewHost(newMemStore())
	p := newACPProvider(host, Options{Env: map[string]string{"GITLAB_TOKEN": "gl-token"}}).(*acpProvider)
	sb := &sandbox.Sandbox{Name: "sb", Host: "127.0.0.1", Port: 1, WorkspaceDir: "/root/workspace"}

	req := NodeReq{
		Config: map[string]any{"create_mr": true},
		Vars:   map[string]any{"repos": `[{"name":"p","url":"https://gitlab.com/g/p.git"}]`},
	}
	p.ensurePushed(context.Background(), sb, req) // best-effort, must not panic

	info := p.detectPush(context.Background(), sb, req)
	if info == nil || !info.Pushed || info.Branch != "feature-x" {
		t.Fatalf("detectPush = %+v", info)
	}
	if info.MrURL != "https://gitlab.com/g/p/-/merge_requests/1" {
		t.Errorf("MR url = %q", info.MrURL)
	}
}

func TestDetectPushNonGitLabSkipsMR(t *testing.T) {
	restore := sandbox.SetExecHook(func(_ context.Context, _ string, _ int, command string, stdin io.Reader) ([]byte, error) {
		body := execHookBody(command, stdin)
		switch {
		case strings.Contains(body, "rev-parse --abbrev-ref"):
			return []byte("feature-x\n"), nil
		case strings.Contains(body, "rev-parse HEAD"):
			return []byte("abc123\n"), nil
		case strings.Contains(body, "ls-remote"):
			return []byte("abc123\trefs/heads/feature-x\n"), nil
		case strings.Contains(body, "glab mr list"):
			t.Error("glab should not be called for non-GitLab repo")
		}
		return []byte(""), nil
	})
	defer restore()

	host := mcp.NewHost(newMemStore())
	p := newACPProvider(host, Options{Env: map[string]string{"GITLAB_TOKEN": "tok"}}).(*acpProvider)
	sb := &sandbox.Sandbox{Name: "sb", Host: "127.0.0.1", Port: 1, WorkspaceDir: "/root/workspace"}

	info := p.detectPush(context.Background(), sb, NodeReq{
		Config: map[string]any{"create_mr": true},
		Vars:   map[string]any{"repos": `[{"name":"r","url":"https://github.com/o/r.git"}]`},
	})
	if info == nil || info.MrURL != "" {
		t.Fatalf("non-GitLab detectPush should have empty mr_url, got %+v", info)
	}
}

func TestCaptureChanges(t *testing.T) {
	// The change report is now computed over SSH (git run in-sandbox): the exec
	// hook returns the tab-separated records sandbox.GitChanges parses. The
	// workspace root is reported as a git repo → single-repo (vcs:"git") mode.
	restore := sandbox.SetExecHook(func(_ context.Context, _ string, _ int, _ string, _ io.Reader) ([]byte, error) {
		return []byte(strings.Join([]string{
			"VCS\tgit",
			"HEAD\tdeadbeef",
			"BRANCH\tfeat",
			"BASEBRANCH\tmain",
			"BASE\tcafebabe",
			"DIRTY\t0",
			"PUSHED\t1",
			"REMOTESHA\tdeadbeef",
			"UNPUSHED\t0",
			"AHEAD\t1",
			"COMMIT\tdeadbeef\tAlice\t2024-01-01T00:00:00Z\tinit",
			"NUM\t1\t0\ta.go",
			"NAME\tM\ta.go",
		}, "\n")), nil
	})
	defer restore()

	host := mcp.NewHost(newMemStore())
	p := newACPProvider(host, Options{}).(*acpProvider)
	sb := &sandbox.Sandbox{Name: "sb", Host: "127.0.0.1", Port: 1, WorkspaceDir: "/root/workspace"}
	out := map[string]any{}
	git := p.captureChanges(context.Background(), sb, NodeReq{
		Vars: map[string]any{"repos": `[{"name":"app","url":"https://h/app.git"}]`},
	}, out)
	if git == nil || git.Branch != "feat" || !git.Pushed {
		t.Fatalf("captureChanges git = %+v", git)
	}
	if out["branch"] != "feat" || out["pushed"] != true {
		t.Errorf("captureChanges out = %+v", out)
	}
	if out["branches"] != `{"app":"feat"}` {
		t.Errorf("captureChanges branches = %v", out["branches"])
	}
}

// TestRunAgentImplementNode drives the implement path (ensurePlanComplete +
// ensureStructured + ensurePushed) via the fake bridge; no plan artifact means
// plan enforcement is a no-op, and the structured result is written up front.
func TestRunAgentImplementNode(t *testing.T) {
	restore := sandbox.SetExecHook(func(context.Context, string, int, string, io.Reader) ([]byte, error) {
		return []byte(""), nil // ensurePushed git script -> harmless
	})
	defer restore()

	store := newMemStore()
	host := mcp.NewHost(store)
	runID, nodeID := "impl-run", "impl-node"
	tok := host.RegisterRun(runID)
	t.Cleanup(func() { host.UnregisterRun(runID) })
	mgr := newFakeManager(t, host, runID, nodeID, tok, func(int) chatFunc {
		return func(int) turnAction {
			return turnAction{narration: "implemented", produces: map[string]string{
				mcp.ImplementationResultArtifactName: `{"summary":"done"}`}}
		}
	})
	p, _ := newTestProvider(t, host, testOpts(), mgr)
	req := reqWithProfile(NodeReq{RunID: runID, NodeID: nodeID, NodeType: "implement", Token: tok,
		Config: map[string]any{"prompt": "build"}, Vars: map[string]any{}})
	res, err := p.RunAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("implement RunAgent: %v", err)
	}
	if res.OutputMd == "" {
		t.Error("expected narration")
	}
	if _, ok := store.Get(runID, mcp.ImplementationResultArtifactName); !ok {
		t.Error("implementation_result not stored")
	}
}

func TestDebugFetchPage(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("path=%s", r.URL.Path)
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade err: %v", err)
			return
		}
		defer conn.Close()
		_, msg, err := conn.ReadMessage()
		t.Logf("read: %s err=%v", msg, err)
		frame, _ := json.Marshal(map[string]any{
			"type": "session_update",
			"update": map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content":       map[string]any{"text": "hello-rehydrate"},
			},
		})
		payload := map[string]any{
			"op": "connected", "sessionId": "s1",
			"eventLog":   []json.RawMessage{frame},
			"totalTurns": 1,
		}
		err = conn.WriteJSON(payload)
		t.Logf("write err=%v frame=%s", err, frame)
	}))
	t.Cleanup(srv.Close)
	host, port := "127.0.0.1", srv.Listener.Addr().(*net.TCPAddr).Port
	t.Logf("host=%s port=%d", host, port)
	page, err := sandbox.FetchEventLogPage(context.Background(), host, port, "", 20)
	t.Logf("page=%+v err=%v", page, err)
	if page != nil {
		ev := sandbox.AggregateFrames(page.Events)
		t.Logf("events=%+v", ev)
	}
}

func TestApproveInjectOpenPrompt(t *testing.T) {
	req := NodeReq{NodeType: "approve"}
	if !approveInjectOpenPrompt(req, nil) {
		t.Fatal("empty history is first turn")
	}
	if !approveInjectOpenPrompt(req, []models.ReactMessage{{Role: "human", Text: "做登录"}}) {
		t.Fatal("sole human message is first turn")
	}
	if approveInjectOpenPrompt(req, []models.ReactMessage{
		{Role: "agent", Text: "请补充"},
		{Role: "human", Text: "做登录"},
	}) {
		t.Fatal("prior agent text is not first turn")
	}
	if !approveInjectOpenPrompt(req, []models.ReactMessage{
		{Role: "human", Text: "做登录"},
		{Role: "agent", Text: "(澄清回复失败:boom)"},
		{Role: "human", Text: "再试"},
	}) {
		t.Fatal("failed open bubble must still inject")
	}
	if !approveInjectOpenPrompt(req, []models.ReactMessage{
		{Role: "human", Text: "做登录"},
		{Role: "agent", Text: "(已中断)", Interrupted: true},
		{Role: "human", Text: "再试"},
	}) {
		t.Fatal("interrupted open bubble must still inject")
	}
	if approveInjectOpenPrompt(NodeReq{NodeType: "react"}, []models.ReactMessage{{Role: "human", Text: "x"}}) {
		t.Fatal("react must not inject approve open prompt")
	}
}

func TestReactHistoryHasDialogue(t *testing.T) {
	if reactHistoryHasDialogue(nil) || reactHistoryHasDialogue([]models.ReactMessage{{Role: "agent"}}) {
		t.Fatal("empty parked session is not dialogue")
	}
	if !reactHistoryHasDialogue([]models.ReactMessage{{Role: "human", Text: "hi"}}) {
		t.Fatal("human text is dialogue")
	}
	if !reactHistoryHasDialogue([]models.ReactMessage{{Role: "agent", Questions: []models.ReactQuestion{{ID: "q1"}}}}) {
		t.Fatal("questions are dialogue")
	}
}

func TestMergePromptImages(t *testing.T) {
	a := []models.PromptImage{{Name: "a.png"}, {Name: "a2.png"}}
	b := []models.PromptImage{{Name: "b.png"}, {Name: "b2.png"}}

	t.Run("both_nonempty_order_and_len", func(t *testing.T) {
		got := mergePromptImages(a, b)
		if len(got) != 4 {
			t.Fatalf("len = %d, want 4", len(got))
		}
		want := []string{"a.png", "a2.png", "b.png", "b2.png"}
		for i, name := range want {
			if got[i].Name != name {
				t.Fatalf("got[%d].Name = %q, want %q", i, got[i].Name, name)
			}
		}
	})

	t.Run("nil_a_returns_b", func(t *testing.T) {
		got := mergePromptImages(nil, b)
		if len(got) != 2 || got[0].Name != "b.png" || got[1].Name != "b2.png" {
			t.Fatalf("nil a = %+v", got)
		}
	})

	t.Run("empty_a_returns_b", func(t *testing.T) {
		got := mergePromptImages([]models.PromptImage{}, b)
		if len(got) != 2 || got[0].Name != "b.png" {
			t.Fatalf("empty a = %+v", got)
		}
	})

	t.Run("nil_b_returns_a", func(t *testing.T) {
		got := mergePromptImages(a, nil)
		if len(got) != 2 || got[0].Name != "a.png" || got[1].Name != "a2.png" {
			t.Fatalf("nil b = %+v", got)
		}
	})

	t.Run("empty_b_returns_a", func(t *testing.T) {
		got := mergePromptImages(a, []models.PromptImage{})
		if len(got) != 2 || got[0].Name != "a.png" {
			t.Fatalf("empty b = %+v", got)
		}
	})

	t.Run("both_empty", func(t *testing.T) {
		got := mergePromptImages(nil, nil)
		if len(got) != 0 {
			t.Fatalf("both nil = %+v", got)
		}
		got = mergePromptImages([]models.PromptImage{}, []models.PromptImage{})
		if len(got) != 0 {
			t.Fatalf("both empty = %+v", got)
		}
	})
}

// plan coverage: g1.2 — chatResultToEvents must stay a thin wrapper over
// AcpEvents (no separate truncate copy). Long Thought equals full text.
func TestChatResultToEventsSharesAcpEventsFullThought(t *testing.T) {
	long := strings.Repeat("思", 2000) // >4000 UTF-8 bytes
	if len(long) <= 4000 {
		t.Fatalf("fixture too short: %d", len(long))
	}
	res := &sandbox.ChatResult{Thought: long, Narration: "ok"}
	viaWrapper := chatResultToEvents(res)
	viaShared := res.AcpEvents()
	if len(viaWrapper) != len(viaShared) {
		t.Fatalf("len wrapper=%d shared=%d", len(viaWrapper), len(viaShared))
	}
	for i := range viaWrapper {
		if viaWrapper[i] != viaShared[i] {
			t.Fatalf("event[%d] diverged: wrapper=%+v shared=%+v", i, viaWrapper[i], viaShared[i])
		}
	}
	var thought string
	for _, e := range viaWrapper {
		if e.Kind == "thought" {
			thought = e.Text
			break
		}
	}
	if thought != long {
		t.Fatalf("wrapper thought len=%d want %d (must not re-truncate)", len(thought), len(long))
	}
	if strings.Contains(thought, "…(truncated)") {
		t.Fatal("wrapper must not append truncated suffix")
	}
}
