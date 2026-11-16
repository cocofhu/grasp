package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexRefreshTargetsSelectedCredentialAndCannotReviveIt(t *testing.T) {
	setCredentialKey(t)
	db := newTestDB(t)
	svc := NewCredentialService(db)
	login := `{"auth_mode":"chatgpt","tokens":{"access_token":"original","refresh_token":"refresh"}}`
	refreshed := `{"auth_mode":"chatgpt","tokens":{"access_token":"refreshed","refresh_token":"refresh"}}`
	first, err := svc.Create(CredentialInput{Purpose: "tool_auth", ToolID: "codex", Type: "ai", Provider: "codex", Name: "First", Value: login})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Create(CredentialInput{Purpose: "tool_auth", ToolID: "codex", Type: "ai", Provider: "codex", Name: "Second", Value: login})
	if err != nil {
		t.Fatal(err)
	}
	if err = saveCodexLoginForTest(svc, second.ID, refreshed); err != nil {
		t.Fatal(err)
	}
	_, got, ok := svc.ResolveSelectedValue(first.ID)
	if !ok || got != login {
		t.Fatal("refresh overwrote another credential")
	}
	_, got, ok = svc.ResolveSelectedValue(second.ID)
	if !ok || got != refreshed {
		t.Fatal("refresh did not update selected credential")
	}
	if err = svc.Revoke(second.ID); err != nil {
		t.Fatal(err)
	}
	if err = saveCodexLoginForTest(svc, second.ID, login); err == nil {
		t.Fatal("revoked credential revived")
	}
	if err = svc.Clear(first.ID); err != nil {
		t.Fatal(err)
	}
	if err = saveCodexLoginForTest(svc, first.ID, refreshed); err == nil {
		t.Fatal("cleared credential revived")
	}
}

func TestMaintenanceArchiveIsPrivateAndRejectsSymlinks(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "archive")
	if err := os.WriteFile(filepath.Join(src, "auth.json"), []byte("private"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := copyPrivateTree(src, dst); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dst, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("archive exposed private files")
	}
	if err = os.Symlink(filepath.Join(src, "auth.json"), filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}
	if err = copyPrivateTree(src, dst); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatal("unsafe archive accepted symlink")
	}
}

func saveCodexLoginForTest(svc *CredentialService, id, content string) error {
	prev, err := svc.codexLogin(id)
	if err != nil {
		return err
	}
	_, err = svc.saveCodexLogin(prev, content)
	return err
}
