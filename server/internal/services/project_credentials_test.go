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
