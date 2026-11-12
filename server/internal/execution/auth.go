package execution

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Credential kinds match the sandbox catalog. Execution does not import that
// package; callers copy the same kind strings.
const (
	credGitHubHTTPS = "github_https"
	credGitLabHTTPS = "gitlab_https"
	credSSH         = "ssh"
)

// Credential is one selected Git identity. Scope is a host or repository URL.
// An empty scope uses the provider default host, or any SSH destination.
type Credential struct {
	Kind       string
	Scope      string
	Secret     string
	KnownHosts string
}

// RepoCredential is a per-repository secret. It wins over the selected catalog
// for that repository URL.
type RepoCredential struct {
	URL        string
	Kind       string
	Secret     string
	KnownHosts string
}

// Auth is the Git identity already resolved for this launch. It owns
// transport: a remote no credential matches fails closed instead of falling
// through to ambient git credentials.
type Auth struct {
	Credentials []Credential
	Repos       []RepoCredential
}

type gitScope struct {
	scheme string
	host   string
	path   string
}

type identity struct {
	kind       string
	secret     string
	knownHosts string
	failClosed bool
}

func (a Auth) gitPrefix(raw string) (env []string, prefix []string, cleanup func(), err error) {
	cleanup = func() {}
	remote, ok := parseScope(raw)
	if !ok || remote.host == "" {
		return nil, nil, cleanup, nil
	}
	id := a.pick(remote)
	// Disabling terminal prompts alone still allows an inherited askpass
	// program to supply an unselected identity or wait for user interaction.
	env = []string{"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=/bin/false", "SSH_ASKPASS=/bin/false"}
	if id.failClosed {
		if remote.scheme == "ssh" {
			env = append(env, "GIT_SSH_COMMAND="+failClosedSSH())
		}
		// An empty helper clears inherited helpers for this process only.
		return append(env, gitConfigEnv([][2]string{{"credential.helper", ""}})...), nil, cleanup, nil
	}
	if id.kind == credSSH {
		sshEnv, clean, err := sshCommand(id)
		if err != nil {
			return nil, nil, cleanup, err
		}
		return append(env, sshEnv...), nil, clean, nil
	}
	extra, args, clean, err := httpsHelper(remote, id)
	if err != nil {
		return nil, nil, cleanup, err
	}
	return append(env, extra...), args, clean, nil
}

func failClosedSSH() string {
	return "ssh -F /dev/null -o IdentitiesOnly=yes -o IdentityAgent=none -o BatchMode=yes -o PasswordAuthentication=no -o KbdInteractiveAuthentication=no"
}

// pick chooses one identity. A repository credential beats the catalog, and
// equal catalog scopes keep the earlier row. No match fails closed.
func (a Auth) pick(remote gitScope) identity {
	if id, ok := a.pickRepo(remote); ok {
		return id
	}
	if id, ok := a.pickCatalog(remote); ok {
		return id
	}
	return identity{failClosed: true}
}

func (a Auth) pickRepo(remote gitScope) (identity, bool) {
	for _, repo := range a.Repos {
		if !usableSecret(repo.Kind, repo.Secret) || !kindFits(repo.Kind, remote.scheme) {
			continue
		}
		scope := scopeOf(repo.URL)
		if scope.host == "" || scope.host != remote.host || scope.path != remote.path {
			continue
		}
		if scope.scheme != "" && scope.scheme != remote.scheme {
			continue
		}
		return identity{kind: repo.Kind, secret: repo.Secret, knownHosts: repo.KnownHosts}, true
	}
	return identity{}, false
}

func (a Auth) pickCatalog(remote gitScope) (identity, bool) {
	bestScore := -1
	var best Credential
	found := false
	for _, cred := range a.Credentials {
		if !usableSecret(cred.Kind, cred.Secret) || !kindFits(cred.Kind, remote.scheme) {
			continue
		}
		scope := scopeOf(cred.Scope)
		if scope.scheme != "" && scope.scheme != remote.scheme {
			continue
		}
		if scope.host == "" {
			switch cred.Kind {
			case credGitHubHTTPS:
				scope.host = "github.com"
			case credGitLabHTTPS:
				scope.host = "gitlab.com"
			}
		}
		score := 0
		if scope.host != "" {
			if scope.host != remote.host {
				continue
			}
			score = 1
		}
		if scope.path != "" {
			if scope.path != remote.path {
				continue
			}
			score = 2
		}
		if score > bestScore {
			bestScore = score
			best = cred
			found = true
		}
	}
	if !found {
		return identity{}, false
	}
	return identity{kind: best.Kind, secret: best.Secret, knownHosts: best.KnownHosts}, true
}

func usableSecret(kind, secret string) bool {
	if strings.TrimSpace(secret) == "" {
		return false
	}
	if kind != credSSH && strings.ContainsAny(secret, "\r\n\x00") {
		return false
	}
	return kind == credGitHubHTTPS || kind == credGitLabHTTPS || kind == credSSH
}

func kindFits(kind, scheme string) bool {
	switch kind {
	case credGitHubHTTPS, credGitLabHTTPS:
		return scheme == "http" || scheme == "https"
	case credSSH:
		return scheme == "ssh"
	default:
		return false
	}
}

func scopeOf(scope string) gitScope {
	parsed, _ := parseScope(scope)
	return parsed
}

