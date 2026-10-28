package modelconnection

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"backend/internal/backend/common"
	"backend/internal/provider"
)

func TestOpenCodeOfficialModeIsolatesRoutingAndKeepsHistory(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "skills"), 0o700); err != nil {
		t.Fatal(err)
	}
	prior := `{"model":"thirdparty/old","provider":{"opencode":{"options":{"baseURL":"https://old.example","apiKey":"old-key"},"models":{"old":{"provider":{"api":"https://old.example","npm":"old-sdk"}}}}},"plugin":["old-plugin"],"mcp":{"test":{"type":"local","command":["test-mcp"]}},"instructions":["AGENTS.md"]}`
	if err := os.WriteFile(filepath.Join(root, "opencode.json"), []byte(prior), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"GRASP_OPENCODE_PROVIDER=opencode", "OPENCODE_API_KEY=official-test-key", "HOME=/original-home",
		"OPENCODE_CONFIG_CONTENT=" + prior, "OPENCODE_CONFIG=/old/config", "OPENCODE_CONFIG_DIR=/old/config-dir",
		"OPENCODE_MODELS_PATH=/old/catalog", "OPENCODE_MODELS_URL=https://old.example", "OPENCODE_CONSOLE_TOKEN=old-login",
		"OPENAI_API_KEY=old-openai", "CONFIG_ROOT=" + root}
	before := append([]string(nil), env...)
	var dataHome string
	for _, model := range []string{"opencode/official-test-model", "", "auto"} {
		p, err := Prepare(provider.OpenCode, env, model, "/unused")
		if err != nil {
			t.Fatal(err)
		}
		if !p.Platform || p.Connection != nil || !reflect.DeepEqual(before, env) {
			t.Fatal("official mode must not create a generic connection or mutate its input")
		}
		if common.GetEnvValue(p.Env, "HOME") != "/original-home" || common.GetEnvValue(p.Env, "OPENCODE_API_KEY") != "official-test-key" {
			t.Fatal("official credential or command HOME changed")
		}
		for _, key := range []string{"OPENCODE_CONFIG", "OPENCODE_MODELS_PATH", "OPENCODE_MODELS_URL", "OPENCODE_CONSOLE_TOKEN", "OPENAI_API_KEY"} {
			if common.GetEnvValue(p.Env, key) != "" {
				t.Errorf("stale routing environment survived: %s", key)
			}
		}
		var config map[string]any
		raw := common.GetEnvValue(p.Env, "OPENCODE_CONFIG_CONTENT")
		if err := json.Unmarshal([]byte(raw), &config); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(raw, "official-test-key") || strings.Contains(raw, "old") || config["mcp"] == nil || config["instructions"] == nil {
			t.Fatal("routing/key was copied or managed MCP/instructions were lost")
		}
		if !reflect.DeepEqual(config["enabled_providers"], []any{"opencode"}) || len(config["provider"].(map[string]any)) != 1 {
			t.Fatal("official mode enabled a third-party provider")
		}
		if model == "opencode/official-test-model" {
			if config["model"] != model || config["small_model"] != model {
				t.Fatal("main and auxiliary model are not pinned to the official provider")
			}
		} else if config["model"] != nil || p.Model != "" {
			t.Fatal("empty default must let the native official catalog choose its model")
		}
		if common.GetEnvValue(p.Env, "OPENCODE_DISABLE_PROJECT_CONFIG") != "true" {
			t.Fatal("project providers can override the official route")
		}
		configDir := common.GetEnvValue(p.Env, "OPENCODE_CONFIG_DIR")
		if _, err := os.Stat(filepath.Join(configDir, "skills")); err != nil {
			t.Fatal("managed skills were not preserved")
		}
		if dataHome != "" && dataHome != common.GetEnvValue(p.Env, "XDG_DATA_HOME") {
			t.Fatal("session history namespace changed between Open calls")
		}
		dataHome = common.GetEnvValue(p.Env, "XDG_DATA_HOME")
		if err := os.WriteFile(filepath.Join(dataHome, "history-fixture"), []byte("history"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := p.ValidateOptions(provider.OpenCode, provider.OpenOptions{CustomArgs: []string{"--model", "thirdparty/other"}}); err == nil {
			t.Fatal("custom model argument bypassed official mode")
		}
		p.Cleanup()
		if _, err := os.Stat(configDir); !os.IsNotExist(err) {
			t.Fatal("temporary routing configuration was not removed")
		}
		if _, err := os.Stat(filepath.Join(dataHome, "history-fixture")); err != nil {
			t.Fatal("session cleanup removed persistent history")
		}
	}
	if data, err := os.ReadFile(filepath.Join(root, "opencode.json")); err != nil || string(data) != prior {
		t.Fatal("official mode changed the saved configuration")
	}
}

func TestOpenCodeOfficialModeRejectsOtherProviderAndLeavesConnectionMode(t *testing.T) {
	for _, model := range []string{"openai/gpt-example", "raw-model", "opencode/", "opencode/  ", "opencode/vendor/model", "opencode-go/", "opencode-go/a/b"} {
		if _, err := Prepare(provider.OpenCode, []string{"GRASP_OPENCODE_PROVIDER=opencode", "OPENCODE_API_KEY=official-test-key"}, model, t.TempDir()); err == nil {
			t.Errorf("nonofficial model accepted: %q", model)
		}
	}
	c := connection(Chat)
	env := append(environ(t, c), "GRASP_OPENCODE_PROVIDER=opencode", "OPENCODE_API_KEY=official-test-key")
	p, err := Prepare(provider.OpenCode, env, "opencode/other", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Cleanup()
	if p.Platform || p.Connection == nil || p.Model != c.RouteID()+"/"+c.SelectedModelID || common.GetEnvValue(p.Env, "OPENCODE_API_KEY") != "" {
		t.Fatal("official authorization overrode the explicit API connection")
	}
}

func TestOfficialModesRejectInvalidKeyBeforeWriting(t *testing.T) {
	for _, test := range []struct {
		tool     provider.Name
		variable string
	}{{provider.OpenCode, "OPENCODE_API_KEY"}, {provider.OpenCode, "GRASP_OPENCODE_API_KEY"}, {provider.DeepSeekHarness, "DEEPSEEK_API_KEY"}} {
		for index, key := range []string{"", " \t ", "synthetic\rkey", "synthetic\nkey", "synthetic\x00key"} {
			t.Run(test.variable+string(rune('0'+index)), func(t *testing.T) {
				temporary := t.TempDir()
				t.Setenv("TMPDIR", temporary)
				root := filepath.Join(t.TempDir(), "not-created")
				env := []string{"GRASP_OPENCODE_PROVIDER=opencode", test.variable + "=" + key}
				if _, err := Prepare(test.tool, env, "", root); err == nil || strings.Contains(err.Error(), "synthetic") {
					t.Fatal("missing or malformed key accepted or disclosed")
				}
				if _, err := os.Stat(root); !os.IsNotExist(err) {
					t.Fatal("invalid authorization created session storage")
				}
				if entries, err := os.ReadDir(temporary); err != nil || len(entries) != 0 {
					t.Fatal("invalid authorization wrote temporary configuration")
				}
			})
		}
	}
}

func TestOpenCodeGoOfficialModeEnablesOnlyGoProvider(t *testing.T) {
	root := t.TempDir()
	env := []string{"GRASP_OPENCODE_PROVIDER=opencode", "OPENCODE_API_KEY=official-test-key", "CONFIG_ROOT=" + root}
	model := "opencode-go/mimo-v2.5-pro"
	p, err := Prepare(provider.OpenCode, env, model, "/unused")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Cleanup()
	raw := common.GetEnvValue(p.Env, "OPENCODE_CONFIG_CONTENT")
	var config map[string]any
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config["enabled_providers"], []any{"opencode-go"}) || config["model"] != model || config["small_model"] != model {
		t.Fatalf("go provider: %s", raw)
	}
	providers := config["provider"].(map[string]any)
	if len(providers) != 1 || providers["opencode"] != nil {
		t.Fatalf("zen provider still enabled: %s", raw)
	}
	options := providers["opencode-go"].(map[string]any)["options"].(map[string]any)
	if options["apiKey"] != "{env:OPENCODE_API_KEY}" || options["baseURL"] != nil || strings.Contains(raw, "official-test-key") || strings.Contains(raw, "npm") {
		t.Fatalf("native endpoint or key copied: %s", raw)
	}
	if common.GetEnvValue(p.Env, "OPENCODE_API_KEY") != "official-test-key" {
		t.Fatal("selected key was not mapped to OPENCODE_API_KEY")
	}
}

func TestOpenCodeGoFamilyFromAuxiliaryModelsWhenPrimaryIsEmpty(t *testing.T) {
	env := []string{"GRASP_OPENCODE_PROVIDER=opencode", "OPENCODE_API_KEY=official-test-key",
		`GRASP_CONFIGURED_MODELS=[{"id":""},{"id":"opencode-go/mimo-v2.5"},{"id":"opencode-go/mimo-v2.5-pro"}]`}
	p, err := Prepare(provider.OpenCode, env, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Cleanup()
	var config map[string]any
	if err := json.Unmarshal([]byte(common.GetEnvValue(p.Env, "OPENCODE_CONFIG_CONTENT")), &config); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config["enabled_providers"], []any{"opencode-go"}) || config["model"] != nil || len(config["provider"].(map[string]any)) != 1 {
		t.Fatalf("empty primary should still enable opencode-go: %v", config)
	}
	mixed := []string{"GRASP_OPENCODE_PROVIDER=opencode", "OPENCODE_API_KEY=official-test-key",
		`GRASP_CONFIGURED_MODELS=[{"id":"opencode-go/mimo-v2.5"},{"id":"opencode/kimi-k3"}]`}
	if _, err := Prepare(provider.OpenCode, mixed, "", t.TempDir()); err == nil || !strings.Contains(err.Error(), "one model family") {
		t.Fatalf("mixed families accepted: %v", err)
	}
}

func TestOpenCodeOfficialModeVersionGate(t *testing.T) {
	for text, want := range map[string]bool{"1.4.0": false, "1.4.1": true, "1.19.4": true, "2.0.0": true, "1.4.1-beta": false, "unknown": false} {
		if supportedOpenCodeVersion(text) != want {
			t.Errorf("version %q support differs from %v", text, want)
		}
		binary := filepath.Join(t.TempDir(), "opencode")
		if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' '"+text+"'\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := CheckVersion(context.Background(), provider.OpenCode, binary, nil); (err == nil) != want {
			t.Errorf("version %q: %v", text, err)
		}
	}
}

func TestDeepSeekPlatformDropsAlternateCredentialAndEndpointEnvironment(t *testing.T) {
	env := []string{"DEEPSEEK_API_KEY=official-test-key", "DEEPSEEK_BASE_URL=https://old.example", "DEEPSEEK_SEARCH_BASE_URL=https://old.example/search",
		"GRASP_DEEPSEEK_API_KEY=old-key", "OPENAI_API_KEY=old-other-key", "DSH_HOME=/role-home", "PATH=/bin"}
	p, err := Prepare(provider.DeepSeekHarness, env, "deepseek-v4-pro", "")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Cleanup()
	if common.GetEnvValue(p.Env, "DEEPSEEK_API_KEY") != "official-test-key" || common.GetEnvValue(p.Env, "DSH_HOME") != "/role-home" {
		t.Fatal("official key or native history root lost")
	}
	for _, key := range []string{"DEEPSEEK_BASE_URL", "DEEPSEEK_SEARCH_BASE_URL", "GRASP_DEEPSEEK_API_KEY", "OPENAI_API_KEY"} {
		if common.GetEnvValue(p.Env, key) != "" {
			t.Errorf("stale official route setting survived: %s", key)
		}
	}
}
