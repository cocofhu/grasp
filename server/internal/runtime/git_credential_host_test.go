package runtime

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestGitCredentialHostConstrainsAgentScopeAndProject(t *testing.T) {
	for _, host := range []string{"https://git.internal:8443", "http://git.internal:8080"} {
		t.Run(host, func(t *testing.T) {
			resolve := func(string) (GitCredentialValue, bool) {
				return GitCredentialValue{EnvKey: "GITLAB_TOKEN", Secret: "private-token", Host: host}, true
			}
			selection := gitSelection("gl")
			for _, scope := range []string{"", host, host + "/team/repo.git"} {
				selection.Items[0].Scope = scope
				rows, err := ResolveGitBindings(selection, resolve)
				if err != nil || len(rows) != 1 || GitCredentialBaseURL(rows[0]) != host {
					t.Fatalf("host was not preserved for scope %q: %v", scope, err)
				}
				if _, ok := GitCredentialForRepo(rows, host+"/team/repo.git", sandbox.RepoCredentialGitLabHTTPS); !ok {
					t.Fatal("selected token cannot reach its configured host")
				}
				if _, ok := GitCredentialForRepo(rows, "https://gitlab.com/team/repo.git", sandbox.RepoCredentialGitLabHTTPS); ok {
					t.Fatal("self-hosted token was routed to the public host")
				}
			}
			for _, scope := range []string{"https://other.example/team/repo", "https://gitlab.com", "git.internal", "https://git.internal:8444/team/repo"} {
				selection.Items[0].Scope = scope
				if _, err := ResolveGitBindings(selection, resolve); err == nil {
					t.Fatalf("scope escaped the credential host: %s", scope)
				}
			}
			projects := []models.Project{
				{RepoURL: host + "/team/repo.git", GitCredentialType: "gitlab_https", GitCredentialID: "gl"},
				{RepoURL: "https://other.example/team/repo.git", GitCredentialType: "gitlab_https", GitCredentialID: "gl"},
			}
			rows := RepoCredentialsFor(projects, resolve)
			if len(rows) != 1 || rows[0].URL != projects[0].RepoURL {
				t.Fatal("project-specific authorization escaped the credential host")
			}
		})
	}
	for _, host := range []string{"", "https://user:token@host"} {
		if _, err := ResolveGitBindings(gitSelection("gl"), func(string) (GitCredentialValue, bool) {
			return GitCredentialValue{EnvKey: "GITLAB_TOKEN", Secret: "private-token", Host: host}, true
		}); err == nil {
			t.Fatal("a missing or malformed credential host was accepted")
		}
	}
	selection := gitSelection("gh")
	selection.Items[0].Scope = "http://github.com/team/repo"
	if _, err := ResolveGitBindings(selection, selectedGitTestValue); err == nil {
		t.Fatal("Agent scope downgraded an HTTPS token to HTTP")
	}
}

func TestGitCredentialHostChangesAuthorizationFingerprint(t *testing.T) {
	fingerprints := map[string]bool{}
	for _, host := range []string{"https://gitlab.com", "https://git.internal:8443", "http://git.internal:8080"} {
		rows, err := ResolveGitBindings(gitSelection("gl"), func(string) (GitCredentialValue, bool) {
			return GitCredentialValue{EnvKey: "GITLAB_TOKEN", Secret: "same-token", Host: host}, true
		})
		if err != nil {
			t.Fatal(err)
		}
		fp := sandbox.SpecFingerprint(sandbox.Spec{GitBindingsConfigured: true, GitCredentials: rows})
		if fp == "" || fingerprints[fp] {
			t.Fatal("host edit did not change the authorization fingerprint")
		}
		fingerprints[fp] = true
	}
}

func TestGitCredentialHostEditReprimesSharedWorkflowAndUpdatesAPIOrigin(t *testing.T) {
	h := newSharedHarness(t, nil)
	h.p.opts.ProfilesRoot = writeAgent(t, "host-agent", `{"acpBackend":"cursor","modelBinding":{"kind":"configuration","configurationId":"test-model"},"gitBindings":{"items":[{"credentialId":"gl"}],"preferredCredentialId":"gl"}}`)
	host := "https://gitlab.com"
	h.p.opts.GitCredential = func(string) (GitCredentialValue, bool) {
		return GitCredentialValue{EnvKey: "GITLAB_TOKEN", Secret: "same-token", Host: host}, true
	}
	req := h.req("first", models.SandboxModeShared)
	req.Config["agent_profile"] = "host-agent"
	h.run(t, req)
	host = "http://git.internal:8080"
	req = h.req("second", models.SandboxModeShared)
	req.Config["agent_profile"] = "host-agent"
	req.Vars = map[string]any{"repos": `[{"name":"repo","url":"http://git.internal:8080/team/repo.git"}]`}
	h.run(t, req)
	if h.mgr.createCount() != 1 || h.mgr.reprimeCount() != 1 || len(h.mgr.reprimes[0].GitCredentials) != 1 || h.mgr.reprimes[0].GitCredentials[0].Scope != host {
		t.Fatal("host edit did not re-prime the existing shared sandbox catalog")
	}
	if h.p.gitLabURL(req) != host || h.p.gitToken(req) != "same-token" {
		t.Fatal("GitLab API did not use the selected credential's host")
	}
	req.Vars = map[string]any{"repos": `[{"name":"repo","url":"https://gitlab.com/team/repo.git"}]`}
	if h.p.gitLabURL(req) != "" || h.p.gitToken(req) != "" {
		t.Fatal("GitLab API forwarded the self-hosted token to the public host")
	}
}
