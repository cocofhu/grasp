package database

import (
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestDeleteRetiredSSHKnownHostsIsIdempotent(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "hosts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	rows := []models.Credential{
		{ID: "hosts", Purpose: "ssh", Type: "ssh", SecretKind: "ssh_known_hosts", SecretRevision: 1, Name: "Hosts", EnvKey: "GIT_SSH_KNOWN_HOSTS", ValueEnc: "x"},
		{ID: "key", Purpose: "ssh", Type: "ssh", SecretKind: "ssh_private_key", SecretRevision: 1, Name: "Key", EnvKey: "GIT_SSH_PRIVATE_KEY", ValueEnc: "x", KnownHosts: "host ssh-ed25519 AAAA"},
		{ID: "other", Purpose: "custom", Type: "custom", SecretKind: "ssh_known_hosts", SecretRevision: 1, Name: "Other", EnvKey: "OTHER", ValueEnc: "x"},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := deleteRetiredSSHKnownHosts(db); err != nil {
			t.Fatal(err)
		}
		var left []models.Credential
		if err := db.Order("id").Find(&left).Error; err != nil {
			t.Fatal(err)
		}
		if len(left) != 2 || left[0].ID != "key" || left[0].KnownHosts != "host ssh-ed25519 AAAA" || left[1].ID != "other" {
			t.Fatalf("remaining credentials = %+v", left)
		}
	}
	if err := deleteRetiredSSHKnownHosts(nil); err != nil {
		t.Fatal(err)
	}
}