func parseScope(scope string) (gitScope, bool) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return gitScope{}, false
	}
	if strings.Contains(scope, "://") {
		u, err := url.Parse(scope)
		if err != nil || u.Host == "" {
			return gitScope{}, false
		}
		scheme := strings.ToLower(u.Scheme)
		switch scheme {
		case "http", "https", "ssh":
		default:
			return gitScope{}, false
		}
		return gitScope{scheme: scheme, host: strings.ToLower(u.Host), path: repoPath(u.Path)}, true
	}
	if at, colon := strings.Index(scope, "@"), strings.Index(scope, ":"); at > 0 && colon > at {
		return gitScope{scheme: "ssh", host: strings.ToLower(scope[at+1 : colon]), path: repoPath(scope[colon+1:])}, true
	}
	return gitScope{host: strings.ToLower(scope)}, true
}

func repoPath(path string) string {
	return strings.TrimSuffix(strings.Trim(strings.TrimSpace(path), "/"), ".git")
}

func splitHostPort(hostport string) (string, string) {
	if hostport == "" {
		return "", ""
	}
	if strings.HasPrefix(hostport, "[") {
		if host, port, err := net.SplitHostPort(hostport); err == nil {
			return host, port
		}
		return strings.Trim(hostport, "[]"), ""
	}
	if host, port, err := net.SplitHostPort(hostport); err == nil && port != "" {
		return host, port
	}
	return hostport, ""
}

func httpsUser(kind string) string {
	if kind == credGitLabHTTPS {
		return "oauth2"
	}
	return "x-access-token"
}

func httpsHelper(remote gitScope, id identity) (env []string, prefix []string, cleanup func(), err error) {
	cleanup = func() {}
	token := strings.TrimSpace(id.secret)
	if token == "" || strings.ContainsAny(token, "\r\n\x00") {
		return nil, nil, cleanup, fmt.Errorf("无效的 Git 凭据")
	}
	dir, err := os.MkdirTemp("", "grasp-git-")
	if err != nil {
		return nil, nil, cleanup, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	host, port := splitHostPort(remote.host)
	files := map[string]string{
		"protocol": remote.scheme,
		"host":     host,
		"port":     port,
		"path":     remote.path,
		"username": httpsUser(id.kind),
		"password": token,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			cleanup()
			return nil, nil, func() {}, err
		}
	}
	helper := filepath.Join(dir, "helper")
	if err := os.WriteFile(helper, []byte(helperScript), 0o700); err != nil {
		cleanup()
		return nil, nil, func() {}, err
	}
	// The helper value contains "!" and a space, which cannot be a git argv.
	// GIT_CONFIG_* applies to this process only and does not write git config.
	env = gitConfigEnv([][2]string{
		{"credential.helper", ""},
		{"credential.helper", "!" + helper + " " + dir},
		{"credential.useHttpPath", "true"},
	})
	return env, nil, cleanup, nil
}

// gitConfigEnv encodes key/value pairs as GIT_CONFIG_COUNT / KEY_n / VALUE_n.
func gitConfigEnv(settings [][2]string) []string {
	env := []string{"GIT_CONFIG_COUNT=" + strconv.Itoa(len(settings))}
	for i, setting := range settings {
		env = append(env,
			fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", i, setting[0]),
			fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", i, setting[1]),
		)
	}
	return env
}

const helperScript = `#!/bin/sh
dir=$1
op=$2
[ "$op" = "get" ] || exit 0
proto= host= port= path=
while IFS= read -r line; do
  [ -n "$line" ] || break
  key=${line%%=*}
  val=${line#*=}
  case "$key" in
    protocol) proto=$val ;;
    host) host=$val ;;
    port) port=$val ;;
    path) path=$val ;;
  esac
done
[ "$proto" = "$(cat "$dir/protocol")" ] || exit 0
want_host=$(cat "$dir/host")
want_port=$(cat "$dir/port")
if [ "$host" != "$want_host" ] && [ "$host" != "${want_host}:${want_port}" ]; then
  exit 0
fi
if [ -n "$port" ] && [ -n "$want_port" ] && [ "$port" != "$want_port" ]; then
  exit 0
fi
want_path=$(cat "$dir/path")
if [ -n "$path" ] && [ -n "$want_path" ]; then
  path=${path#/}
  path=${path%.git}
  [ "$path" = "$want_path" ] || exit 0
fi
printf 'username=%s\n' "$(cat "$dir/username")"
printf 'password=%s\n' "$(cat "$dir/password")"
`

func sshCommand(id identity) (env []string, cleanup func(), err error) {
	cleanup = func() {}
	dir, err := os.MkdirTemp("", "grasp-ssh-")
	if err != nil {
		return nil, cleanup, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	key := filepath.Join(dir, "id")
	body := strings.TrimRight(id.secret, "\r\n") + "\n"
	if err := os.WriteFile(key, []byte(body), 0o600); err != nil {
		cleanup()
		return nil, func() {}, err
	}
	hosts := filepath.Join(dir, "known_hosts")
	strict := "accept-new"
	known := strings.TrimSpace(id.knownHosts)
	if known != "" {
		strict = "yes"
		if !strings.HasSuffix(id.knownHosts, "\n") {
			known = strings.TrimRight(id.knownHosts, "\r\n") + "\n"
		} else {
			known = id.knownHosts
		}
	}
	if err := os.WriteFile(hosts, []byte(known), 0o644); err != nil {
		cleanup()
		return nil, func() {}, err
	}
	command := fmt.Sprintf(
		"ssh -F /dev/null -i %s -o IdentitiesOnly=yes -o IdentityAgent=none -o BatchMode=yes -o PasswordAuthentication=no -o KbdInteractiveAuthentication=no -o UserKnownHostsFile=%s -o GlobalKnownHostsFile=/dev/null -o StrictHostKeyChecking=%s",
		shellQuote(key), shellQuote(hosts), strict,
	)
	return []string{"GIT_SSH_COMMAND=" + command}, cleanup, nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
