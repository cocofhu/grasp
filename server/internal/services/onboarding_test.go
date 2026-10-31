package services_test

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/crypto"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestOnboardingBootstrapRequiresModelConfiguration(t *testing.T) {
	svc := newOnboardingHarness(t)
	_, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		ModelConfigurationID: "",
	})
	if err != services.ErrOnboardingModelRequired {
		t.Fatalf("want ErrOnboardingModelRequired, got %v", err)
	}
	if n := len(svc.Skills.List()); n != 0 {
		t.Fatalf("no agents should be created without a model configuration, got %d", n)
	}
	if n := len(svc.WF.List()); n != 0 {
		t.Fatalf("no workflows should be created without a model configuration, got %d", n)
	}
}

func TestOnboardingBootstrapCreatesTeamAndDefaultWorkflow(t *testing.T) {
	svc := newOnboardingHarness(t)
	res, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		ModelConfigurationID: "model-onboarding",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !res.Published {
		t.Fatal("expected published workflow")
	}
	if res.WorkflowID == "" {
		t.Fatal("missing workflowId")
	}
	if got := strings.Join(res.AgentIDs, ","); got != "需求澄清,实现,测试评审,交付" {
		t.Fatalf("want 4 engineers in workflow order, got %v", res.AgentIDs)
	}

	shared := svc.SharedAgent.Get()
	if shared.Env["VNC_PREVIEW"] != "1" || shared.Env["BROWSER_MCP"] != "1" {
		t.Fatalf("preview flags default on: %+v", shared.Env)
	}

	for _, name := range services.OnboardingAgentNames {
		a, ok := svc.Skills.Get(name)
		if !ok {
			t.Fatalf("agent %s missing", name)
		}
		if a.Env["GITHUB_TOKEN"] != "" {
			t.Fatalf("agent %s must not store git token", name)
		}
		if a.AcpBackend != "cursor" {
			t.Fatalf("agent %s backend = %q", name, a.AcpBackend)
		}
		if got := a.Env["GIT_REPOS"]; got != "${vars.repos}" {
			t.Fatalf("agent %s GIT_REPOS = %q, want ${vars.repos}", name, got)
		}
	}

	wf, ok := svc.WF.Get(res.WorkflowID)
	if !ok {
		t.Fatal("workflow missing")
	}
	if wf.Name != services.OnboardingWorkflowName || wf.Status() != "published" || !wf.NeedsRepo {
		t.Fatalf("workflow meta: name=%s status=%s needsRepo=%v", wf.Name, wf.Status(), wf.NeedsRepo)
	}
	if !wf.ShowOnHome {
		t.Fatal("default workflow should be visible on Home")
	}
	assertSharedSandbox(t, wf)
	assertDefaultWorkflowGraph(t, wf.Graph)
}

func assertSharedSandbox(t *testing.T, wf models.WorkflowDef) {
	t.Helper()
	if wf.Defaults == nil || wf.Defaults.Sandbox == nil || wf.Defaults.Sandbox.Mode != models.SandboxModeShared {
		t.Fatalf("defaults = %+v, want sandbox.mode=shared", wf.Defaults)
	}
	if !wf.Defaults.InheritWorkspace {
		t.Fatalf("defaults.inheritWorkspace = false, want true")
	}
}

