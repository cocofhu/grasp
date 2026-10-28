package kimicode

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backend/internal/acp"
	"backend/internal/provider"
)

func setupKimiFixture(t *testing.T, mode string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "kimi"), []byte("#!/bin/sh\nexec \"$GRASP_KIMI_TEST_BINARY\" -test.run=^TestKimiACPProcess$ -- \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GRASP_KIMI_TEST_BINARY", os.Args[0])
	t.Setenv("GRASP_KIMI_TEST_HELPER", "1")
	t.Setenv("GRASP_KIMI_TEST_MODE", mode)
	t.Setenv("CONFIG_ROOT", filepath.Join(dir, "role-home"))
	t.Setenv(APIKeyEnv, "synthetic-kimi-fixture-key")
	t.Setenv("GRASP_MODEL_CONNECTION", "")
	t.Setenv("KIMI_MODEL_THINKING_EFFORT", "inherited-invalid-strength")
	t.Setenv("KIMI_MODEL_NAME", "inherited-model")
	t.Setenv("KIMI_MODEL_API_KEY", "synthetic-inherited-key")
	t.Setenv("KIMI_MODEL_BASE_URL", "https://inherited.example.invalid")
	return filepath.Join(dir, "role-home")
}

func TestOpenRequiresAdvertisedConfirmedStrengthAndRetainsSafeConfiguration(t *testing.T) {
	for _, mode := range []string{"success", "missing", "boolean", "rejected", "unconfirmed"} {
		t.Run(mode, func(t *testing.T) {
			root := setupKimiFixture(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			session, err := New().Open(ctx, ctx, provider.OpenOptions{Cwd: t.TempDir(), Model: "k3.1-pro", ReasoningEffort: "max", ReasoningEffortSet: true}, nil, nil)
			if mode == "success" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := session.Prompt(ctx, "hello", nil); err != nil {
					t.Fatal(err)
				}
				if err := session.Close(); err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				_ = session.Close()
				t.Fatal("unavailable or unconfirmed native strength was accepted")
			}
			entries, err := os.ReadDir(filepath.Join(root, ".grasp-configs"))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Fatalf("expected one bounded configuration for this route, got %d", len(entries))
			}
			for _, entry := range entries {
				body, err := os.ReadFile(filepath.Join(root, ".grasp-configs", entry.Name(), "config.toml"))
				if err != nil || strings.Contains(string(body), "synthetic-kimi-fixture-key") || !strings.Contains(string(body), cnBase) && !strings.Contains(string(body), intlBase) {
					t.Fatal("retained configuration contains credentials or lost its Coding Plan route", err)
				}
			}
		})
	}
}

func TestOpenEmptyResetIgnoresInheritedEffortAndRejectsUnknownModels(t *testing.T) {
	root := setupKimiFixture(t, "success")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := New().Open(ctx, ctx, provider.OpenOptions{Cwd: t.TempDir(), ReasoningEffortSet: true}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	if session.Info().ModelID != defaultModel {
		t.Fatalf("auto did not select the Coding Plan default: %+v", session.Info())
	}
	entries, err := os.ReadDir(filepath.Join(root, ".grasp-configs"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		body, err := os.ReadFile(filepath.Join(root, ".grasp-configs", entry.Name(), "config.toml"))
		if err != nil || strings.Contains(string(body), "[thinking]") {
			t.Fatalf("default reset retained selected strength: %v", err)
		}
	}
	for _, options := range []provider.OpenOptions{
		{Model: "vendor/k3", ReasoningEffort: "low"},
		{Model: "gpt-5"},
		{Model: "kimi-k2.7-code"},
		{Model: "kimi-for-coding-highspeed", ReasoningEffort: "low"},
		{Model: "k3", ReasoningEffort: "ultra"},
	} {
		if unsupported, err := New().Open(ctx, ctx, options, nil, nil); err == nil {
			_ = unsupported.Close()
			t.Fatalf("unsupported model/strength reached CLI: %+v", options)
		}
	}
}

func TestReasoningOptionRequiresThoughtLevelAndExactValue(t *testing.T) {
	options := []acp.ConfigOption{{ID: "native-thinking", Category: "thought_level", CurrentValue: "low", Groups: []acp.ConfigOptionGroup{{Options: []acp.ConfigOptionValue{{Value: "low"}, {Value: "max"}}}}}}
	if option, ok := reasoningOption(options, "max"); !ok || option.ID != "native-thinking" {
		t.Fatal("advertised grouped effort was not resolved")
	}
	if _, ok := reasoningOption(options, "high"); ok {
		t.Fatal("unadvertised effort accepted")
	}
	options[0].Category = "mode"
	if _, ok := reasoningOption(options, "max"); ok {
		t.Fatal("a mode value was mistaken for a reasoning strength")
	}
}

func TestKimiACPProcess(t *testing.T) {
	if os.Getenv("GRASP_KIMI_TEST_HELPER") != "1" {
		return
	}
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "KIMI_MODEL_") {
			os.Exit(2)
		}
	}
	config, err := os.ReadFile(filepath.Join(os.Getenv("KIMI_CODE_HOME"), "config.toml"))
	if err != nil || strings.Contains(string(config), os.Getenv(APIKeyEnv)) {
		os.Exit(3)
	}
	model := defaultModel
	for line := range strings.SplitSeq(string(config), "\n") {
		if strings.HasPrefix(line, "default_model = ") {
			_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "default_model = ")), &model)
		}
	}
	mode, effort := os.Getenv("GRASP_KIMI_TEST_MODE"), "off"
	options := func() []map[string]any {
		out := []map[string]any{{"id": "model", "category": "model", "currentValue": model, "options": []map[string]string{{"value": model}}}}
		if mode == "missing" {
			return out
		}
		values := []map[string]string{{"value": "off"}, {"value": "low"}, {"value": "high"}, {"value": "max"}}
		if mode == "boolean" {
			values = []map[string]string{{"value": "off"}, {"value": "on"}}
		}
		return append(out, map[string]any{"id": "thinking", "category": "thought_level", "currentValue": effort, "options": values})
	}
	enc := json.NewEncoder(os.Stdout)
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Value string `json:"value"`
			} `json:"params"`
		}
		if json.Unmarshal(scan.Bytes(), &request) != nil {
			os.Exit(4)
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "agentInfo": map[string]string{"name": "kimi-fixture", "version": "2.4.0"}}
		case "session/new":
			result = map[string]any{"sessionId": "kimi-fixture-session", "configOptions": options()}
		case "session/set_config_option":
			if mode == "rejected" {
				_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32602, "message": "fixture rejection"}})
				continue
			}
			if mode != "unconfirmed" {
				effort = request.Params.Value
			}
			result = map[string]any{"configOptions": options()}
		case "session/prompt":
			if effort != "max" {
				os.Exit(5)
			}
			result = map[string]any{"stopReason": "end_turn"}
		default:
			continue
		}
		_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}
	os.Exit(0)
}
