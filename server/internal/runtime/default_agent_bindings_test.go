package runtime

import (
	"encoding/json"
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
)

func TestRuntimeAgentInheritanceDefaultsPreserveExplicitFalse(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		model, git  bool
	}{
		{"empty", `{}`, true, true},
		{"explicit defaults", `{"inheritModel":true,"inheritGit":true}`, true, true},
		{"stored bindings do not imply local", `{"acpBackend":"cursor","modelBinding":{"kind":"configuration","configurationId":"local"},"gitBindings":{"items":[{"credentialId":"local-git"}]}}`, true, true},
		{"local model", `{"inheritModel":false}`, false, true},
		{"local Git", `{"inheritGit":false}`, true, false},
		{"all local", `{"inheritModel":false,"inheritGit":false}`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var agent agentFile
			if err := json.Unmarshal([]byte(tc.input), &agent); err != nil {
				t.Fatal(err)
			}
			if agent.InheritModel != tc.model || agent.InheritGit != tc.git {
				t.Fatalf("inheritance = model:%v Git:%v, want model:%v Git:%v", agent.InheritModel, agent.InheritGit, tc.model, tc.git)
			}
			encoded, err := json.Marshal(agent)
			if err != nil {
				t.Fatal(err)
			}
			var roundTrip agentFile
			if err := json.Unmarshal(encoded, &roundTrip); err != nil {
				t.Fatal(err)
			}
			if roundTrip.InheritModel != tc.model || roundTrip.InheritGit != tc.git {
				t.Fatal("JSON round trip changed an explicit inheritance choice")
			}
		})
	}
	var agent agentFile
	if err := json.Unmarshal([]byte(`{"inheritModel":"false"}`), &agent); err == nil {
		t.Fatal("invalid inheritance flag was accepted")
	}
}

func TestRuntimeMissingInheritanceUsesCurrentDefaults(t *testing.T) {
	root := t.TempDir()
	writeProfileInto(t, root, "default", `{"acpBackend":"cursor","modelBinding":{"kind":"configuration","configurationId":"local"},"gitBindings":{"items":[{"credentialId":"local-git"}]}}`)
	shared := SharedAgentView{AcpBackend: "opencode", ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: "first"}, GitBindings: &models.GitBindings{Items: []models.GitCredentialBinding{{CredentialID: "default-git"}}}}
	c := &acpProvider{opts: Options{ProfilesRoot: root, SharedAgent: func() SharedAgentView { return shared }}}
	for _, workflow := range []string{"", "workflow"} {
		req := reqFor("default")
		req.WorkflowID = workflow
		got := c.effectiveAgent(req)
		if got.AcpBackend != shared.AcpBackend || got.ModelBinding != shared.ModelBinding || got.GitBindings != shared.GitBindings {
			t.Fatalf("workflow %q did not inherit current defaults: %+v", workflow, got)
		}
	}
	shared.ModelBinding = nil
	shared.GitBindings = nil
	got := c.effectiveAgent(reqFor("default"))
	if got.ModelBinding != nil || got.GitBindings != nil {
		t.Fatal("removed defaults fell back to stored local bindings")
	}
}

func TestRuntimeModelAndGitInheritanceIsExplicitAndIndependent(t *testing.T) {
	localModel := &modelrouting.Binding{Kind: "configuration", ConfigurationID: "local"}
	defaultModel := &modelrouting.Binding{Kind: "configuration", ConfigurationID: "default"}
	localGit := &models.GitBindings{Items: []models.GitCredentialBinding{{CredentialID: "local-git"}}, PreferredCredentialID: "local-git"}
	defaultGit := &models.GitBindings{Items: []models.GitCredentialBinding{{CredentialID: "default-git"}}, PreferredCredentialID: "default-git"}
	shared := SharedAgentView{AcpBackend: "opencode", ModelBinding: defaultModel, GitBindings: defaultGit}
	for _, model := range []bool{false, true} {
		for _, git := range []bool{false, true} {
			local := agentFile{InheritModel: model, InheritGit: git, AcpBackend: "cursor", ModelBinding: localModel, GitBindings: localGit, Layout: agentLayout{ConfigRoot: "/root/.cursor"}}
			effective := overlayAgentFile(shared, local)
			wantModel, wantBackend, wantGit := localModel, "cursor", localGit
			if model {
				wantModel, wantBackend = defaultModel, "opencode"
			}
			if git {
				wantGit = defaultGit
			}
			if effective.ModelBinding != wantModel || effective.AcpBackend != wantBackend || effective.GitBindings != wantGit {
				t.Fatal("model/Git inheritance switches were not independent")
			}
			if model && effective.Layout.ConfigRoot != "/root/.config/opencode" {
				t.Fatal("model CLI did not update its default config directory")
			}
			missing := overlayAgentFile(SharedAgentView{}, local)
			if model && (missing.ModelBinding != nil || missing.AcpBackend != "") {
				t.Fatal("missing default fell back to local model")
			}
			if git && missing.GitBindings != nil {
				t.Fatal("missing default fell back to local Git")
			}
		}
	}
}

