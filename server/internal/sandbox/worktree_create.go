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
	"syscall"
	"time"

	"github.com/cocofhu/grasp/internal/envauth"
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
		if !worktreeGitEnvKey(k) {
			env[k] = v
		}
	}
	if strings.TrimSpace(env[BridgePasswordEnv]) == "" {
		return nil, fmt.Errorf("sandbox spec missing %s", BridgePasswordEnv)
	}
	provider := strings.TrimSpace(env["AGENT_PROVIDER"])
	dropUnavailableLocalPreview(env)
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
	if execution.Local() && strings.TrimSpace(env["GRASP_LOCAL_AUTH"]) == "1" {
		seedLocalAuthConfigHome(provider, configRoot)
	}
	env["WORKSPACE_DIR"] = launch.Root
	env[execution.EnvMode] = execution.Mode()
	cleanupGit, err := prepareWorktreeGit(ctx, spec, env)
	if err != nil {
		return nil, err
	}
	defer func() {
		if failed {
			cleanupGit()
		}
	}()

	port, stop, err := startWorktreeBackend(ctx, env, launch.Root)
	if err != nil {
		return nil, err
	}
	kind := strings.TrimSpace(env[envLaunchKind])
	endOnStop := kind != execution.LaunchRun
	local := &localExec{
		configRoot:  spec.ConfigRoot,
		configHome:  configHome,
		env:         envList(env),
		backendStop: worktreeSessionStop(stop, cleanupGit),
		stop:        func() { execution.Release(launchID, endOnStop) },
	}
	execution.AddStop(launchID, local.shutdown)
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
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return 0, nil, fmt.Errorf("启动本地执行后端: %w", err)
	}
	var once sync.Once
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	stop := func() {
		once.Do(func() {
			if cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				_ = cmd.Process.Kill()
				<-done
			}
		})
	}
	return port, stop, nil
}

// reprimeWorktree changes only the local backend process. A Solo launch owns
// its worktree until final shutdown, so restarting a tool must not Release or
// Prepare the launch (both can discard or recreate the user's current files).
func (m *Manager) reprimeWorktree(ctx context.Context, sb *Sandbox, spec Spec) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	env := make(map[string]string)
	for key, value := range spec.Env {
		if !worktreeGitEnvKey(key) {
			env[key] = value
		}
	}
	if strings.TrimSpace(env[BridgePasswordEnv]) == "" {
		return fmt.Errorf("sandbox spec missing %s", BridgePasswordEnv)
	}
	provider := strings.TrimSpace(env["AGENT_PROVIDER"])
	dropUnavailableLocalPreview(env)
	if err := execution.RequireTools(provider, env); err != nil {
		return err
	}
	configHome := strings.TrimSpace(spec.ConfigHome)
	configRoot := configHome
	if configRoot == "" {
		configRoot = strings.TrimSpace(spec.ConfigRoot)
	}
	if configRoot == "" {
		configRoot = sb.ConfigRoot
	}
	env["CONFIG_ROOT"] = configRoot
	if provider == "codex" {
		env["CODEX_HOME"] = configRoot
	} else {
		delete(env, "CODEX_HOME")
	}
	if execution.Local() && strings.TrimSpace(env["GRASP_LOCAL_AUTH"]) == "1" {
		seedLocalAuthConfigHome(provider, configRoot)
	}
	env["WORKSPACE_DIR"] = sb.WorkspaceDir
	env[execution.EnvMode] = execution.Mode()
	cleanupGit, err := prepareWorktreeGit(ctx, spec, env)
	if err != nil {
		return err
	}
	port, stop, err := startWorktreeBackend(ctx, env, sb.WorkspaceDir)
	if err != nil {
		cleanupGit()
		return err
	}
	stopSession := worktreeSessionStop(stop, cleanupGit)
	local := sb.local
	local.mu.Lock()
	if local.closed {
		local.mu.Unlock()
		stopSession()
		return fmt.Errorf("工作树会话已关闭")
	}
	previousStop := local.backendStop
	local.backendStop = stopSession
	local.env = envList(env)
	local.configRoot, local.configHome = spec.ConfigRoot, configHome
	sb.Host, sb.Port, sb.ConfigRoot = "127.0.0.1", port, configRoot
	sb.Password = strings.TrimSpace(env[BridgePasswordEnv])
	local.mu.Unlock()
	if previousStop != nil {
		previousStop()
	}
	return nil
}

