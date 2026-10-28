package sandbox

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cocofhu/grasp/internal/execution"
)

// worktreeGitEnvKey excludes process-wide identities and Git configuration.
// Only the private catalog prepared below may supply them to a local backend.
func worktreeGitEnvKey(key string) bool {
	if gitCredentialEnv[key] || strings.HasPrefix(key, "GIT_CONFIG") || strings.HasPrefix(key, "GIT_SSH") {
		return true
	}
	switch key {
	case "GIT_ASKPASS", "SSH_ASKPASS", "SSH_ASKPASS_REQUIRE", "SSH_AGENT_PID",
		"GH_CONFIG_DIR", "GLAB_CONFIG_DIR", "GH_HOST", "GITLAB_HOST", "GRASP_GIT_CREDENTIAL_HELPER":
		return true
	}
	return false
}

// prepareWorktreeGit installs the same file-only catalog as a sandbox, but in
// a private home for this backend. Workspaces can be shared by concurrent nodes
// with different credentials, so neither repository nor host config is changed.
func prepareWorktreeGit(ctx context.Context, spec Spec, env map[string]string) (func(), error) {
	dir, err := os.MkdirTemp("", "grasp-worktree-git-")
	if err != nil {
		return nil, fmt.Errorf("prepare worktree Git credentials: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	failed := true
	defer func() {
		if failed {
			cleanup()
		}
	}()
	files, err := gitCatalogFiles(worktreeGitCredentials(spec))
	if err != nil {
		return nil, fmt.Errorf("prepare worktree Git credentials: %w", err)
	}
	if execution.Local() && len(worktreeGitCredentials(spec)) == 0 {
		home := filepath.Join(dir, "home")
		if err := os.Mkdir(home, 0o700); err != nil {
			return nil, fmt.Errorf("prepare worktree Git credentials: %w", err)
		}
		gitConfigPath := filepath.Join(home, ".gitconfig")
		if hostHome, err := os.UserHomeDir(); err == nil && strings.TrimSpace(hostHome) != "" {
			hostGitConfig := filepath.Join(hostHome, ".gitconfig")
			if _, statErr := os.Stat(hostGitConfig); statErr == nil {
				cmd := exec.CommandContext(ctx, "git", "config", "--file", gitConfigPath, "--add", "include.path", hostGitConfig)
				if err := cmd.Run(); err != nil {
					return nil, fmt.Errorf("include host Git config: %w", err)
				}
			}
		}
		hostName, hostEmail := execution.HostGitIdentity()
		if hostName == "" {
			hostName = "sandbox"
		}
		if hostEmail == "" {
			hostEmail = "sandbox@localhost"
		}
		identity := [][2]string{
			{"user.name", worktreeGitIdentity(spec.Env["GIT_USER_NAME"], hostName)},
			{"user.email", worktreeGitIdentity(spec.Env["GIT_USER_EMAIL"], hostEmail)},
		}
		for _, setting := range identity {
			cmd := exec.CommandContext(ctx, "git", "config", "--file", gitConfigPath, "--", setting[0], setting[1])
			if err := cmd.Run(); err != nil {
				return nil, fmt.Errorf("configure worktree Git author: %w", err)
			}
		}
		env["GIT_CONFIG_GLOBAL"] = gitConfigPath
		env["GIT_TERMINAL_PROMPT"] = "0"
		failed = false
		return cleanup, nil
	}
	source := filepath.Join(dir, "stage", "git")
	if err := os.MkdirAll(source, 0o700); err != nil {
		return nil, fmt.Errorf("prepare worktree Git credentials: %w", err)
	}
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(source, file.name), []byte(file.body), 0o600); err != nil {
			return nil, fmt.Errorf("prepare worktree Git credentials: %w", err)
		}
	}
	home := filepath.Join(dir, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		return nil, fmt.Errorf("prepare worktree Git credentials: %w", err)
	}
	gitConfigPath := filepath.Join(home, ".gitconfig")
	env["GIT_CONFIG_GLOBAL"] = gitConfigPath
	env["GIT_CONFIG_NOSYSTEM"] = "1"
	env["GH_CONFIG_DIR"] = filepath.Join(home, ".config", "gh")
	env["GLAB_CONFIG_DIR"] = filepath.Join(home, ".config", "glab-cli")
	env["GIT_TERMINAL_PROMPT"] = "0"
	backend := strings.TrimSpace(os.Getenv("GRASP_WORKTREE_BACKEND"))
	if backend == "" {
		backend = "/opt/grasp-runtime/current/bin/backend"
	}
	helper := filepath.Join(filepath.Dir(filepath.Dir(backend)), "scripts", "git-credentials.py")
	cmd := exec.CommandContext(ctx, "python3", helper, "install", source, home)
	cmd.Env = envList(env)
	// A malformed catalog or subprocess error may contain a secret. Keep the
	// installer's output out of error strings and logs.
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("install worktree Git credentials: %w", err)
	}
	// Match sandbox startup: these are global defaults, so a repository may
	// still select its own author with git config --local user.*.
	identity := [][2]string{
		{"user.name", worktreeGitIdentity(spec.Env["GIT_USER_NAME"], "sandbox")},
		{"user.email", worktreeGitIdentity(spec.Env["GIT_USER_EMAIL"], "sandbox@localhost")},
	}
	for _, setting := range identity {
		cmd := exec.CommandContext(ctx, "git", "config", "--file", gitConfigPath, "--", setting[0], setting[1])
		cmd.Env = envList(env)
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("configure worktree Git author: %w", err)
		}
	}
	installed := filepath.Join(home, ".grasp", "git-credentials", "helper.py")
	env["GRASP_GIT_CREDENTIAL_HELPER"] = installed
	command := "python3 " + quoteWorktreeGit(installed)
	sshCommand := command + " ssh"
	env["GIT_SSH_COMMAND"] = sshCommand
	env["GIT_SSH_VARIANT"] = "ssh"
	// Environment config follows repository-local config. Reset inherited
	// helpers here as well, so a local helper cannot bypass an empty selection.
	settings := [][2]string{
		{"credential.helper", ""},
		{"credential.helper", "!" + command},
		{"credential.useHttpPath", "true"},
		{"core.sshCommand", sshCommand},
	}
	env["GIT_CONFIG_COUNT"] = strconv.Itoa(len(settings))
	for i, setting := range settings {
		env[fmt.Sprintf("GIT_CONFIG_KEY_%d", i)] = setting[0]
		env[fmt.Sprintf("GIT_CONFIG_VALUE_%d", i)] = setting[1]
	}
	failed = false
	return cleanup, nil
}

