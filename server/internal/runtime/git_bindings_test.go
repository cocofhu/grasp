package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
)

func gitSelection(ids ...string) *models.GitBindings {
	items := make([]models.GitCredentialBinding, 0, len(ids))
	for _, id := range ids {
		items = append(items, models.GitCredentialBinding{CredentialID: id})
	}
	preferred := ""
	if len(ids) > 0 {
		preferred = ids[0]
	}
	return &models.GitBindings{Items: items, PreferredCredentialID: preferred}
}

func selectedGitTestValue(id string) (GitCredentialValue, bool) {
	values := map[string][2]string{
		"gh": {envauth.EnvGitHubToken, "gh-secret"}, "gh2": {envauth.EnvGitHubToken, "gh2-secret"},
		"gl": {envauth.EnvGitLabToken, "gl-secret"}, "ssh": {envauth.EnvGitSSHPrivateKey, "SSH\nPRIVATE KEY"},
		"ssh2": {envauth.EnvGitSSHPrivateKey, "SECOND KEY"}, "ssh-bare": {envauth.EnvGitSSHPrivateKey, "BARE KEY"}, "model": {envauth.EnvCursorAPIKey, "model-secret"},
		"empty": {envauth.EnvGitHubToken, " "}, "newline": {envauth.EnvGitHubToken, "token\nother"}, "nul": {envauth.EnvGitLabToken, "token\x00other"},
	}
	row, ok := values[id]
	host := ""
	if row[0] == envauth.EnvGitHubToken {
		host = "https://github.com"
	} else if row[0] == envauth.EnvGitLabToken {
		host = "https://gitlab.com"
	}
	knownHosts := map[string]string{"ssh": "a.example ssh-ed25519 AAAA", "ssh2": "b.example ssh-ed25519 BBBB", "gh": "ignored for HTTPS"}[id]
	return GitCredentialValue{EnvKey: row[0], Secret: row[1], Host: host, KnownHosts: knownHosts}, ok
}

func TestGitBindingsResolveAllAndClearGlobalAuthentication(t *testing.T) {
	selection := gitSelection("gh", "gh2", "gl", "ssh")
	selection.PreferredCredentialID = "gh2"
	env := map[string]string{"GITHUB_TOKEN": "unselected", "GITLAB_TOKEN": "unselected", "GH_TOKEN": "unselected", "GITLAB_URL": "https://old", "GIT_REPOS": "repo|https://github.com/a/b", "FEATURE": "keep"}
	creds := map[string]string{"GIT_SSH_PRIVATE_KEY": "unselected", "GIT_SSH_KNOWN_HOSTS": "unselected", "GRASP_CURSOR_API_KEY": "keep-model"}
	rows, err := ApplyGitBindings(env, creds, selection, selectedGitTestValue)
	if err != nil || len(rows) != 4 || rows[0].Preferred || rows[1].Preferred || rows[0].ID != "gh" || rows[1].ID != "gh2" || rows[3].KnownHosts == "" || rows[3].Secret != "SSH\nPRIVATE KEY" {
		t.Fatalf("catalog = %+v, %v", rows, err)
	}
	for _, values := range []map[string]string{env, creds} {
		for key := range values {
			if IsGitAuthorizationEnvKey(key) {
				t.Fatalf("legacy key survived: %s", key)
			}
		}
	}
	if env["FEATURE"] != "keep" || env["GIT_REPOS"] == "" || creds["GRASP_CURSOR_API_KEY"] != "keep-model" {
		t.Fatal("non-Git values changed")
	}
	global := map[string]string{"GITHUB_TOKEN": "global", "SSH_AUTH_SOCK": "/tmp/agent.sock"}
	if rows, err := ApplyGitBindings(global, nil, nil, nil); err != nil || rows == nil || len(rows) != 0 || len(global) != 0 {
		t.Fatalf("omitted selection retained global authentication: %+v, %v", rows, err)
	}
	if rows, err := ApplyGitBindings(global, nil, gitSelection(), nil); err != nil || rows == nil || len(rows) != 0 || len(global) != 0 {
		t.Fatalf("explicit none = %+v, %v", rows, err)
	}
	for _, id := range []string{"gone", "model", "empty", "newline", "nul"} {
		selection := gitSelection("gh", id)
		selection.PreferredCredentialID = id
		if _, err := ResolveGitBindings(selection, selectedGitTestValue); err == nil || strings.Contains(err.Error(), "-secret") {
			t.Fatalf("invalid %s did not fail safely: %v", id, err)
		}
	}
	if _, err := ResolveGitBindings(gitSelection("gh"), nil); err == nil {
		t.Fatal("missing resolver accepted")
	}
	if rows, err := ResolveGitBindings(&models.GitBindings{Items: []models.GitCredentialBinding{{CredentialID: "gh"}}}, selectedGitTestValue); err != nil || len(rows) != 1 || rows[0].ID != "gh" || rows[0].Preferred {
		t.Fatalf("selection without a preferred credential = %+v, %v", rows, err)
	}
	for _, tc := range []struct{ id, scope string }{{"ssh", "https://host/repo"}, {"gh", "git@host:repo"}} {
		selection := gitSelection(tc.id)
		selection.Items[0].Scope = tc.scope
		if _, err := ResolveGitBindings(selection, selectedGitTestValue); err == nil {
			t.Fatalf("bad scope accepted: %+v", tc)
		}
	}
}

