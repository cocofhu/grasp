package runtime

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
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
		"hosts": {envauth.EnvGitSSHKnownHosts, "host ssh-ed25519 AAAA"}, "model": {envauth.EnvCursorAPIKey, "model-secret"},
		"empty": {envauth.EnvGitHubToken, " "}, "newline": {envauth.EnvGitHubToken, "token\nother"}, "nul": {envauth.EnvGitLabToken, "token\x00other"},
	}
	row, ok := values[id]
	host := ""
	if row[0] == envauth.EnvGitHubToken {
		host = "https://github.com"
	} else if row[0] == envauth.EnvGitLabToken {
		host = "https://gitlab.com"
	}
	return GitCredentialValue{EnvKey: row[0], Secret: row[1], Host: host}, ok
}

func TestGitBindingsResolveAllAndClearGlobalAuthentication(t *testing.T) {
	selection := gitSelection("gh", "gh2", "gl", "ssh")
	selection.PreferredCredentialID = "gh2"
	selection.Items[3].SshHostsCredentialID = "hosts"
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
	for _, tc := range []struct{ id, scope, hosts string }{{"ssh", "https://host/repo", ""}, {"gh", "git@host:repo", ""}, {"ssh", "", "gone"}, {"ssh", "", "gh"}, {"gh", "", "hosts"}} {
		selection := gitSelection(tc.id)
		selection.Items[0].Scope = tc.scope
		selection.Items[0].SshHostsCredentialID = tc.hosts
		if _, err := ResolveGitBindings(selection, selectedGitTestValue); err == nil {
			t.Fatalf("bad scope/hosts accepted: %+v", tc)
		}
	}
}

func TestGitBindingsScopeRouting(t *testing.T) {
	rows := []sandbox.GitCredential{
		{ID: "first", Kind: "github_https"}, {ID: "preferred", Kind: "github_https", Preferred: true},
		{ID: "exact", Kind: "github_https", Scope: "https://github.com/team/repo.git"},
		{ID: "enterprise", Kind: "github_https", Scope: "https://git.example:8443"},
		{ID: "ssh-first", Kind: "ssh"}, {ID: "ssh-preferred", Kind: "ssh", Preferred: true}, {ID: "ssh-repo", Kind: "ssh", Scope: "git@git.example:team/repo"},
		{ID: "gl", Kind: "gitlab_https"}, {ID: "gl-exact", Kind: "gitlab_https", Scope: "https://gitlab.com/team/repo"},
	}
	for _, tc := range []struct{ repo, kind, want string }{
		{"https://github.com/team/repo", "github_https", "exact"}, {"https://github.com/team/other", "github_https", "first"},
		{"http://github.com/team/repo", "github_https", "first"}, {"https://git.example:8443/team/a", "github_https", "enterprise"},
		{"http://git.example:8443/team/a", "github_https", ""}, {"https://unknown/a", "github_https", ""}, {"", "ssh", ""},
		{"git@git.example:team/repo.git", "ssh", "ssh-repo"}, {"ssh://git@other/team/repo", "ssh", "ssh-first"},
		{"https://gitlab.com/team/repo.git", "gitlab_https", "gl-exact"}, {"http://gitlab.com/team/repo.git", "gitlab_https", "gl"},
	} {
		got, ok := GitCredentialForRepo(rows, tc.repo, tc.kind)
		if got.ID != tc.want || ok != (tc.want != "") {
			t.Fatalf("%s %s = %s, %v; want %s", tc.kind, tc.repo, got.ID, ok, tc.want)
		}
	}
	got, ok := GitCredentialForRepo([]sandbox.GitCredential{{ID: "a", Kind: "ssh"}, {ID: "b", Kind: "ssh"}}, "git@host:repo", "ssh")
	if !ok || got.ID != "a" {
		t.Fatal("same scope must keep first selected account")
	}
	got, ok = gitCredentialForRepo(rows, "git@gitlab.com:team/repo.git", "gitlab_https", true)
	if !ok || got.ID != "gl-exact" {
		t.Fatal("SSH GitLab repository must use scoped HTTP API authorization")
	}
	if _, ok := GitCredentialForRepo(rows[8:], "git@gitlab.com:team/repo.git", "gitlab_https"); ok {
		t.Fatal("transport selection ignored scheme")
	}
	for _, tc := range []struct {
		row  sandbox.GitCredential
		want string
	}{
		{sandbox.GitCredential{Kind: "github_https"}, "https://github.com"}, {sandbox.GitCredential{Kind: "gitlab_https"}, "https://gitlab.com"},
		{sandbox.GitCredential{Kind: "gitlab_https", Scope: "http://host:8080/repo"}, "http://host:8080"}, {sandbox.GitCredential{Kind: "ssh"}, ""},
	} {
		if got := GitCredentialBaseURL(tc.row); got != tc.want {
			t.Fatalf("origin = %q want %q", got, tc.want)
		}
	}
	if parseGitScope("https://%zz").host != "" {
		t.Fatal("invalid URL parsed")
	}
}

