package runtime

import (
	"testing"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestRepoCredentialsFor(t *testing.T) {
	creds := map[string][2]string{
		"gh":    {envauth.EnvGitHubToken, "gh-tok"},
		"gl":    {envauth.EnvGitLabToken, "gl-tok"},
		"key":   {envauth.EnvGitSSHPrivateKey, "KEY"},
		"hosts": {envauth.EnvGitSSHKnownHosts, "github.com ssh-ed25519 AAAA"},
	}
	resolve := func(id string) (GitCredentialValue, bool) {
		c, ok := creds[id]
		host := "https://github.com"
		if id == "gl" {
			host = "https://gitlab.com"
		}
		return GitCredentialValue{EnvKey: c[0], Secret: c[1], Host: host}, ok
	}
	got := RepoCredentialsFor([]models.Project{
		{RepoURL: "https://github.com/acme/app.git", GitCredentialType: "github_https", GitCredentialID: "gh"},
		{RepoURL: "git@github.com:acme/infra.git", GitCredentialType: "ssh", GitCredentialID: "key", SshHostsCredentialID: "hosts"},
		// Inherits the host-wide credential: nothing per repository.
		{RepoURL: "https://github.com/acme/open.git"},
		// Credential of the wrong kind is never substituted.
		{RepoURL: "https://gitlab.com/acme/svc.git", GitCredentialType: "gitlab_https", GitCredentialID: "gh"},
		// URL does not fit the access method.
		{RepoURL: "https://github.com/acme/x.git", GitCredentialType: "ssh", GitCredentialID: "key"},
		{RepoURL: "https://gitlab.com/acme/gone.git", GitCredentialType: "gitlab_https", GitCredentialID: "missing"},
		{RepoURL: "https://gitlab.com/acme/ok.git", GitCredentialType: "gitlab_https", GitCredentialID: "gl"},
	}, resolve)
	want := []sandbox.RepoCredential{
		{URL: "https://github.com/acme/app.git", Kind: "github_https", Secret: "gh-tok"},
		{URL: "git@github.com:acme/infra.git", Kind: "ssh", Secret: "KEY", KnownHosts: "github.com ssh-ed25519 AAAA"},
		{URL: "https://gitlab.com/acme/ok.git", Kind: "gitlab_https", Secret: "gl-tok"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("[%d] got %+v want %+v", i, got[i], want[i])
		}
	}
}
