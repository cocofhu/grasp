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
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestOnboardingBootstrapRequiresAPIKey(t *testing.T) {
	svc := newOnboardingHarness(t)
	_, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		AcpBackend: "cursor",
		APIKey:     "  ",
	})
	if err != services.ErrOnboardingAPIKeyRequired {
		t.Fatalf("want ErrOnboardingAPIKeyRequired, got %v", err)
	}
	if n := len(svc.Skills.List()); n != 0 {
		t.Fatalf("no agents should be created without key, got %d", n)
	}
	if n := len(svc.WF.List()); n != 0 {
		t.Fatalf("no workflows should be created without key, got %d", n)
	}
}

func TestOnboardingBootstrapCreatesTeamAndDefaultWorkflow(t *testing.T) {
	svc := newOnboardingHarness(t)
	res, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
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
	if got := strings.Join(res.AgentIDs, ","); got != "需求澄清,实现,测试评审,交付" {
		t.Fatalf("want 4 engineers in workflow order, got %v", res.AgentIDs)
	}

	shared := svc.SharedAgent.Get()
	creds := svc.Credentials.ResolveEnv()
	if creds["GRASP_CURSOR_API_KEY"] != "test-key-cursor" {
		t.Fatalf("workspace credentials missing auth key: %+v", creds)
	}
	if creds["GITHUB_TOKEN"] != "ghp_test" {
		t.Fatalf("workspace git token missing: %+v", creds)
	}
	if _, ok := shared.Env["GRASP_CURSOR_API_KEY"]; ok {
		t.Fatalf("shared env must not store auth key: %+v", shared.Env)
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
	svc := newOnboardingHarness(t)
	res, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
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
	svc := newOnboardingHarness(t)
	off := false
	_, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
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
	svc := newOnboardingHarness(t)
	res, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
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
	shared := svc.SharedAgent.Get()
	if shared.Env["GRASP_CODEBUDDY_REGION"] != "internal" {
		t.Fatalf("shared env missing region: %+v", shared.Env)
	}
}

func TestOnboardingBootstrapWritesOpenCodeEnvToShared(t *testing.T) {
	svc := newOnboardingHarness(t)
	res, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
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
	shared := svc.SharedAgent.Get()
	selected := svc.Credentials.ResolveOpenCodeCredential("cred-builtin-opencode")
	if got := selected["GRASP_OPENCODE_API_KEY"]; got != "sk-oc" {
		t.Fatalf("workspace credential key = %q", got)
	}
	for _, name := range res.AgentIDs {
		a, ok := svc.Skills.Get(name)
		if !ok {
			t.Fatalf("agent %s missing", name)
		}
		if a.OpenCodeCredentialID != "cred-builtin-opencode" {
			t.Fatalf("agent %s credential id = %q", name, a.OpenCodeCredentialID)
		}
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
	svc := newOnboardingHarness(t)
	vision := true
	_, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		AcpBackend:          "opencode",
		APIKey:              "sk-oc",
		OpenCodeProvider:    "tencent-tokenhub",
		OpenCodeModel:       "deepseek/deepseek-flash",
		OpenCodeModelVision: &vision,
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	shared := svc.SharedAgent.Get()
	if shared.Env["GRASP_OPENCODE_MODEL_VISION"] != "1" {
		t.Fatalf("shared vision: %+v", shared.Env)
	}
}

func TestOnboardingBootstrapDefaultsPublicRegionForCodeBuddy(t *testing.T) {
	svc := newOnboardingHarness(t)
	_, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		AcpBackend: "codebuddy",
		APIKey:     "cb-key",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	shared := svc.SharedAgent.Get()
	if got := shared.Env["GRASP_CODEBUDDY_REGION"]; got != "public" {
		t.Fatalf("default region = %q, want public", got)
	}
}

func TestOnboardingBootstrapIdempotent(t *testing.T) {
	svc := newOnboardingHarness(t)
	req := services.OnboardingBootstrapRequest{AcpBackend: "cursor", APIKey: "k1"}
	r1, err := svc.Bootstrap(req)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := svc.WF.UpdateShowOnHome(r1.WorkflowID, false); err != nil {
		t.Fatalf("hide before second bootstrap: %v", err)
	}
	req.APIKey = "k2-rotated"
	r2, err := svc.Bootstrap(req)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if r1.WorkflowID != r2.WorkflowID {
		t.Fatalf("workflow id changed: %s vs %s", r1.WorkflowID, r2.WorkflowID)
	}
	if len(svc.Skills.List()) != len(services.OnboardingAgentNames) {
		t.Fatalf("agents doubled: %d", len(svc.Skills.List()))
	}
	if n := len(svc.WF.List()); n != 1 {
		t.Fatalf("workflows doubled: %d", n)
	}
	if got := svc.Credentials.ResolveEnv()["GRASP_CURSOR_API_KEY"]; got != "k2-rotated" {
		t.Fatalf("auth not updated: %q", got)
	}
	wf, ok := svc.WF.Get(r2.WorkflowID)
	if !ok || !wf.ShowOnHome {
		t.Fatalf("second bootstrap should restore Home visibility: ok=%v showOnHome=%v", ok, wf.ShowOnHome)
	}
}

func TestOnboardingBootstrapLanguageSwitchReusesWorkflow(t *testing.T) {
	svc := newOnboardingHarness(t)
	req := services.OnboardingBootstrapRequest{AcpBackend: "cursor", APIKey: "k", Language: "en"}
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

func TestOnboardingBootstrapRenamesAndSetsModel(t *testing.T) {
	svc := newOnboardingHarness(t)
	res, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		AcpBackend: "cursor",
		APIKey:     "k",
		Agents: []services.OnboardingAgentChoice{
			{TemplateID: "test_review", Name: "实现"},
			{TemplateID: "clarify", Name: " 产品澄清 "},
			{TemplateID: "implement", Name: "编码", Model: "gpt-5"},
		},
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if got := strings.Join(res.AgentIDs, ","); got != "产品澄清,编码,实现" {
		t.Fatalf("agents in workflow order = %s", got)
	}
	impl, ok := svc.Skills.Get("编码")
	if !ok || impl.Env["ACP_BRIDGE_MODEL"] != "gpt-5" {
		t.Fatalf("implement model env = %#v", impl.Env)
	}
	if clarify, _ := svc.Skills.Get("产品澄清"); clarify.Env["ACP_BRIDGE_MODEL"] != "" {
		t.Fatalf("clarify must not get a model: %#v", clarify.Env)
	}
	wf, _ := svc.WF.Get(res.WorkflowID)
	profiles := map[string]string{}
	for _, n := range wf.Graph.Nodes {
		profiles[n.ID] = models.AgentProfile(n.Config)
	}
	if profiles["clarify"] != "产品澄清" || profiles["implement"] != "编码" || profiles["test_review"] != "实现" {
		t.Fatalf("profiles = %v", profiles)
	}
}

func TestOnboardingBootstrapWritesCliModelToSharedEnv(t *testing.T) {
	svc := newOnboardingHarness(t)
	_, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		AcpBackend: "cursor",
		APIKey:     "k",
		CliModel:   " composer-2.5 ",
		Agents: []services.OnboardingAgentChoice{
			{TemplateID: "clarify", Name: "需求澄清"},
			{TemplateID: "implement", Name: "实现", Model: "gpt-5"},
		},
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	shared := svc.SharedAgent.Get()
	if shared.Env["ACP_BRIDGE_MODEL"] != "composer-2.5" {
		t.Fatalf("shared model = %q", shared.Env["ACP_BRIDGE_MODEL"])
	}
	impl, ok := svc.Skills.Get("实现")
	if !ok || impl.Env["ACP_BRIDGE_MODEL"] != "gpt-5" {
		t.Fatalf("agent override = %#v ok=%v", impl.Env, ok)
	}
	clarify, ok := svc.Skills.Get("需求澄清")
	if !ok || clarify.Env["ACP_BRIDGE_MODEL"] != "" {
		t.Fatalf("unset agent must not copy the shared model: %#v", clarify.Env)
	}
	inherited := services.ExtendOverlay(shared, clarify)
	if inherited.Env["ACP_BRIDGE_MODEL"] != "composer-2.5" {
		t.Fatalf("unset agent should inherit the shared model, got %#v", inherited.Env)
	}
	overridden := services.ExtendOverlay(shared, impl)
	if overridden.Env["ACP_BRIDGE_MODEL"] != "gpt-5" {
		t.Fatalf("agent model should override the shared model, got %#v", overridden.Env)
	}
}

func TestOnboardingBootstrapClearsCliModelWhenAuto(t *testing.T) {
	svc := newOnboardingHarness(t)
	shared := svc.SharedAgent.Get()
	shared.Env = map[string]string{"ACP_BRIDGE_MODEL": "old-model", "KEEP": "1"}
	if err := svc.SharedAgent.Save(shared); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		AcpBackend: "claude_code",
		APIKey:     "k",
		CliModel:   "   ",
	}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	got := svc.SharedAgent.Get()
	if _, ok := got.Env["ACP_BRIDGE_MODEL"]; ok {
		t.Fatalf("auto must delete the pinned model: %+v", got.Env)
	}
	if got.Env["KEEP"] != "1" {
		t.Fatalf("other env must stay: %+v", got.Env)
	}
}

func TestOnboardingBootstrapCliModelKeepsCodeBuddyRegion(t *testing.T) {
	svc := newOnboardingHarness(t)
	_, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		AcpBackend: "codebuddy",
		APIKey:     "cb-key",
		Region:     "public",
		CliModel:   "gpt-5",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	shared := svc.SharedAgent.Get()
	if shared.Env["ACP_BRIDGE_MODEL"] != "gpt-5" {
		t.Fatalf("shared model = %+v", shared.Env)
	}
	if shared.Env["GRASP_CODEBUDDY_REGION"] != "public" {
		t.Fatalf("region = %+v", shared.Env)
	}
}

func TestOnboardingBootstrapIgnoresCliModelForOpenCode(t *testing.T) {
	svc := newOnboardingHarness(t)
	_, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		AcpBackend:       "opencode",
		APIKey:           "sk-oc",
		OpenCodeProvider: "anthropic",
		OpenCodeModel:    "anthropic/claude-sonnet-4-5",
		CliModel:         "composer-2.5",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	got := svc.SharedAgent.Get()
	if got.Env["ACP_BRIDGE_MODEL"] != "anthropic/claude-sonnet-4-5" {
		t.Fatalf("opencode must use openCodeModel, got %+v", got.Env)
	}
}

func TestOnboardingBootstrapOpenCodeDoesNotClearModelViaCliModel(t *testing.T) {
	svc := newOnboardingHarness(t)
	shared := svc.SharedAgent.Get()
	shared.Env = map[string]string{"ACP_BRIDGE_MODEL": "keep-me"}
	if err := svc.SharedAgent.Save(shared); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		AcpBackend: "opencode",
		APIKey:     "sk-oc",
	}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	got := svc.SharedAgent.Get()
	if got.Env["ACP_BRIDGE_MODEL"] != "keep-me" {
		t.Fatalf("opencode must ignore an empty cliModel, got %+v", got.Env)
	}
}

func TestOnboardingBootstrapEnglishNamesLabelTheWorkflow(t *testing.T) {
	svc := newOnboardingHarness(t)
	res, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		AcpBackend: "cursor",
		APIKey:     "k",
		Language:   "en",
		Agents: []services.OnboardingAgentChoice{
			{TemplateID: "clarify", Name: "Clarify"},
			{TemplateID: "implement", Name: "Implement"},
			{TemplateID: "test_review", Name: "TestReview"},
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
		"clarify": "Clarify", "implement": "Implement", "test_review": "TestReview",
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

func TestOnboardingBootstrapWithoutTestReview(t *testing.T) {
	svc := newOnboardingHarness(t)
	res, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		AcpBackend: "cursor",
		APIKey:     "k",
		Agents: []services.OnboardingAgentChoice{
			{TemplateID: "clarify"},
			{TemplateID: "implement"},
		},
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if got := strings.Join(res.AgentIDs, ","); got != "需求澄清,实现" {
		t.Fatalf("agents = %s", got)
	}
	if _, ok := svc.Skills.Get("测试评审"); ok {
		t.Fatal("unchecked template must not be saved")
	}
	wf, _ := svc.WF.Get(res.WorkflowID)
	g := wf.Graph
	if err := g.Validate(); err != nil {
		t.Fatalf("graph invalid: %v", err)
	}
	for _, n := range g.Nodes {
		if n.ID == "test_review" {
			t.Fatal("test_review node must be dropped")
		}
		if n.Type == "output" {
			for _, r := range n.Config["results"].([]any) {
				if strings.Contains(r.(string), "test_review") {
					t.Fatalf("output still reads test_review: %v", r)
				}
			}
		}
	}
	var direct bool
	for _, e := range g.Edges {
		if e.Source == "test_review" || e.Target == "test_review" {
			t.Fatalf("dangling edge %+v", e)
		}
		if e.Source == "implement" && e.Target == "output" && e.SourceHandle == "" {
			direct = true
		}
	}
	if !direct || len(g.Edges) != 3 {
		t.Fatalf("want input→clarify→implement→output, got %+v", g.Edges)
	}
}

func TestOnboardingBootstrapDeliverWithoutTestReview(t *testing.T) {
	svc := newOnboardingHarness(t)
	res, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		AcpBackend: "cursor",
		APIKey:     "k",
		Agents: []services.OnboardingAgentChoice{
			{TemplateID: "clarify"},
			{TemplateID: "implement"},
			{TemplateID: "deliver"},
		},
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if got := strings.Join(res.AgentIDs, ","); got != "需求澄清,实现,交付" {
		t.Fatalf("agents = %s", got)
	}
	wf, _ := svc.WF.Get(res.WorkflowID)
	g := wf.Graph
	if err := g.Validate(); err != nil {
		t.Fatalf("graph invalid: %v", err)
	}
	var implDeliver, deliverOut bool
	for _, e := range g.Edges {
		if e.Source == "implement" && e.Target == "deliver" && e.SourceHandle == "" {
			implDeliver = true
		}
		if e.Source == "deliver" && e.Target == "output" {
			deliverOut = true
		}
	}
	if !implDeliver || !deliverOut || len(g.Edges) != 4 {
		t.Fatalf("want input→clarify→implement→deliver→output, got %+v", g.Edges)
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
			_, err := svc.Bootstrap(services.OnboardingBootstrapRequest{AcpBackend: "cursor", APIKey: "k", Agents: team})
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
		AcpBackend: "cursor", APIKey: "k",
		Agents: []services.OnboardingAgentChoice{{TemplateID: "clarify", Name: "a/b"}, {TemplateID: "implement"}},
	})
	if !errors.Is(err, services.ErrInvalidAgentName) {
		t.Fatalf("want ErrInvalidAgentName, got %v", err)
	}
}

func TestCreateFromBaselineDefaultsShowOnHome(t *testing.T) {
	svc := newOnboardingHarness(t)
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

	if _, err := svc.CreateFromBaseline(services.CreateBaselineWorkflowRequest{
		Name:  "首页可见工作流",
		Repos: []services.BaselineRepo{{URL: "https://github.com/acme/app.git"}},
	}); !errors.Is(err, services.ErrWorkflowNameExists) {
		t.Fatalf("duplicate name: %v", err)
	}
	if n := len(svc.WF.List()); n != 1 {
		t.Fatalf("duplicate must not insert, got %d workflows", n)
	}

	if _, err := svc.CreateFromBaseline(services.CreateBaselineWorkflowRequest{
		Name:  "无仓库",
		Repos: []services.BaselineRepo{{URL: "  "}},
	}); !errors.Is(err, services.ErrBaselineReposRequired) {
		t.Fatalf("empty repos: %v", err)
	}
	if n := len(svc.WF.List()); n != 1 {
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

func TestCreateFromBaselineCreatesAgentsWithSharedBackend(t *testing.T) {
	svc := newOnboardingHarness(t)
	shared := svc.SharedAgent.Get()
	shared.AcpBackend = services.AcpBackendCodex
	if err := svc.SharedAgent.Save(shared); err != nil {
		t.Fatalf("save shared: %v", err)
	}
	wf, err := svc.CreateFromBaseline(services.CreateBaselineWorkflowRequest{
		Name:  "db-workbench",
		Repos: []services.BaselineRepo{{URL: "https://github.com/acme/app.git"}},
	})
	if err != nil {
		t.Fatalf("CreateFromBaseline: %v", err)
	}
	profiles := baselineAgentProfiles(t, wf)
	for _, role := range services.TeamEngineerTemplates {
		want := role.RoleLabelZH
		if profiles[role.ID] != want {
			t.Fatalf("node %s bound to %q, want %q", role.ID, profiles[role.ID], want)
		}
		a, ok := svc.Skills.Get(want)
		if !ok || a.AcpBackend != services.AcpBackendCodex {
			t.Fatalf("agent %s = %+v (ok=%v)", want, a, ok)
		}
		if a.Layout.ConfigRoot != "/root/.codex" {
			t.Fatalf("agent %s configRoot = %q", want, a.Layout.ConfigRoot)
		}
	}
}

func TestCreateFromBaselineReusesExistingAgents(t *testing.T) {
	svc := newOnboardingHarness(t)
	if _, err := svc.Bootstrap(services.OnboardingBootstrapRequest{AcpBackend: "cursor", APIKey: "k"}); err != nil {
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
	return services.NewOnboardingService(skills, services.NewSharedAgentService(t.TempDir()), wf, creds)
}

func TestOnboardingBootstrapDeepSeekHarnessUsesIndependentCredential(t *testing.T) {
	svc := newOnboardingHarness(t)
	result, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
		AcpBackend: services.AcpBackendDeepSeekHarness,
		APIKey:     "test-deepseek-key",
		CliModel:   "deepseek-v4-pro",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Published || len(result.AgentIDs) == 0 {
		t.Fatal("DeepSeek Harness onboarding did not publish the team")
	}
	credentials := svc.Credentials.ResolveEnv()
	if credentials["GRASP_DEEPSEEK_API_KEY"] != "test-deepseek-key" || credentials["GRASP_OPENCODE_API_KEY"] != "" {
		t.Fatal("DeepSeek key was not stored in its independent credential slot")
	}
	shared := svc.SharedAgent.Get()
	if shared.AcpBackend != services.AcpBackendDeepSeekHarness || shared.Env["ACP_BRIDGE_MODEL"] != "deepseek-v4-pro" {
		t.Fatalf("incorrect shared backend/model: %+v", shared)
	}
	for _, name := range result.AgentIDs {
		agent, ok := svc.Skills.Get(name)
		if !ok || agent.AcpBackend != services.AcpBackendDeepSeekHarness || agent.Layout.ConfigRoot != "/root/.dsh" || agent.OpenCodeCredentialID != "" {
			t.Fatalf("incorrect generated agent %q", name)
		}
		if agent.Env["GRASP_DEEPSEEK_API_KEY"] != "" || agent.Env["DEEPSEEK_API_KEY"] != "" {
			t.Fatalf("agent %q persisted a credential", name)
		}
	}
}