// Each backend owns its selected private Git catalog. Re-prime retires that
// backend and its catalog together while the launch keeps its worktree holder.
func worktreeSessionStop(stop, cleanupGit func()) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			if stop != nil {
				stop()
			}
			if cleanupGit != nil {
				cleanupGit()
			}
		})
	}
}

// A host without the preview toolchain still runs the Agent, without preview.
func dropUnavailableLocalPreview(env map[string]string) {
	if !execution.Local() || execution.PreviewAvailable() {
		return
	}
	if execution.PreviewRequested(env) {
		log.Warn().Str("provider", env["AGENT_PROVIDER"]).Msg("local browser preview requested but Chromium/VNC tools are not installed; running without preview")
	}
	for _, key := range []string{"BROWSER_MCP", "VNC_PREVIEW", "PREVIEW_DIRECT"} {
		delete(env, key)
	}
}

// authFromSpec copies the credentials this launch already resolved. An explicit
// catalog does not fall back to host tokens or the legacy SSH key. A
// repository-scoped catalog row outranks a broader one. Solo, workflow runs,
// conversation tests, and the assistant all arrive through Create with those
// fields set.
func authFromSpec(spec Spec) execution.Auth {
	// Local's empty binding deliberately uses host Git authorization, but must
	// not revive unrelated legacy credentials from Spec.Env or SSH fields.
	if execution.Local() && len(spec.GitCredentials) == 0 && spec.GitBindingsConfigured {
		return execution.Auth{}
	}
	configured := spec.GitBindingsConfigured
	auth := execution.Auth{Configured: configured}
	for _, cred := range spec.GitCredentials {
		auth.Credentials = append(auth.Credentials, execution.Credential{
			Kind: cred.Kind, Scope: cred.Scope, Secret: cred.Secret, KnownHosts: cred.KnownHosts,
		})
	}
	if configured {
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
	out := make([]string, 0)
	skip := map[string]bool{}
	for k := range overlay {
		skip[k] = true
	}
	for _, item := range base {
		key, _, _ := strings.Cut(item, "=")
		if skip[key] || envauth.IsPlatformReservedEnvKey(key) {
			continue
		}
		if worktreeGitEnvKey(key) || envauth.IsSecretEnvKey(key) {
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
	mu          sync.Mutex
	configRoot  string
	configHome  string
	env         []string
	backendStop func()
	closed      bool
	once        sync.Once
	stop        func()
}

func (e *localExec) shutdown() {
	if e == nil {
		return
	}
	e.once.Do(func() {
		e.mu.Lock()
		e.closed = true
		stop := e.backendStop
		e.backendStop = nil
		e.mu.Unlock()
		if stop != nil {
			stop()
		}
		if e.stop != nil {
			e.stop()
		}
	})
}

// IsLocal reports an in-process worktree backend rather than a container.
func (s *Sandbox) IsLocal() bool { return s != nil && s.local != nil }

func (s *Sandbox) mapLocalPath(path string) string {
	if s.local == nil {
		return path
	}
	s.local.mu.Lock()
	root := strings.TrimRight(s.local.configRoot, "/")
	home := s.local.configHome
	s.local.mu.Unlock()
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
	s.local.mu.Lock()
	cmd.Env = append([]string{}, s.local.env...)
	s.local.mu.Unlock()
	if stdin != nil {
		cmd.Stdin = stdin
	}
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
