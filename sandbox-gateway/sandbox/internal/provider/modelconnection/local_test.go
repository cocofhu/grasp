package modelconnection

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backend/internal/backend/common"
	"backend/internal/provider"
)

func TestLocalOpenCodeKeepsSavedKeysOutOfEnvironment(t *testing.T) {
	root := t.TempDir()
	config := `{"model":"saved/main","provider":{"saved":{"options":{"apiKey":"private-host-token"}}},"mcp":{"managed":{"type":"local","command":["mcp"]}}}`
	path := filepath.Join(root, "opencode.json")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"main", "saved/main", "auto"} {
		p, err := Prepare(provider.OpenCode, []string{"GRASP_LOCAL_AUTH=1", "CONFIG_ROOT=" + root}, model, "unused")
		if err != nil {
			t.Fatal(err)
		}
		defer p.Cleanup()
		if common.GetEnvValue(p.Env, "OPENCODE_CONFIG") != path {
			t.Fatal("native CLI did not receive the saved configuration path")
		}
		if model != "auto" {
			if p.Model != "saved/main" || p.ChatModelID() != "main" {
				t.Fatalf("saved provider route lost: model=%q, route=%q", p.Model, p.RouteID)
			}
			p, err = ApplyOpenCodeVariant(p, "high")
			if err != nil {
				t.Fatal(err)
			}
			variant := configJSON(t, p)["provider"].(map[string]any)["saved"].(map[string]any)["models"].(map[string]any)["main"].(map[string]any)["variants"]
			if variant == nil {
				t.Fatal("local variant was not declared on the selected provider/model")
			}
		}
		if strings.Contains(strings.Join(p.Env, "\n"), "private-host-token") {
			t.Fatal("saved credentials leaked into process environment")
		}
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != config {
		t.Fatal("per-turn configuration modified the saved host snapshot")
	}
}

func TestLocalOpenCodeOfficialAuthIsPrivateCopy(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	dataHome := filepath.Join(home, ".local", "share")
	if err := os.MkdirAll(filepath.Join(dataHome, "opencode"), 0o700); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dataHome, "opencode", "auth.json")
	const auth = `{"opencode":{"type":"api","key":"local-login"}}`
	if err := os.WriteFile(src, []byte(auth), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"GRASP_LOCAL_AUTH=1", "GRASP_OPENCODE_PROVIDER=opencode", "HOME=" + home, "CONFIG_ROOT=" + root}
	p, err := Prepare(provider.OpenCode, env, "opencode/test-model", root)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Cleanup()
	dst := filepath.Join(common.GetEnvValue(p.Env, "XDG_DATA_HOME"), "opencode", "auth.json")
	info, err := os.Lstat(dst)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("auth must be a private regular file: %v, %v", info, err)
	}
	if data, err := os.ReadFile(dst); err != nil || string(data) != auth {
		t.Fatal("native login was not materialized")
	}
	if err := os.WriteFile(dst, []byte("new-login"), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(src); err != nil || string(data) != auth {
		t.Fatal("CLI auth update wrote back to the source login")
	}
	if strings.Contains(strings.Join(p.Env, "\n"), "local-login") {
		t.Fatal("login leaked into the environment")
	}
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	next, err := Prepare(provider.OpenCode, env, "opencode/test-model", root)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Cleanup()
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Fatal("removed login survived in the official session store")
	}
}

func TestLocalOpenCodeAuthCannotFollowLinkOutsideDataHome(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	if err := os.Symlink(target, filepath.Join(source, "opencode")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "auth.json"), []byte("outside-login"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyLocalOpenCodeAuth([]string{"XDG_DATA_HOME=" + source}, t.TempDir()); err == nil {
		t.Fatal("accepted a credential symlink outside the private source")
	}
}
