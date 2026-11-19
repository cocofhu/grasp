package chatcmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstalledSkillFindsConfiguredRegularFile(t *testing.T) {
	home := t.TempDir()
	review := filepath.Join(home, "skills", "review")
	if err := os.MkdirAll(review, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(review, "SKILL.md"), []byte("# Review"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "skills", "draft", "SKILL.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !InstalledSkill("review", t.TempDir(), home) {
		t.Fatal("configured skill was not found in the second home")
	}
	for _, name := range []string{"rev", "draft", "other"} {
		if InstalledSkill(name, home) {
			t.Errorf("found an installed skill for %q", name)
		}
	}
}
