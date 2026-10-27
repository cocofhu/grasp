package modelconnection

import (
	"encoding/json"
	"strings"
	"testing"

	"backend/internal/backend/common"
	"backend/internal/provider"
)

func officialPrepared(t *testing.T, model string) Prepared {
	t.Helper()
	root := t.TempDir()
	env := []string{"GRASP_OPENCODE_PROVIDER=opencode", "OPENCODE_API_KEY=official-test-key", "CONFIG_ROOT=" + root}
	p, err := Prepare(provider.OpenCode, env, model, "/unused")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Cleanup)
	return p
}

func configJSON(t *testing.T, p Prepared) map[string]any {
	t.Helper()
	var config map[string]any
	if err := json.Unmarshal([]byte(common.GetEnvValue(p.Env, "OPENCODE_CONFIG_CONTENT")), &config); err != nil {
		t.Fatal(err)
	}
	return config
}

func TestOpenCodeDefaultDoesNotAttachVariant(t *testing.T) {
	p := officialPrepared(t, "opencode/gpt-5.5")
	next, err := ApplyOpenCodeVariant(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if next.Model != "opencode/gpt-5.5" || strings.Contains(next.Model, "#") {
		t.Fatalf("default attached a variant: %s", next.Model)
	}
	raw := common.GetEnvValue(next.Env, "OPENCODE_CONFIG_CONTENT")
	if strings.Contains(raw, "#") || strings.Contains(raw, "variants") {
		t.Fatalf("default rewrote the request: %s", raw)
	}
}

func TestOpenCodeCatalogVariantIsPassedThrough(t *testing.T) {
	p := officialPrepared(t, "opencode/gpt-5.5")
	next, err := ApplyOpenCodeVariant(p, "high")
	if err != nil {
		t.Fatal(err)
	}
	if next.Model != "opencode/gpt-5.5#high" {
		t.Fatalf("model: %s", next.Model)
	}
	config := configJSON(t, next)
	if config["model"] != "opencode/gpt-5.5#high" || config["small_model"] != "opencode/gpt-5.5#high" {
		t.Fatalf("config model: %v / %v", config["model"], config["small_model"])
	}
	raw := common.GetEnvValue(next.Env, "OPENCODE_CONFIG_CONTENT")
	if strings.Contains(raw, "variants") || strings.Contains(raw, "reasoningEffort") {
		t.Fatalf("catalog variant rewrote the protocol body: %s", raw)
	}
}

func TestOpenCodeCustomVariantIsDeclaredForTheRun(t *testing.T) {
	p := officialPrepared(t, "opencode/gpt-5.5")
	next, err := ApplyOpenCodeVariant(p, "turbo")
	if err != nil {
		t.Fatal(err)
	}
	if next.Model != "opencode/gpt-5.5#turbo" {
		t.Fatalf("model: %s", next.Model)
	}
	config := configJSON(t, next)
	provider := config["provider"].(map[string]any)["opencode"].(map[string]any)
	model := provider["models"].(map[string]any)["gpt-5.5"].(map[string]any)
	overlay := model["variants"].(map[string]any)["turbo"].(map[string]any)["settings"].(map[string]any)
	if overlay["reasoningEffort"] != "turbo" || overlay["reasoningSummary"] != "auto" {
		t.Fatalf("custom declaration: %#v", overlay)
	}
	if _, err := ApplyOpenCodeVariant(p, "not a variant"); err == nil {
		t.Fatal("invalid custom variant was accepted")
	}
}

func TestConnectedOpenCodeDeclaresEffortWithoutForcingDefault(t *testing.T) {
	c := connection(Chat)
	c.ProviderID = "tokenhub"
	c.BaseURL = "https://tokenhub.tencentmaas.com/v1"
	c.SelectedModelID = "deepseek-v4-flash"
	c.Models = []Model{{ID: "deepseek-v4-flash"}, {ID: "fast/model"}}
	c.ToolSettings.ModelRoles = map[string]string{"fast": "fast/model"}
	p := prepare(t, provider.OpenCode, c)
	if strings.Contains(p.Model, "#") || strings.Contains(common.GetEnvValue(p.Env, "OPENCODE_CONFIG_CONTENT"), "reasoningEffort") {
		t.Fatal("preparing a connection forced a reasoning effort")
	}
	next, err := ApplyOpenCodeVariant(p, "low")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(next.Model, "/deepseek-v4-flash#low") {
		t.Fatalf("model: %s", next.Model)
	}
	config := configJSON(t, next)
	if config["small_model"] == next.Model {
		t.Fatal("the fast model inherited the primary variant")
	}
	entry := config["provider"].(map[string]any)[next.RouteID].(map[string]any)
	model := entry["models"].(map[string]any)["deepseek-v4-flash"].(map[string]any)
	if _, forced := model["options"]; forced {
		t.Fatal("connection options forced a reasoning effort")
	}
	overlay := model["variants"].(map[string]any)["low"].(map[string]any)["settings"].(map[string]any)
	if overlay["reasoningEffort"] != "low" || overlay["reasoningSummary"] != nil {
		t.Fatalf("chat declaration: %#v", overlay)
	}
}
