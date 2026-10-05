package handlers

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

// Acceptance: export helper emits Sandbox (docker) Log section with archived content.
func TestExportWriteStateRunIncludesSandboxArchive(t *testing.T) {
	var b strings.Builder
	used := map[string]bool{}
	logs := []models.SandboxLog{{
		Name:    "grasp-sb-x",
		NodeID:  "research",
		Content: "[boot] ok\n[fatal] fail",
	}}
	writeStateRun(&b, models.StateRun{NodeID: "research", NodeType: "agent", Status: "failed", Iteration: 1}, logs, used)
	out := b.String()
	if !strings.Contains(out, "Sandbox (docker) Log") {
		t.Fatalf("expected sandbox section, got: %q", out)
	}
	if !strings.Contains(out, "[boot] ok") || !strings.Contains(out, "[fatal] fail") {
		t.Fatalf("expected archived content, got: %q", out)
	}
	if !used["grasp-sb-x"] {
		t.Fatal("expected sandbox name marked used")
	}

	// Second emission for the same container must not duplicate.
	var b2 strings.Builder
	writeStateRun(&b2, models.StateRun{NodeID: "research", NodeType: "agent", Status: "failed", Iteration: 2}, logs, used)
	if strings.Contains(b2.String(), "Sandbox (docker) Log") {
		t.Fatalf("duplicate sandbox section on second iteration: %q", b2.String())
	}
}

// Acceptance: no archive → no forged sandbox section.
func TestExportWriteStateRunOmitsMissingSandboxArchive(t *testing.T) {
	var b strings.Builder
	used := map[string]bool{}
	writeStateRun(&b, models.StateRun{NodeID: "research", NodeType: "agent", Status: "completed", Iteration: 1}, nil, used)
	if strings.Contains(b.String(), "Sandbox (docker) Log") {
		t.Fatalf("must not forge sandbox section without archive: %q", b.String())
	}
}
