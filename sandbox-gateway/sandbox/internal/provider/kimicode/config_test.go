package kimicode

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLaunchArgvIgnoresModel(t *testing.T) {
	argv := LaunchArgv("kimi-k3.1-pro")
	if strings.Join(argv, " ") != "kimi acp" {
		t.Fatal(argv)
	}
	for _, flag := range argv {
		if flag == "-m" || flag == "--model" || flag == "--login" || strings.HasPrefix(flag, "-m") {
			t.Fatalf("acp argv must not select a model or log in: %v", argv)
		}
	}
}

func TestReasoningConfigUsesExactK3CapabilitiesAndNativeDefault(t *testing.T) {
	for _, model := range []string{"k3", "k3.1-pro", "k3.1-pro-highspeed", "k3-256k", "kimi-for-coding", "kimi-k3", "kimi-k3.1-pro", "kimi-k3.1-pro-highspeed"} {
		if !reflect.DeepEqual(ReasoningEfforts(model), []string{"low", "high", "max"}) {
			t.Fatalf("missing exact model capabilities: %s", model)
		}
		for _, effort := range []string{"", "low", "high", "max"} {
			if err := validateReasoningEffort(model, effort); err != nil {
				t.Fatal(err)
			}
			config := renderConfig(intlBase, model, effort)
			for _, declaration := range []string{`"thinking"`, `support_efforts = ["low", "high", "max"]`, "default_effort = " + tomlString(defaultReasoningEffort(model))} {
				if !strings.Contains(config, declaration) {
					t.Fatalf("missing native declaration %q for %s", declaration, model)
				}
			}
			if effort == "" && strings.Contains(config, "[thinking]") {
				t.Fatal("empty reset must retain the native default")
			}
			if effort != "" && !strings.Contains(config, "[thinking]\nenabled = true\neffort = "+tomlString(effort)) {
				t.Fatalf("selected strength missing for %s/%s", model, effort)
			}
		}
	}
	for _, model := range []string{"", "auto", "kimi-for-coding-highspeed", "kimi-k2.6", "kimi-k2.7-code", "kimi-k2.7-code-highspeed", "vendor/kimi-k3", "kimi-k3-preview"} {
		if len(ReasoningEfforts(model)) != 0 || validateReasoningEffort(model, "low") == nil {
			t.Fatalf("model spelling granted unsupported strength: %s", model)
		}
		if strings.Contains(RenderConfig(intlBase, model), "support_efforts") {
			t.Fatalf("unsupported model acquired strength declarations: %s", model)
		}
	}
	if err := writeConfig(t.TempDir(), intlBase, "kimi-k3", "ultra"); err == nil {
		t.Fatal("invalid strength reached a native config file")
	}
}

func TestCodingPlanModelsUseExactWireIDsAndRegionEndpoints(t *testing.T) {
	for model, wire := range map[string]string{
		"k3.1-pro": "k3.1-pro", "k3.1-pro-highspeed": "k3.1-pro-highspeed", "k3": "k3", "k3-256k": "k3-256k",
		"kimi-for-coding": "kimi-for-coding", "kimi-for-coding-highspeed": "kimi-for-coding-highspeed",
		"kimi-k3.1-pro": "k3.1-pro", "kimi-k3.1-pro-highspeed": "k3.1-pro-highspeed", "kimi-k3": "k3",
	} {
		config := RenderConfig(cnBase, model)
		if !strings.Contains(config, "default_model = "+tomlString(model)) || !strings.Contains(config, "model = "+tomlString(wire)+"\n") {
			t.Fatalf("saved identity or canonical wire ID changed: %s", model)
		}
	}
	if cn, err := BaseURL("cn"); err != nil || cn != "https://api.kimi.com/coding/v1" {
		t.Fatalf("wrong mainland Coding Plan endpoint: %s %v", cn, err)
	}
	if intl, err := BaseURL("intl"); err != nil || intl != "https://api.kimi.ai/coding/v1" {
		t.Fatalf("wrong international Coding Plan endpoint: %s %v", intl, err)
	}
	if contextTokens("k3-256k") != 262144 || contextTokens("k3.1-pro") != 1000000 || contextTokens("kimi-for-coding") != 1000000 || !strings.Contains(RenderConfig(cnBase, "kimi-for-coding"), "max_context_size = 1000000") {
		t.Fatal("Coding Plan model context capacity changed")
	}
	if !strings.Contains(RenderConfig(cnBase, "kimi-for-coding"), `default_effort = "max"`) {
		t.Fatal("K2.8 Preview must keep its documented max default")
	}
	if err := WriteConfig(t.TempDir(), cnBase, "invalid-model"); err == nil {
		t.Fatal("an unknown model reached native configuration and vendor fallback")
	}
}

