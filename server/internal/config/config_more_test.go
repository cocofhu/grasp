package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestTTLAndDurationGetters(t *testing.T) {
	c := &Config{}
	c.Sandbox.TestSandboxTTLMinutes = 10
	c.Sandbox.RunSandboxTTLMinutes = 20
	c.Sandbox.AgentChatTimeoutSeconds = 30
	c.Sandbox.ChatIdleTimeoutSeconds = 40
	c.Sandbox.RetryBackoffSeconds = 5

	if c.TestSandboxTTL() != 10*time.Minute {
		t.Error("TestSandboxTTL")
	}
	if c.RunSandboxTTL() != 20*time.Minute {
		t.Error("RunSandboxTTL")
	}
	if c.AgentChatTimeout() != 30*time.Second {
		t.Error("AgentChatTimeout")
	}
	if c.ChatIdleTimeout() != 40*time.Second {
		t.Error("ChatIdleTimeout")
	}
	if c.SandboxRetryBackoff() != 5*time.Second {
		t.Error("SandboxRetryBackoff")
	}
}

func TestMergeEnvListViaEnv(t *testing.T) {
	c := &Config{}
	t.Setenv("GRASP_SANDBOX_ENV", "A=1, B=2 , =skip, C , D=3")
	applyEnvOverrides(c)
	if c.Sandbox.Env["A"] != "1" || c.Sandbox.Env["B"] != "2" || c.Sandbox.Env["D"] != "3" {
		t.Fatalf("env merge: %+v", c.Sandbox.Env)
	}
	if _, ok := c.Sandbox.Env["C"]; ok {
		t.Error("bare key without = should be skipped")
	}
	if _, ok := c.Sandbox.Env[""]; ok {
		t.Error("empty key should be skipped")
	}
}

func TestEnvIntInvalid(t *testing.T) {
	t.Setenv("GRASP_PORT", "not-a-number")
	if v := envInt("GRASP_PORT"); v != 0 {
		t.Errorf("invalid int should be 0, got %d", v)
	}
}

func TestApplyAllEnvOverrides(t *testing.T) {
	c := &Config{}
	t.Setenv("GRASP_MCP_ADVERTISE", "http://adv")
	t.Setenv("GRASP_DB", "/db")
	t.Setenv("GRASP_MAX_RUNS", "7")
	t.Setenv("GRASP_PROFILES_ROOT", "/pr")
	t.Setenv("GRASP_AGENT_TIMEOUT_SEC", "11")
	t.Setenv("GRASP_CHAT_IDLE_SEC", "12")
	t.Setenv("GRASP_SANDBOX_MAX_ATTEMPTS", "4")
	t.Setenv("GRASP_SANDBOX_RETRY_BACKOFF_SEC", "3")
	t.Setenv("GRASP_SANDBOX_WORK_DIR", "/wd")
	t.Setenv("GRASP_STORAGE_DRIVER", "local")
	t.Setenv("GRASP_BLOBS_ROOT", "/blobs")
	applyEnvOverrides(c)
	if c.Server.MCPAdvertise != "http://adv" || c.Database.Path != "/db" ||
		c.Engine.MaxConcurrentRuns != 7 || c.Engine.ProfilesRoot != "/pr" ||
		c.Sandbox.AgentChatTimeoutSeconds != 11 || c.Sandbox.ChatIdleTimeoutSeconds != 12 ||
		c.Sandbox.MaxAttempts != 4 || c.Sandbox.RetryBackoffSeconds != 3 || c.Sandbox.WorkDir != "/wd" ||
		c.Storage.Driver != "local" || c.Storage.BlobsRoot != "/blobs" {
		t.Fatalf("env overrides not fully applied: %+v", c)
	}
}

func TestWatchAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("engine:\n  max_concurrent_runs: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Load(path); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reloaded := make(chan struct{}, 1)
	if err := WatchAndReload(ctx, path, func(old, new_ *Config) {
		select {
		case reloaded <- struct{}{}:
		default:
		}
	}); err != nil {
		t.Fatalf("watch: %v", err)
	}

	// Trigger a change; the watcher debounces ~1s before reloading.
	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(path, []byte("engine:\n  max_concurrent_runs: 6\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reloaded:
		if GetConfig().Engine.MaxConcurrentRuns != 6 {
			t.Errorf("reload did not apply: %d", GetConfig().Engine.MaxConcurrentRuns)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watcher did not reload within 5s")
	}
}

func TestWatchAndReloadBadDir(t *testing.T) {
	origNew := watcherNewFn
	origAdd := watcherAddFn
	origClose := watcherCloseFn
	t.Cleanup(func() {
		watcherNewFn = origNew
		watcherAddFn = origAdd
		watcherCloseFn = origClose
	})

	stub := &fsnotify.Watcher{
		Events: make(chan fsnotify.Event),
		Errors: make(chan error),
	}
	watcherNewFn = func() (*fsnotify.Watcher, error) { return stub, nil }
	watcherAddFn = func(w *fsnotify.Watcher, path string) error {
		return errors.New("no such file or directory")
	}
	watcherCloseFn = func(w *fsnotify.Watcher) error { return nil }

	ctx := context.Background()
	err := WatchAndReload(ctx, filepath.Join(t.TempDir(), "nope", "config.yaml"))
	if err == nil {
		t.Fatal("expected error watching missing dir")
	}
	if isInotifyLimitError(err) {
		t.Fatalf("expected non-inotify error, got: %v", err)
	}
}

func TestLogReloadDiff(t *testing.T) {
	// Exercises the diff branches (nil old, port/db change) without asserting logs.
	logReloadDiff(nil, &Config{})
	old := &Config{}
	old.Server.Port = 1
	old.Database.Path = "a"
	new_ := &Config{}
	new_.Server.Port = 2
	new_.Database.Path = "b"
	logReloadDiff(old, new_)
}

func TestAuthConfigDurations(t *testing.T) {
	a := AuthConfig{SessionTTL: "7d"}
	if a.SessionTTLDuration() != 7*24*time.Hour {
		t.Fatal("session 7d")
	}
	def := AuthConfig{}
	if def.SessionTTLDuration() != 7*24*time.Hour {
		t.Fatal("default session")
	}
	bad := AuthConfig{SessionTTL: "invalid"}
	if bad.SessionTTLDuration() != 7*24*time.Hour {
		t.Fatal("bad session fallback")
	}
}
