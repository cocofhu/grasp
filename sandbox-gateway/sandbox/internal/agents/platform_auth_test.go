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
	dir, root := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "opencode"), []byte("#!/bin/sh\nexec \"$GRASP_OFFICIAL_TEST_BINARY\" -test.run=^TestOfficialOpenCodeCLIProcess$ -- \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GRASP_OFFICIAL_TEST_BINARY", os.Args[0])
	t.Setenv("GRASP_OFFICIAL_TEST_VERSION", "1.4.1")
	t.Setenv("GRASP_OPENCODE_PROVIDER", "opencode")
	t.Setenv("GRASP_OPENCODE_API_KEY", "official-synthetic-secret")
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("OPENCODE_CONFIG_CONTENT", `{"model":"old-provider/model","provider":{"opencode":{"options":{"baseURL":"https://old.example"}}}}`)
	t.Setenv("OPENCODE_CONFIG", "/old/config")
	t.Setenv("OPENCODE_MODELS_PATH", "/old/catalog")
	t.Setenv("CONFIG_ROOT", root)
	t.Setenv(modelconnection.EnvConfig, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, model := range []string{"", "opencode/official-model"} {
		sess, err := Get(provider.OpenCode).Open(ctx, ctx, provider.OpenOptions{Cwd: t.TempDir(), Model: model}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		result, err := sess.Prompt(ctx, "hello", nil)
		_ = sess.Close()
		if err != nil || result.StopReason != "end_turn" {
			t.Fatalf("official subprocess failed: %+v %v", result, err)
		}
	}
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
	if config.Model != "" && config.Model != "opencode/official-model" {
		fail("native default was replaced with a third-party model")
	}
	if os.Getenv("XDG_DATA_HOME") != filepath.Join(os.Getenv("CONFIG_ROOT"), ".grasp-official", "data") {
		fail("official session history is not stable")
	}
	fmt.Println(`{"type":"text","sessionID":"official-session","part":{"text":"ok"}}`)
	fmt.Println(`{"type":"step_finish","sessionID":"official-session","part":{"reason":"stop"}}`)
	os.Exit(0)
}
