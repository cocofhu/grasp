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
		if _, ok := a.Env["GIT_REPOS"]; ok {
			t.Fatalf("agent %s must not wire repositories through env", name)
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
	assertInheritsWorkspace(t, wf)
	assertDefaultWorkflowGraph(t, wf.Graph)
}

func assertInheritsWorkspace(t *testing.T, wf models.WorkflowDef) {
	t.Helper()
	if wf.Defaults == nil || !wf.Defaults.InheritWorkspace {
		t.Fatalf("defaults = %+v, want inheritWorkspace", wf.Defaults)
	}
}

func TestOnboardingBootstrapKeepsExistingWorkflowDefaults(t *testing.T) {
	cases := []struct {
		name     string
		defaults *models.WorkflowDefaults
	}{
		{name: "unset", defaults: nil},
		{name: "own", defaults: &models.WorkflowDefaults{Variables: []models.WorkspaceVariable{{Name: "REGION", Value: "east"}}}},
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
			if wf.Defaults == nil || wf.Defaults.InheritWorkspace || len(wf.Defaults.Variables) != 1 || wf.Defaults.Variables[0].Name != "REGION" {
				t.Fatalf("defaults = %+v, want existing defaults kept", wf.Defaults)
			}
		})
	}
}

func TestOnboardingBootstrapBindsWizardRepoWithItsGitSettings(t *testing.T) {
	svc := newOnboardingHarness(t)
	token, err := svc.Credentials.Create(services.CredentialInput{Purpose: "git", Provider: "github", Name: "GitHub", Value: "synthetic-token"})
	if err != nil {
		t.Fatal(err)
	}
	req := services.OnboardingBootstrapRequest{
		ModelConfigurationID: "model-onboarding",
		RepoURL:              "https://github.com/org/web.git",
		RepoBranch:           "develop",
		RepoCredentialID:     token.ID,
		CommitName:           "Ada Lovelace",
		CommitEmail:          "ada@example.com",
	}
	res, err := svc.Bootstrap(req)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	projects, err := svc.Projects.List()
	if err != nil || len(projects) != 1 {
		t.Fatalf("projects = %+v, %v", projects, err)
	}
	p := projects[0]
	if p.Name != "web" || p.DefaultBranch != "develop" || p.CredentialID != token.ID || p.CommitName != "Ada Lovelace" || p.CommitEmail != "ada@example.com" {
		t.Fatalf("project = %+v", p)
	}
	wf, _ := svc.WF.Get(res.WorkflowID)
	if wf.Defaults == nil || len(wf.Defaults.Repos) != 1 || wf.Defaults.Repos[0].ProjectID != p.ID {
		t.Fatalf("workflow repos = %+v", wf.Defaults)
	}
	if env := svc.SharedAgent.Get().Env; env["GIT_USER_NAME"] != "" || env["GIT_USER_EMAIL"] != "" {
		t.Fatalf("commit author leaked into workspace defaults: %+v", env)
	}

	req.CommitName = "Grace Hopper"
	if _, err := svc.Bootstrap(req); err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	projects, _ = svc.Projects.List()
	wf, _ = svc.WF.Get(res.WorkflowID)
	if len(projects) != 1 || projects[0].CommitName != "Grace Hopper" || len(wf.Defaults.Repos) != 1 {
		t.Fatalf("re-entry did not reuse the repository: %+v %+v", projects, wf.Defaults)
	}
}

func TestOnboardingBootstrapWritesPreviewFlags(t *testing.T) {
	svc := newOnboardingHarness(t)
	off := false
	_, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		ModelConfigurationID: "model-onboarding",
		VncPreview:           &off,
		BrowserMcp:           &off,
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	shared := svc.SharedAgent.Get()
	if shared.Env["VNC_PREVIEW"] != "0" {
		t.Errorf("VNC_PREVIEW = %q, want 0 so approve nodes do not force the stack on", shared.Env["VNC_PREVIEW"])
	}
	if shared.Env["BROWSER_MCP"] != "0" {
		t.Errorf("BROWSER_MCP = %q, want 0", shared.Env["BROWSER_MCP"])
	}
}

