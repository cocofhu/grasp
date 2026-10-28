package sandbox

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestCodexConfigHomeProvidesNativeMCPAndPreservesUserSettings(t *testing.T) {
	work := t.TempDir()
	const original = `model = "user-model"
model_reasoning_effort = "low"
[model_providers.user]
base_url = "https://user.example/v1"
[mcp_servers.user-server]
command = "user-mcp"
[mcp_servers.artifact-store]
url = "https://old.example/mcp"
bearer_token_env_var = "OLD_AUTH_TOKEN"
default_tools_approval_mode = "prompt"
disabled_tools = ["write_artifact"]
required = true
[mcp_servers.artifact-store.http_headers]
Authorization = "Bearer old-synthetic-auth"
[mcp_servers.artifact-store.env_http_headers]
Authorization = "OLD_AUTH_TOKEN"
[mcp_servers.artifact-store.tools.get_history]
approval_mode = "prompt"
`
	if err := os.WriteFile(filepath.Join(work, "config.toml"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"first", "second"} {
		specs := []MCPServerSpec{
			{Name: "artifact-store", URL: "http://mcp.example/runs/" + route, Headers: map[string]string{"Authorization": "Bearer synthetic-" + route}},
			{Name: "local-tool", Command: "node", Args: []string{"/tools/mcp.js"}, Env: map[string]string{"FIXTURE": route}},
		}
		home, err := BuildConfigHome(ConfigHomeSpec{WorkDirSrc: work, Codex: true, MCP: specs})
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(home)
		path := filepath.Join(home, "config.toml")
		read := func() map[string]any {
			t.Helper()
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var config map[string]any
			if err := toml.Unmarshal(data, &config); err != nil {
				t.Fatal(err)
			}
			return config
		}
		config := read()
		servers := config["mcp_servers"].(map[string]any)
		http := servers["artifact-store"].(map[string]any)
		stdio := servers["local-tool"].(map[string]any)
		if config["model"] != "user-model" || config["model_reasoning_effort"] != "low" || config["model_providers"] == nil || servers["user-server"] == nil {
			t.Fatal("native MCP injection discarded user configuration")
		}
		if http["url"] != specs[0].URL || http["http_headers"].(map[string]any)["Authorization"] != specs[0].Headers["Authorization"] || stdio["command"] != "node" ||
			!reflect.DeepEqual(stdio["args"], []any{"/tools/mcp.js"}) || stdio["env"].(map[string]any)["FIXTURE"] != route {
			t.Fatal("native Codex HTTP/stdio MCP configuration does not match selected servers")
		}
		if http["bearer_token_env_var"] != nil || http["env_http_headers"] != nil || http["default_tools_approval_mode"] != "prompt" || http["required"] != true ||
			http["tools"].(map[string]any)["get_history"].(map[string]any)["approval_mode"] != "prompt" || !reflect.DeepEqual(http["disabled_tools"], []any{"write_artifact"}) {
			t.Fatal("transport refresh retained stale authorization or discarded explicit user policy")
		}
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatal("native MCP configuration containing authorization is not private")
		}
		// A refresh replaces the same managed entry rather than appending a
		// duplicate TOML table or retaining the previous scoped authorization.
		specs[0].URL += "/updated"
		if err := writeCodexMCPConfig(home, specs); err != nil {
			t.Fatal(err)
		}
		if read()["mcp_servers"].(map[string]any)["artifact-store"].(map[string]any)["url"] != specs[0].URL {
			t.Fatal("native Codex MCP refresh did not replace the managed server")
		}
	}
	if data, err := os.ReadFile(filepath.Join(work, "config.toml")); err != nil || string(data) != original {
		t.Fatal("building native MCP config modified the source workspace")
	}
}

func TestCodexNativeMCPRejectsMalformedUserConfigWithoutEchoingIt(t *testing.T) {
	work := t.TempDir()
	const content = `private-unparseable-credential = "unterminated`
	if err := os.WriteFile(filepath.Join(work, "config.toml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildConfigHome(ConfigHomeSpec{WorkDirSrc: work, Codex: true, MCP: []MCPServerSpec{{Name: "history", URL: "http://mcp.example"}}}); err == nil || strings.Contains(err.Error(), "private-unparseable-credential") {
		t.Fatal("malformed native config must fail without echoing its contents")
	}
}

func TestCodexNativeMCPDoesNotCreateEmptyConfig(t *testing.T) {
	home, err := BuildConfigHome(ConfigHomeSpec{Codex: true, MCP: []MCPServerSpec{{Name: "empty"}, {URL: "http://ignored.example"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	if _, err := os.Stat(filepath.Join(home, "config.toml")); !os.IsNotExist(err) {
		t.Fatal("an empty managed MCP list created a native config")
	}
}
