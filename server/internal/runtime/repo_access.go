package runtime

import (
	"strings"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// RepoCredentialsFunc resolves selected repository ids into per-repository Git
// credentials. Unknown ids are skipped.
type RepoCredentialsFunc func(repoIDs []string) []sandbox.RepoCredential

// RepoCredentialsFor turns registered repositories into per-repository Git
// credentials. A repository with no access method of its own, a URL that does
// not fit the method, or a selected credential that is missing, of another kind
// or bound to another host is skipped without forwarding its secret.
func RepoCredentialsFor(projects []models.Project, resolve GitCredentialFunc) []sandbox.RepoCredential {
	if resolve == nil {
		return nil
	}
	out := make([]sandbox.RepoCredential, 0, len(projects))
	for _, p := range projects {
		kind := strings.TrimSpace(p.GitCredentialType)
		url := strings.TrimSpace(p.RepoURL)
		if !repoURLFitsKind(url, kind) {
			continue
		}
		value, ok := resolve(p.GitCredentialID)
		if !ok || value.EnvKey != CredentialEnvKeyForGit(kind) || strings.TrimSpace(value.Secret) == "" {
			continue
		}
		if kind != sandbox.RepoCredentialSSH {
			if _, err := gitScopeForHost(value.Host, url); err != nil {
				continue
			}
		}
		rc := sandbox.RepoCredential{URL: url, Kind: kind, Secret: value.Secret}
		if kind == sandbox.RepoCredentialSSH {
			if hosts, ok := resolve(p.SshHostsCredentialID); ok && hosts.EnvKey == envauth.EnvGitSSHKnownHosts {
				rc.KnownHosts = hosts.Secret
			}
		}
		out = append(out, rc)
	}
	return out
}

func repoURLFitsKind(url, kind string) bool {
	switch kind {
	case sandbox.RepoCredentialGitHubHTTPS, sandbox.RepoCredentialGitLabHTTPS:
		return strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://")
	case sandbox.RepoCredentialSSH:
		if strings.HasPrefix(url, "ssh://") {
			return true
		}
		at, colon := strings.Index(url, "@"), strings.Index(url, ":")
		return !strings.Contains(url, "://") && at > 0 && colon > at
	}
	return false
}
