package modelconnection

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"backend/internal/backend/common"
	"backend/internal/provider"
	"gopkg.in/yaml.v3"
)

func connection(protocol string) Connection {
	auth := "bearer"
	if protocol == Messages {
		auth = "x-api-key"
	}
	return Connection{ConnectionID: "connection-team-a", ProviderID: "gateway", Protocol: protocol,
		BaseURL: "https://gateway.example/prefix/v1", Auth: Auth{Scheme: auth},
		Models:          []Model{{ID: "vendor/model/with/slashes", ContextWindow: 128000, MaxTokens: 4096}, {ID: "fast/model"}},
		SelectedModelID: "vendor/model/with/slashes"}
}

func environ(t *testing.T, c Connection) []string {
	t.Helper()
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return []string{EnvConfig + "=" + string(data), EnvAPIKey + "=selected-secret-12345", "PATH=/bin",
		"ANTHROPIC_API_KEY=old-claude", "ANTHROPIC_AUTH_TOKEN=old-login", "CLAUDE_CODE_OAUTH_TOKEN=old-oauth",
		"CODEBUDDY_API_KEY=old-buddy", "OPENAI_API_KEY=old-openai", "DEEPSEEK_API_KEY=old-deepseek",
		"GRASP_OPENCODE_PROVIDER=old-provider", "OPENROUTER_API_KEY=old-router", "CLAUDE_CODE_USE_BEDROCK=1"}
}

func prepare(t *testing.T, name provider.Name, c Connection) Prepared {
	t.Helper()
	p, err := Prepare(name, environ(t, c), "stale-platform-model", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Cleanup)
	return p
}

func TestPlatformEnvironmentUnchanged(t *testing.T) {
	env := []string{"CODEX_HOME=/original", "OPENAI_API_KEY=legacy"}
	p, err := Prepare(provider.Codex, env, "platform-model", "/original")
	if err != nil || !reflect.DeepEqual(env, p.Env) || p.Model != "platform-model" || p.Connection != nil || len(p.Args) != 0 {
		t.Fatalf("platform changed: %+v %v", p, err)
	}
	p.Cleanup()
}

func TestSelectedConnectionIsolationAndIdentity(t *testing.T) {
	c := connection(Chat)
	p := prepare(t, provider.OpenCode, c)
	for _, name := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CODEBUDDY_API_KEY", "OPENAI_API_KEY", "DEEPSEEK_API_KEY", "OPENROUTER_API_KEY", "GRASP_OPENCODE_PROVIDER", "CLAUDE_CODE_USE_BEDROCK"} {
		if common.GetEnvValue(p.Env, name) != "" {
			t.Errorf("old auth survived: %s", name)
		}
	}
	if common.GetEnvValue(p.Env, EnvAPIKey) != "selected-secret-12345" || common.GetEnvValue(p.Env, "PATH") != "/bin" {
		t.Fatal("selected secret or process environment lost")
	}
	other := c
	other.ConnectionID = "connection-team-b"
	if c.RouteID() == other.RouteID() || c.RouteID() != c.RouteID() {
		t.Fatal("route must be stable per connection and unique across accounts")
	}
	if p.Model != c.RouteID()+"/"+c.SelectedModelID {
		t.Fatal("OpenCode must prefix route without splitting upstream model ID")
	}
}