func TestOnboardingBootstrapKeepsExistingWorkflowSandboxMode(t *testing.T) {
	cases := []struct {
		name     string
		defaults *models.WorkflowDefaults
	}{
		{name: "unset", defaults: nil},
		{name: "per_node", defaults: &models.WorkflowDefaults{InheritWorkspace: true, Sandbox: &models.WorkflowSandboxPolicy{Mode: models.SandboxModePerNode}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newOnboardingHarness(t)
			req := services.OnboardingBootstrapRequest{ModelConfigurationID: "model-onboarding"}
			r1, err := svc.Bootstrap(req)
			if err != nil {
				t.Fatalf("first: %v", err)
			}
			seed, _ := svc.WF.Get(r1.WorkflowID)
			if err := svc.WF.Delete(seed.ID); err != nil {
				t.Fatalf("delete seed: %v", err)
			}
			old := models.WorkflowDef{ID: "wf-old", Name: services.OnboardingWorkflowName, NeedsRepo: true, Graph: seed.Graph, Defaults: tc.defaults}
			if err := svc.WF.Save(&old); err != nil {
				t.Fatalf("save existing: %v", err)
			}
			r2, err := svc.Bootstrap(req)
			if err != nil {
				t.Fatalf("second: %v", err)
			}
			if r2.WorkflowID != old.ID {
				t.Fatalf("bootstrap should reuse existing workflow, got %s", r2.WorkflowID)
			}
			wf, _ := svc.WF.Get(old.ID)
			if tc.defaults == nil {
				if wf.Defaults != nil {
					t.Fatalf("defaults = %+v, want unset", wf.Defaults)
				}
				return
			}
			if wf.Defaults == nil || wf.Defaults.Sandbox == nil || wf.Defaults.Sandbox.Mode != models.SandboxModePerNode {
				t.Fatalf("defaults = %+v, want per_node kept", wf.Defaults)
			}
		})
	}
}

func reposVarValue(t *testing.T, graph models.Graph) []any {
	t.Helper()
	for _, v := range graph.Variables {
		if v.Name != "repos" {
			continue
		}
		items, ok := v.Value.([]any)
		if !ok {
			t.Fatalf("repos value is %T, want a list", v.Value)
		}
		return items
	}
	t.Fatal("workflow has no repos variable")
	return nil
}

func TestOnboardingBootstrapWritesWizardRepoIntoWorkflow(t *testing.T) {
	svc := newOnboardingHarness(t)
	res, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		ModelConfigurationID: "model-onboarding",
		RepoURL:              "https://github.com/org/web.git",
		RepoBranch:           "develop",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	wf, ok := svc.WF.Get(res.WorkflowID)
	if !ok {
		t.Fatal("workflow missing")
	}
	items := reposVarValue(t, wf.Graph)
	if len(items) != 1 {
		t.Fatalf("want 1 repo row, got %d", len(items))
	}
	row, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("repo row is %T", items[0])
	}
	if row["url"] != "https://github.com/org/web.git" {
		t.Errorf("url = %v", row["url"])
	}
	if row["name"] != "web" {
		t.Errorf("name = %v, want derived from URL", row["name"])
	}
	if row["branch"] != "develop" {
		t.Errorf("branch = %v", row["branch"])
	}
}

func TestOnboardingBootstrapWritesGitIdentityAndPreviewFlags(t *testing.T) {
	svc := newOnboardingHarness(t)
	off := false
	_, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		ModelConfigurationID: "model-onboarding",
		GitUserName:          "Ada Lovelace",
		GitUserEmail:         "ada@example.com",
		VncPreview:           &off,
		BrowserMcp:           &off,
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	shared := svc.SharedAgent.Get()
	if shared.Env["GIT_USER_NAME"] != "Ada Lovelace" {
		t.Errorf("GIT_USER_NAME = %q", shared.Env["GIT_USER_NAME"])
	}
	if shared.Env["GIT_USER_EMAIL"] != "ada@example.com" {
		t.Errorf("GIT_USER_EMAIL = %q", shared.Env["GIT_USER_EMAIL"])
	}
	if shared.Env["VNC_PREVIEW"] != "0" {
		t.Errorf("VNC_PREVIEW = %q, want 0 so approve nodes do not force the stack on", shared.Env["VNC_PREVIEW"])
	}
	if shared.Env["BROWSER_MCP"] != "0" {
		t.Errorf("BROWSER_MCP = %q, want 0", shared.Env["BROWSER_MCP"])
	}
}

func TestOnboardingBootstrapLeavesReposBlankWhenRepoSkipped(t *testing.T) {
	svc := newOnboardingHarness(t)
	res, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		ModelConfigurationID: "model-onboarding",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	wf, ok := svc.WF.Get(res.WorkflowID)
	if !ok {
		t.Fatal("workflow missing")
	}
	for i, item := range reposVarValue(t, wf.Graph) {
		row, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("repos[%d] is %T", i, item)
		}
		for _, field := range []string{"url", "name", "branch"} {
			if got, _ := row[field].(string); strings.TrimSpace(got) != "" {
				t.Errorf("repos[%d].%s = %q, want blank", i, field, got)
			}
		}
	}
}

