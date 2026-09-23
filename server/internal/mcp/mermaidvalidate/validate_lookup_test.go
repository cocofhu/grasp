package mermaidvalidate

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckRetriesNodeLookupAfterMiss(t *testing.T) {
	real, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("node is required to prove a later lookup succeeds")
	}

	nodeMu.Lock()
	saved := nodePath
	nodePath = ""
	nodeMu.Unlock()
	t.Cleanup(func() {
		nodeMu.Lock()
		nodePath = saved
		nodeMu.Unlock()
	})

	t.Setenv("GRASP_NODE", "")
	t.Setenv("PATH", filepath.Join(t.TempDir(), "empty"))
	err = Check("flowchart LR\n  A-->B")
	if err == nil || !strings.Contains(err.Error(), "executable file not found") {
		t.Fatalf("want node miss, got %v", err)
	}

	t.Setenv("PATH", filepath.Dir(real))
	if err := Check("flowchart LR\n  A-->B"); err != nil {
		t.Fatalf("lookup must succeed after node appears on PATH: %v", err)
	}

	// A successful path stays cached when PATH later loses node.
	t.Setenv("PATH", filepath.Join(t.TempDir(), "empty-again"))
	if err := Check("flowchart LR\n  A-->B"); err != nil {
		t.Fatalf("successful node path should stay cached: %v", err)
	}
}
