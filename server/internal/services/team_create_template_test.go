package services

import (
	"testing"

	"github.com/cocofhu/grasp/internal/nodereg"
)

func TestAllCreateTemplates_BuiltInAgentsDeclareCapabilities(t *testing.T) {
	want := []string{"clarify", "implement", "test_review"}
	all := AllCreateTemplates()
	if len(all) != len(want) {
		t.Fatalf("templates = %d, want %d", len(all), len(want))
	}
	for i, r := range all {
		if r.ID != want[i] {
			t.Fatalf("template[%d] = %s, want %s", i, r.ID, want[i])
		}
		if r.Capabilities == nil {
			t.Fatalf("%s: missing capabilities", r.ID)
		}
		if err := nodereg.ValidateCapabilities(r.ID, r.Capabilities); err != nil {
			t.Fatalf("%s: invalid capabilities: %v", r.ID, err)
		}
		if _, ok := TeamRoleByID(r.ID); !ok {
			t.Fatalf("TeamRoleByID(%s) missing", r.ID)
		}
	}
	if !all[0].Capabilities.Clarify() || all[1].Capabilities.Clarify() || all[2].Capabilities.Clarify() {
		t.Fatal("only 需求澄清 is a clarify Agent")
	}
	if !all[1].Capabilities.CommitsCode() {
		t.Fatal("实现 must write implementation_result")
	}
	for _, r := range all {
		if !r.Capabilities.CanPreview() {
			t.Fatalf("%s must be granted set_preview", r.ID)
		}
	}
}

func TestApplyCreateTemplate(t *testing.T) {
	for _, r := range TeamEngineerTemplates {
		ag := Agent{Name: "qa-1", AcpBackend: "cursor"}
		if err := ApplyCreateTemplate(r.ID, &ag); err != nil {
			t.Fatalf("%s: %v", r.ID, err)
		}
		if ag.Name != "qa-1" {
			t.Fatalf("%s name mutated: %s", r.ID, ag.Name)
		}
		if !agentHasFilePath(ag, "AGENTS.md") {
			t.Fatalf("%s missing AGENTS.md: %v", r.ID, filePaths(ag))
		}
		if ag.Capabilities == nil {
			t.Fatalf("%s: capabilities not applied", r.ID)
		}
	}
	ag := Agent{Name: "x"}
	if err := ApplyCreateTemplate("no-such", &ag); err == nil {
		t.Fatal("expected unknown templateId error")
	}
}

func TestTeamEmbedPackageNames(t *testing.T) {
	got := TeamEmbedPackageNames()
	want := []string{TeamPMEmbedName, "ClarifyAgent", "ImplementAgent", "TestReviewAgent"}
	if len(got) != len(want) {
		t.Fatalf("packages = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("packages = %v, want %v", got, want)
		}
		if _, err := LoadTeamAgentTemplate(want[i]); err != nil {
			t.Fatalf("load %s: %v", want[i], err)
		}
	}
}