func TestIsolatedHomesKeepDistinctConfigAndDurableHistory(t *testing.T) {
	root := t.TempDir()
	for _, entry := range []string{"config.toml", "credentials", "mcp.json"} {
		if err := os.WriteFile(filepath.Join(root, entry), []byte("retained asset"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	first, err := isolatedHome(root, intlBase, "kimi-k3", "low")
	if err != nil {
		t.Fatal(err)
	}
	second, err := isolatedHome(root, intlBase, "kimi-k3.1-pro", "max")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeConfig(first, intlBase, "kimi-k3", "low"); err != nil {
		t.Fatal(err)
	}
	if err := writeConfig(second, intlBase, "kimi-k3.1-pro", "max"); err != nil {
		t.Fatal(err)
	}
	for home, effort := range map[string]string{first: "low", second: "max"} {
		body, err := os.ReadFile(filepath.Join(home, "config.toml"))
		if err != nil || !strings.Contains(string(body), "effort = "+tomlString(effort)) {
			t.Fatalf("session config interfered with another home: %s %v", body, err)
		}
		if _, err := os.Stat(filepath.Join(home, "credentials")); !os.IsNotExist(err) {
			t.Fatal("saved credentials entered the isolated home")
		}
	}
	history := filepath.Join(first, "sessions", "history.json")
	if err := os.WriteFile(history, []byte("original history"), 0o600); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(second, "sessions", "history.json")); err != nil || string(body) != "original history" {
		t.Fatal("closing one session lost its native history", err)
	}
	if body, err := os.ReadFile(filepath.Join(root, "config.toml")); err != nil || string(body) != "retained asset" {
		t.Fatal("shared configuration was overwritten", err)
	}
	reused, err := isolatedHome(root, intlBase, "kimi-k3", "low")
	if err != nil || reused != first {
		t.Fatal("the same route must reuse its bounded stable home", err)
	}
	for _, entry := range []string{"locks", "session_index.jsonl"} {
		if target, err := os.Readlink(filepath.Join(reused, entry)); err != nil || target != filepath.Join(root, entry) {
			t.Fatalf("native shared %s was isolated from other sessions: %s %v", entry, target, err)
		}
	}
	index := []byte(`{"sessionId":"original","sessionDir":` + tomlString(filepath.Dir(history)) + `,"workDir":"/workspace"}` + "\n")
	if err := os.WriteFile(filepath.Join(first, "session_index.jsonl"), index, 0o600); err != nil {
		t.Fatal(err)
	}
	if indexed, err := os.ReadFile(filepath.Join(second, "session_index.jsonl")); err != nil || string(indexed) != string(index) {
		t.Fatal("native index was lost between routes", err)
	}
	if _, err := os.Stat(filepath.Dir(history)); err != nil {
		t.Fatal("absolute native index paths must stay valid", err)
	}
	before, err := os.Stat(filepath.Join(first, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeConfig(reused, intlBase, "kimi-k3", "low"); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(filepath.Join(first, "config.toml"))
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("reusing identical config replaced the watched native file", err)
	}
}

func TestRenderConfigOmitsSecretAndEmptyModel(t *testing.T) {
	secret := "sk-live-secret-value"
	empty := RenderConfig(intlBase, "")
	if strings.Contains(empty, secret) || strings.Contains(empty, "default_model") || strings.Contains(empty, "[models.") {
		t.Fatalf("empty model leaked a selection:\n%s", empty)
	}
	if !strings.Contains(empty, `api_key_env = "KIMI_CODE_API_KEY"`) || !strings.Contains(empty, intlBase) {
		t.Fatalf("provider table:\n%s", empty)
	}
	chosen := RenderConfig(cnBase, "kimi-k3.1-pro")
	if strings.Contains(chosen, secret) || !strings.Contains(chosen, `default_model = "kimi-k3.1-pro"`) || !strings.Contains(chosen, `[models."kimi-k3.1-pro"]`) || !strings.Contains(chosen, "max_context_size = 1000000") || !strings.Contains(chosen, cnBase) {
		t.Fatalf("chosen model:\n%s", chosen)
	}
	auto := RenderConfig(intlBase, "auto")
	if strings.Contains(auto, "default_model") {
		t.Fatal("auto must be omitted")
	}
}

func TestBaseURLRefusesUnknownSite(t *testing.T) {
	cn, err := BaseURL("domestic")
	if err != nil || cn != cnBase {
		t.Fatal(cn, err)
	}
	intl, err := BaseURL("")
	if err != nil || intl != intlBase {
		t.Fatal(intl, err)
	}
	if _, err := BaseURL("eu"); err == nil || !strings.Contains(err.Error(), "未知的 Kimi Code 服务站点") {
		t.Fatal(err)
	}
}

func TestWriteConfigRejectsRelativeDirectory(t *testing.T) {
	if err := WriteConfig("relative/kimi", intlBase, ""); err == nil || !strings.Contains(err.Error(), "absolute path") {
		t.Fatal(err)
	}
}

func TestWriteConfigKeepsTheKeyOutOfTheFile(t *testing.T) {
	dir := t.TempDir()
	secret := "sk-live-secret-value"
	if err := WriteConfig(dir, intlBase, ""); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(dir + "/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), secret) || !strings.Contains(string(body), `api_key_env = "KIMI_CODE_API_KEY"`) {
		t.Fatalf("config.toml:\n%s", body)
	}
}
