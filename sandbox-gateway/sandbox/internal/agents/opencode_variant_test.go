package agents

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backend/internal/provider"
	"backend/internal/provider/modelconnection"
)

// Record the subprocess boundary so a valid variant declaration cannot hide an
// invalid --model argument. The fixture never contacts a model service.
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
			dir := t.TempDir()
			argvPath := filepath.Join(dir, "argv")
			configPath := filepath.Join(dir, "config.json")
			fixture := `#!/bin/sh
set -eu
if [ "${1:-}" = "--version" ]; then
  printf '1.19.4\n'
  exit 0
fi
printf '%s\n' "$@" > "$GRASP_VARIANT_TEST_ARGV_OUT"
printf '%s' "$OPENCODE_CONFIG_CONTENT" > "$GRASP_VARIANT_TEST_CONFIG_OUT"
printf '%s\n' '{"type":"text","part":{"text":"connected"}}' '{"type":"step_finish","part":{"reason":"stop"}}'
`
			if err := os.WriteFile(filepath.Join(dir, "opencode"), []byte(fixture), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("GRASP_VARIANT_TEST_ARGV_OUT", argvPath)
			t.Setenv("GRASP_VARIANT_TEST_CONFIG_OUT", configPath)
			t.Setenv("CONFIG_ROOT", t.TempDir())
			t.Setenv(modelconnection.EnvConfig, "")
			modelID := "opencode/gpt-5.5"
			if tc.protocol == "" {
				t.Setenv("GRASP_OPENCODE_PROVIDER", "opencode")
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
			sess, err := Get(provider.OpenCode).Open(ctx, ctx, provider.OpenOptions{
				Cwd: t.TempDir(), Model: modelID, ReasoningEffort: tc.effort,
			}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer sess.Close()
			result, err := sess.Prompt(ctx, "hello", nil)
			if err != nil || result.StopReason != "end_turn" {
				t.Fatalf("fixture turn: %+v, %v", result, err)
			}
			data, err := os.ReadFile(argvPath)
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Split(strings.TrimSpace(string(data)), "\n")
			modelAt, variantAt := indexOfArg(args, "--model"), indexOfArg(args, "--variant")
			if modelAt < 0 || modelAt+1 >= len(args) || args[modelAt+1] != modelID {
				t.Fatalf("model ID changed at the subprocess boundary: %v", args)
			}
			if tc.effort == "" {
				if variantAt >= 0 {
					t.Fatalf("default forced a variant: %v", args)
				}
			} else if variantAt < 0 || variantAt+1 >= len(args) || args[variantAt+1] != tc.effort {
				t.Fatalf("variant missing at the subprocess boundary: %v", args)
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
