package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/mcp"
)

func TestLegacyOpenCodeWorkflowPreservesNativeCatalogAndSelectedKey(t *testing.T) {
	for _, vendor := range []string{"google", "azure", "xai", "alibaba", "anthropic", "openai", "opencode", "custom"} {
		t.Run(vendor, func(t *testing.T) {
			metadata := map[string]string{EnvGraspOpenCodeAPIKey: "original-key", EnvOpenCodeProvider: vendor, EnvOpenCodeBaseURL: "", EnvACPBridgeModel: vendor + "/vendor/raw-model", EnvOpenCodeModelVision: "1"}
			if vendor == "custom" {
				metadata[EnvOpenCodeBaseURL] = "https://gateway.example/prefix/v1"
			}
			root := writeAgent(t, "legacy-native", `{"acpBackend":"opencode","openCodeCredentialId":"original","env":{"GRASP_OPENCODE_PROVIDER":"stale","ACP_BRIDGE_MODEL":"stale/model"}}`)
			selected := true
			p := newBaseACPProvider(mcp.NewHost(newMemStore()), Options{
				ProfilesRoot: root,
				Credentials: func() map[string]string {
					return map[string]string{envauth.EnvOpenCodeAPIKey: "another-key", "GOOGLE_GENERATIVE_AI_API_KEY": "another-native-key", "GITHUB_TOKEN": "git"}
				},
				OpenCodeCredential: func(id string) map[string]string {
					if id != "original" {
						t.Fatal("selected a different credential")
					}
					if !selected {
						return nil
					}
					return metadata
				},
				OpenCodeCatalog: &stubCatalog{readable: true, known: map[string]bool{vendor: vendor != "custom"}, models: map[string][]string{vendor: {"vendor/raw-model"}}},
			}, BackendOpenCode).(*acpProvider)
			req := NodeReq{RunID: "native-preserved", NodeID: "n", Token: "token", Caps: testPlainCaps, Config: map[string]any{"agent_profile": "legacy-native"}}
			spec, err := p.spec(req)
			if err != nil {
				t.Fatal(err)
			}
			defer removeHome(spec.ConfigHome)
			if spec.Env[EnvModelConnection] != "" || spec.Env[EnvOpenCodeAPIKey] != "original-key" || spec.Env["GITHUB_TOKEN"] != "git" {
				t.Fatal("original execution path or selected key was replaced")
			}
			if native := openCodeNativeAPIKey(vendor); native != "" && spec.Env[native] != "original-key" {
				t.Fatal("another native credential survived")
			}
			body, err := os.ReadFile(filepath.Join(spec.ConfigHome, "opencode.json"))
			if err != nil {
				t.Fatal(err)
			}
			var doc struct {
				Model    string
				Provider map[string]struct {
					NPM     string
					Options map[string]string
				}
			}
			if err := json.Unmarshal(body, &doc); err != nil {
				t.Fatal(err)
			}
			entry := doc.Provider[vendor]
			if doc.Model != vendor+"/vendor/raw-model" || entry.Options["apiKey"] != "{env:OPENCODE_API_KEY}" {
				t.Fatal("native model or key reference was changed")
			}
			if vendor != "custom" && (entry.NPM != "" || entry.Options["baseURL"] != "") {
				t.Fatal("catalog SDK or endpoint was guessed")
			}
			if vendor == "custom" && (entry.NPM != openCodeCompatibleNPM || entry.Options["baseURL"] != metadata[EnvOpenCodeBaseURL]) {
				t.Fatal("existing custom gateway adapter was changed")
			}
			selected = false
			if _, err := p.spec(req); err == nil || strings.Contains(err.Error(), "another-key") {
				t.Fatal("unavailable credential fell back or exposed a secret")
			}
		})
	}
}

func TestLegacyOpenCodeCredentialFingerprintAndFailure(t *testing.T) {
	env := map[string]string{"ANTHROPIC_BASE_URL": "https://old-native-prefix", "GITLAB_TOKEN": "git", "MCP_TOKEN": "mcp"}
	selected := map[string]string{EnvGraspOpenCodeAPIKey: "key", EnvOpenCodeProvider: "azure", EnvACPBridgeModel: "azure/raw"}
	resolve := func(string) map[string]string { return selected }
	first, err := ApplyLegacyOpenCodeCredential(env, "chosen", resolve)
	if err != nil || len(first) != 64 || env["ANTHROPIC_BASE_URL"] == "" || env["GITLAB_TOKEN"] != "git" || env["MCP_TOKEN"] != "mcp" {
		t.Fatal("legacy non-secret hints or independent credentials were changed")
	}
	selected[EnvGraspOpenCodeAPIKey] = "rotated"
	second, err := ApplyLegacyOpenCodeCredential(env, "chosen", resolve)
	if err != nil || first == second {
		t.Fatal("rotation did not change the fingerprint")
	}
	if _, err := ApplyLegacyOpenCodeCredential(env, "chosen", nil); err == nil {
		t.Fatal("missing resolver accepted")
	}
	if _, err := ApplyLegacyOpenCodeCredential(env, "chosen", func(string) map[string]string { return nil }); err == nil {
		t.Fatal("missing selected key accepted")
	}
	if got, err := ApplyLegacyOpenCodeCredential(env, "", nil); got != "" || err != nil {
		t.Fatal("unselected legacy configuration changed")
	}
}