func TestOnboardingBootstrapIdempotent(t *testing.T) {
	svc := newOnboardingHarness(t)
	req := services.OnboardingBootstrapRequest{ModelConfigurationID: "model-onboarding"}
	r1, err := svc.Bootstrap(req)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := svc.WF.UpdateShowOnHome(r1.WorkflowID, false); err != nil {
		t.Fatalf("hide before second bootstrap: %v", err)
	}
	r2, err := svc.Bootstrap(req)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if r1.WorkflowID != r2.WorkflowID {
		t.Fatalf("workflow id changed: %s vs %s", r1.WorkflowID, r2.WorkflowID)
	}
	if len(svc.Skills.List()) != len(services.OnboardingAgentNames)+1 {
		t.Fatalf("agents doubled: %d", len(svc.Skills.List()))
	}
	if r1.SoloAgentID != services.OnboardingSoloAgentName || r2.SoloAgentID != r1.SoloAgentID {
		t.Fatalf("Solo identity changed: %q -> %q", r1.SoloAgentID, r2.SoloAgentID)
	}
	if n := len(svc.WF.List()); n != 1 {
		t.Fatalf("workflows doubled: %d", n)
	}
	wf, ok := svc.WF.Get(r2.WorkflowID)
	if !ok || !wf.ShowOnHome {
		t.Fatalf("second bootstrap should restore Home visibility: ok=%v showOnHome=%v", ok, wf.ShowOnHome)
	}
}

func TestOnboardingBootstrapLanguageSwitchReusesWorkflow(t *testing.T) {
	svc := newOnboardingHarness(t)
	req := services.OnboardingBootstrapRequest{ModelConfigurationID: "model-onboarding", Language: "en"}
	r1, err := svc.Bootstrap(req)
	if err != nil {
		t.Fatalf("en: %v", err)
	}
	req.Language = "zh-CN"
	r2, err := svc.Bootstrap(req)
	if err != nil {
		t.Fatalf("zh: %v", err)
	}
	if r1.WorkflowID != r2.WorkflowID {
		t.Fatalf("workflow id changed: %s vs %s", r1.WorkflowID, r2.WorkflowID)
	}
	if n := len(svc.WF.List()); n != 1 {
		t.Fatalf("workflows doubled: %d", n)
	}
	wf, _ := svc.WF.Get(r2.WorkflowID)
	if wf.Name != services.OnboardingWorkflowName {
		t.Fatalf("name = %q, want %q", wf.Name, services.OnboardingWorkflowName)
	}
}

func TestFirstInstallDefaultWorkflowValidates(t *testing.T) {
	svc := newOnboardingHarness(t)
	res, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		ModelConfigurationID: "model-onboarding",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	wf, ok := svc.WF.Get(res.WorkflowID)
	if !ok {
		t.Fatal("missing workflow")
	}
	if err := wf.Graph.Validate(); err != nil {
		t.Fatalf("graph invalid: %v", err)
	}
}

