package services

import (
	"errors"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestCredentialPurposesAndRevisionLifecycle(t *testing.T) {
	setCredentialKey(t)
	service := NewCredentialService(newTestDB(t))
	for _, input := range []CredentialInput{
		{Type: "ai", Provider: "cursor", Name: "Missing purpose", Value: "s"},
		{Purpose: "unknown", Name: "Bad", Value: "s"},
		{Purpose: "model_api", ToolID: "opencode", Name: "Bad", Value: "s"},
		{Purpose: "model_api", EnvKey: "GLOBAL_KEY", Name: "Bad", Value: "s"},
		{Purpose: "model_api", Metadata: map[string]any{"model": "x"}, Name: "Bad", Value: "s"},
		{Purpose: "model_api", SecretKind: "login_file", Name: "Bad", Value: "s"},
		{Purpose: "tool_auth", ToolID: "opencode", Name: "Bad", Value: "s"},
		{Purpose: "tool_auth", ToolID: "unknown", Name: "Bad", Value: "s"},
		{Purpose: "tool_auth", ToolID: "codex", SecretKind: "api_key", Name: "Bad", Value: "s"},
		{Purpose: "tool_auth", ToolID: "cursor", SecretKind: "ssh_private_key", Name: "Bad", Value: "s"},
		{Purpose: "git", SecretKind: "login_file", Name: "Bad", Value: "s"},
		{Purpose: "ssh", SecretKind: "api_key", Name: "Bad", Value: "s"},
	} {
		if _, err := service.Create(input); !errors.Is(err, ErrCredentialPurpose) {
			t.Fatalf("invalid domain input accepted: %+v %v", input, err)
		}
	}
	for _, input := range []CredentialInput{
		{Purpose: "model_api", Issuer: "custom", SecretKind: "token", Name: "Custom key", Value: "s"},
		{Purpose: "tool_auth", ToolID: "trae", Name: "Trae", Value: "s"},
		{Purpose: "git", Provider: "github", Name: "Repository", Value: "s"},
		{Purpose: "ssh", Name: "Private", Value: "s"},
		{Purpose: "ssh", SecretKind: "ssh_known_hosts", Name: "Hosts", Value: "s"},
	} {
		created, err := service.Create(input)
		if err != nil {
			t.Fatalf("valid domain input rejected: %+v %v", input, err)
		}
		if created.SecretRevision != 1 {
			t.Fatal("initial revision")
		}
		if _, err := service.Update(created.ID, CredentialInput{Purpose: "unknown"}); !errors.Is(err, ErrCredentialPurpose) {
			t.Fatal("purpose changed silently")
		}
		if _, err := service.Update(created.ID, CredentialInput{Name: created.Name}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Update(created.ID, CredentialInput{Clear: true}); err != nil {
			t.Fatal(err)
		}
		cleared, err := service.Get(created.ID)
		if err != nil || cleared.Configured || cleared.SecretRevision != 2 {
			t.Fatalf("clear revision: %+v %v", cleared, err)
		}
	}
	for _, row := range []models.Credential{
		{Type: "ai", Provider: "anthropic"}, {Type: "ai", Provider: "codex"}, {Type: "ssh", EnvKey: "GIT_SSH_KNOWN_HOSTS"},
		{Type: "ai", Provider: "opencode", Metadata: map[string]any{"provider": "openrouter"}},
	} {
		purpose, _, _, kind := credentialDomain(row)
		if purpose != "" || kind != "" {
			t.Fatalf("domain was inferred from unstored attributes: %+v", row)
		}
	}
	if nextSecretRevision(1) != 2 {
		t.Fatal("rotation should advance the current secret revision")
	}
}

func TestCustomIssuerNamesDoNotSelectLegacyCredentialBehavior(t *testing.T) {
	setCredentialKey(t)
	db := newTestDB(t)
	service := NewCredentialService(db)
	for _, issuer := range []string{"codex", "opencode", "deepseek_harness", "团队模型网关"} {
		created, err := service.Create(CredentialInput{
			Purpose: "model_api", Provider: "custom", Issuer: issuer,
			Name: issuer, Value: "sk-custom-test",
		})
		if err != nil {
			t.Fatalf("custom issuer %q was treated as a tool login: %v", issuer, err)
		}
		loaded, err := service.Get(created.ID)
		if err != nil || loaded.Provider != "custom" || loaded.Issuer != issuer || loaded.EnvKey != "" || loaded.Purpose != "model_api" {
			t.Fatalf("custom issuer was not preserved as metadata: %+v %v", loaded, err)
		}
		updated, err := service.Update(created.ID, CredentialInput{Value: "sk-rotated-custom-test"})
		if err != nil {
			t.Fatalf("custom issuer %q prevented key rotation: %v", issuer, err)
		}
		if updated.Provider != "custom" || updated.Issuer != issuer || updated.EnvKey != "" {
			t.Fatalf("rotation changed the provider identity: %+v", updated)
		}
	}
	connections := NewModelConnectionService(db)
	rows, err := connections.List()
	if err != nil || len(rows) != 0 {
		t.Fatalf("custom issuer names created legacy tool connections: %d %v", len(rows), err)
	}
}
