package sandbox

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/execution"
	"github.com/rs/zerolog/log"
)

const (
	envLaunchKind = "GRASP_LAUNCH_KIND"
	envRunID      = "GRASP_RUN_ID"
)

// startWorktreeBackend launches the in-image ACP backend. Tests replace it.
var startWorktreeBackend = realStartWorktreeBackend

// SetWorktreeBackendForTest replaces the local backend starter.
func SetWorktreeBackendForTest(fn func(context.Context, map[string]string, string) (int, func(), error)) func() {
	prev := startWorktreeBackend
	if fn == nil {
		startWorktreeBackend = realStartWorktreeBackend
	} else {
		startWorktreeBackend = fn
	}
	return func() { startWorktreeBackend = prev }
}

// createWorktree prepares a shared directory and starts the agent backend in
// this process. It never calls the sandbox gateway.
func (m *Manager) createWorktree(ctx context.Context, spec Spec) (*Sandbox, error) {
	env := map[string]string{}
	for k, v := range spec.Env {
		env[k] = v
	}
	if strings.TrimSpace(env[BridgePasswordEnv]) == "" {
		return nil, fmt.Errorf("sandbox spec missing %s", BridgePasswordEnv)
	}
	provider := strings.TrimSpace(env["AGENT_PROVIDER"])
	if err := execution.RequireTools(provider, env); err != nil {
		return nil, err
	}
	launchID := strings.TrimSpace(env[envRunID])
	if launchID == "" {
		launchID = strings.TrimSpace(spec.Name)
	}
	if launchID == "" {
		launchID = NewContainerName()
	}
	sources := make([]execution.Source, 0)
	for _, repo := range DecodeRepos(env["GIT_REPOS"]) {
		sources = append(sources, execution.Source{Name: repo.Name, URL: repo.URL, Branch: repo.Branch})
	}
	launch, err := execution.PrepareAuth(launchID, sources, authFromSpec(spec))
	if err != nil {
		return nil, fmt.Errorf("准备工作树: %w", err)
	}
	failed := true
	defer func() {
		if failed {
			execution.ReleaseFailed(launchID)
		}
	}()

	configRoot := spec.ConfigRoot
	configHome := strings.TrimSpace(spec.ConfigHome)
	if configHome != "" {
		configRoot = configHome
	}
	if configRoot == "" {
		configRoot = "/root/.cursor"
	}
	env["CONFIG_ROOT"] = configRoot
	if env["CODEX_HOME"] != "" || provider == "codex" {
		env["CODEX_HOME"] = configRoot
	}
	env["WORKSPACE_DIR"] = launch.Root
	env[execution.EnvMode] = execution.ModeWorktree

	port, stop, err := startWorktreeBackend(ctx, env, launch.Root)
	if err != nil {
		return nil, err
	}
	kind := strings.TrimSpace(env[envLaunchKind])
	endOnStop := kind != execution.LaunchRun
	local := &localExec{
		configRoot: spec.ConfigRoot,
		configHome: configHome,
		stop: func() {
			if stop != nil {
				stop()
			}
			execution.Release(launchID, endOnStop)
		},
	}
	execution.AddStop(launchID, stop)
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		name = NewContainerName()
	}
	failed = false
	log.Info().Str("launch", launchID).Str("root", launch.Root).Str("kind", kind).
		Msg("worktree launch (no sandbox container)")
	return &Sandbox{
		ID: name, Name: name, Host: "127.0.0.1", Port: port,
		WorkspaceDir: launch.Root, ConfigRoot: configRoot,
		Password: strings.TrimSpace(env[BridgePasswordEnv]),
		Blobs:    m.blobs, mgr: m, local: local,
	}, nil
}