func assertDefaultWorkflowGraph(t *testing.T, g models.Graph) {
	t.Helper()
	byID := map[string]models.Node{}
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}
	for _, id := range []string{"input", "clarify", "implement", "test_review", "deliver", "output"} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("missing node %s", id)
		}
	}
	if len(g.Nodes) != 6 {
		t.Fatalf("default workflow nodes = %d, want 6", len(g.Nodes))
	}
	for _, id := range []string{"clarify", "implement", "test_review", "deliver"} {
		if byID[id].Type != "agent" {
			t.Fatalf("%s type = %s, want agent", id, byID[id].Type)
		}
	}
	type edgeKey struct{ src, handle, dst string }
	edges := map[edgeKey]bool{}
	for _, e := range g.Edges {
		edges[edgeKey{e.Source, e.SourceHandle, e.Target}] = true
	}
	for _, want := range []edgeKey{
		{"input", "", "clarify"},
		{"clarify", "", "implement"},
		{"implement", "", "test_review"},
		{"test_review", "pass", "deliver"},
		{"test_review", "fail", "implement"},
		{"deliver", "", "output"},
	} {
		if !edges[want] {
			t.Fatalf("missing edge %s -%s-> %s in %+v", want.src, want.handle, want.dst, g.Edges)
		}
	}
	if len(g.Edges) != 6 {
		t.Fatalf("default workflow edges = %d, want 6", len(g.Edges))
	}
	results, _ := byID["output"].Config["results"].([]any)
	var readsMR bool
	for _, r := range results {
		if s, _ := r.(string); s == "{{nodes.deliver.outputs.merge_request}}" {
			readsMR = true
		}
	}
	if !readsMR {
		t.Fatalf("output must read deliver merge_request: %v", results)
	}
	var reposVar *models.Variable
	for i := range g.Variables {
		if g.Variables[i].Name == "repos" {
			reposVar = &g.Variables[i]
			break
		}
	}
	if reposVar == nil {
		t.Fatal("missing repos variable")
	}
	list, ok := reposVar.Value.([]any)
	if !ok || len(list) == 0 {
		t.Fatalf("repos value want []any, got %T %#v", reposVar.Value, reposVar.Value)
	}
	first, ok := list[0].(map[string]any)
	if !ok {
		t.Fatalf("repos[0] = %T", list[0])
	}
	url, _ := first["url"].(string)
	if strings.TrimSpace(url) != "" {
		t.Fatalf("repos[0].url = %q, want blank", url)
	}
	if err := g.Validate(); err != nil {
		t.Fatalf("graph validate: %v", err)
	}
}

func TestOnboardingBootstrapRejectsCustomNames(t *testing.T) {
	svc := newOnboardingHarness(t)
	_, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		ModelConfigurationID: "model-onboarding",
		Agents: []services.OnboardingAgentChoice{
			{TemplateID: "clarify", Name: " 产品澄清 "},
			{TemplateID: "implement", Name: "编码", ModelConfigurationID: "model-onboarding-alt"},
			{TemplateID: "test_review", Name: "测试评审"},
			{TemplateID: "deliver", Name: "交付"},
		},
	})
	if !errors.Is(err, services.ErrOnboardingInvalidTeam) {
		t.Fatalf("want ErrOnboardingInvalidTeam, got %v", err)
	}
	if n := len(svc.Skills.List()); n != 0 {
		t.Fatalf("custom names must not create agents, got %d", n)
	}
	if n := len(svc.WF.List()); n != 0 {
		t.Fatalf("custom names must not create a workflow, got %d", n)
	}
}

func TestOnboardingBootstrapSetsModelOnCanonicalName(t *testing.T) {
	svc := newOnboardingHarness(t)
	res, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		ModelConfigurationID: "model-onboarding",
		Agents: []services.OnboardingAgentChoice{
			{TemplateID: "clarify", Name: "需求澄清"},
			{TemplateID: "implement", Name: "实现", ModelConfigurationID: "model-onboarding-alt"},
			{TemplateID: "test_review"},
			{TemplateID: "deliver", Name: "交付"},
		},
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if got := strings.Join(res.AgentIDs, ","); got != "需求澄清,实现,测试评审,交付" {
		t.Fatalf("agents = %s", got)
	}
	impl, ok := svc.Skills.Get("实现")
	if !ok || impl.ModelBinding == nil || impl.ModelBinding.ConfigurationID != "model-onboarding-alt" {
		t.Fatalf("implement model binding = %#v", impl.ModelBinding)
	}
	if clarify, _ := svc.Skills.Get("需求澄清"); clarify.ModelBinding == nil || clarify.ModelBinding.ConfigurationID != "model-onboarding" {
		t.Fatalf("clarify must reference selected configuration: %#v", clarify.ModelBinding)
	}
}

