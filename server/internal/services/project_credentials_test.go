package services

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/cocofhu/grasp/internal/crypto"
	"github.com/cocofhu/grasp/internal/models"
)

func setCredentialKey(t *testing.T) {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	t.Setenv(crypto.SecretsKeyEnv, base64.StdEncoding.EncodeToString(b))
}

func TestProjectCredentialsCRUDAndResolution(t *testing.T) {
	setCredentialKey(t)
	db := newTestDB(t)
	p, err := NewProjectService(db).Create("Credential project", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := NewProjectCredentialService(db)
	created, err := s.Create(p.ID, ProjectCredentialInput{Type: "ai", Provider: "cursor", Name: "Cursor", Value: "secret-value"})
	if err != nil {
		t.Fatal(err)
	}
	if !created.Configured || created.Masked == "secret-value" || created.Masked == "" {
		t.Fatalf("unsafe view: %+v", created)
	}
	var raw models.ProjectCredential
	if err := db.First(&raw, "id = ?", created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if raw.ValueEnc == "secret-value" || raw.ValueEnc == "" {
		t.Fatalf("plaintext/empty ciphertext: %q", raw.ValueEnc)
	}
	rows, err := s.List(p.ID)
	if err != nil || len(rows) < 9 {
		t.Fatalf("list defaults: n=%d err=%v", len(rows), err)
	}
	resolved := s.ResolveEnv(p.ID)
	if resolved["GRASP_CURSOR_API_KEY"] != "secret-value" {
		t.Fatalf("resolved=%v", resolved)
	}
	updated, err := s.Update(p.ID, created.ID, ProjectCredentialInput{Value: "rotated"})
	if err != nil || !updated.Configured {
		t.Fatalf("update: %+v %v", updated, err)
	}
	if s.ResolveEnv(p.ID)["GRASP_CURSOR_API_KEY"] != "rotated" {
		t.Fatal("rotation not resolved")
	}
	if err := s.Clear(p.ID, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.ResolveEnv(p.ID)["GRASP_CURSOR_API_KEY"]; ok {
		t.Fatal("cleared credential still resolved")
	}
	if _, err := s.Update(p.ID, created.ID, ProjectCredentialInput{Value: "again"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Revoke(p.ID, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.ResolveEnv(p.ID)["GRASP_CURSOR_API_KEY"]; ok {
		t.Fatal("revoked credential still resolved")
	}
}

func TestProjectCredentialRejectsReservedAndInvalidKeys(t *testing.T) {
	setCredentialKey(t)
	db := newTestDB(t)
	p, err := NewProjectService(db).Create("Reserved keys", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := NewProjectCredentialService(db)
	for _, in := range []ProjectCredentialInput{
		{Type: "custom", Name: "artifact", EnvKey: "GRASP_ARTIFACT_TOKEN", Value: "v"},
		{Type: "custom", Name: "pm", EnvKey: "GRASP_PM_TOKEN", Value: "v"},
		{Type: "custom", Name: "root", EnvKey: "CONFIG_ROOT", Value: "v"},
		{Type: "custom", Name: "bad", EnvKey: "vars.repo_url", Value: "v"},
		{Type: "custom", Name: "fallback", EnvKey: "X", FallbackEnvKey: "GRASP_RUN_ID", Value: "v"},
		{Type: "custom", Name: "fallback-bad", EnvKey: "X", FallbackEnvKey: "A-B", Value: "v"},
	} {
		if _, err := s.Create(p.ID, in); !errors.Is(err, ErrCredentialEnvKey) {
			t.Fatalf("%s: want ErrCredentialEnvKey, got %v", in.Name, err)
		}
	}
	for _, typ := range []string{"channel", "external_mcp", "workflow"} {
		if _, err := s.Create(p.ID, ProjectCredentialInput{Type: typ, Name: typ, EnvKey: "X", Value: "v"}); !errors.Is(err, ErrCredentialType) {
			t.Fatalf("%s: adapter type must not be creatable, got %v", typ, err)
		}
	}
	created, err := s.Create(p.ID, ProjectCredentialInput{Type: "custom", Name: "ok", EnvKey: "MY_TOKEN", Value: "v"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(p.ID, created.ID, ProjectCredentialInput{EnvKey: "GRASP_MEMORY_TOKEN"}); !errors.Is(err, ErrCredentialEnvKey) {
		t.Fatalf("update to reserved key: got %v", err)
	}
}

func TestProjectCredentialEnvKeysIgnoreEmptySlots(t *testing.T) {
	setCredentialKey(t)
	db := newTestDB(t)
	p, err := NewProjectService(db).Create("Empty slots", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := NewProjectCredentialService(db)
	if _, err := s.List(p.ID); err != nil {
		t.Fatal(err)
	}
	if keys := s.CredentialEnvKeys(p.ID); len(keys) != 0 {
		t.Fatalf("empty default slots must not block Run env: %v", keys)
	}
	if _, err := s.SetByEnvKey(p.ID, ProjectCredentialInput{Type: "git", Provider: "github", Name: "GitHub", EnvKey: "GITHUB_TOKEN", Value: "ghp"}); err != nil {
		t.Fatal(err)
	}
	keys := s.CredentialEnvKeys(p.ID)
	if _, ok := keys["GITHUB_TOKEN"]; !ok || len(keys) != 1 {
		t.Fatalf("configured key should be protected: %v", keys)
	}
}

func TestOverlayProjectCredentialEnvKeepsPlatformKeys(t *testing.T) {
	env := map[string]string{"GRASP_PM_TOKEN": "platform", "GITHUB_TOKEN": "agent"}
	overlayProjectCredentialEnv(env, map[string]string{"GRASP_PM_TOKEN": "user", "GITHUB_TOKEN": "ui"})
	if env["GRASP_PM_TOKEN"] != "platform" || env["GITHUB_TOKEN"] != "ui" {
		t.Fatalf("env=%v", env)
	}
}

func TestDeleteProjectRemovesCredentials(t *testing.T) {
	setCredentialKey(t)
	db := newTestDB(t)
	projects := NewProjectService(db)
	p, err := projects.Create("Delete creds", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewProjectCredentialService(db).Create(p.ID, ProjectCredentialInput{Type: "custom", Name: "x", EnvKey: "X", Value: "v"}); err != nil {
		t.Fatal(err)
	}
	if err := projects.Delete(p.ID); err != nil {
		t.Fatal(err)
	}
	var n int64
	db.Model(&models.ProjectCredential{}).Where("project_id = ?", p.ID).Count(&n)
	if n != 0 {
		t.Fatalf("orphan credentials: %d", n)
	}
}

func TestProjectCredentialRequiresMasterKey(t *testing.T) {
	db := newTestDB(t)
	p, err := NewProjectService(db).Create("No key", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(crypto.SecretsKeyEnv, "")
	if _, err := NewProjectCredentialService(db).Create(p.ID, ProjectCredentialInput{Type: "custom", Name: "x", EnvKey: "X", Value: "v"}); !errors.Is(err, crypto.ErrNoSecretsKey) {
		t.Fatalf("expected missing key, got %v", err)
	}
}