func realStartWorktreeBackend(ctx context.Context, env map[string]string, workspace string) (int, func(), error) {
	bin := strings.TrimSpace(os.Getenv("GRASP_WORKTREE_BACKEND"))
	if bin == "" {
		bin = "/opt/grasp-runtime/current/bin/backend"
	}
	if _, err := os.Stat(bin); err != nil {
		return 0, nil, fmt.Errorf("工作树镜像缺少本地执行后端 %s，无法启动。不会改用沙箱容器", bin)
	}
	web := filepath.Join(filepath.Dir(filepath.Dir(bin)), "share", "backend", "web")
	if _, err := os.Stat(web); err != nil {
		return 0, nil, fmt.Errorf("工作树镜像缺少本地执行后端页面资源 %s，无法启动。不会改用沙箱容器", web)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, nil, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	args := []string{"-listen", fmt.Sprintf("127.0.0.1:%d", port), "-web", web, "-gin-mode", "release"}
	if pw := strings.TrimSpace(env[BridgePasswordEnv]); pw != "" {
		args = append(args, "-password", pw)
	}
	if err := ctx.Err(); err != nil {
		return 0, nil, err
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = workspace
	cmd.Env = envList(env)
	if err := cmd.Start(); err != nil {
		return 0, nil, fmt.Errorf("启动本地执行后端: %w", err)
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
				_, _ = cmd.Process.Wait()
			}
		})
	}
	go func() { _ = cmd.Wait() }()
	return port, stop, nil
}

// authFromSpec copies the credentials this launch already resolved. An explicit
// catalog does not fall back to host tokens or the legacy SSH key. Solo,
// workflow runs, conversation tests, and the assistant all arrive through Create
// with those fields set.
func authFromSpec(spec Spec) execution.Auth {
	auth := execution.Auth{Configured: spec.GitBindingsConfigured}
	for _, cred := range spec.GitCredentials {
		auth.Credentials = append(auth.Credentials, execution.Credential{
			Kind: cred.Kind, Scope: cred.Scope, Secret: cred.Secret, KnownHosts: cred.KnownHosts,
		})
	}
	for _, repo := range spec.RepoCredentials {
		auth.Repos = append(auth.Repos, execution.RepoCredential{
			URL: repo.URL, Kind: repo.Kind, Secret: repo.Secret, KnownHosts: repo.KnownHosts,
		})
	}
	if spec.GitBindingsConfigured {
		return auth
	}
	auth.SSHPrivateKey = spec.SSHPrivateKey
	auth.SSHKnownHosts = spec.SSHKnownHosts
	if spec.Env == nil {
		return auth
	}
	auth.GitHubToken = firstEnv(spec.Env, "GITHUB_TOKEN", "GH_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GH_ENTERPRISE_TOKEN")
	auth.GitHubURL = spec.Env["GITHUB_URL"]
	auth.GitLabToken = firstEnv(spec.Env, "GITLAB_TOKEN", "GLAB_TOKEN", "GITLAB_ACCESS_TOKEN")
	auth.GitLabURL = spec.Env["GITLAB_URL"]
	if strings.TrimSpace(auth.SSHPrivateKey) == "" {
		auth.SSHPrivateKey = spec.Env["GIT_SSH_PRIVATE_KEY"]
	}
	if strings.TrimSpace(auth.SSHKnownHosts) == "" {
		auth.SSHKnownHosts = spec.Env["GIT_SSH_KNOWN_HOSTS"]
	}
	return auth
}

func firstEnv(env map[string]string, keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(env[key]); v != "" {
			return v
		}
	}
	return ""
}

func envList(overlay map[string]string) []string {
	base := os.Environ()
	out := make([]string, 0, len(base)+len(overlay))
	skip := map[string]bool{}
	for k := range overlay {
		skip[k] = true
	}
	for _, item := range base {
		key, _, _ := strings.Cut(item, "=")
		if skip[key] {
			continue
		}
		out = append(out, item)
	}
	for k, v := range overlay {
		out = append(out, k+"="+v)
	}
	return out
}

// localExec is the in-process data plane for a worktree launch.
type localExec struct {
	configRoot string
	configHome string
	once       sync.Once
	stop       func()
}

func (e *localExec) shutdown() {
	if e == nil || e.stop == nil {
		return
	}
	e.once.Do(e.stop)
}

func (s *Sandbox) mapLocalPath(path string) string {
	if s.local == nil {
		return path
	}
	root := strings.TrimRight(s.local.configRoot, "/")
	home := s.local.configHome
	if root != "" && home != "" && (path == root || strings.HasPrefix(path, root+"/")) {
		return filepath.Join(home, strings.TrimPrefix(path, root))
	}
	return path
}

func (s *Sandbox) runLocal(ctx context.Context, timeout time.Duration, argv []string, stdin io.Reader) (string, error) {
	cctx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		cctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
	cmd.Dir = s.workspaceDir()
	if stdin != nil {
		cmd.Stdin = stdin
	}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