func TestOpenCodeUsesExplicitProtocolAndEnvironmentReference(t *testing.T) {
	for protocol, npm := range map[string]string{Chat: "@ai-sdk/openai-compatible", Responses: "@ai-sdk/openai", Messages: "@ai-sdk/anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			c := connection(protocol)
			p := prepare(t, provider.OpenCode, c)
			raw := common.GetEnvValue(p.Env, "OPENCODE_CONFIG_CONTENT")
			if strings.Contains(raw, "selected-secret") || strings.Contains(raw, "old-") {
				t.Fatal("inline config leaked a key or retained another provider")
			}
			var doc struct {
				Model      string
				SmallModel string `json:"small_model"`
				Provider   map[string]struct {
					NPM     string
					Options map[string]string
					Models  map[string]any
				}
			}
			if err := json.Unmarshal([]byte(raw), &doc); err != nil {
				t.Fatal(err)
			}
			entry := doc.Provider[p.RouteID]
			if entry.NPM != npm || entry.Options["apiKey"] != "{env:"+EnvAPIKey+"}" || entry.Models[c.SelectedModelID] == nil || doc.SmallModel != doc.Model {
				t.Fatalf("invalid explicit provider: %s", raw)
			}
		})
	}
	c := connection(Messages)
	c.BaseURL = "https://api.deepseek.com/anthropic"
	if openCodeBaseURL(&c) != c.BaseURL+"/v1" {
		t.Fatal("Anthropic SDK requires versioned base URL")
	}
}

func TestMessagesRootsKeepGatewayPrefixAcrossNativeClients(t *testing.T) {
	for _, test := range []struct{ base, root string }{
		{"https://gateway.example", "https://gateway.example"},
		{"https://gateway.example/v1", "https://gateway.example"},
		{"https://gateway.example/prefix", "https://gateway.example/prefix"},
		{"https://gateway.example/prefix/v1/", "https://gateway.example/prefix"},
		{"https://gateway.example/v1/gateway", "https://gateway.example/v1/gateway"},
	} {
		t.Run(test.base, func(t *testing.T) {
			c := connection(Messages)
			c.BaseURL = test.base
			for _, tool := range []provider.Name{provider.ClaudeCode, provider.DeepSeekHarness} {
				p := prepare(t, tool, c)
				base := common.GetEnvValue(p.Env, "ANTHROPIC_BASE_URL")
				if tool == provider.DeepSeekHarness {
					var rows []map[string]any
					if err := yaml.Unmarshal(p.DSHPatch, &rows); err != nil {
						t.Fatal(err)
					}
					for _, row := range rows {
						if row["id"] == "llm-pi-ai" {
							base = row["config"].(map[string]any)["providers"].(map[string]any)[p.RouteID].(map[string]any)["baseURL"].(string)
						}
					}
				}
				if base != test.root {
					t.Errorf("%s native root = %q, want %q", tool, base, test.root)
				}
				if p.Connection.BaseURL != test.base {
					t.Fatal("adapter changed the saved connection URL")
				}
			}
			if got := openCodeBaseURL(&c); got != test.root+"/v1" {
				t.Errorf("OpenCode versioned root = %q", got)
			}
			for _, protocol := range []string{Chat, Responses} {
				c.Protocol = protocol
				if nativeBaseURL(&c) != test.base {
					t.Fatal("Messages normalization changed an OpenAI protocol endpoint")
				}
			}
		})
	}
}

func TestClaudePinsEveryRoleWithoutStoredAuthentication(t *testing.T) {
	for _, auth := range []string{"x-api-key", "bearer"} {
		t.Run(auth, func(t *testing.T) {
			c := connection(Messages)
			c.Auth.Scheme = auth
			c.ToolSettings.ModelRoles = map[string]string{"fast": "fast/model", "haiku": "fast/model", "subagent": "fast/model"}
			p := prepare(t, provider.ClaudeCode, c)
			for variable, model := range map[string]string{"ANTHROPIC_DEFAULT_OPUS_MODEL": c.SelectedModelID, "ANTHROPIC_DEFAULT_SONNET_MODEL": c.SelectedModelID, "ANTHROPIC_DEFAULT_HAIKU_MODEL": "fast/model", "ANTHROPIC_SMALL_FAST_MODEL": "fast/model", "CLAUDE_CODE_SUBAGENT_MODEL": "fast/model"} {
				if common.GetEnvValue(p.Env, variable) != model {
					t.Errorf("%s not pinned", variable)
				}
			}
			nativeKey, otherKey := "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"
			if auth == "bearer" {
				nativeKey, otherKey = otherKey, nativeKey
			}
			if common.GetEnvValue(p.Env, nativeKey) != "selected-secret-12345" || common.GetEnvValue(p.Env, otherKey) != "" {
				t.Fatal("conflicting Claude auth")
			}
			dir := common.GetEnvValue(p.Env, "CLAUDE_CONFIG_DIR")
			data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "secret") || !strings.Contains(strings.Join(p.Args, " "), "--setting-sources user") {
				t.Fatal("native config must contain no secret and must isolate project settings")
			}
			p.Cleanup()
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatal("session config not cleaned")
			}
		})
	}
}