// Legacy callers still pass their explicitly supplied identity in Spec.Env.
// Convert it to files as well; never consult the server's ambient credentials.
func worktreeGitCredentials(spec Spec) []GitCredential {
	if spec.GitBindingsConfigured {
		return spec.GitCredentials
	}
	auth := authFromSpec(spec)
	var credentials []GitCredential
	if auth.GitHubToken != "" {
		credentials = append(credentials, GitCredential{
			ID: "legacy-github", Kind: RepoCredentialGitHubHTTPS,
			Scope: legacyWorktreeGitHost(auth.GitHubURL, "https://github.com"), Secret: auth.GitHubToken,
		})
	}
	if auth.GitLabToken != "" {
		credentials = append(credentials, GitCredential{
			ID: "legacy-gitlab", Kind: RepoCredentialGitLabHTTPS,
			Scope: legacyWorktreeGitHost(auth.GitLabURL, "https://gitlab.com"), Secret: auth.GitLabToken,
		})
	}
	if auth.SSHPrivateKey != "" {
		credentials = append(credentials, GitCredential{
			ID: "legacy-ssh", Kind: RepoCredentialSSH, Secret: auth.SSHPrivateKey, KnownHosts: auth.SSHKnownHosts,
		})
	}
	return credentials
}

func legacyWorktreeGitHost(raw, fallback string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
		return parsed.Scheme + "://" + parsed.Host
	}
	return raw
}

func worktreeGitIdentity(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func quoteWorktreeGit(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