func TestOnboardingBootstrapEnglishNamesLabelTheWorkflow(t *testing.T) {
	svc := newOnboardingHarness(t)
	res, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		ModelConfigurationID: "model-onboarding",
		Language:             "en",
		Agents: []services.OnboardingAgentChoice{
			{TemplateID: "clarify", Name: "Clarify"},
			{TemplateID: "implement", Name: "Implement"},
			{TemplateID: "test_review", Name: "TestReview"},
			{TemplateID: "deliver", Name: "Deliver"},
		},
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	wf, _ := svc.WF.Get(res.WorkflowID)
	if wf.Name != services.OnboardingWorkflowNameEN || strings.ContainsAny(wf.Description, "需求澄清") {
		t.Fatalf("workflow copy not English: name=%q desc=%q", wf.Name, wf.Description)
	}
	want := map[string]string{
		"input": "Start", "output": "End",
		"clarify": "Clarify", "implement": "Implement", "test_review": "TestReview", "deliver": "Deliver",
	}
	for _, n := range wf.Graph.Nodes {
		name, ok := want[n.ID]
		if !ok {
			continue
		}
		if n.Type == "input" || n.Type == "output" {
			if n.Label != name {
				t.Fatalf("node %s label = %q, want %q", n.ID, n.Label, name)
			}
			continue
		}
		if got := models.AgentProfile(n.Config); got != name {
			t.Fatalf("node %s agent_profile = %q, want %q", n.ID, got, name)
		}
		if n.Label != name {
			t.Fatalf("node %s label = %q, want %q", n.ID, n.Label, name)
		}
	}
}

func TestOnboardingBootstrapRejectsMissingRole(t *testing.T) {
	svc := newOnboardingHarness(t)
	_, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		ModelConfigurationID: "model-onboarding",
		Agents: []services.OnboardingAgentChoice{
			{TemplateID: "clarify"},
			{TemplateID: "implement"},
		},
	})
	if !errors.Is(err, services.ErrOnboardingInvalidTeam) {
		t.Fatalf("want ErrOnboardingInvalidTeam, got %v", err)
	}
	if n := len(svc.Skills.List()); n != 0 {
		t.Fatalf("missing role must not create agents, got %d", n)
	}
	if n := len(svc.WF.List()); n != 0 {
		t.Fatalf("missing role must not create a workflow, got %d", n)
	}
}

func TestOnboardingBootstrapKeepsExistingAndFillsMissing(t *testing.T) {
	svc := newOnboardingHarness(t)
	kept := services.Agent{Name: "需求澄清", AcpBackend: services.AcpBackendCursor, Env: map[string]string{"MARKER": "keep"}}
	if err := svc.Skills.Save(kept); err != nil {
		t.Fatalf("seed: %v", err)
	}
	res, err := svc.Bootstrap(services.OnboardingBootstrapRequest{ModelConfigurationID: "model-onboarding"})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if got := strings.Join(res.AgentIDs, ","); got != "需求澄清,实现,测试评审,交付" {
		t.Fatalf("agents = %s", got)
	}
	got, ok := svc.Skills.Get("需求澄清")
	if !ok || got.Env["MARKER"] != "keep" {
		t.Fatalf("existing agent overwritten: %+v", got.Env)
	}
	if _, ok := svc.Skills.Get("交付"); !ok {
		t.Fatal("missing canonical name was not created")
	}
	if n := len(svc.Skills.List()); n != 5 {
		t.Fatalf("agents = %d, want four roles and Solo", n)
	}
}

func TestOnboardingBootstrapRejectsInvalidTeam(t *testing.T) {
	cases := map[string][]services.OnboardingAgentChoice{
		"missing clarify": {{TemplateID: "implement"}, {TemplateID: "test_review"}},
		"unknown":         {{TemplateID: "clarify"}, {TemplateID: "implement"}, {TemplateID: "deploy"}},
		"duplicate id":    {{TemplateID: "clarify"}, {TemplateID: "implement"}, {TemplateID: "implement"}},
		"duplicate name":  {{TemplateID: "clarify", Name: "同名"}, {TemplateID: "implement", Name: "同名"}},
	}
	for name, team := range cases {
		t.Run(name, func(t *testing.T) {
			svc := newOnboardingHarness(t)
			_, err := svc.Bootstrap(services.OnboardingBootstrapRequest{ModelConfigurationID: "model-onboarding", Agents: team})
			if !errors.Is(err, services.ErrOnboardingInvalidTeam) {
				t.Fatalf("want ErrOnboardingInvalidTeam, got %v", err)
			}
			if n := len(svc.Skills.List()); n != 0 {
				t.Fatalf("no agents should be created, got %d", n)
			}
		})
	}
	svc := newOnboardingHarness(t)
	_, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		ModelConfigurationID: "model-onboarding",
		Agents: []services.OnboardingAgentChoice{
			{TemplateID: "clarify", Name: "a/b"},
			{TemplateID: "implement", Name: "实现"},
			{TemplateID: "test_review", Name: "测试评审"},
			{TemplateID: "deliver", Name: "交付"},
		},
	})
	if !errors.Is(err, services.ErrInvalidAgentName) {
		t.Fatalf("want ErrInvalidAgentName, got %v", err)
	}
	if n := len(svc.Skills.List()); n != 0 {
		t.Fatalf("invalid name must not create agents, got %d", n)
	}
}

