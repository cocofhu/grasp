package kimicode

import (
	"os"
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
