package services

import (
	"errors"
	"reflect"
	"testing"

	"github.com/cocofhu/grasp/internal/crypto"
	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
)

func TestCredentialReadsNeverCreateDefaultSlots(t *testing.T) {
	db := newTestDB(t)
	s := NewCredentialService(db)
	for range 2 {
		rows, err := s.List()
		if err != nil || rows == nil || len(rows) != 0 {
			t.Fatal("empty credential store did not return an empty array")
		}
		s.ResolveEnv()
		s.ResolveReferences()
		s.CredentialEnvKeys()
		var count int64
		if err := db.Model(&models.Credential{}).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("read materialized credential slots")
		}
	}
}

func TestOnlyExplicitCurrentCredentialsAreVisibleAndUsable(t *testing.T) {
	setCredentialKey(t)
	db := newTestDB(t)
	s := NewCredentialService(db)
	ciphertext, err := crypto.Encrypt("synthetic-old-secret")
	if err != nil {
		t.Fatal(err)
	}
	rows := []models.Credential{
		{ID: "old-untyped", Type: "ai", Provider: "cursor", Name: "Cursor", EnvKey: envauth.EnvCursorAPIKey},
		{ID: "old-custom", Type: "custom", Name: "Old custom", EnvKey: "OLD_TOKEN"},
		{ID: "old-slot", Purpose: "tool_auth", ToolID: "cursor", Issuer: "cursor", Type: "ai", Provider: "cursor", SecretKind: "api_key", SecretRevision: 1, Name: "Cursor", EnvKey: envauth.EnvCursorAPIKey, BuiltinSlot: envauth.EnvCursorAPIKey},
		{ID: "old-model", Purpose: "model_api", Issuer: "deepseek", Type: "ai", Provider: "deepseek", SecretKind: "api_key", Name: "Missing revision"},
		{ID: "old-git", Purpose: "git", Issuer: "gitlab", Type: "git", Provider: "gitlab", SecretKind: "token", SecretRevision: 1, Name: "Missing host", EnvKey: envauth.EnvGitLabToken},
		{ID: "old-codex", Type: "ai", Provider: "codex", Name: "Codex", EnvKey: envauth.EnvCodexAuthFile},
		{ID: "unknown-domain", Purpose: "old-purpose", Type: "custom", SecretKind: "token", SecretRevision: 1, Name: "Old purpose", EnvKey: "ANOTHER_OLD_TOKEN"},
	}
	for i := range rows {
		rows[i].Enabled, rows[i].ValueEnc = true, ciphertext
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	var before []models.Credential
	if err := db.Order("id").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		listed, err := s.List()
		if err != nil || len(listed) != 0 {
			t.Fatal("old credentials appeared in the current store")
		}
		if len(s.ResolveEnv()) != 0 || len(s.ResolveReferences()) != 0 || len(s.CredentialEnvKeys()) != 0 {
			t.Fatal("old credentials entered runtime substitutions")
		}
		for _, row := range rows {
			if _, err := s.Get(row.ID); !errors.Is(err, ErrCredentialNotFound) {
				t.Fatal("old credential was readable")
			}
			if _, _, ok := s.ResolveSelectedValue(row.ID); ok {
				t.Fatal("old credential secret was resolved")
			}
			if _, ok := s.ResolveGitValue(row.ID); ok {
				t.Fatal("old Git credential was resolved")
			}
			if _, err := s.Update(row.ID, CredentialInput{Purpose: "tool_auth", ToolID: "cursor", Value: "synthetic-replacement"}); !errors.Is(err, ErrCredentialNotFound) {
				t.Fatal("update converted an old credential")
			}
			if err := s.Clear(row.ID); !errors.Is(err, ErrCredentialNotFound) {
				t.Fatal("clear rewrote an old credential")
			}
			if err := s.Revoke(row.ID); !errors.Is(err, ErrCredentialNotFound) {
				t.Fatal("revoke rewrote an old credential")
			}
		}
	}
	if _, err := NewModelConnectionService(db).Resolve("cursor", modelrouting.Binding{Kind: "platform", CredentialID: "old-slot", ModelID: "auto"}); err == nil {
		t.Fatal("model binding resolved an obsolete credential slot")
	}
	if _, err := NewModelConnectionService(db).Create(connectionInput("old-model")); err == nil {
		t.Fatal("model connection accepted incomplete credential metadata")
	}
	login := `{"auth_mode":"chatgpt","tokens":{"access_token":"new","refresh_token":"new"}}`
	if err := s.WriteBackCodexLoginCredential("old-codex", login); err == nil {
		t.Fatal("login refresh converted an old credential")
	}
	var after []models.Credential
	if err := db.Order("id").Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("reads or rejected operations migrated the stored metadata/ciphertext")
	}
	created, err := s.Create(CredentialInput{Purpose: "tool_auth", ToolID: "cursor", Name: "Cursor", Value: "synthetic-current-key"})
	if err != nil {
		t.Fatal("hidden old slots blocked a current alias")
	}
	if _, value, ok := s.ResolveSelectedValue(created.ID); !ok || value != "synthetic-current-key" {
		t.Fatal("current credential did not resolve")
	}
	listed, err := s.List()
	if err != nil || len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatal("list mixed current credentials with old slots")
	}
}

func TestCredentialCreateRequiresPurposeWithoutInferringType(t *testing.T) {
	setCredentialKey(t)
	s := NewCredentialService(newTestDB(t))
	for _, kind := range []string{"", "ai", "git", "ssh", "mcp", "custom"} {
		if _, err := s.Create(CredentialInput{Type: kind, Provider: "github", Name: "Old input", Value: "synthetic"}); !errors.Is(err, ErrCredentialPurpose) {
			t.Fatalf("type-only %q input did not require purpose", kind)
		}
	}
	if _, err := s.Create(CredentialInput{Purpose: "tool_auth", Provider: "cursor", Name: "Missing tool", Value: "synthetic"}); !errors.Is(err, ErrCredentialPurpose) {
		t.Fatal("provider was treated as a missing toolId")
	}
}

func TestCurrentCredentialCreateNormalizesProviderBeforeIssuerDefault(t *testing.T) {
	setCredentialKey(t)
	for _, input := range []CredentialInput{
		{Purpose: "custom", Provider: " vault ", EnvKey: "VAULT_TOKEN", Name: "Vault"},
		{Purpose: "mcp", Provider: " gateway ", EnvKey: "MCP_TOKEN", Name: "MCP"},
		{Purpose: "model_api", Provider: " openai ", Name: "Model API"},
		{Purpose: "git", Provider: " gitlab ", Name: "GitLab"},
		{Purpose: "ssh", Provider: " ssh ", Name: "SSH"},
	} {
		t.Run(input.Purpose, func(t *testing.T) {
			service := NewCredentialService(newTestDB(t))
			input.Value = "synthetic-current-secret"
			created, err := service.Create(input)
			if err != nil {
				t.Fatal("explicit current credential could not be created")
			}
			loaded, err := service.Get(created.ID)
			if err != nil || loaded.Provider == input.Provider || loaded.Issuer != loaded.Provider {
				t.Fatal("new credential was not readable with normalized provider metadata")
			}
			listed, err := service.List()
			if err != nil || len(listed) != 1 || listed[0].ID != created.ID {
				t.Fatal("new credential disappeared from the current list")
			}
			if _, value, ok := service.ResolveSelectedValue(created.ID); !ok || value != input.Value {
				t.Fatal("new credential could not be resolved")
			}
		})
	}
}
