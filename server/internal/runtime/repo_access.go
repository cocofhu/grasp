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
// not fit the method, or a selected credential that is missing or of another
// kind is skipped and keeps using the host-wide credential.
func RepoCredentialsFor(projects []models.Project, resolve SelectedCredentialFunc) []sandbox.RepoCredential {
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
		secret, ok := resolveKind(resolve, p.GitCredentialID, CredentialEnvKeyForGit(kind))
		if !ok {
			continue
		}
		rc := sandbox.RepoCredential{URL: url, Kind: kind, Secret: secret}
		if kind == sandbox.RepoCredentialSSH {
			rc.KnownHosts, _ = resolveKind(resolve, p.SshHostsCredentialID, envauth.EnvGitSSHKnownHosts)
		}
		out = append(out, rc)
	}
	return out
}

func resolveKind(resolve SelectedCredentialFunc, id, expected string) (string, bool) {
	id = strings.TrimSpace(id)
	if id == "" || expected == "" {
		return "", false
	}
	key, value, ok := resolve(id)
	if !ok || strings.TrimSpace(key) != expected || strings.TrimSpace(value) == "" {
		return "", false
	}
	return value, true
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