func TestCreateFromBaselineDefaultsShowOnHome(t *testing.T) {
	svc := newOnboardingHarness(t)
	if _, err := svc.Bootstrap(services.OnboardingBootstrapRequest{ModelConfigurationID: "model-onboarding"}); err != nil {
		t.Fatal(err)
	}
	wf, err := svc.CreateFromBaseline(services.CreateBaselineWorkflowRequest{
		Name:  "首页可见工作流",
		Repos: []services.BaselineRepo{{URL: "https://github.com/acme/app.git"}},
	})
	if err != nil {
		t.Fatalf("CreateFromBaseline: %v", err)
	}
	if wf.Status() != "published" || !wf.ShowOnHome {
		t.Fatalf("returned status=%s showOnHome=%v (plan g1.1)", wf.Status(), wf.ShowOnHome)
	}
	stored, ok := svc.WF.Get(wf.ID)
	if !ok {
		t.Fatal("workflow not persisted")
	}
	if stored.Status() != "published" || !stored.ShowOnHome {
		t.Fatalf("persisted status=%s showOnHome=%v (plan g1.1)", stored.Status(), stored.ShowOnHome)
	}
	assertSharedSandbox(t, wf)
	assertSharedSandbox(t, stored)

	if _, err := svc.CreateFromBaseline(services.CreateBaselineWorkflowRequest{
		Name:  "首页可见工作流",
		Repos: []services.BaselineRepo{{URL: "https://github.com/acme/app.git"}},
	}); !errors.Is(err, services.ErrWorkflowNameExists) {
		t.Fatalf("duplicate name: %v", err)
	}
	if n := len(svc.WF.List()); n != 2 {
		t.Fatalf("duplicate must not insert, got %d workflows", n)
	}

	if _, err := svc.CreateFromBaseline(services.CreateBaselineWorkflowRequest{
		Name:  "无仓库",
		Repos: []services.BaselineRepo{{URL: "  "}},
	}); !errors.Is(err, services.ErrBaselineReposRequired) {
		t.Fatalf("empty repos: %v", err)
	}
	if n := len(svc.WF.List()); n != 2 {
		t.Fatalf("empty repos must not insert, got %d workflows", n)
	}
}

func baselineAgentProfiles(t *testing.T, wf models.WorkflowDef) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, n := range wf.Graph.Nodes {
		if n.Type == "agent" {
			out[n.ID] = models.AgentProfile(n.Config)
		}
	}
	return out
}

func TestCreateFromBaselineRejectsIncompleteSet(t *testing.T) {
	svc := newOnboardingHarness(t)
	saveNamedAgents(t, svc, []string{"需求澄清", "实现"})
	before := len(svc.Skills.List())
	_, err := svc.CreateFromBaseline(services.CreateBaselineWorkflowRequest{
		Name:  "db-workbench",
		Repos: []services.BaselineRepo{{URL: "https://github.com/acme/app.git"}},
	})
	if !errors.Is(err, services.ErrOnboardingAgentsIncomplete) {
		t.Fatalf("want ErrOnboardingAgentsIncomplete, got %v", err)
	}
	if n := len(svc.Skills.List()); n != before {
		t.Fatalf("agents changed %d -> %d", before, n)
	}
	if n := len(svc.WF.List()); n != 0 {
		t.Fatalf("workflow created despite missing agents: %d", n)
	}
}

