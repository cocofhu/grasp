package runtime

import (
	"errors"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func repoGitTestValue(id string) (GitCredentialValue, bool) {
	values := map[string]GitCredentialValue{
		"gh":      {EnvKey: envauth.EnvGitHubToken, Secret: "gh-secret", Host: "https://github.com", Login: "octocat"},
		"gl":      {EnvKey: envauth.EnvGitLabToken, Secret: "gl-secret", Host: "https://gitlab.com", Login: "not-github"},
		"ssh":     {EnvKey: envauth.EnvGitSSHPrivateKey, Secret: "SSH\nPRIVATE KEY", KnownHosts: "github.com ssh-ed25519 AAAA"},
		"model":   {EnvKey: envauth.EnvCursorAPIKey, Secret: "model-secret"},
		"empty":   {EnvKey: envauth.EnvGitHubToken, Secret: " ", Host: "https://github.com"},
		"newline": {EnvKey: envauth.EnvGitHubToken, Secret: "token\nother", Host: "https://github.com"},
	}
	v, ok := values[id]
	return v, ok
}

func TestResolveRepoCredentialsScopesEachRepository(t *testing.T) {
	rows, err := ResolveRepoCredentials([]RepoCredential{
		{Repo: "web", URL: "https://github.com/team/web.git", CredentialID: "gh"},
		{Repo: "api", URL: "https://github.com/team/api.git", CredentialID: "gh"},
		{Repo: "ops", URL: "git@github.com:team/ops.git", CredentialID: "ssh"},
		{Repo: "docs", URL: "https://github.com/team/docs.git"},
	}, repoGitTestValue)
	if err != nil || len(rows) != 3 {
		t.Fatalf("rows = %+v, %v", rows, err)
	}
	if rows[0].ID != "repo:web" || rows[0].Scope != "https://github.com/team/web" || rows[0].Kind != sandbox.RepoCredentialGitHubHTTPS {
		t.Fatalf("web = %+v", rows[0])
	}
	if rows[0].Login != "octocat" || rows[2].Login != "" {
		t.Fatalf("GitHub account label = %q, ssh = %q", rows[0].Login, rows[2].Login)
	}
	if rows[1].ID != "repo:api" || rows[1].Scope != "https://github.com/team/api" || rows[1].Secret != "gh-secret" {
		t.Fatalf("one credential shared by two repositories = %+v", rows[1])
	}
	if rows[2].Kind != sandbox.RepoCredentialSSH || rows[2].KnownHosts == "" || !strings.HasPrefix(rows[2].Scope, "ssh://") && !strings.Contains(rows[2].Scope, "github.com") {
		t.Fatalf("ssh = %+v", rows[2])
	}
}

func TestResolveRepoCredentialsFailsClosed(t *testing.T) {
	cases := []struct {
		name string
		repo RepoCredential
		want error
	}{
		{"gone", RepoCredential{Repo: "r", URL: "https://github.com/a/b", CredentialID: "gone"}, ErrRepoCredentialMissing},
		{"model key", RepoCredential{Repo: "r", URL: "https://github.com/a/b", CredentialID: "model"}, ErrRepoCredentialMissing},
		{"blank secret", RepoCredential{Repo: "r", URL: "https://github.com/a/b", CredentialID: "empty"}, ErrRepoCredentialMissing},
		{"token newline", RepoCredential{Repo: "r", URL: "https://github.com/a/b", CredentialID: "newline"}, ErrRepoCredentialToken},
		{"ssh on https", RepoCredential{Repo: "r", URL: "https://github.com/a/b", CredentialID: "ssh"}, ErrRepoCredentialProtocol},
		{"token on ssh", RepoCredential{Repo: "r", URL: "git@github.com:a/b.git", CredentialID: "gh"}, ErrRepoCredentialProtocol},
		{"other host", RepoCredential{Repo: "r", URL: "https://gitlab.com/a/b", CredentialID: "gh"}, ErrRepoCredentialHost},
		{"downgrade", RepoCredential{Repo: "r", URL: "http://github.com/a/b", CredentialID: "gh"}, ErrRepoCredentialHost},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResolveRepoCredentials([]RepoCredential{tc.repo}, repoGitTestValue)
			if !errors.Is(err, tc.want) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
	if err := CheckRepoCredential("https://github.com/a/b", "gh", nil); !errors.Is(err, ErrRepoCredentialMissing) {
		t.Fatalf("missing resolver = %v", err)
	}
}

func TestGitCredentialHostConstrainsRepositoryURL(t *testing.T) {
	for _, host := range []string{"https://git.internal:8443", "http://git.internal:8080"} {
		resolve := func(string) (GitCredentialValue, bool) {
			return GitCredentialValue{EnvKey: envauth.EnvGitLabToken, Secret: "private-token", Host: host}, true
		}
		if err := CheckRepoCredential(host+"/team/repo.git", "gl", resolve); err != nil {
			t.Fatalf("%s: %v", host, err)
		}
		for _, url := range []string{"https://other.example/team/repo", "https://git.internal:8444/team/repo"} {
			if err := CheckRepoCredential(url, "gl", resolve); err == nil {
				t.Fatalf("%s escaped the credential host %s", url, host)
			}
		}
	}
	for _, host := range []string{"", "https://user:token@host"} {
		if err := CheckRepoCredential("https://host/team/repo", "gl", func(string) (GitCredentialValue, bool) {
			return GitCredentialValue{EnvKey: envauth.EnvGitLabToken, Secret: "private-token", Host: host}, true
		}); err == nil {
			t.Fatalf("credential host %q accepted", host)
		}
	}
}

func TestCloneReposAddsProjectAuthorAndCredential(t *testing.T) {
	projects := map[string]*models.Project{
		"p1": {ID: "p1", CredentialID: "gh", CommitName: "Bot", CommitEmail: "bot@example.com"},
		"p2": {ID: "p2"},
	}
	lookup := func(id string) (*models.Project, bool) { p, ok := projects[id]; return p, ok }
	repos := []models.SessionRepo{
		{ProjectID: "p1", RepoURL: "https://github.com/team/web.git", RepoDirName: "web", Branch: "main"},
		{ProjectID: "p2", RepoURL: "https://github.com/team/api.git"},
		{ProjectID: "gone", RepoURL: "https://github.com/team/old.git", RepoDirName: "old"},
		{ProjectID: "p1", RepoURL: "https://github.com/team/web.git", RepoDirName: "web"},
		{ProjectID: "bad", RepoURL: "https://github.com/team/x.git", RepoDirName: "../x"},
	}
	specs, creds := CloneRepos(repos, map[string]string{"api": "feature"}, lookup)
	if len(specs) != 3 || specs[0].Name != "web" || specs[0].Branch != "main" || specs[1].Name != "api" || specs[1].Branch != "feature" || specs[2].Name != "old" {
		t.Fatalf("specs = %+v", specs)
	}
	if specs[0].AuthorName != "Bot" || specs[0].AuthorEmail != "bot@example.com" || specs[1].AuthorName != "" || specs[2].AuthorName != "" {
		t.Fatalf("authors = %+v", specs)
	}
	if len(creds) != 1 || creds[0] != (RepoCredential{Repo: "web", URL: "https://github.com/team/web.git", CredentialID: "gh"}) {
		t.Fatalf("creds = %+v", creds)
	}
	if specs, creds := CloneRepos(repos, nil, nil); len(specs) != 3 || creds != nil || specs[0].AuthorName != "" {
		t.Fatalf("without lookup = %+v %+v", specs, creds)
	}
}

func TestWorkflowSpecUsesRepositoryCredentials(t *testing.T) {
	root := writeAgent(t, "dev", `{"acpBackend":"cursor","credentialBindings":{"GITLAB_TOKEN":"gl"},"env":{"GITLAB_URL":"https://unselected","GIT_REPOS":"stale|https://x/y"}}`)
	project := &models.Project{ID: "p1", CredentialID: "gl", CommitName: "Bot", CommitEmail: "bot@example.com"}
	p := newACPProvider(mcp.NewHost(newMemStore()), Options{
		ProfilesRoot: root, GitCredential: repoGitTestValue, ModelBinding: testModelBinding,
		Project: func(id string) (*models.Project, bool) { return project, id == "p1" },
	}).(*acpProvider)
	req := NodeReq{RunID: "git-run", NodeID: "n", Token: "token", Config: map[string]any{"agent_profile": "dev"},
		Repos: []models.SessionRepo{{ProjectID: "p1", RepoURL: "https://gitlab.com/team/repo.git", RepoDirName: "repo"}}}
	spec, err := p.spec(req)
	if err != nil {
		t.Fatal(err)
	}
	defer removeHome(spec.ConfigHome)
	if len(spec.GitCredentials) != 1 || spec.GitCredentials[0].ID != "repo:repo" || spec.GitCredentials[0].Scope != "https://gitlab.com/team/repo" {
		t.Fatalf("catalog = %+v", spec.GitCredentials)
	}
	if spec.Env["GITLAB_TOKEN"] != "" || spec.Env["GITLAB_URL"] != "" || spec.Env["GIT_REPOS"] != "repo|https://gitlab.com/team/repo.git" {
		t.Fatalf("env = %+v", spec.Env)
	}
	if authors := sandbox.DecodeRepoAuthors(spec.Env[sandbox.EnvRepoAuthors]); authors["repo"] != [2]string{"Bot", "bot@example.com"} {
		t.Fatalf("authors = %+v", authors)
	}
	project.CredentialID = "gone"
	if _, err := p.spec(req); err == nil {
		t.Fatal("a revoked repository credential did not block the sandbox")
	}
}

func TestSharedWorkflowReprimesWhenRepositoryCredentialChanges(t *testing.T) {
	h := newSharedHarness(t, nil)
	project := &models.Project{ID: "p1", CredentialID: "gh"}
	h.p.opts.GitCredential = repoGitTestValue
	h.p.opts.Project = func(id string) (*models.Project, bool) { return project, id == "p1" }
	repos := []models.SessionRepo{{ProjectID: "p1", RepoURL: "https://github.com/team/web.git", RepoDirName: "web"}}
	req := h.req("first")
	req.Repos = repos
	h.run(t, req)
	project.CredentialID = ""
	req = h.req("second")
	req.Repos = repos
	h.run(t, req)
	if h.mgr.createCount() != 1 || h.mgr.reprimeCount() != 1 {
		t.Fatalf("creates=%d reprimes=%d", h.mgr.createCount(), h.mgr.reprimeCount())
	}
	if got := h.mgr.reprimes[0]; len(got.GitCredentials) != 0 {
		t.Fatalf("removed credential survived: %+v", got.GitCredentials)
	}
}
