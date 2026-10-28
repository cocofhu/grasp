package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backend/internal/backend/common"
	"backend/internal/provider"
	"backend/internal/provider/modelconnection"
)

// Runs the real registry and one-shot preparation with synthetic credentials.
// The peer checks native configuration, and sends no upstream model requests.
func TestOfficialOpenCodeAuthorizationSubprocess(t *testing.T) {
	callsPath := setupOfficialOpenCodeCLI(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, model := range []string{"", "opencode/official-model"} {
		sess, err := Get(provider.OpenCode).Open(ctx, ctx, provider.OpenOptions{Cwd: t.TempDir(), Model: model}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			result, err := sess.Prompt(ctx, "hello", nil)
			if err != nil || result.StopReason != "end_turn" {
				_ = sess.Close()
				t.Fatalf("official subprocess failed: %+v %v", result, err)
			}
		}
		_ = sess.Close()
	}
	assertOfficialOpenCodeCalls(t, callsPath, 1, 4)
	for _, opts := range []provider.OpenOptions{{Model: "other/model"}, {CustomArgs: []string{"--model", "other/model"}}} {
		if sess, err := Get(provider.OpenCode).Open(ctx, ctx, opts, nil, nil); err == nil {
			_ = sess.Close()
			t.Fatal("routing override reached official subprocess")
		}
	}
	// A separate executable path avoids the successful-version cache entry.
	oldDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(oldDir, "opencode"), []byte("#!/bin/sh\nprintf '1.4.0\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", oldDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if sess, err := Get(provider.OpenCode).Open(ctx, ctx, provider.OpenOptions{}, nil, nil); err == nil {
		_ = sess.Close()
		t.Fatal("CLI without verified isolation support accepted")
	}
}

func setupOfficialOpenCodeCLI(t *testing.T) string {
	t.Helper()
	dir, root := t.TempDir(), t.TempDir()
	callsPath := filepath.Join(dir, "calls")
	if err := os.WriteFile(filepath.Join(dir, "opencode"), []byte("#!/bin/sh\nexec \"$GRASP_OFFICIAL_TEST_BINARY\" -test.run=^TestOfficialOpenCodeCLIProcess$ -- \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GRASP_OFFICIAL_TEST_BINARY", os.Args[0])
	t.Setenv("GRASP_OFFICIAL_TEST_VERSION", "1.4.1")
	t.Setenv("GRASP_OFFICIAL_TEST_CALLS", callsPath)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("OPENCODE_ENABLE_EXPERIMENTAL_MODELS", "false")
	t.Setenv("GRASP_OPENCODE_PROVIDER", "opencode")
	t.Setenv("GRASP_CONFIGURED_MODELS", "")
	t.Setenv("GRASP_OPENCODE_API_KEY", "official-synthetic-secret")
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("OPENCODE_CONFIG_CONTENT", `{"model":"old-provider/model","provider":{"opencode":{"options":{"baseURL":"https://old.example"}}}}`)
	t.Setenv("OPENCODE_CONFIG", "/old/config")
	t.Setenv("OPENCODE_MODELS_PATH", "/old/catalog")
	t.Setenv("CONFIG_ROOT", root)
	t.Setenv(modelconnection.EnvConfig, "")
	return callsPath
}

func assertOfficialOpenCodeCalls(t *testing.T, path string, wantRefresh, wantRun int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	refresh, run := 0, 0
	for _, line := range strings.Fields(string(data)) {
		switch line {
		case "models":
			refresh++
		case "run":
			run++
		}
	}
	if refresh != wantRefresh || run != wantRun {
		t.Fatalf("native calls: refresh=%d run=%d, want refresh=%d run=%d", refresh, run, wantRefresh, wantRun)
	}
}

func TestOfficialOpenCodeRejectsUnavailableModelsBeforeRun(t *testing.T) {
	for _, id := range []string{"missing-model", "retired-model", "alpha-model"} {
		t.Run(id, func(t *testing.T) {
			callsPath := setupOfficialOpenCodeCLI(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			model := "opencode/" + id
			sess, err := Get(provider.OpenCode).Open(ctx, ctx, provider.OpenOptions{Cwd: t.TempDir(), Model: model}, nil, nil)
			if err == nil {
				_ = sess.Close()
				t.Fatal("unavailable official model accepted")
			}
			if !strings.Contains(err.Error(), model) {
				t.Fatalf("selected model missing from diagnostic: %v", err)
			}
			assertOfficialOpenCodeCalls(t, callsPath, 1, 0)
		})
	}
}

func TestOfficialOpenCodeRefreshesOnlyWhenOpeningSessions(t *testing.T) {
	callsPath := setupOfficialOpenCodeCLI(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	workspace, worktree := t.TempDir(), t.TempDir()
	sess, err := Get(provider.OpenCode).Open(ctx, ctx, provider.OpenOptions{Cwd: workspace, Model: "opencode/official-model"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	cache := filepath.Join(os.Getenv("XDG_CACHE_HOME"), "opencode", "models.json")
	old := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(cache, old, old); err != nil {
		t.Fatal(err)
	}
	// An ongoing session must not refresh at a message boundary, even after
	// the native cache expires. The selected model and normal runs stay intact.
	for range 3 {
		result, err := sess.Prompt(ctx, "continue", nil)
		if err != nil || result.StopReason != "end_turn" {
			t.Fatalf("existing session refreshed an expired catalog: %+v %v", result, err)
		}
	}
	assertOfficialOpenCodeCalls(t, callsPath, 1, 3)
	// A new session in another worktree uses the same startup policy and the
	// same sandbox cache, independent of its working directory.
	other, err := Get(provider.OpenCode).Open(ctx, ctx, provider.OpenOptions{Cwd: worktree, Model: "opencode/official-model"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	assertOfficialOpenCodeCalls(t, callsPath, 2, 3)
	result, err := other.Prompt(ctx, "hello from worktree", nil)
	if err != nil || result.StopReason != "end_turn" {
		t.Fatalf("worktree session failed: %+v %v", result, err)
	}
	assertOfficialOpenCodeCalls(t, callsPath, 2, 4)
}

func TestOfficialOpenCodeAuthAliasesCannotHideMalformedCredentials(t *testing.T) {
	for _, alias := range []string{"OPENCODE_API_KEY", "GRASP_OPENCODE_API_KEY", "ACP_OPENCODE_API_KEY"} {
		for _, value := range []string{"", " \t ", "synthetic\r", "synthetic\n", "synthetic\x00"} {
			env := Get(provider.OpenCode).AuthEnv([]string{"GRASP_OPENCODE_PROVIDER=opencode", alias + "=" + value})
			if value != "" && common.GetEnvValue(env, "OPENCODE_API_KEY") != value {
				t.Fatal("auth normalization hid a malformed selected credential")
			}
			if _, err := modelconnection.Prepare(provider.OpenCode, env, "", t.TempDir()); err == nil {
				t.Fatalf("malformed alias %s reached native launch", alias)
			}
		}
	}
}

func TestOfficialOpenCodeCLIProcess(t *testing.T) {
	if os.Getenv("GRASP_OFFICIAL_TEST_BINARY") == "" {
		return
	}
	for _, arg := range os.Args {
		if arg == "--version" {
			fmt.Println(os.Getenv("GRASP_OFFICIAL_TEST_VERSION"))
			os.Exit(0)
		}
	}
	fail := func(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(2) }
	var args []string
	for i, arg := range os.Args {
		if arg == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	if len(args) == 0 {
		fail("native command missing")
	}
	calls, err := os.OpenFile(os.Getenv("GRASP_OFFICIAL_TEST_CALLS"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		fail("cannot record native command")
	}
	_, _ = fmt.Fprintln(calls, args[0])
	_ = calls.Close()
	if os.Getenv("OPENCODE_API_KEY") != "official-synthetic-secret" || os.Getenv("OPENCODE_CONFIG") != "" || os.Getenv("OPENCODE_MODELS_PATH") != "" || os.Getenv("OPENCODE_DISABLE_PROJECT_CONFIG") != "true" {
		fail("official credential/environment isolation failed")
	}
	raw := os.Getenv("OPENCODE_CONFIG_CONTENT")
	var config struct {
		Model    string
		Enabled  []string `json:"enabled_providers"`
		Provider map[string]struct {
			Options map[string]string
		}
	}
	if json.Unmarshal([]byte(raw), &config) != nil || len(config.Enabled) != 1 || config.Enabled[0] != "opencode" || len(config.Provider) != 1 || config.Provider["opencode"].Options["apiKey"] != "{env:OPENCODE_API_KEY}" {
		fail("official provider configuration missing")
	}
	if strings.Contains(raw, "secret") || strings.Contains(raw, "old.example") || strings.Contains(raw, "baseURL") {
		fail("credential or old provider endpoint copied into official config")
	}
	if args[0] == "models" {
		if len(args) != 3 || args[1] != "opencode" || args[2] != "--refresh" {
			fail("unexpected native model refresh arguments")
		}
		dir := filepath.Join(os.Getenv("XDG_CACHE_HOME"), "opencode")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			fail("cannot create native model cache")
		}
		if err := os.WriteFile(filepath.Join(dir, "models.json"), []byte(`{"opencode":{"id":"opencode","name":"OpenCode Zen","models":{"official-model":{"id":"official-model","name":"Official Model","status":"active"},"retired-model":{"id":"retired-model","status":"deprecated"},"alpha-model":{"id":"alpha-model","status":"alpha"}}}}`), 0o600); err != nil {
			fail("cannot write native model cache")
		}
		fmt.Println("opencode/official-model")
		os.Exit(0)
	}
	if args[0] != "run" {
		fail("unexpected native command")
	}
	if config.Model != "" && config.Model != "opencode/official-model" {
		fail("native default was replaced with a third-party model")
	}
	if config.Model != "" {
		at := indexOfArg(args, "--model")
		if at < 0 || at+1 >= len(args) || args[at+1] != config.Model {
			fail("selected model changed before native run")
		}
	}
	if os.Getenv("XDG_DATA_HOME") != filepath.Join(os.Getenv("CONFIG_ROOT"), ".grasp-official", "data") {
		fail("official session history is not stable")
	}
	fmt.Println(`{"type":"text","sessionID":"official-session","part":{"text":"ok"}}`)
	fmt.Println(`{"type":"step_finish","sessionID":"official-session","part":{"reason":"stop"}}`)
	os.Exit(0)
}