func TestCodeBuddyModelsAndRoles(t *testing.T) {
	for _, protocol := range []string{Chat} {
		t.Run(protocol, func(t *testing.T) {
			c := connection(protocol)
			c.ToolSettings.ModelRoles = map[string]string{"fast": "fast/model", "reasoning": "fast/model", "subagent": "fast/model"}
			p := prepare(t, provider.CodeBuddy, c)
			for _, variable := range []string{"CODEBUDDY_SMALL_FAST_MODEL", "CODEBUDDY_BIG_SLOW_MODEL", "CODEBUDDY_CODE_SUBAGENT_MODEL"} {
				if common.GetEnvValue(p.Env, variable) != "fast/model" {
					t.Errorf("%s not pinned", variable)
				}
			}
			dir := common.GetEnvValue(p.Env, "CODEBUDDY_CONFIG_DIR")
			data, err := os.ReadFile(filepath.Join(dir, "models.json"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "selected-secret") {
				t.Fatal("API key was written into models.json")
			}
			var doc struct{ Models []map[string]any }
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatal(err)
			}
			m := doc.Models[0]
			if m["id"] != c.SelectedModelID || m["apiKey"] != "${"+EnvAPIKey+"}" {
				t.Fatal("upstream model or env reference lost")
			}
			if m["url"] != c.BaseURL+"/chat/completions" {
				t.Fatal("Chat Completions needs complete endpoint")
			}
		})
	}
}

