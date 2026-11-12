package runtime

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// RepoCredential is the authorization a repository grants for its own clone
// URL. An empty CredentialID clones without authorization.
type RepoCredential struct {
	Repo         string
	URL          string
	CredentialID string
}

// ResolveRepoCredentials resolves each repository's credential, scoped to that
// repository's URL. A missing or revoked credential is an error; another
// account is never substituted.
func ResolveRepoCredentials(repos []RepoCredential, resolve GitCredentialFunc) ([]sandbox.GitCredential, error) {
	out := make([]sandbox.GitCredential, 0, len(repos))
	for _, repo := range repos {
		id := strings.TrimSpace(repo.CredentialID)
		if id == "" {
			continue
		}
		credential, err := resolveRepoCredential(repo, id, resolve)
		if err != nil {
			return nil, fmt.Errorf("仓库「%s」的凭据不可用：%w", repo.Repo, err)
		}
		out = append(out, credential)
	}
	return out, nil
}

// CheckRepoCredential validates a repository's credential against its URL.
func CheckRepoCredential(repoURL, credentialID string, resolve GitCredentialFunc) error {
	_, err := resolveRepoCredential(RepoCredential{URL: repoURL}, strings.TrimSpace(credentialID), resolve)
	return err
}

var (
	ErrRepoCredentialMissing  = errors.New("凭据不存在、已清空或已吊销，或不是 Git token / SSH 私钥")
	ErrRepoCredentialToken    = errors.New("token 含有换行等非法字符")
	ErrRepoCredentialProtocol = errors.New("SSH 私钥只能用于 SSH 地址，token 只能用于 HTTP(S) 地址")
	ErrRepoCredentialHost     = errors.New("仓库地址不在凭据的主机上")
)

func resolveRepoCredential(repo RepoCredential, id string, resolve GitCredentialFunc) (sandbox.GitCredential, error) {
	var value GitCredentialValue
	ok := false
	if resolve != nil {
		value, ok = resolve(id)
	}
	kind := gitKindForEnvKey(value.EnvKey)
	if !ok || strings.TrimSpace(value.Secret) == "" || kind == "" {
		return sandbox.GitCredential{}, ErrRepoCredentialMissing
	}
	if kind != sandbox.RepoCredentialSSH && strings.ContainsAny(value.Secret, "\r\n\x00") {
		return sandbox.GitCredential{}, ErrRepoCredentialToken
	}
	scope, err := models.NormalizeGitScope(repo.URL)
	if err != nil || scope == "" {
		return sandbox.GitCredential{}, ErrRepoCredentialProtocol
	}
	if (kind == sandbox.RepoCredentialSSH) != (parseGitScope(scope).scheme == "ssh") {
		return sandbox.GitCredential{}, ErrRepoCredentialProtocol
	}
	if kind != sandbox.RepoCredentialSSH {
		if scope, err = gitScopeForHost(value.Host, scope); err != nil {
			return sandbox.GitCredential{}, ErrRepoCredentialHost
		}
	}
	credential := sandbox.GitCredential{ID: "repo:" + repo.Repo, Kind: kind, Scope: scope, Secret: value.Secret}
	if kind == sandbox.RepoCredentialSSH {
		credential.KnownHosts = value.KnownHosts
	}
	return credential, nil
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
// always excluded from Agent environments in favor of the repositories' file catalog.
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

// StripGitAuthorizationEnv clears all global Git authentication. Repository
// credentials reach the sandbox only through its file catalog.
func StripGitAuthorizationEnv(env, creds map[string]string) {
	for _, values := range []map[string]string{env, creds} {
		for key := range values {
			if IsGitAuthorizationEnvKey(key) {
				delete(values, key)
			}
		}
	}
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