func TestRegistryInheritedModelUsesCurrentDefaultCLI(t *testing.T) {
	root := t.TempDir()
	writeProfileInto(t, root, "inherits", `{"acpBackend":"cursor","modelBinding":{"kind":"configuration","configurationId":"local"}}`)
	writeProfileInto(t, root, "local", `{"inheritModel":false,"inheritGit":false,"acpBackend":"cursor"}`)
	shared := SharedAgentView{AcpBackend: "opencode", ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: "default"}}
	registry := NewProviderRegistry(mcp.NewHost(newMemStore()), Options{ProfilesRoot: root, SharedAgent: func() SharedAgentView { return shared }})
	if got, err := registry.backendFor(reqFor("inherits")); err != nil || got != BackendOpenCode {
		t.Fatalf("inherited route=%s err=%v", got, err)
	}
	shared.AcpBackend = "claude_code"
	if got, err := registry.backendFor(reqFor("inherits")); err != nil || got != BackendClaudeCode {
		t.Fatalf("changed default route=%s err=%v", got, err)
	}
	if got, err := registry.backendFor(reqFor("local")); err != nil || got != BackendCursor {
		t.Fatal("default changed a non-inheriting Agent")
	}
	shared.ModelBinding = nil
	if _, err := registry.backendFor(reqFor("inherits")); err == nil {
		t.Fatal("registry fell back to local CLI after default removed")
	}
}

func TestInheritedModelSiteDoesNotUseLocalSiteAliases(t *testing.T) {
	local := agentFile{InheritModel: true, Env: map[string]string{EnvCodeBuddyRegion: "internal", EnvCodeBuddyInternet: "internal", EnvTraeCLIHost: "https://local.example", EnvTraeRegion: "cn", "FEATURE": "local"}}
	shared := SharedAgentView{Env: map[string]string{EnvCodeBuddyRegion: "public", EnvTraeRegion: "intl", "FEATURE": "shared"}}
	effective := overlayAgentFile(shared, local)
	if effective.Env[EnvCodeBuddyRegion] != "public" || effective.Env[EnvTraeRegion] != "intl" || effective.Env[EnvCodeBuddyInternet] != "" || effective.Env[EnvTraeCLIHost] != "" || effective.Env["FEATURE"] != "local" {
		t.Fatal("local site aliases redirected inherited authorization")
	}
	if local.Env[EnvCodeBuddyInternet] != "internal" || local.Env[EnvTraeCLIHost] != "https://local.example" {
		t.Fatal("local settings mutated")
	}
}

func TestRunAgentInheritedDefaultsReprimeSharedSandbox(t *testing.T) {
	h := newSharedHarness(t, nil)
	writeProfileInto(t, h.p.opts.ProfilesRoot, testAgentProfile, `{"acpBackend":"cursor"}`)
	shared := SharedAgentView{AcpBackend: "cursor", ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: "first"}, GitBindings: &models.GitBindings{Items: []models.GitCredentialBinding{{CredentialID: "gh"}}, PreferredCredentialID: "gh"}}
	h.p.opts.SharedAgent = func() SharedAgentView { return shared }
	h.p.opts.GitCredential = selectedGitTestValue
	h.p.opts.ModelBinding = func(backend string, binding *modelrouting.Binding) (modelrouting.Resolved, error) {
		r, err := testModelBinding(backend, binding)
		if err == nil {
			r.Binding.ModelID = binding.ConfigurationID
		}
		return r, err
	}
	req := h.req("n1", models.SandboxModeShared)
	req.WorkflowID = "workflow"
	h.run(t, req)
	shared.ModelBinding = &modelrouting.Binding{Kind: "configuration", ConfigurationID: "second"}
	shared.GitBindings = &models.GitBindings{Items: []models.GitCredentialBinding{{CredentialID: "gl"}}, PreferredCredentialID: "gl"}
	h.run(t, req)
	if h.mgr.createCount() != 1 || h.mgr.reprimeCount() != 1 {
		t.Fatal("changed inherited defaults did not re-prime the existing shared sandbox")
	}
	spec := h.mgr.reprimes[0]
	if spec.Env[EnvACPBridgeModel] != "second" || len(spec.GitCredentials) != 1 || spec.GitCredentials[0].ID != "gl" {
		t.Fatal("shared node kept previous model/Git authorization")
	}
	shared.GitBindings = nil
	h.run(t, req)
	if h.mgr.reprimeCount() != 2 || len(h.mgr.reprimes[1].GitCredentials) != 0 {
		t.Fatal("empty default Git selection retained previous permissions")
	}
}
