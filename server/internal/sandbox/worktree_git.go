package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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
	files, err := gitCatalogFiles(spec.GitCredentials)
	if err != nil {
		return nil, fmt.Errorf("prepare worktree Git credentials: %w", err)
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
	if err := includeWorktreeRepoAuthors(ctx, spec, env, home, gitConfigPath); err != nil {
		return nil, err
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

// includeWorktreeRepoAuthors scopes each repository's author to its own Git
// directory through the private global config. A local source is a worktree
// of the user's repository, so its config must not be written.
func includeWorktreeRepoAuthors(ctx context.Context, spec Spec, env map[string]string, home, gitConfigPath string) error {
	authors := DecodeRepoAuthors(spec.Env[EnvRepoAuthors])
	if len(authors) == 0 {
		return nil
	}
	names := make([]string, 0, len(authors))
	for name := range authors {
		names = append(names, name)
	}
	sort.Strings(names)
	for i, name := range names {
		if name == "" || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
			continue
		}
		cmd := exec.CommandContext(ctx, "git", "-C", filepath.Join(env["WORKSPACE_DIR"], name), "rev-parse", "--absolute-git-dir")
		cmd.Env = envList(env)
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		file := filepath.Join(home, ".grasp", "authors", strconv.Itoa(i)+".gitconfig")
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			return fmt.Errorf("configure worktree Git author: %w", err)
		}
		settings := [][2]string{{"user.name", authors[name][0]}, {"user.email", authors[name][1]}}
		for _, setting := range settings {
			if strings.TrimSpace(setting[1]) == "" {
				continue
			}
			if err := runWorktreeGitConfig(ctx, env, file, setting[0], strings.TrimSpace(setting[1])); err != nil {
				return err
			}
		}
		if err := runWorktreeGitConfig(ctx, env, gitConfigPath, "includeIf.gitdir:"+strings.TrimSpace(string(out))+".path", file); err != nil {
			return err
		}
	}
	return nil
}

func runWorktreeGitConfig(ctx context.Context, env map[string]string, file, key, value string) error {
	cmd := exec.CommandContext(ctx, "git", "config", "--file", file, "--", key, value)
	cmd.Env = envList(env)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("configure worktree Git author: %w", err)
	}
	return nil
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
