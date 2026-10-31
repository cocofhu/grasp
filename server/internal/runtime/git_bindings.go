package runtime

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// ResolveGitBindings resolves every explicitly selected authorization. A
// missing/revoked selection is an error; another account is never substituted.
func ResolveGitBindings(bindings *models.GitBindings, resolve GitCredentialFunc) ([]sandbox.GitCredential, error) {
	selection, err := models.NormalizeGitBindings(bindings)
	if err != nil {
		return nil, err
	}
	out := make([]sandbox.GitCredential, 0, len(selection.Items))
	for _, item := range selection.Items {
		if resolve == nil {
			return nil, fmt.Errorf("Git authorization %q cannot be resolved", item.CredentialID)
		}
		value, ok := resolve(item.CredentialID)
		key, secret := value.EnvKey, value.Secret
		kind := gitKindForEnvKey(key)
		if !ok || strings.TrimSpace(secret) == "" || kind == "" {
			return nil, fmt.Errorf("Git authorization %q is missing, cleared, revoked, or has the wrong kind", item.CredentialID)
		}
		if kind != sandbox.RepoCredentialSSH && strings.ContainsAny(secret, "\r\n\x00") {
			return nil, fmt.Errorf("Git authorization %q contains an invalid token", item.CredentialID)
		}
		scope := parseGitScope(item.Scope)
		if scope.scheme != "" && (kind == sandbox.RepoCredentialSSH) != (scope.scheme == "ssh") {
			return nil, fmt.Errorf("Git authorization %q has an incompatible scope protocol", item.CredentialID)
		}
		effectiveScope := item.Scope
		if kind != sandbox.RepoCredentialSSH {
			effectiveScope, err = gitScopeForHost(value.Host, item.Scope)
			if err != nil {
				return nil, fmt.Errorf("Git authorization %q scope must use the credential host", item.CredentialID)
			}
		}
		credential := sandbox.GitCredential{ID: item.CredentialID, Kind: kind, Scope: effectiveScope, Secret: secret}
		if kind == sandbox.RepoCredentialSSH {
			credential.KnownHosts = value.KnownHosts
		}
		out = append(out, credential)
	}
	return out, nil
}

func gitScopeForHost(host, scope string) (string, error) {
	if strings.TrimSpace(host) == "" {
		return "", models.ErrGitCredentialHost
	}
	origin, err := models.NormalizeGitCredentialHost("", host)
	if err != nil {
		return "", err
	}
	authority := parseGitScope(origin)
	narrow := parseGitScope(scope)
	if scope == "" {
		return origin, nil
	}
	if narrow.host != authority.host || narrow.scheme != "" && narrow.scheme != authority.scheme {
		return "", models.ErrGitCredentialHost
	}
	if narrow.path == "" {
		return origin, nil
	}
	return origin + "/" + narrow.path, nil
}

func gitKindForEnvKey(key string) string {
	switch strings.TrimSpace(key) {
	case envauth.EnvGitHubToken:
		return sandbox.RepoCredentialGitHubHTTPS
	case envauth.EnvGitLabToken:
		return sandbox.RepoCredentialGitLabHTTPS
	case envauth.EnvGitSSHPrivateKey:
		return sandbox.RepoCredentialSSH
	default:
		return ""
	}
}

// IsGitAuthorizationEnvKey identifies global Git authentication, which is
// always excluded from Agent environments in favor of the selected file catalog.
func IsGitAuthorizationEnvKey(key string) bool {
	switch strings.TrimSpace(key) {
	case envauth.EnvGitHubToken, envauth.EnvGitLabToken, envauth.EnvGitSSHPrivateKey, envauth.EnvGitSSHKnownHosts,
		"GITHUB_URL", "GITLAB_URL", "GH_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GLAB_TOKEN", "GITLAB_ACCESS_TOKEN",
		"GIT_SSH_COMMAND", "GIT_SSH", "SSH_AUTH_SOCK":
		return true
	default:
		return false
	}
}

