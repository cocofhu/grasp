package services

import (
	"errors"
	"testing"

	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func knownHostsPtr(s string) *string { return &s }

func TestSSHKeyCredentialStoresKnownHosts(t *testing.T) {
	setCredentialKey(t)
	s := NewCredentialService(newTestDB(t))
	created, err := s.Create(CredentialInput{Purpose: "ssh", Name: "Deploy key", Value: "synthetic-private-key", KnownHosts: knownHostsPtr("  a.example ssh-ed25519 AAAA\r\nb.example ssh-ed25519 BBBB\r\n")})
	if err != nil {
		t.Fatal(err)
	}
	want := "a.example ssh-ed25519 AAAA\nb.example ssh-ed25519 BBBB"
	if created.KnownHosts != want || created.SecretKind != "ssh_private_key" || created.SecretRevision != 1 {
		t.Fatalf("created = %+v", created)
	}
	listed, err := s.List()
	if err != nil || len(listed) != 1 || listed[0].KnownHosts != want {
		t.Fatalf("list = %+v, %v", listed, err)
	}
	value, ok := s.ResolveGitValue(created.ID)
	if !ok || value.Secret != "synthetic-private-key" || value.KnownHosts != want {
		t.Fatalf("resolved = %+v, %v", value, ok)
	}

	updated, err := s.Update(created.ID, CredentialInput{Name: "Deploy key"})
	if err != nil || updated.KnownHosts != want || updated.SecretRevision != 1 {
		t.Fatalf("omitted known hosts changed: %+v, %v", updated, err)
	}
	updated, err = s.Update(created.ID, CredentialInput{KnownHosts: knownHostsPtr(want + "\n")})
	if err != nil || updated.SecretRevision != 1 {
		t.Fatalf("unchanged known hosts bumped revision: %+v, %v", updated, err)
	}
	updated, err = s.Update(created.ID, CredentialInput{KnownHosts: knownHostsPtr("c.example ssh-ed25519 CCCC")})
	if err != nil || updated.KnownHosts != "c.example ssh-ed25519 CCCC" || updated.SecretRevision != 2 || !updated.Configured {
		t.Fatalf("known-hosts-only edit = %+v, %v", updated, err)
	}
	if value, ok := s.ResolveGitValue(created.ID); !ok || value.Secret != "synthetic-private-key" {
		t.Fatal("known-hosts-only edit changed the private key")
	}
	updated, err = s.Update(created.ID, CredentialInput{Value: "rotated-key", KnownHosts: knownHostsPtr("d.example ssh-ed25519 DDDD")})
	if err != nil || updated.SecretRevision != 3 {
		t.Fatalf("rotation with known hosts = %+v, %v", updated, err)
	}
	updated, err = s.Update(created.ID, CredentialInput{KnownHosts: knownHostsPtr("")})
	if err != nil || updated.KnownHosts != "" || updated.SecretRevision != 4 {
		t.Fatalf("clear known hosts = %+v, %v", updated, err)
	}
	rows, err := runtime.ResolveGitBindings(serviceGitSelection(created.ID), s.ResolveGitValue)
	if err != nil || len(rows) != 1 || rows[0].Kind != sandbox.RepoCredentialSSH || rows[0].KnownHosts != "" || rows[0].Secret != "rotated-key" {
		t.Fatalf("cleared known hosts still injected: %+v, %v", rows, err)
	}

	if _, err := s.Update(created.ID, CredentialInput{KnownHosts: knownHostsPtr("host\x00key")}); !errors.Is(err, ErrCredentialKnownHosts) {
		t.Fatalf("NUL update accepted: %v", err)
	}
	if _, err := s.Create(CredentialInput{Purpose: "ssh", Name: "Bad", Value: "k", KnownHosts: knownHostsPtr("host\x00key")}); !errors.Is(err, ErrCredentialKnownHosts) {
		t.Fatalf("NUL create accepted: %v", err)
	}
	if err := s.Clear(created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(created.ID, CredentialInput{KnownHosts: knownHostsPtr("e.example ssh-ed25519 EEEE")}); err != nil {
		t.Fatal(err)
	}
	if cleared, err := s.Get(created.ID); err != nil || cleared.Configured || cleared.KnownHosts != "e.example ssh-ed25519 EEEE" {
		t.Fatalf("cleared key lost known hosts: %+v, %v", cleared, err)
	}
	if _, ok := s.ResolveGitValue(created.ID); ok {
		t.Fatal("cleared key without a private key resolved")
	}
}

func TestKnownHostsRejectedOutsideSSHKeys(t *testing.T) {
	setCredentialKey(t)
	s := NewCredentialService(newTestDB(t))
	if _, err := s.Create(CredentialInput{Purpose: "git", Provider: "github", Name: "GitHub", Value: "token", KnownHosts: knownHostsPtr("host key")}); !errors.Is(err, ErrCredentialPurpose) {
		t.Fatalf("Git credential accepted known hosts: %v", err)
	}
	gh, err := s.Create(CredentialInput{Purpose: "git", Provider: "github", Name: "GitHub", Value: "token"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(gh.ID, CredentialInput{KnownHosts: knownHostsPtr("")}); !errors.Is(err, ErrCredentialPurpose) {
		t.Fatalf("Git update accepted known hosts: %v", err)
	}
	if value, ok := s.ResolveGitValue(gh.ID); !ok || value.KnownHosts != "" {
		t.Fatalf("HTTPS value = %+v, %v", value, ok)
	}
}
