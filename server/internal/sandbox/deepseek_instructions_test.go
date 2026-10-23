package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildDeepSeekConfigHomePreservesOverlayRulesAndSkills(t *testing.T) {
	base, agent := t.TempDir(), t.TempDir()
	write := func(root, rel, body string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(base, "AGENTS.md", "shared role is overlaid")
	write(agent, "AGENTS.md", "agent delivery contract")
	write(base, "rules/team.md", "---\nalwaysApply: true\n---\nteam-wide rule")
	write(agent, "rules/conditional.md", "---\nglobs: '**/*.go'\nalwaysApply: false\n---\nconditional Go rule")
	write(agent, "skills/review/SKILL.md", "review skill")
	write(agent, "skills/review/scripts/check.sh", "#!/bin/sh\necho review")
	for _, omitOutcome := range []bool{false, true} {
		dir, err := BuildConfigHome(ConfigHomeSpec{
			BaseWorkDirSrc: base, WorkDirSrc: agent, DeepSeekHarness: true,
			IncludeArtifactStore: true, OmitOutcomeRule: omitOutcome,
			EmbeddedSkills: []string{"skills/live-variants"},
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, required := range []string{"agent delivery contract", "team-wide rule", "基础约定", "artifact-store MCP"} {
			if !strings.Contains(text, required) {
				t.Errorf("missing instructions %q", required)
			}
		}
		for _, excluded := range []string{"shared role is overlaid", "conditional Go rule", "alwaysApply:"} {
			if strings.Contains(text, excluded) {
				t.Errorf("unexpected global instructions %q", excluded)
			}
		}
		if strings.Contains(text, "## 完成标记") == omitOutcome {
			t.Errorf("clarify outcome omission = %v", omitOutcome)
		}
		for _, rel := range []string{"rules/conditional.md", "skills/review/scripts/check.sh", "skills/live-variants/SKILL.md"} {
			if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
				t.Errorf("lost %s: %v", rel, err)
			}
		}
	}
	original, err := os.ReadFile(filepath.Join(agent, "AGENTS.md"))
	if err != nil || string(original) != "agent delivery contract" {
		t.Fatal("agent source instructions were modified")
	}
}