func TestCreateFromBaselineBindsEnglishSet(t *testing.T) {
	svc := newOnboardingHarness(t)
	saveNamedAgents(t, svc, services.OnboardingAgentNamesEN)
	wf, err := svc.CreateFromBaseline(services.CreateBaselineWorkflowRequest{
		Name:  "english-baseline",
		Repos: []services.BaselineRepo{{URL: "https://github.com/acme/app.git"}},
	})
	if err != nil {
		t.Fatalf("CreateFromBaseline: %v", err)
	}
	profiles := baselineAgentProfiles(t, wf)
	for i, role := range services.TeamEngineerTemplates {
		want := services.OnboardingAgentNamesEN[i]
		if profiles[role.ID] != want {
			t.Fatalf("node %s bound to %q, want %q", role.ID, profiles[role.ID], want)
		}
		if _, ok := svc.Skills.Get(role.RoleLabelZH); ok {
			t.Fatalf("baseline created Chinese agent %s", role.RoleLabelZH)
		}
	}
	if n := len(svc.Skills.List()); n != len(services.OnboardingAgentNamesEN) {
		t.Fatalf("agents = %d, want %d", n, len(services.OnboardingAgentNamesEN))
	}
}

func TestCreateFromBaselinePrefersChineseWhenBothSetsExist(t *testing.T) {
	svc := newOnboardingHarness(t)
	saveNamedAgents(t, svc, append(append([]string{}, services.OnboardingAgentNames...), services.OnboardingAgentNamesEN...))
	before := len(svc.Skills.List())
	wf, err := svc.CreateFromBaseline(services.CreateBaselineWorkflowRequest{
		Name:  "both-sets",
		Repos: []services.BaselineRepo{{URL: "https://github.com/acme/app.git"}},
	})
	if err != nil {
		t.Fatalf("CreateFromBaseline: %v", err)
	}
	if got := baselineAgentProfiles(t, wf)["clarify"]; got != "需求澄清" {
		t.Fatalf("clarify bound to %q", got)
	}
	if n := len(svc.Skills.List()); n != before {
		t.Fatalf("agents changed %d -> %d", before, n)
	}
}

func TestCreateFromBaselineReusesExistingAgents(t *testing.T) {
	svc := newOnboardingHarness(t)
	if _, err := svc.Bootstrap(services.OnboardingBootstrapRequest{ModelConfigurationID: "model-onboarding"}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	before, _ := svc.Skills.Get("实现")
	before.Env["MARKER"] = "keep"
	if err := svc.Skills.Save(before); err != nil {
		t.Fatalf("save: %v", err)
	}
	wf, err := svc.CreateFromBaseline(services.CreateBaselineWorkflowRequest{
		Name:  "基线",
		Repos: []services.BaselineRepo{{URL: "https://github.com/acme/app.git"}},
	})
	if err != nil {
		t.Fatalf("CreateFromBaseline: %v", err)
	}
	if got := baselineAgentProfiles(t, wf)["implement"]; got != "实现" {
		t.Fatalf("implement bound to %q", got)
	}
	after, _ := svc.Skills.Get("实现")
	if after.Env["MARKER"] != "keep" {
		t.Fatalf("existing agent overwritten: %+v", after.Env)
	}
}

func saveNamedAgents(t *testing.T, svc *services.OnboardingService, names []string) {
	t.Helper()
	for _, name := range names {
		if err := svc.Skills.Save(services.Agent{
			Name:         name,
			AcpBackend:   services.AcpBackendCursor,
			ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: "model-onboarding"},
		}); err != nil {
			t.Fatalf("save %s: %v", name, err)
		}
	}
}

