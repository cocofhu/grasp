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

	"backend/internal/provider"
	"backend/internal/provider/modelconnection"
	"backend/internal/provider/opencode/opencodetest"
)

// TestOpenCodeVariantCLIProcess is the fake `opencode` of the variant tests:
// it refreshes a one-model catalog and serves prompts, recording the
// generated configuration and each prompt. It never contacts a model service.
func TestOpenCodeVariantCLIProcess(t *testing.T) {
	if os.Getenv("GRASP_VARIANT_TEST_BINARY") == "" {
		return
	}
	args := os.Args
	for i, arg := range args {
		if arg == "--" {
			args = args[i+1:]
			break
		}
	}
	record := func(call string) {
		f, err := os.OpenFile(os.Getenv("GRASP_VARIANT_TEST_CALLS_OUT"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintln(f, call)
			_ = f.Close()
		}
	}
	switch {
	case len(args) > 0 && args[0] == "--version":
		fmt.Println("1.19.4")
		os.Exit(0)
	case len(args) == 3 && args[0] == "models" && args[1] == "opencode" && args[2] == "--refresh":
		record("models")
		dir := filepath.Join(os.Getenv("XDG_CACHE_HOME"), "opencode")
		_ = os.MkdirAll(dir, 0o700)
		_ = os.WriteFile(filepath.Join(dir, "models.json"), []byte(`{"opencode":{"id":"opencode","models":{"gpt-5.5":{"id":"gpt-5.5","name":"GPT-5.5","status":"active"}}}}`), 0o600)
		fmt.Println("opencode/gpt-5.5")
		os.Exit(0)
	}
	_ = os.WriteFile(os.Getenv("GRASP_VARIANT_TEST_CONFIG_OUT"), []byte(os.Getenv("OPENCODE_CONFIG_CONTENT")), 0o600)
	serveFakeOpenCode(opencodetest.Options{Version: "1.19.4", Reply: "connected", Reasoning: "fixture model thought",
		OnPrompt: func(p opencodetest.Prompt) error {
			record("prompt")
			data, _ := json.Marshal(p)
			return os.WriteFile(os.Getenv("GRASP_VARIANT_TEST_PROMPT_OUT"), data, 0o600)
		}})
}

func installOpenCodeVariantFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nexec \"$GRASP_VARIANT_TEST_BINARY\" -test.run=^TestOpenCodeVariantCLIProcess$ -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "opencode"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GRASP_VARIANT_TEST_BINARY", os.Args[0])
	t.Setenv("GRASP_VARIANT_TEST_PROMPT_OUT", filepath.Join(dir, "prompt.json"))
	t.Setenv("GRASP_VARIANT_TEST_CONFIG_OUT", filepath.Join(dir, "config.json"))
	t.Setenv("GRASP_VARIANT_TEST_CALLS_OUT", filepath.Join(dir, "calls"))
	return dir
}