func TestGitBindingsWorkflowSpecAndMRUseSelectedCatalog(t *testing.T) {
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
	if p.gitToken(req) != "gl-secret" || p.gitLabURL(req) != "https://gitlab.com" {
		t.Fatal("MR gate read workspace/default authorization")
	}
	other := req
	other.Vars = map[string]any{"repos": `[{"name":"other","url":"https://gitlab.com/team/other.git"}]`}
	if p.gitToken(other) != "" || p.gitLabURL(other) != "" {
		t.Fatal("repository scope applied to another repo")
	}
	var scripts []string
	restore := sandbox.SetExecHook(func(_ context.Context, _ string, _ int, command string, stdin io.Reader) ([]byte, error) {
		body := execHookBody(command, stdin)
		scripts = append(scripts, body)
		if strings.Contains(body, "mr create") {
			return []byte("https://gitlab.com/team/repo/-/merge_requests/9\n"), nil
		}
		return []byte("[]"), nil
	})
	defer restore()
	sb := &sandbox.Sandbox{Host: "127.0.0.1", Port: 1, Password: "test"}
	if got := p.findOrCreateMR(context.Background(), sb, "/root/workspace/repo", "feature", "https://gitlab.com/team/repo.git"); !strings.Contains(got, "/9") {
		t.Fatalf("MR = %s", got)
	}
	for _, script := range scripts {
		if !strings.Contains(script, "helper.py exec-cli glab") || !strings.Contains(script, "--repo 'https://gitlab.com/team/repo.git'") || strings.Contains(script, "gl-secret") || strings.Contains(script, "workspace-unselected") {
			t.Fatalf("unsafe/wrong MR script: %s", script)
		}
	}
	p.opts.GitCredential = func(string) (GitCredentialValue, bool) { return GitCredentialValue{}, false }
	if _, err := p.spec(req); err == nil {
		t.Fatal("invalid selected authorization did not block workflow sandbox")
	}
	if p.gitToken(req) != "" {
		t.Fatal("invalid selected authorization fell back in MR gate")
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
	req := h.req("first", models.SandboxModeShared)
	req.Config["agent_profile"] = "agent-a"
	h.run(t, req)
	req = h.req("second", models.SandboxModeShared)
	req.Config["agent_profile"] = "agent-b"
	h.run(t, req)
	if h.mgr.createCount() != 1 || h.mgr.reprimeCount() != 1 {
		t.Fatalf("creates=%d reprimes=%d", h.mgr.createCount(), h.mgr.reprimeCount())
	}
	if got := h.mgr.reprimes[0]; !got.GitBindingsConfigured || len(got.GitCredentials) != 1 || got.GitCredentials[0].ID != "gh2" || got.Env["GITHUB_TOKEN"] != "" {
		t.Fatalf("Agent B authorization leaked: %+v", got.GitCredentials)
	}
	req = h.req("third", models.SandboxModeShared)
	req.Config["agent_profile"] = "agent-none"
	h.run(t, req)
	if got := h.mgr.reprimes[1]; !got.GitBindingsConfigured || len(got.GitCredentials) != 0 {
		t.Fatal("explicit empty Agent retained the previous catalog")
	}
	if !reflect.DeepEqual(h.mgr.reprimes[0].GitCredentials[0].ID, "gh2") {
		t.Fatal("later prime mutated earlier selection")
	}
}
