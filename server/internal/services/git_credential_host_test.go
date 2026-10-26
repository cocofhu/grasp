package services

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

func TestGitCredentialHostCRUDAndRuntimeResolution(t *testing.T) {
	setCredentialKey(t)
	db := newTestDB(t)
	s := NewCredentialService(db)
	key, err := s.Create(CredentialInput{Purpose: "git", Provider: "gitlab", Name: "GitLab team", Value: "private-gitlab-token"})
	if err != nil || key.Host != "https://gitlab.com" {
		t.Fatalf("default GitLab host: %s, %v", key.Host, err)
	}
	selected, ok := s.ResolveGitValue(key.ID)
	if !ok || selected.Host != key.Host || selected.EnvKey != "GITLAB_TOKEN" || selected.Secret != "private-gitlab-token" {
		t.Fatal("Git resolver did not return the selected credential's host and secret")
	}
	before, err := runtime.ResolveGitBindings(serviceGitSelection(key.ID), s.ResolveGitValue)
	if err != nil {
		t.Fatal(err)
	}
	key, err = s.Update(key.ID, CredentialInput{Host: " http://GIT.Internal:8080/ ", Name: "Internal GitLab"})
	if err != nil || key.Host != "http://git.internal:8080" || key.SecretRevision != 1 {
		t.Fatalf("edit host without rotating token: %s, %v", key.Host, err)
	}
	key, err = s.Update(key.ID, CredentialInput{Value: "rotated-gitlab-token"})
	if err != nil || key.Host != "http://git.internal:8080" || key.SecretRevision != 2 {
		t.Fatalf("token rotation changed host: %s, %v", key.Host, err)
	}
	selected, ok = s.ResolveGitValue(key.ID)
	if !ok || selected.Host != key.Host || selected.Secret != "rotated-gitlab-token" {
		t.Fatal("Git resolver used stale host or secret metadata")
	}
	after, err := runtime.ResolveGitBindings(serviceGitSelection(key.ID), s.ResolveGitValue)
	if err != nil || len(after) != 1 || after[0].Scope != key.Host || gitBindingsFingerprint(before) == gitBindingsFingerprint(after) {
		t.Fatal("host edit did not reach the sandbox catalog or authorization fingerprint")
	}
	views, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, view := range views {
		if view.ID == key.ID {
			found = true
			if view.Host != key.Host {
				t.Fatal("credential list lost the configured host")
			}
		}
	}
	encoded, err := json.Marshal(views)
	if !found || err != nil || strings.Contains(string(encoded), "rotated-gitlab-token") {
		t.Fatal("credential list omitted the row or exposed its token")
	}
	if err := s.Revoke(key.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.ResolveGitValue(key.ID); ok {
		t.Fatal("revoked Git credential still resolved")
	}
	if _, ok := s.ResolveGitValue("missing"); ok {
		t.Fatal("unknown Git credential resolved")
	}
	if _, ok := (*CredentialService)(nil).ResolveGitValue("anything"); ok {
		t.Fatal("missing resolver returned a credential")
	}
}

func TestGitCredentialHostRejectsInvalidEditsAndHidesStandaloneURL(t *testing.T) {
	setCredentialKey(t)
	db := newTestDB(t)
	s := NewCredentialService(db)
	key, err := s.Create(CredentialInput{Purpose: "git", Provider: "github", Name: "GitHub team", Value: "private-token"})
	if err != nil || key.Host != "https://github.com" {
		t.Fatalf("default GitHub host: %s, %v", key.Host, err)
	}
	for _, host := range []string{"https://", "https://gitlab.com/path", "https://user:token@gitlab.com", "https://gitlab.com?token=x"} {
		if _, err := s.Update(key.ID, CredentialInput{Host: host, Value: "should-not-save"}); !errors.Is(err, models.ErrGitCredentialHost) {
			t.Fatalf("invalid host edit accepted: %v", err)
		}
		if value, ok := s.ResolveGitValue(key.ID); !ok || value.Host != key.Host || value.Secret != "private-token" {
			t.Fatal("failed metadata edit changed a stored credential")
		}
	}
	if _, err := s.Create(CredentialInput{Purpose: "tool_auth", ToolID: "cursor", Host: "https://gitlab.com", Name: "Wrong host", Value: "value"}); !errors.Is(err, models.ErrGitCredentialHost) {
		t.Fatal("non-Git credential accepted a Git host")
	}
	if err := db.Create(&models.Credential{ID: "old-url", Type: "git", Provider: "gitlab_url", EnvKey: "GITLAB_URL", Name: "GitLab URL", ValueEnc: "untouched-ciphertext"}).Error; err != nil {
		t.Fatal(err)
	}
	views, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, view := range views {
		if view.ID == "old-url" || view.EnvKey == "GITLAB_URL" {
			t.Fatal("standalone GitLab URL is still listed as a credential")
		}
	}
	var row models.Credential
	if err := db.First(&row, "id = ?", "old-url").Error; err != nil || row.ValueEnc != "untouched-ciphertext" {
		t.Fatal("listing credentials modified or deleted the hidden old URL row")
	}
}
