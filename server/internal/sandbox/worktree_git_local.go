package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cocofhu/grasp/internal/execution"
)

// Local backends receive selected snapshots, rather than use the server's HOME
// implicitly. This limits accidental auth/config writes; local processes still
// run as the host user and do not provide an OS security boundary.
func prepareLocalProcessHome(home string, env map[string]string, configRoot, ownedConfigHome string) {
	if configRoot != "" {
		env["CONFIG_ROOT"] = configRoot
	}
	env["HOME"] = home
	for key, rel := range map[string]string{
		"XDG_CONFIG_HOME": ".config", "XDG_DATA_HOME": ".local/share",
		"XDG_STATE_HOME": ".local/state", "XDG_CACHE_HOME": ".cache",
	} {
		env[key] = filepath.Join(home, rel)
	}
	hostHome, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(hostHome) == "" {
		return
	}
	provider := strings.TrimSpace(env["AGENT_PROVIDER"])
	// Trae reads its login below HOME, while the other providers also support
	// explicit config-root variables. Keep conventional paths private as well.
	roots := map[string]string{
		"cursor": ".cursor", "claude_code": ".claude", "codex": ".codex",
		"codebuddy": ".codebuddy", "trae": ".trae", "opencode": ".config/opencode",
		"deepseek_harness": ".dsh", "kimi_code": ".kimi",
	}
	if rel := roots[provider]; rel != "" && configRoot != "" {
		destination := filepath.Join(home, rel)
		// Trae has no config-directory selector. Its private HOME path must
		// retain native history when a backend is replaced. Only the caller's
		// owned ConfigHome may be linked; host login directories are copied.
		if provider == "trae" && localOwnedConfigHome(configRoot, ownedConfigHome, hostHome) {
			if root, err := os.OpenRoot(home); err == nil {
				linkErr := root.Symlink(ownedConfigHome, ".trae")
				_ = root.Close()
				if linkErr == nil {
					return
				}
			}
		}
		snapshotLocalConfig(configRoot, destination, false)
	}
	if strings.TrimSpace(env["GRASP_LOCAL_AUTH"]) == "1" && provider == "claude_code" {
		snapshotLocalEntries(hostHome, home, []string{".claude.json"})
	}
	if strings.TrimSpace(env["GRASP_LOCAL_AUTH"]) == "1" && provider == "opencode" {
		hostData := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
		if !filepath.IsAbs(hostData) {
			hostData = filepath.Join(hostHome, ".local", "share")
		}
		snapshotLocalEntries(filepath.Join(hostData, "opencode"), filepath.Join(home, ".local", "share", "opencode"), []string{"auth.json"})
	}
}

func localOwnedConfigHome(configRoot, ownedConfigHome, hostHome string) bool {
	if strings.TrimSpace(ownedConfigHome) == "" || filepath.Clean(configRoot) != filepath.Clean(ownedConfigHome) {
		return false
	}
	info, err := os.Lstat(configRoot)
	if err != nil || !info.IsDir() {
		return false
	}
	resolved, err := filepath.EvalSymlinks(configRoot)
	if err != nil || resolved == string(filepath.Separator) || resolved == filepath.Clean(hostHome) {
		return false
	}
	for _, rel := range []string{".cursor", ".claude", ".codex", ".codebuddy", ".trae", ".config/opencode", ".dsh", ".kimi", ".kimi-code"} {
		host := filepath.Join(hostHome, rel)
		if native, err := filepath.EvalSymlinks(host); err == nil && resolved == native {
			return false
		}
	}
	return true
}

func snapshotLocalEntries(source, destination string, names []string) {
	if info, err := os.Lstat(source); err != nil || !info.IsDir() {
		return
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return
	}
	src, err := os.OpenRoot(source)
	if err != nil {
		return
	}
	defer func() { _ = src.Close() }()
	dst, err := os.OpenRoot(destination)
	if err != nil {
		return
	}
	defer func() { _ = dst.Close() }()
	for _, name := range names {
		copyLocalConfigEntry(src, dst, name)
	}
}