// Record the prompt boundary so a valid variant declaration cannot hide an
// invalid model reference.
func TestOpenCodeVariantSubprocessKeepsModelID(t *testing.T) {
	for _, tc := range []struct {
		name, protocol, effort string
	}{
		{"official/default", "", ""},
		{"official/catalog", "", "high"},
		{"official/custom", "", "turbo"},
		{"connection/chat", modelconnection.Chat, "max"},
		{"connection/responses", modelconnection.Responses, "turbo"},
		{"connection/messages", modelconnection.Messages, "low"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := installOpenCodeVariantFixture(t)
			promptPath, configPath := filepath.Join(dir, "prompt.json"), filepath.Join(dir, "config.json")
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			t.Setenv("CONFIG_ROOT", t.TempDir())
			t.Setenv(modelconnection.EnvConfig, "")
			modelID := "opencode/gpt-5.5"
			if tc.protocol == "" {
				t.Setenv("GRASP_OPENCODE_PROVIDER", "opencode")
				t.Setenv("GRASP_CONFIGURED_MODELS", "")
				t.Setenv("OPENCODE_API_KEY", "official-test-key")
			} else {
				modelID = "kimi-k3.1-pro"
				connection := modelconnection.Connection{
					ConnectionID: "variant-fixture", ProviderID: "gateway", Protocol: tc.protocol,
					BaseURL: "https://api.example.test/v1", Auth: modelconnection.Auth{Scheme: "bearer"},
					Models: []modelconnection.Model{{ID: modelID}, {ID: "fast/model"}}, SelectedModelID: modelID,
					ToolSettings: modelconnection.ToolSettings{ModelRoles: map[string]string{"fast": "fast/model"}},
				}
				if tc.protocol == modelconnection.Messages {
					connection.Auth.Scheme = "x-api-key"
				}
				data, err := json.Marshal(connection)
				if err != nil {
					t.Fatal(err)
				}
				t.Setenv(modelconnection.EnvConfig, string(data))
				t.Setenv(modelconnection.EnvAPIKey, "connection-test-key")
				modelID = connection.RouteID() + "/" + modelID
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var thoughts []string
			sess, err := Get(provider.OpenCode).Open(ctx, ctx, provider.OpenOptions{
				Cwd: t.TempDir(), Model: modelID, ReasoningEffort: tc.effort,
			}, func(raw json.RawMessage) {
				var event struct {
					Update struct {
						SessionUpdate string                `json:"sessionUpdate"`
						Content       struct{ Text string } `json:"content"`
					} `json:"update"`
				}
				if err := json.Unmarshal(raw, &event); err != nil {
					t.Error(err)
				}
				if event.Update.SessionUpdate == "agent_thought_chunk" {
					thoughts = append(thoughts, event.Update.Content.Text)
				}
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer sess.Close()
			result, err := sess.Prompt(ctx, "hello", nil)
			if err != nil || result.StopReason != "end_turn" {
				t.Fatalf("fixture turn: %+v, %v", result, err)
			}
			if strings.Join(thoughts, "") != "fixture model thought" {
				t.Fatalf("native reasoning did not reach chat events: %q", thoughts)
			}
			data, err := os.ReadFile(promptPath)
			if err != nil {
				t.Fatal(err)
			}
			var prompt opencodetest.Prompt
			if err := json.Unmarshal(data, &prompt); err != nil {
				t.Fatal(err)
			}
			if prompt.Model == nil || prompt.Model.ProviderID+"/"+prompt.Model.ModelID != modelID {
				t.Fatalf("model ID changed at the prompt boundary: %s", data)
			}
			if prompt.Variant != tc.effort {
				t.Fatalf("variant %q at the prompt boundary, want %q", prompt.Variant, tc.effort)
			}
			data, err = os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			var config map[string]any
			if err := json.Unmarshal(data, &config); err != nil {
				t.Fatal(err)
			}
			if config["model"] != modelID || tc.effort != "" && strings.Contains(string(data), "#"+tc.effort) {
				t.Fatalf("variant changed a configured model ID: %s", data)
			}
			if tc.protocol != "" {
				route := strings.TrimSuffix(modelID, "/kimi-k3.1-pro")
				entry := config["provider"].(map[string]any)[route].(map[string]any)
				models := entry["models"].(map[string]any)
				if models["kimi-k3.1-pro"] == nil || models["fast/model"].(map[string]any)["variants"] != nil || config["small_model"] != route+"/fast/model" {
					t.Fatalf("primary or fast model changed: %s", data)
				}
			}
		})
	}
}

func TestOfficialOpenCodeCustomVariantCannotFabricateModel(t *testing.T) {
	dir := installOpenCodeVariantFixture(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("CONFIG_ROOT", t.TempDir())
	t.Setenv(modelconnection.EnvConfig, "")
	t.Setenv("GRASP_OPENCODE_PROVIDER", "opencode")
	t.Setenv("GRASP_CONFIGURED_MODELS", "")
	t.Setenv("OPENCODE_API_KEY", "official-test-key")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess, err := Get(provider.OpenCode).Open(ctx, ctx, provider.OpenOptions{
		Cwd: t.TempDir(), Model: "opencode/missing-model", ReasoningEffort: "turbo",
	}, nil, nil)
	if err == nil {
		_ = sess.Close()
		t.Fatal("custom variant fabricated an official model missing from the native catalog")
	}
	if !strings.Contains(err.Error(), "opencode/missing-model") {
		t.Fatalf("missing official model diagnostic lost selected ID: %v", err)
	}
	assertOfficialOpenCodeCalls(t, filepath.Join(dir, "calls"), 1, 0)
	for _, file := range []string{"prompt.json", "config.json"} {
		if _, err := os.Stat(filepath.Join(dir, file)); !os.IsNotExist(err) {
			t.Fatalf("catalog validation reached native run: file=%s err=%v", file, err)
		}
	}
}
