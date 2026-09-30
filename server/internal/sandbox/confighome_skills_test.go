package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildConfigHomeEmbeddedSkills(t *testing.T) {
	HomeBaseDir = ""
	ws := t.TempDir()
	stale := filepath.Join(ws, "skills", "live-variants", "stale.md")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("agent copy"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir, err := BuildConfigHome(ConfigHomeSpec{
		WorkDirSrc:     ws,
		EmbeddedSkills: []string{"skills/live-variants"},
	})
	if err != nil {
		t.Fatalf("BuildConfigHome: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	b, err := os.ReadFile(filepath.Join(dir, "skills", "live-variants", "SKILL.md"))
	if err != nil {
		t.Fatalf("SKILL.md missing: %v", err)
	}
	if !strings.Contains(string(b), "live_update") {
		t.Error("SKILL.md should describe live_update")
	}
	for _, a := range []string{"bolder", "quieter", "polish", "typeset", "colorize", "layout", "distill", "adapt", "freeform"} {
		if _, err := os.Stat(filepath.Join(dir, "skills", "live-variants", "reference", a+".md")); err != nil {
			t.Errorf("reference/%s.md missing: %v", a, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "live-variants", "stale.md")); !os.IsNotExist(err) {
		t.Error("platform skill should replace the agent workspace copy")
	}
}

func TestBuildConfigHomeEmbeddedSkillsRejectsBadPath(t *testing.T) {
	HomeBaseDir = ""
	for _, rel := range []string{"../etc", "/abs", ".", "skills/missing"} {
		dir, err := BuildConfigHome(ConfigHomeSpec{EmbeddedSkills: []string{rel}})
		if err == nil {
			os.RemoveAll(dir)
			t.Errorf("%q: expected error", rel)
		}
	}
}