func prepareLocalWorktreeGit(ctx context.Context, spec Spec, home string, env map[string]string) error {
	hostHome, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(hostHome) == "" {
		return fmt.Errorf("prepare local Git credentials: host home is unavailable")
	}
	snapshotLocalEntries(hostHome, home, []string{".gitconfig", ".git-credentials"})
	// Private keys stay in place: the process already runs as their owner, and
	// copies would outlive a crashed backend. Only files SSH may write are copied.
	hostSSH := filepath.Join(hostHome, ".ssh")
	snapshotLocalEntries(hostSSH, filepath.Join(home, ".ssh"), []string{"config", "known_hosts", "known_hosts2"})
	hostConfig := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if !filepath.IsAbs(hostConfig) {
		hostConfig = filepath.Join(hostHome, ".config")
	}
	for _, name := range []string{"git", "gh", "glab-cli"} {
		snapshotLocalConfig(filepath.Join(hostConfig, name), filepath.Join(home, ".config", name), false)
	}
	gitConfigPath := filepath.Join(home, ".gitconfig")
	env["GIT_CONFIG_GLOBAL"] = gitConfigPath
	env["GIT_CONFIG_NOSYSTEM"] = "1"
	env["GH_CONFIG_DIR"] = filepath.Join(home, ".config", "gh")
	env["GLAB_CONFIG_DIR"] = filepath.Join(home, ".config", "glab-cli")
	env["GIT_TERMINAL_PROMPT"] = "0"
	for _, key := range []string{"SSH_AUTH_SOCK", "SSH_AGENT_PID"} {
		if value := os.Getenv(key); value != "" {
			env[key] = value
		}
	}
	// OpenSSH resolves default ~/.ssh paths through passwd, ignoring HOME.
	// Name the private config and known_hosts, and the host identities, explicitly.
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return fmt.Errorf("prepare local SSH credentials: %w", err)
	}
	sshConfig := filepath.Join(sshDir, "config")
	if body, err := os.ReadFile(sshConfig); err == nil {
		body = []byte(strings.ReplaceAll(string(body), "~/", hostHome+"/"))
		if err := os.WriteFile(sshConfig, body, 0o600); err != nil {
			return fmt.Errorf("prepare local SSH config: %w", err)
		}
	} else if os.IsNotExist(err) {
		if err := os.WriteFile(sshConfig, nil, 0o600); err != nil {
			return fmt.Errorf("prepare local SSH config: %w", err)
		}
	} else {
		return fmt.Errorf("prepare local SSH config: %w", err)
	}
	command := "ssh -F " + quoteWorktreeGit(sshConfig) + " -o IdentityFile=none -o BatchMode=yes -o UserKnownHostsFile=" + quoteWorktreeGit(filepath.Join(sshDir, "known_hosts")) + " -o GlobalKnownHostsFile=/dev/null"
	for _, name := range []string{"id_rsa", "id_ecdsa", "id_ecdsa_sk", "id_ed25519", "id_ed25519_sk", "id_dsa"} {
		if info, err := os.Stat(filepath.Join(hostSSH, name)); err == nil && info.Mode().IsRegular() {
			command += " -i " + quoteWorktreeGit(filepath.Join(hostSSH, name))
		}
	}
	env["GIT_SSH_COMMAND"] = command
	env["GIT_SSH_VARIANT"] = "ssh"
	hostName, hostEmail := execution.HostGitIdentity()
	if hostName == "" {
		hostName = "sandbox"
	}
	if hostEmail == "" {
		hostEmail = "sandbox@localhost"
	}
	for _, setting := range [][2]string{
		{"user.name", worktreeGitIdentity(spec.Env["GIT_USER_NAME"], hostName)},
		{"user.email", worktreeGitIdentity(spec.Env["GIT_USER_EMAIL"], hostEmail)},
	} {
		cmd := exec.CommandContext(ctx, "git", "config", "--file", gitConfigPath, "--", setting[0], setting[1])
		cmd.Env = envList(env)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("configure local Git author: %w", err)
		}
	}
	return nil
}
