package services

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

func TestOfficialToolAuthorizationLifecycleAndIsolation(t *testing.T) {
	for _, test := range []struct{ tool, env, model string }{
		{"opencode", envauth.EnvOpenCodeAPIKey, "opencode/kimi-k3"},
		{"deepseek_harness", envauth.EnvDeepSeekAPIKey, "deepseek-flash"},
	} {
		t.Run(test.tool, func(t *testing.T) {
			setCredentialKey(t)
			db := newTestDB(t)
			credentials, connections, configurations := NewCredentialService(db), NewModelConnectionService(db), NewModelConfigurationService(db)
			key, err := credentials.Create(CredentialInput{Purpose: "tool_auth", ToolID: test.tool, Name: "Official account", Value: "official-test-key"})
			if err != nil || key.Provider != test.tool || key.Issuer != test.tool || key.EnvKey != test.env || key.SecretKind != "api_key" || key.SecretRevision != 1 {
				t.Fatalf("official credential schema: %+v %v", key, err)
			}
			if _, err := connections.Create(connectionInput(key.ID)); err == nil {
				t.Fatal("tool account was accepted as a generic API key")
			}
			config, err := configurations.Create(modelrouting.Configuration{Name: "Official model", Backend: test.tool, Binding: modelrouting.Binding{Kind: "platform", CredentialID: key.ID, ModelID: test.model}})
			if err != nil {
				t.Fatal(err)
			}
			ref := modelrouting.Binding{Kind: "configuration", ConfigurationID: config.ID}
			assertResolve := func(secret string, revision uint64) {
				t.Helper()
				resolved, err := connections.Resolve(test.tool, ref)
				if err != nil || resolved.Connection != nil || resolved.Binding.Kind != "platform" || resolved.Secret != secret || resolved.SecretRevision != revision {
					t.Fatalf("official configuration resolved incorrectly: %+v %v", resolved, err)
				}
			}
			assertResolve("official-test-key", 1)
			if _, err := connections.Resolve("cursor", modelrouting.Binding{Kind: "platform", CredentialID: key.ID}); err == nil {
				t.Fatal("tool authorization crossed coding tools")
			}
			if _, err := credentials.Update(key.ID, CredentialInput{Name: "Renamed account", Value: "rotated-test-key"}); err != nil {
				t.Fatal(err)
			}
			assertResolve("rotated-test-key", 2)
			items, err := credentials.List()
			encoded, _ := json.Marshal(items)
			if err != nil || len(items) != 1 || items[0].Name != "Renamed account" || strings.Contains(string(encoded), "test-key") {
				t.Fatalf("credential DTO lost or exposed secret: %s %v", encoded, err)
			}
			if len(credentials.ResolveEnv()) != 0 {
				t.Fatal("tool credentials were injected without an explicit binding")
			}
			if err := credentials.Clear(key.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := connections.Resolve(test.tool, ref); err == nil {
				t.Fatal("cleared official credential resolved")
			}
			if _, err := credentials.Update(key.ID, CredentialInput{Value: "restored-test-key"}); err != nil {
				t.Fatal(err)
			}
			if err := credentials.Revoke(key.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := connections.Resolve(test.tool, ref); err == nil {
				t.Fatal("revoked official credential resolved")
			}
			var count int64
			if err := db.Model(&models.ModelConnection{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("tool authorization created a generic connection: %d %v", count, err)
			}
		})
	}
}

func TestOfficialToolCredentialRotationDoesNotAffectExistingSandbox(t *testing.T) {
	for _, tool := range []string{"opencode", "deepseek_harness"} {
		agent := Agent{AcpBackend: tool, ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: "official"}}
		key := runtime.CredentialEnvKeyForBackend(tool)
		previous := modelBindingFingerprint(agent, map[string]string{runtime.EnvModelCredentialID: "selected", key: "previous-test-key"})
		current := modelBindingFingerprint(agent, map[string]string{runtime.EnvModelCredentialID: "selected", key: "rotated-test-key"})
		if previous == current || previous == "" {
			t.Fatalf("%s credential rotation was not recorded on the sandbox fingerprint", tool)
		}
	}
}
