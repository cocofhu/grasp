package sandbox

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalWorkspaceBelowVirtualConfigRetainsFilesAcrossConfigurationHomes(t *testing.T) {
	virtual, firstHome, nextHome := t.TempDir(), t.TempDir(), t.TempDir()
	workspace := filepath.Join(virtual, "tasks", "original")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	sb := &Sandbox{WorkspaceDir: workspace, local: &localExec{configRoot: virtual, configHome: firstHome}}
	wanted := []byte{0, 0xff, 0x12, 0, 0x80}
	if err := sb.WriteFile(t.Context(), "dirty.bin", wanted); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(workspace, "dirty.bin")); err != nil || !bytes.Equal(data, wanted) {
		t.Fatalf("workspace write followed the virtual config prefix: %v", err)
	}
	sb.local.configHome = nextHome
	if data, err := sb.ReadFile(t.Context(), "dirty.bin"); err != nil || !bytes.Equal(data, wanted) {
		t.Fatalf("configuration switch lost the original working file: %v", err)
	}
	if err := sb.WriteFile(t.Context(), filepath.Join(virtual, "auth.json"), []byte("synthetic-current-auth")); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(nextHome, "auth.json")); err != nil || string(data) != "synthetic-current-auth" {
		t.Fatal("configuration file stopped following the current private home", err)
	}
	if _, err := os.Stat(filepath.Join(virtual, "auth.json")); !os.IsNotExist(err) {
		t.Fatal("configuration write escaped its private home")
	}
	copyRoot := filepath.Join(virtual, "tasks", "restored-copy")
	if err := os.MkdirAll(copyRoot, 0700); err != nil {
		t.Fatal(err)
	}
	sb.SetActiveWorkspace(copyRoot, "owned-canonical-copy")
	if err := sb.WriteFile(t.Context(), "restored.bin", wanted); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(copyRoot, "restored.bin")); err != nil || !bytes.Equal(data, wanted) {
		t.Fatal("canonical workspace write followed the config prefix", err)
	}
}