func newOnboardingHarness(t *testing.T) *services.OnboardingService {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	t.Setenv(crypto.SecretsKeyEnv, base64.StdEncoding.EncodeToString(key))
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "onboarding.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	root := t.TempDir()
	skills := services.NewAgentService(root)
	wf := services.NewWorkflowService(db)
	creds := services.NewCredentialService(db)
	keyRow, err := creds.Create(services.CredentialInput{Purpose: "tool_auth", ToolID: "cursor", Name: "Onboarding key", Value: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	row := models.ModelConfiguration(modelrouting.Configuration{ID: "model-onboarding", Name: "Cursor model", Backend: "cursor", State: "active", Revision: 1, Binding: modelrouting.Binding{Kind: "platform", CredentialID: keyRow.ID, ModelID: "composer-2.5"}})
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	row.ID = "model-onboarding-alt"
	row.Name = "Cursor alternative"
	row.Binding.ModelID = "another-model"
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	skills.SetModelBindingResolver(func(backend string, binding *modelrouting.Binding) error {
		_, err := services.NewModelConnectionService(db).Resolve(backend, *binding)
		return err
	})
	return services.NewOnboardingService(skills, services.NewSharedAgentService(t.TempDir()), wf, creds)
}

func TestBaselineRequiresConfiguredAgents(t *testing.T) {
	svc := newOnboardingHarness(t)
	_, err := svc.CreateFromBaseline(services.CreateBaselineWorkflowRequest{Name: "Missing roles", Repos: []services.BaselineRepo{{URL: "https://github.com/example/app.git"}}})
	if !errors.Is(err, services.ErrOnboardingAgentsIncomplete) {
		t.Fatalf("want ErrOnboardingAgentsIncomplete, got %v", err)
	}
	if len(svc.Skills.List()) != 0 || len(svc.WF.List()) != 0 {
		t.Fatal("baseline must not manufacture agents/default routes")
	}
	for _, name := range services.OnboardingAgentNames {
		if err := svc.Skills.Save(services.Agent{Name: name, AcpBackend: services.AcpBackendCursor}); err != nil {
			t.Fatal(err)
		}
	}
	_, err = svc.CreateFromBaseline(services.CreateBaselineWorkflowRequest{Name: "Unconfigured roles", Repos: []services.BaselineRepo{{URL: "https://github.com/example/app.git"}}})
	if err == nil || !strings.Contains(err.Error(), "complete onboarding") {
		t.Fatalf("want actionable missing configuration error, got %v", err)
	}
	if len(svc.WF.List()) != 0 {
		t.Fatal("baseline must not create a workflow for unconfigured agents")
	}
}
func TestOnboardingRejectsInvalidModelOverrideBeforeSaving(t *testing.T) {
	svc := newOnboardingHarness(t)
	_, err := svc.Bootstrap(services.OnboardingBootstrapRequest{ModelConfigurationID: "model-onboarding", Agents: []services.OnboardingAgentChoice{{TemplateID: "clarify"}, {TemplateID: "implement", ModelConfigurationID: "missing"}, {TemplateID: "test_review"}, {TemplateID: "deliver"}}})
	if err == nil {
		t.Fatal("missing model override accepted")
	}
	if len(svc.Skills.List()) != 0 || len(svc.WF.List()) != 0 {
		t.Fatal("invalid model created partial resources")
	}
}

func TestOnboardingRejectsRevokedOrMissingReferences(t *testing.T) {
	for _, mode := range []string{"missing-model", "revoked-model-key", "missing-git"} {
		t.Run(mode, func(t *testing.T) {
			svc := newOnboardingHarness(t)
			request := services.OnboardingBootstrapRequest{ModelConfigurationID: "model-onboarding"}
			switch mode {
			case "missing-model":
				request.ModelConfigurationID = "missing"
			case "revoked-model-key":
				credentials, err := svc.Credentials.List()
				if err != nil {
					t.Fatal(err)
				}
				for _, credential := range credentials {
					if credential.Configured {
						if err := svc.Credentials.Revoke(credential.ID); err != nil {
							t.Fatal(err)
						}
					}
				}
			case "missing-git":
				request.GitBindings = &models.GitBindings{Items: []models.GitCredentialBinding{{CredentialID: "missing"}}, PreferredCredentialID: "missing"}
			}
			if _, err := svc.Bootstrap(request); err == nil {
				t.Fatal("unavailable reference accepted")
			}
			if len(svc.Skills.List()) != 0 || len(svc.WF.List()) != 0 {
				t.Fatal("unavailable reference created partial resources")
			}
		})
	}
}
