package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOmitOutcomeRuleStripsCompletionSection(t *testing.T) {
	HomeBaseDir = ""
	dir, err := BuildConfigHome(ConfigHomeSpec{
		IncludeArtifactStore: true,
		OmitOutcomeRule:      true,
	})
	if err != nil {
		t.Fatalf("BuildConfigHome: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	storeBody, err := os.ReadFile(filepath.Join(dir, "rules/artifact-store.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(storeBody), "node_complete") {
		t.Fatalf("clarify artifact-store.md must not mention node_complete:\n%s", storeBody)
	}
	if strings.Contains(string(storeBody), "完成标记") {
		t.Fatalf("clarify artifact-store.md must omit 完成标记 section:\n%s", storeBody)
	}

}

func TestArtifactStoreKeepsOutcomeSectionByDefault(t *testing.T) {
	HomeBaseDir = ""
	dir, err := BuildConfigHome(ConfigHomeSpec{IncludeArtifactStore: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	body, err := os.ReadFile(filepath.Join(dir, "rules/artifact-store.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "node_complete") {
		t.Fatal("auto Agent artifact-store.md must keep node_complete section")
	}
}

func TestStripOutcomeRuleSection(t *testing.T) {
	in := "## 强制产物\nbody\n\n## 完成标记 (node_complete,强制)\nsecret\n\n## 结构化产物\nkeep\n"
	got := string(stripOutcomeRuleSection([]byte(in)))
	if strings.Contains(got, "node_complete") || strings.Contains(got, "secret") {
		t.Fatalf("section not stripped: %q", got)
	}
	if !strings.Contains(got, "## 结构化产物") {
		t.Fatalf("following section lost: %q", got)
	}
}
