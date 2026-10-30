package kimicode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backend/internal/provider"
	"github.com/pelletier/go-toml/v2"
)

func savedLocalKimiConfig(auth string) string {
	return "default_model = \"kimi-code/k3\"\n[providers.\"managed:kimi-code\"]\ntype = \"kimi\"\nbase_url = \"" + intlBase + "\"\n" + auth + "\n[models.\"kimi-code/k3\"]\nprovider = \"managed:kimi-code\"\nmodel = \"k3\"\n"
}

func TestLocalKimiOpenUsesSavedLoginWithoutAnEnvironmentKey(t *testing.T) {
	root := setupKimiFixture(t, "success")
	t.Setenv("GRASP_LOCAL_AUTH", "1")
	t.Setenv(APIKeyEnv, "")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	saved := savedLocalKimiConfig("api_key = \"synthetic-saved-kimi-key\"")
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(saved), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := New().Open(ctx, ctx, provider.OpenOptions{Cwd: t.TempDir(), Model: "k3.1-pro", ReasoningEffort: "max", ReasoningEffortSet: true}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	if session.Info().ModelID != "k3.1-pro" {
		t.Fatal("saved local login overrode the selected model")
	}
	if body, err := os.ReadFile(filepath.Join(root, "config.toml")); err != nil || string(body) != saved {
		t.Fatal("preparing local login changed its source configuration")
	}
}

func TestLocalKimiConfigKeepsOAuthAndIsolatesCredentialWrites(t *testing.T) {
	root := t.TempDir()
	const originalToken = `{"access_token":"synthetic-local-token","refresh_token":"synthetic-refresh-token"}`
	if err := os.Mkdir(filepath.Join(root, "credentials"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "credentials", "kimi-code.json"), []byte(originalToken), 0o600); err != nil {
		t.Fatal(err)
	}
	config := savedLocalKimiConfig("[providers.\"managed:kimi-code\".oauth]\nstorage = \"file\"\nkey = \"oauth/kimi-code\"\noauth_host = \"https://auth.kimi.ai\"")
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	home, err := isolatedHome(root, intlBase, "k3.1-pro", "max")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = home.Close() }()
	if err := writeLocalConfigAt(root, home, intlBase, "k3.1-pro", "max"); err != nil {
		t.Fatal(err)
	}
	body, err := home.ReadFile("config.toml")
	var parsed map[string]any
	if err != nil || toml.Unmarshal(body, &parsed) != nil {
		t.Fatal("invalid generated local config", err)
	}
	kimi := parsed["providers"].(map[string]any)["kimi"].(map[string]any)
	if kimi["api_key_env"] != nil || kimi["base_url"] != intlBase || kimi["oauth"].(map[string]any)["key"] != "oauth/kimi-code" || parsed["default_model"] != "k3.1-pro" || parsed["thinking"].(map[string]any)["effort"] != "max" {
		t.Fatal("local OAuth lost its native reference or overrode the selected route")
	}
	token := filepath.Join(home.Name(), "credentials", "kimi-code.json")
	if info, err := os.Lstat(token); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatal("OAuth snapshot is not a private regular file", err)
	}
	if body, err := os.ReadFile(token); err != nil || string(body) != originalToken {
		t.Fatal("selected OAuth token did not reach the native home")
	}
	if err := os.WriteFile(token, []byte("refreshed-private-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(root, "credentials", "kimi-code.json")); err != nil || string(body) != originalToken {
		t.Fatal("native OAuth refresh wrote through to its source login")
	}
}

func TestLocalKimiConfigRejectsMissingOrDifferentSiteAuth(t *testing.T) {
	for name, config := range map[string]string{
		"missing saved auth": savedLocalKimiConfig(""),
		"different site":     strings.ReplaceAll(savedLocalKimiConfig("api_key = \"synthetic-saved-key\""), intlBase, cnBase),
		"environment only":   savedLocalKimiConfig("api_key_env = \"KIMI_CODE_API_KEY\""),
		"mixed auth":         savedLocalKimiConfig("api_key = \"synthetic-saved-key\"\n[providers.\"managed:kimi-code\".oauth]\nstorage = \"file\"\nkey = \"oauth/kimi-code\""),
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			home, err := isolatedHome(root, intlBase, "k3.1-pro", "max")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = home.Close() }()
			if err := writeLocalConfigAt(root, home, intlBase, "k3.1-pro", "max"); err == nil || strings.Contains(err.Error(), "synthetic-saved-key") {
				t.Fatal("invalid local auth was accepted or exposed a secret", err)
			}
		})
	}
}

func TestLocalKimiOAuthRejectsSourceAndDestinationDirectoryLinks(t *testing.T) {
	for _, destinationLink := range []bool{false, true} {
		root, outside := t.TempDir(), t.TempDir()
		if err := os.Mkdir(filepath.Join(root, "credentials"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(outside, "kimi-code.json"), []byte("outside-token"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "credentials", "kimi-code.json"), []byte("selected-token"), 0o600); err != nil {
			t.Fatal(err)
		}
		source, err := os.OpenRoot(root)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = source.Close() }()
		home, err := os.OpenRoot(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = home.Close() }()
		if destinationLink {
			if err := home.Symlink(outside, "credentials"); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := source.Remove("credentials/kimi-code.json"); err != nil {
				t.Fatal(err)
			}
			if err := source.Symlink(filepath.Join(outside, "kimi-code.json"), "credentials/kimi-code.json"); err != nil {
				t.Fatal(err)
			}
		}
		if err := copyLocalOAuth(source, home, map[string]any{"storage": "file", "key": "oauth/kimi-code"}); err == nil {
			t.Fatal("OAuth snapshot followed an external symlink")
		}
		if body, err := os.ReadFile(filepath.Join(outside, "kimi-code.json")); err != nil || string(body) != "outside-token" {
			t.Fatal("OAuth snapshot wrote outside its private directory")
		}
	}
}
