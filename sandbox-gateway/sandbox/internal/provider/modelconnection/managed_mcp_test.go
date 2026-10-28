package modelconnection

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"backend/internal/backend/common"
	"backend/internal/provider"
)

func TestConnectedOpenCodeKeepsCurrentManagedMCPWithoutOldRouting(t *testing.T) {
	c := connection(Chat)
	for _, session := range []string{"first-route", "second-route"} {
		t.Run(session, func(t *testing.T) {
			root := t.TempDir()
			url := "http://127.0.0.1:9999/mcp/runs/" + session
			managed := map[string]any{
				"mcp": map[string]any{"artifact-store": map[string]any{"type": "remote", "url": url,
					"headers": map[string]any{"Authorization": "Bearer synthetic-" + session}, "enabled": true}},
				"instructions": []any{filepath.Join(root, "AGENTS.md")},
				"model":        "old/model", "small_model": "old/fast", "enabled_providers": []string{"old"},
				"provider": map[string]any{"old": map[string]any{"options": map[string]any{"apiKey": "old-key"}}},
				"plugin":   []string{"old-plugin"},
			}
			data, err := json.Marshal(managed)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "opencode.json")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			env := append(environ(t, c), "CONFIG_ROOT="+root, "OPENCODE_CONFIG_CONTENT="+string(data))
			p, err := Prepare(provider.OpenCode, env, c.SelectedModelID, "/unused-image-config-root")
			if err != nil {
				t.Fatal(err)
			}
			defer p.Cleanup()
			var config map[string]any
			if err := json.Unmarshal([]byte(common.GetEnvValue(p.Env, "OPENCODE_CONFIG_CONTENT")), &config); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(config["mcp"], managed["mcp"]) || !reflect.DeepEqual(config["instructions"], managed["instructions"]) {
				t.Fatal("current worktree's native MCP servers or instructions were dropped")
			}
			if config["plugin"] != nil || config["model"] != c.RouteID()+"/"+c.SelectedModelID ||
				!reflect.DeepEqual(config["enabled_providers"], []any{c.RouteID()}) || len(config["provider"].(map[string]any)) != 1 ||
				config["provider"].(map[string]any)[c.RouteID()] == nil {
				t.Fatal("managed config preservation changed selected model/account routing")
			}
			if current, err := os.ReadFile(path); err != nil || string(current) != string(data) {
				t.Fatal("preparing a session modified the managed config")
			}
		})
	}
}

func TestOpenCodeManagedConfigMissingOrInvalidIsNotImported(t *testing.T) {
	for _, content := range []string{"", "{invalid"} {
		root := t.TempDir()
		if content != "" {
			if err := os.WriteFile(filepath.Join(root, "opencode.json"), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		p, err := Prepare(provider.OpenCode, environ(t, connection(Chat)), "", root)
		if err != nil {
			t.Fatal(err)
		}
		var config map[string]any
		if err := json.Unmarshal([]byte(common.GetEnvValue(p.Env, "OPENCODE_CONFIG_CONTENT")), &config); err != nil {
			t.Fatal(err)
		}
		p.Cleanup()
		if config["mcp"] != nil || config["instructions"] != nil {
			t.Fatal("invalid or absent managed config supplied native MCP settings")
		}
	}
}