// ApplyGitBindings runs after generic credential bindings and clears all
// global Git authentication, even when the selection is omitted or invalid. Secrets are
// returned only for the sandbox's file catalog, never as environment values.
func ApplyGitBindings(env, creds map[string]string, bindings *models.GitBindings, resolve GitCredentialFunc) ([]sandbox.GitCredential, error) {
	for _, values := range []map[string]string{env, creds} {
		for key := range values {
			if IsGitAuthorizationEnvKey(key) {
				delete(values, key)
			}
		}
	}
	return ResolveGitBindings(bindings, resolve)
}

type gitScope struct {
	scheme string
	host   string
	path   string
}

func parseGitScope(scope string) gitScope {
	if strings.Contains(scope, "://") {
		u, err := url.Parse(scope)
		if err != nil {
			return gitScope{}
		}
		return gitScope{scheme: u.Scheme, host: strings.ToLower(u.Host), path: gitScopeRepoPath(u.Path)}
	}
	if at, colon := strings.Index(scope, "@"), strings.Index(scope, ":"); at > 0 && colon > at {
		return gitScope{scheme: "ssh", host: strings.ToLower(scope[at+1 : colon]), path: gitScopeRepoPath(scope[colon+1:])}
	}
	return gitScope{host: strings.ToLower(scope)}
}

func gitScopeRepoPath(path string) string {
	return strings.TrimSuffix(strings.Trim(strings.TrimSpace(path), "/"), ".git")
}

// GitCredentialForRepo mirrors the catalog routing rule: a repository scope
// beats a host scope, which beats an unscoped SSH default. Equally specific
// scopes keep the earlier selected row. It never retries a different account
// after an authentication failure.
func GitCredentialForRepo(credentials []sandbox.GitCredential, repoURL, kind string) (sandbox.GitCredential, bool) {
	return gitCredentialForRepo(credentials, repoURL, kind, false)
}

func gitCredentialForRepo(credentials []sandbox.GitCredential, repoURL, kind string, sshAPI bool) (sandbox.GitCredential, bool) {
	repo := parseGitScope(strings.TrimSpace(repoURL))
	if repo.host == "" {
		return sandbox.GitCredential{}, false
	}
	best, score := sandbox.GitCredential{}, -1
	for _, credential := range credentials {
		if credential.Kind != kind {
			continue
		}
		scope := parseGitScope(credential.Scope)
		if scope.scheme != "" && scope.scheme != repo.scheme && !(sshAPI && repo.scheme == "ssh" && kind == sandbox.RepoCredentialGitLabHTTPS && (scope.scheme == "http" || scope.scheme == "https")) {
			continue
		}
		if scope.host == "" {
			switch kind {
			case sandbox.RepoCredentialGitHubHTTPS:
				scope.host = "github.com"
			case sandbox.RepoCredentialGitLabHTTPS:
				scope.host = "gitlab.com"
			}
		}
		candidateScore := 0
		if scope.host != "" {
			if scope.host != repo.host {
				continue
			}
			candidateScore = 1
		}
		if scope.path != "" {
			if scope.path != repo.path {
				continue
			}
			candidateScore = 2
		}
		if candidateScore > score {
			best, score = credential, candidateScore
		}
	}
	return best, score >= 0
}

// GitCredentialBaseURL is the API origin for a resolved provider credential.
func GitCredentialBaseURL(credential sandbox.GitCredential) string {
	scope := parseGitScope(credential.Scope)
	if scope.host == "" {
		if credential.Kind == sandbox.RepoCredentialGitHubHTTPS {
			scope.host = "github.com"
		} else if credential.Kind == sandbox.RepoCredentialGitLabHTTPS {
			scope.host = "gitlab.com"
		}
	}
	if scope.host == "" {
		return ""
	}
	scheme := "https"
	if strings.HasPrefix(credential.Scope, "http://") {
		scheme = "http"
	}
	return scheme + "://" + scope.host
}
