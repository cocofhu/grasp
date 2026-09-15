package services_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestOnboardingBootstrapRequiresAPIKey(t *testing.T) {
	svc, projectID := newOnboardingHarness(t)
	_, err := svc.Bootstrap(projectID, services.OnboardingBootstrapRequest{
		AcpBackend: "cursor",
		APIKey:     "  ",
	})
	if err != services.ErrOnboardingAPIKeyRequired {
		t.Fatalf("want ErrOnboardingAPIKeyRequired, got %v", err)
	}
	if n := len(svc.Skills.List()); n != 0 {
		t.Fatalf("no agents should be created without key, got %d", n)
	}
	if n := len(svc.WF.List(projectID)); n != 0 {
		t.Fatalf("no workflows should be created without key, got %d", n)
	}
}

func TestOnboardingBootstrapAllowsNonDefaultProjectWithDerivedNames(t *testing.T) {
	svc, defaultID := newOnboardingHarness(t)
	other, err := svc.Projects.Create("中国象棋", "", nil, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	res, err := svc.Bootstrap(other.ID, services.OnboardingBootstrapRequest{
		AcpBackend: "cursor",
		APIKey:     "k-other",
	})
	if err != nil {
		t.Fatalf("bootstrap non-default: %v", err)
	}
	if len(res.AgentIDs) != 6 {
		t.Fatalf("want 6 agents, got %v", res.AgentIDs)
	}
	wantName := "中国象棋研发工程师"
	found := false
	for _, id := range res.AgentIDs {
		if id == wantName {
			found = true
		}
		if strings.HasPrefix(id, "综合") {
			t.Fatalf("non-default must not use 综合* name %q", id)
		}
		a, ok := svc.Skills.Get(id)
		if !ok || a.ProjectID != other.ID {
			t.Fatalf("agent %s missing or wrong project", id)
		}
	}
	if !found {
		t.Fatalf("missing derived agent %q in %v", wantName, res.AgentIDs)
	}
	if res.GroupName != "中国象棋项目组" {
		t.Fatalf("groupName = %q", res.GroupName)
	}
	// Default project agents must remain untouched.
	if n := len(svc.Skills.List()); n != 6 {
		t.Fatalf("only other-project agents expected before default bootstrap, got %d", n)
	}
	_, err = svc.Bootstrap(defaultID, services.OnboardingBootstrapRequest{
		AcpBackend: "cursor",
		APIKey:     "k-default",
	})
	if err != nil {
		t.Fatalf("default bootstrap: %v", err)
	}
	for _, name := range services.OnboardingAgentNames {
		a, ok := svc.Skills.Get(name)
		if !ok {
			t.Fatalf("default agent %s missing", name)
		}
		if a.ProjectID != defaultID {
			t.Fatalf("default agent %s projectId = %q", name, a.ProjectID)
		}
	}
	wf, ok := svc.WF.Get(res.WorkflowID)
	if !ok {
		t.Fatal("workflow missing")
	}
	for _, node := range wf.Graph.Nodes {
		if node.Config == nil {
			continue
		}
		prof, _ := node.Config["agent_profile"].(string)
		if strings.HasPrefix(prof, "综合") {
			t.Fatalf("workflow agent_profile still 综合*: %q", prof)
		}
	}
}

func TestOnboardingBootstrapCreatesTeamAndDefaultWorkflow(t *testing.T) {
	svc, projectID := newOnboardingHarness(t)
	res, err := svc.Bootstrap(projectID, services.OnboardingBootstrapRequest{
		AcpBackend:        "cursor",
		APIKey:            "test-key-cursor",
		GitCredentialType: "github_https",
		GitHubToken:       "ghp_test",
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
	if len(res.AgentIDs) != 6 {
		t.Fatalf("want 6 agents, got %v", res.AgentIDs)
	}
	if res.GroupName != services.FirstInstallGroupName {
		t.Fatalf("groupName = %q", res.GroupName)
	}

	shared := svc.SharedAgent.Get(projectID)
	if shared.Env["GRASP_CURSOR_API_KEY"] != "test-key-cursor" {
		t.Fatalf("shared env missing auth key: %+v", shared.Env)
	}
	if shared.Env["GITHUB_TOKEN"] != "ghp_test" {
		t.Fatalf("shared git token missing: %+v", shared.Env)
	}
	if shared.GitCredentialType != "github_https" {
		t.Fatalf("gitCredentialType = %q", shared.GitCredentialType)
	}
	if shared.Env["VNC_PREVIEW"] != "1" || shared.Env["BROWSER_MCP"] != "1" {
		t.Fatalf("preview flags default on: %+v", shared.Env)
	}

	for _, name := range services.OnboardingAgentNames {
		a, ok := svc.Skills.Get(name)
		if !ok {
			t.Fatalf("agent %s missing", name)
		}
		if a.ProjectID != projectID {
			t.Fatalf("agent %s projectId = %q", name, a.ProjectID)
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
	if wf.Name != services.OnboardingWorkflowName || wf.Status != "published" || !wf.NeedsRepo {
		t.Fatalf("workflow meta: name=%s status=%s needsRepo=%v", wf.Name, wf.Status, wf.NeedsRepo)
	}
	if !wf.ShowOnHome {
		t.Fatal("default workflow should be visible on Home")
	}
	assertDefaultWorkflowGraph(t, wf.Graph)
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
	svc, projectID := newOnboardingHarness(t)
	res, err := svc.Bootstrap(projectID, services.OnboardingBootstrapRequest{
		AcpBackend: "cursor",
		APIKey:     "k",
		RepoURL:    "https://github.com/org/web.git",
		RepoBranch: "develop",
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
	svc, projectID := newOnboardingHarness(t)
	off := false
	_, err := svc.Bootstrap(projectID, services.OnboardingBootstrapRequest{
		AcpBackend:   "cursor",
		APIKey:       "k",
		GitUserName:  "Ada Lovelace",
		GitUserEmail: "ada@example.com",
		VncPreview:   &off,
		BrowserMcp:   &off,
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	shared := svc.SharedAgent.Get(projectID)
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
	svc, projectID := newOnboardingHarness(t)
	res, err := svc.Bootstrap(projectID, services.OnboardingBootstrapRequest{
		AcpBackend: "cursor",
		APIKey:     "k",
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

func TestOnboardingBootstrapWritesCodeBuddyRegionToSharedOnly(t *testing.T) {
	svc, projectID := newOnboardingHarness(t)
	res, err := svc.Bootstrap(projectID, services.OnboardingBootstrapRequest{
		AcpBackend: "codebuddy",
		APIKey:     "cb-key",
		Region:     "internal",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	for _, name := range res.AgentIDs {
		a, ok := svc.Skills.Get(name)
		if !ok {
			t.Fatalf("agent %s missing", name)
		}
		if a.AcpBackend != "codebuddy" {
			t.Fatalf("agent %s backend = %q", name, a.AcpBackend)
		}
		if got := a.Env["GRASP_CODEBUDDY_REGION"]; got != "" {
			t.Fatalf("agent %s must not copy region, got %q", name, got)
		}
		if a.Layout.ConfigRoot != "/root/.codebuddy" {
			t.Fatalf("agent %s configRoot = %q", name, a.Layout.ConfigRoot)
		}
	}
	shared := svc.SharedAgent.Get(projectID)
	if shared.Env["GRASP_CODEBUDDY_REGION"] != "internal" {
		t.Fatalf("shared env missing region: %+v", shared.Env)
	}
}

func TestOnboardingBootstrapWritesOpenCodeEnvToShared(t *testing.T) {
	svc, projectID := newOnboardingHarness(t)
	res, err := svc.Bootstrap(projectID, services.OnboardingBootstrapRequest{
		AcpBackend:       "opencode",
		APIKey:           "sk-oc",
		OpenCodeProvider: "anthropic",
		OpenCodeBaseURL:  "https://proxy.example/v1",
		OpenCodeModel:    "anthropic/claude-sonnet-4-5",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	for _, name := range res.AgentIDs {
		a, ok := svc.Skills.Get(name)
		if !ok {
			t.Fatalf("agent %s missing", name)
		}
		if a.AcpBackend != "opencode" {
			t.Fatalf("agent %s backend = %q", name, a.AcpBackend)
		}
		if a.Env["GRASP_OPENCODE_API_KEY"] != "" {
			t.Fatalf("agent %s must not copy API key", name)
		}
		if a.Layout.ConfigRoot != "/root/.config/opencode" {
			t.Fatalf("agent %s configRoot = %q", name, a.Layout.ConfigRoot)
		}
	}
	shared := svc.SharedAgent.Get(projectID)
	if shared.Env["GRASP_OPENCODE_API_KEY"] != "sk-oc" {
		t.Fatalf("shared key: %+v", shared.Env)
	}
	if shared.Env["GRASP_OPENCODE_PROVIDER"] != "anthropic" {
		t.Fatalf("shared provider: %+v", shared.Env)
	}
	if shared.Env["GRASP_OPENCODE_BASE_URL"] != "https://proxy.example/v1" {
		t.Fatalf("shared base: %+v", shared.Env)
	}
	if shared.Env["ACP_BRIDGE_MODEL"] != "anthropic/claude-sonnet-4-5" {
		t.Fatalf("shared model: %+v", shared.Env)
	}
	if shared.Env["GRASP_OPENCODE_MODEL_VISION"] != "" {
		t.Fatalf("vision must stay off unless opted in: %+v", shared.Env)
	}
}

func TestOnboardingBootstrapWritesOpenCodeVisionEnv(t *testing.T) {
	svc, projectID := newOnboardingHarness(t)
	vision := true
	_, err := svc.Bootstrap(projectID, services.OnboardingBootstrapRequest{
		AcpBackend:          "opencode",
		APIKey:              "sk-oc",
		OpenCodeProvider:    "tencent-tokenhub",
		OpenCodeModel:       "deepseek/deepseek-flash",
		OpenCodeModelVision: &vision,
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	shared := svc.SharedAgent.Get(projectID)
	if shared.Env["GRASP_OPENCODE_MODEL_VISION"] != "1" {
		t.Fatalf("shared vision: %+v", shared.Env)
	}
}

func TestOnboardingBootstrapDefaultsPublicRegionForCodeBuddy(t *testing.T) {
	svc, projectID := newOnboardingHarness(t)
	_, err := svc.Bootstrap(projectID, services.OnboardingBootstrapRequest{
		AcpBackend: "codebuddy",
		APIKey:     "cb-key",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	shared := svc.SharedAgent.Get(projectID)
	if got := shared.Env["GRASP_CODEBUDDY_REGION"]; got != "public" {
		t.Fatalf("default region = %q, want public", got)
	}
}

func TestOnboardingBootstrapIdempotent(t *testing.T) {
	svc, projectID := newOnboardingHarness(t)
	req := services.OnboardingBootstrapRequest{AcpBackend: "cursor", APIKey: "k1"}
	r1, err := svc.Bootstrap(projectID, req)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := svc.WF.UpdateShowOnHome(r1.WorkflowID, false); err != nil {
		t.Fatalf("hide before second bootstrap: %v", err)
	}
	req.APIKey = "k2-rotated"
	r2, err := svc.Bootstrap(projectID, req)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if r1.WorkflowID != r2.WorkflowID {
		t.Fatalf("workflow id changed: %s vs %s", r1.WorkflowID, r2.WorkflowID)
	}
	if len(svc.Skills.List()) != 6 {
		t.Fatalf("agents doubled: %d", len(svc.Skills.List()))
	}
	if n := len(svc.WF.List(projectID)); n != 1 {
		t.Fatalf("workflows doubled: %d", n)
	}
	shared := svc.SharedAgent.Get(projectID)
	if shared.Env["GRASP_CURSOR_API_KEY"] != "k2-rotated" {
		t.Fatalf("auth not updated: %+v", shared.Env)
	}
	wf, ok := svc.WF.Get(r2.WorkflowID)
	if !ok || !wf.ShowOnHome {
		t.Fatalf("second bootstrap should restore Home visibility: ok=%v showOnHome=%v", ok, wf.ShowOnHome)
	}
}

func TestOnboardingBootstrapRejectsCrossProjectAgentConflict(t *testing.T) {
	svc, projectA := newOnboardingHarness(t)
	other, err := svc.Projects.Create("Other", "", nil, nil)
	if err != nil {
		t.Fatalf("create B: %v", err)
	}
	if err := svc.Skills.Save(services.Agent{
		Name:       services.OnboardingAgentNames[0],
		AcpBackend: "cursor",
		ProjectID:  other.ID,
		Env:        map[string]string{},
	}); err != nil {
		t.Fatalf("seed other: %v", err)
	}
	_, err = svc.Bootstrap(projectA, services.OnboardingBootstrapRequest{
		AcpBackend: "cursor",
		APIKey:     "key-a",
	})
	if !errors.Is(err, services.ErrOnboardingAgentConflict) {
		t.Fatalf("want ErrOnboardingAgentConflict, got %v", err)
	}
	if n := len(svc.WF.List(projectA)); n != 0 {
		t.Fatalf("must not get workflow on conflict, got %d", n)
	}
}

func TestOnboardingBootstrapAllowsClaimingUnboundAgents(t *testing.T) {
	svc, projectID := newOnboardingHarness(t)
	unbound := services.Agent{
		Name:       services.OnboardingAgentNames[0],
		AcpBackend: "cursor",
		ProjectID:  "",
		Files:      []services.AgentFile{{Path: "AGENTS.md", Content: "# unbound\n"}},
		Env:        map[string]string{},
	}
	if err := svc.Skills.Save(unbound); err != nil {
		t.Fatalf("seed unbound: %v", err)
	}
	res, err := svc.Bootstrap(projectID, services.OnboardingBootstrapRequest{
		AcpBackend: "cursor",
		APIKey:     "claim-key",
	})
	if err != nil {
		t.Fatalf("bootstrap should claim unbound: %v", err)
	}
	if len(res.AgentIDs) != 6 {
		t.Fatalf("want 6 agents, got %v", res.AgentIDs)
	}
	a, ok := svc.Skills.Get(services.OnboardingAgentNames[0])
	if !ok || a.ProjectID != projectID {
		t.Fatalf("agent not claimed: ok=%v projectId=%q", ok, a.ProjectID)
	}
}

func TestFirstInstallDefaultWorkflowValidates(t *testing.T) {
	svc, projectID := newOnboardingHarness(t)
	res, err := svc.Bootstrap(projectID, services.OnboardingBootstrapRequest{
		AcpBackend: "cursor",
		APIKey:     "k",
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
	for _, id := range []string{"input_d3s1", "grasp_7gl6", "implement_qnlc", "test_7qy3", "review_hfqm", "submit_mr_i46x", "output_mh48"} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("missing node %s", id)
		}
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

func TestCreateFromBaselineDefaultsShowOnHome(t *testing.T) {
	svc, projectID := newOnboardingHarness(t)
	wf, err := svc.CreateFromBaseline(services.CreateBaselineWorkflowRequest{
		ProjectID: projectID,
		Name:      "首页可见流水线",
		Repos:     []services.BaselineRepo{{URL: "https://github.com/acme/app.git"}},
	})
	if err != nil {
		t.Fatalf("CreateFromBaseline: %v", err)
	}
	if wf.Status != "published" || !wf.ShowOnHome {
		t.Fatalf("returned status=%s showOnHome=%v (plan g1.1)", wf.Status, wf.ShowOnHome)
	}
	stored, ok := svc.WF.Get(wf.ID)
	if !ok {
		t.Fatal("workflow not persisted")
	}
	if stored.Status != "published" || !stored.ShowOnHome {
		t.Fatalf("persisted status=%s showOnHome=%v (plan g1.1)", stored.Status, stored.ShowOnHome)
	}

	if _, err := svc.CreateFromBaseline(services.CreateBaselineWorkflowRequest{
		ProjectID: projectID,
		Name:      "首页可见流水线",
		Repos:     []services.BaselineRepo{{URL: "https://github.com/acme/app.git"}},
	}); !errors.Is(err, services.ErrWorkflowNameExists) {
		t.Fatalf("duplicate name: %v", err)
	}
	if n := len(svc.WF.List(projectID)); n != 1 {
		t.Fatalf("duplicate must not insert, got %d workflows", n)
	}

	if _, err := svc.CreateFromBaseline(services.CreateBaselineWorkflowRequest{
		ProjectID: projectID,
		Name:      "无仓库",
		Repos:     []services.BaselineRepo{{URL: "  "}},
	}); !errors.Is(err, services.ErrBaselineReposRequired) {
		t.Fatalf("empty repos: %v", err)
	}
	if n := len(svc.WF.List(projectID)); n != 1 {
		t.Fatalf("empty repos must not insert, got %d workflows", n)
	}
}

func TestOnboardingBootstrapDoesNotReuseSameNamedForeignGroup(t *testing.T) {
	svc, _ := newOnboardingHarness(t)
	org, err := svc.Org.Get()
	if err != nil {
		t.Fatalf("org: %v", err)
	}
	org.Groups = append(org.Groups, services.OrgGroup{ID: "g_foreign_same_name", Name: "支付中台项目组"})
	if _, err := svc.Org.Put(org, org.Revision); err != nil {
		t.Fatalf("seed org: %v", err)
	}

	p, err := svc.Projects.Create("支付中台", "", nil, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	res, err := svc.Bootstrap(p.ID, services.OnboardingBootstrapRequest{AcpBackend: "cursor", APIKey: "k-pay"})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if res.GroupName != "支付中台项目组" {
		t.Fatalf("groupName = %q", res.GroupName)
	}

	org2, err := svc.Org.Get()
	if err != nil {
		t.Fatalf("org2: %v", err)
	}
	wantID := "g_onb_" + p.ID
	var foundWant, agentsOnForeign bool
	for _, g := range org2.Groups {
		if g.ID == wantID {
			foundWant = true
		}
	}
	if !foundWant {
		t.Fatalf("expected new group %s, groups=%+v", wantID, org2.Groups)
	}
	for _, name := range res.AgentIDs {
		m := org2.Agents[name]
		for _, gid := range m.GroupIDs {
			if gid == "g_foreign_same_name" {
				agentsOnForeign = true
			}
		}
	}
	if agentsOnForeign {
		t.Fatal("agents must not join the foreign same-named group")
	}
}

func newOnboardingHarness(t *testing.T) (*services.OnboardingService, string) {
	t.Helper()
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "onboarding.db"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	projects := services.NewProjectService(db)
	projectID := projects.DefaultProjectID()
	if projectID == "" {
		t.Fatal("default project missing")
	}
	root := t.TempDir()
	skills := services.NewAgentService(root)
	org := services.NewOrgService(root, skills)
	wf := services.NewWorkflowService(db)
	return services.NewOnboardingService(projects, skills, services.NewSharedAgentService(t.TempDir()), wf, org), projectID
}