func TestCodexUsesResponsesProviderAndPreservesPlatformHome(t *testing.T) {
	c := connection(Responses)
	env := append(environ(t, c), "CODEX_HOME=/original-auth-home")
	p, err := Prepare(provider.Codex, env, "old-model", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Cleanup()
	args := strings.Join(p.Args, " ")
	for _, want := range []string{"model_provider=", "wire_api=\"responses\"", "env_key=\"" + EnvAPIKey + "\"", "requires_openai_auth=false", c.BaseURL} {
		if !strings.Contains(args, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Contains(args, "selected-secret") || common.GetEnvValue(p.Env, "CODEX_HOME") != "/original-auth-home" || p.Model != c.SelectedModelID {
		t.Fatal("custom provider must use separate env credential and preserve auth home")
	}
}

func TestDSHThreeProtocolsAndNativeRoute(t *testing.T) {
	for _, protocol := range []string{Chat, Responses, Messages} {
		t.Run(protocol, func(t *testing.T) {
			c := connection(protocol)
			p := prepare(t, provider.DeepSeekHarness, c)
			var rows []map[string]any
			if err := yaml.Unmarshal(p.DSHPatch, &rows); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(p.DSHPatch), "selected-secret") {
				t.Fatal("DSH patch leaked secret")
			}
			found := false
			for _, row := range rows {
				if row["id"] == "llm-pi-ai" {
					config := row["config"].(map[string]any)
					routes := config["providers"].(map[string]any)
					route := routes[p.RouteID].(map[string]any)
					want := protocol
					if protocol == Chat {
						want = "openai-completions"
					}
					if route["api"] != want || route["apiKeyEnv"] != EnvAPIKey {
						t.Fatal("wrong dsh route protocol/auth reference")
					}
					found = true
				}
			}
			if !found {
				t.Fatal("pi-ai adapter missing")
			}
		})
	}
	c := connection(Messages)
	c.ProviderID = "deepseek"
	c.BaseURL = "https://api.deepseek.com/anthropic"
	c.ToolSettings.RouteProfile = "deepseek-native"
	p := prepare(t, provider.DeepSeekHarness, c)
	if p.RouteID != "deepseek-official" || !strings.Contains(string(p.DSHPatch), "id: llm-deepseek") || strings.Contains(string(p.DSHPatch), "api: anthropic-messages") {
		t.Fatal("native adapter must retain its own route and transport")
	}
}

func TestRejectsInvalidConnectionWithoutFallback(t *testing.T) {
	for _, test := range []struct {
		name   string
		tool   provider.Name
		change func(*Connection)
	}{
		{"missing identity", provider.OpenCode, func(c *Connection) { c.ConnectionID = "" }},
		{"URL userinfo", provider.OpenCode, func(c *Connection) { c.BaseURL = "https://user:password@example.com/v1" }},
		{"URL query", provider.OpenCode, func(c *Connection) { c.BaseURL += "?token=private" }},
		{"unknown protocol", provider.OpenCode, func(c *Connection) { c.Protocol = "imaginary" }},
		{"unknown selected model", provider.OpenCode, func(c *Connection) { c.SelectedModelID = "not-in-connection" }},
		{"duplicate model", provider.OpenCode, func(c *Connection) { c.Models = append(c.Models, c.Models[0]) }},
		{"role model from other connection", provider.OpenCode, func(c *Connection) { c.ToolSettings.ModelRoles = map[string]string{"fast": "elsewhere"} }},
		{"unsupported role", provider.OpenCode, func(c *Connection) { c.ToolSettings.ModelRoles = map[string]string{"opus": c.SelectedModelID} }},
		{"main conflict", provider.OpenCode, func(c *Connection) { c.ToolSettings.ModelRoles = map[string]string{"main": "fast/model"} }},
		{"unsupported tool", provider.Cursor, func(*Connection) {}},
		{"Codex Chat", provider.Codex, func(*Connection) {}},
		{"Claude Chat", provider.ClaudeCode, func(*Connection) {}},
		{"CodeBuddy Responses", provider.CodeBuddy, func(c *Connection) { c.Protocol = Responses }},
		{"CodeBuddy Messages", provider.CodeBuddy, func(c *Connection) { c.Protocol = Messages; c.Auth.Scheme = "x-api-key" }},
		{"CodeBuddy ACP Messages", provider.CodeBuddyACP, func(c *Connection) { c.Protocol = Messages; c.Auth.Scheme = "x-api-key" }},
		{"unknown auth", provider.OpenCode, func(c *Connection) { c.Auth.Scheme = "custom-header"; c.Auth.HeaderName = "X-Secret" }},
		{"unknown route profile", provider.OpenCode, func(c *Connection) { c.ToolSettings.RouteProfile = "native" }},
		{"native nonofficial service", provider.DeepSeekHarness, func(c *Connection) {
			c.Protocol = Messages
			c.Auth.Scheme = "x-api-key"
			c.ToolSettings.RouteProfile = "deepseek-native"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := connection(Chat)
			test.change(&c)
			if _, err := Prepare(test.tool, environ(t, c), "old", t.TempDir()); err == nil {
				t.Fatal("invalid selection fell through to native account")
			}
		})
	}
	c := connection(Chat)
	for _, env := range [][]string{{EnvConfig + "={invalid", "OPENAI_API_KEY=old"}, {EnvConfig + "=null", EnvAPIKey + "=selected"}} {
		if _, err := Prepare(provider.OpenCode, env, "old", t.TempDir()); err == nil {
			t.Fatal("invalid JSON accepted")
		}
	}
	env := environ(t, c)
	env[1] = EnvAPIKey + "="
	if _, err := Prepare(provider.OpenCode, env, "old", t.TempDir()); err == nil {
		t.Fatal("missing selected secret cannot use old native key")
	}
}

func TestIsolatedConfigPreservesAssetsWithoutLogin(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "skills"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "auth.json"), []byte("old-login-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir, cleanup, err := isolatedConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if _, err := os.Stat(filepath.Join(dir, "skills")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "auth.json")); !os.IsNotExist(err) {
		t.Fatal("login file copied into connection config")
	}
}