func TestOnboardingBootstrapBindsNoRepoWhenSkipped(t *testing.T) {
	svc := newOnboardingHarness(t)
	res, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		ModelConfigurationID: "model-onboarding",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	wf, _ := svc.WF.Get(res.WorkflowID)
	if wf.Defaults != nil && len(wf.Defaults.Repos) != 0 {
		t.Fatalf("skipped repository still bound: %+v", wf.Defaults.Repos)
	}
	if projects, _ := svc.Projects.List(); len(projects) != 0 {
		t.Fatalf("skipped repository created %d projects", len(projects))
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
	for _, v := range g.Variables {
		if v.Name == "repos" {
			t.Fatal("default workflow still declares a repos variable")
		}
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
		Repos: seedRepo(t, svc),
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
	assertInheritsWorkspace(t, wf)
	assertInheritsWorkspace(t, stored)

	if _, err := svc.CreateFromBaseline(services.CreateBaselineWorkflowRequest{
		Name:  "首页可见工作流",
		Repos: seedRepo(t, svc),
	}); !errors.Is(err, services.ErrWorkflowNameExists) {
		t.Fatalf("duplicate name: %v", err)
	}
	if n := len(svc.WF.List()); n != 2 {
		t.Fatalf("duplicate must not insert, got %d workflows", n)
	}

	if _, err := svc.CreateFromBaseline(services.CreateBaselineWorkflowRequest{
		Name:  "无仓库",
		Repos: []models.RepoChoice{{ProjectID: "  "}},
	}); !errors.Is(err, services.ErrBaselineReposRequired) {
		t.Fatalf("empty repos: %v", err)
	}
	if n := len(svc.WF.List()); n != 2 {
		t.Fatalf("empty repos must not insert, got %d workflows", n)
	}
	if _, err := svc.CreateFromBaseline(services.CreateBaselineWorkflowRequest{
		Name:  "已删除仓库",
		Repos: []models.RepoChoice{{ProjectID: "missing"}},
	}); err == nil {
		t.Fatal("missing repository accepted")
	}
}

// seedRepo registers one repository, reusing it on later calls.
func seedRepo(t *testing.T, svc *services.OnboardingService) []models.RepoChoice {
	t.Helper()
	if list, _ := svc.Projects.List(); len(list) > 0 {
		return []models.RepoChoice{{ProjectID: list[0].ID}}
	}
	p, err := svc.Projects.Create(services.ProjectInput{Name: "app", RepoURL: "https://github.com/acme/app.git"}, "")
	if err != nil {
		t.Fatal(err)
	}
	return []models.RepoChoice{{ProjectID: p.ID}}
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
		Repos: seedRepo(t, svc),
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
		Repos: seedRepo(t, svc),
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
		Repos: seedRepo(t, svc),
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
		Repos: seedRepo(t, svc),
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
	svc := services.NewOnboardingService(skills, services.NewSharedAgentService(t.TempDir()), wf, creds)
	svc.Projects = services.NewProjectService(db)
	svc.Projects.SetGitCredentialResolver(creds.ResolveGitValue)
	return svc
}

func TestBaselineRequiresConfiguredAgents(t *testing.T) {
	svc := newOnboardingHarness(t)
	_, err := svc.CreateFromBaseline(services.CreateBaselineWorkflowRequest{Name: "Missing roles", Repos: seedRepo(t, svc)})
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
	_, err = svc.CreateFromBaseline(services.CreateBaselineWorkflowRequest{Name: "Unconfigured roles", Repos: seedRepo(t, svc)})
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
	for _, mode := range []string{"missing-model", "revoked-model-key", "missing-git", "bad-commit-email"} {
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
				request.RepoURL, request.RepoCredentialID = "https://github.com/acme/app.git", "missing"
			case "bad-commit-email":
				request.RepoURL, request.CommitEmail = "https://github.com/acme/app.git", "not-an-email"
			}
			if _, err := svc.Bootstrap(request); err == nil {
				t.Fatal("unavailable reference accepted")
			}
			projects, _ := svc.Projects.List()
			if len(svc.Skills.List()) != 0 || len(svc.WF.List()) != 0 || len(projects) != 0 {
				t.Fatal("unavailable reference created partial resources")
			}
		})
	}
}