func TestGitBindingsSSHKnownHostsComeFromEachKey(t *testing.T) {
	selection := gitSelection("ssh", "ssh2", "ssh-bare", "gh")
	selection.Items[0].Scope = "git@a.example:team/x"
	rows, err := ResolveGitBindings(selection, selectedGitTestValue)
	if err != nil || len(rows) != 4 {
		t.Fatalf("rows = %+v, %v", rows, err)
	}
	if rows[0].KnownHosts != "a.example ssh-ed25519 AAAA" || rows[1].KnownHosts != "b.example ssh-ed25519 BBBB" || rows[2].KnownHosts != "" || rows[2].Secret != "BARE KEY" {
		t.Fatalf("known hosts mixed between SSH keys: %+v", rows)
	}
	if rows[3].KnownHosts != "" {
		t.Fatal("HTTPS authorization received known hosts")
	}
}

func TestGitBindingsWorkflowSpecUsesSelectedCatalog(t *testing.T) {
	root := writeAgent(t, "dev", `{"acpBackend":"cursor","gitBindings":{"items":[{"credentialId":"gl","scope":"https://gitlab.com/team/repo"},{"credentialId":"gh"}],"preferredCredentialId":"gh"},"credentialBindings":{"GITLAB_TOKEN":"gl"},"env":{"GITLAB_URL":"https://unselected"}}`)
	p := newACPProvider(mcp.NewHost(newMemStore()), Options{ProfilesRoot: root, GitCredential: selectedGitTestValue, ModelBinding: testModelBinding}).(*acpProvider)
	req := NodeReq{RunID: "git-run", NodeID: "n", Token: "token", Config: map[string]any{"agent_profile": "dev"}, Vars: map[string]any{"repos": `[{"name":"repo","url":"https://gitlab.com/team/repo.git"}]`, "GITLAB_URL": "https://other"}}
	spec, err := p.spec(req)
	if err != nil {
		t.Fatal(err)
	}
	defer removeHome(spec.ConfigHome)
	if !spec.GitBindingsConfigured || len(spec.GitCredentials) != 2 || spec.GitCredentials[0].ID != "gl" || spec.Env["GITLAB_TOKEN"] != "" || spec.Env["GITLAB_URL"] != "" {
		t.Fatalf("spec = %+v", spec)
	}
	p.opts.GitCredential = func(string) (GitCredentialValue, bool) { return GitCredentialValue{}, false }
	if _, err := p.spec(req); err == nil {
		t.Fatal("invalid selected authorization did not block workflow sandbox")
	}
}

func TestGitBindingsSharedWorkflowReprimesAllSelectedAndClearsPreviousAgent(t *testing.T) {
	h := newSharedHarness(t, nil)
	h.p.opts.GitCredential = selectedGitTestValue
	root := h.p.opts.ProfilesRoot
	write := func(name string, bindings *models.GitBindings) {
		t.Helper()
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(agentFile{AcpBackend: "cursor", GitBindings: bindings, ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: "test-model"}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "agent.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("agent-a", gitSelection("gh", "gl"))
	write("agent-b", gitSelection("gh2"))
	write("agent-none", gitSelection())
	req := h.req("first")
	req.Config["agent_profile"] = "agent-a"
	h.run(t, req)
	req = h.req("second")
	req.Config["agent_profile"] = "agent-b"
	h.run(t, req)
	if h.mgr.createCount() != 1 || h.mgr.reprimeCount() != 1 {
		t.Fatalf("creates=%d reprimes=%d", h.mgr.createCount(), h.mgr.reprimeCount())
	}
	if got := h.mgr.reprimes[0]; !got.GitBindingsConfigured || len(got.GitCredentials) != 1 || got.GitCredentials[0].ID != "gh2" || got.Env["GITHUB_TOKEN"] != "" {
		t.Fatalf("Agent B authorization leaked: %+v", got.GitCredentials)
	}
	req = h.req("third")
	req.Config["agent_profile"] = "agent-none"
	h.run(t, req)
	if got := h.mgr.reprimes[1]; !got.GitBindingsConfigured || len(got.GitCredentials) != 0 {
		t.Fatal("explicit empty Agent retained the previous catalog")
	}
	if !reflect.DeepEqual(h.mgr.reprimes[0].GitCredentials[0].ID, "gh2") {
		t.Fatal("later prime mutated earlier selection")
	}
}
