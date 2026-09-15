package services

import (
	"testing"
)

func TestAllCreateTemplates_IncludesPreflightKeepsNineEngineers(t *testing.T) {
	if len(TeamEngineerTemplates) != 9 {
		t.Fatalf("bootstrap roster want 9, got %d", len(TeamEngineerTemplates))
	}
	all := AllCreateTemplates()
	var hasPreflight bool
	for _, r := range all {
		if r.ID == "preflight" {
			hasPreflight = true
			if r.EmbedName != "PreflightAgent" {
				t.Fatalf("preflight embed = %s", r.EmbedName)
			}
		}
	}
	if !hasPreflight {
		t.Fatal("AllCreateTemplates missing preflight")
	}
	if _, ok := TeamRoleByID("preflight"); !ok {
		t.Fatal("TeamRoleByID(preflight) missing")
	}
	if _, ok := TeamRoleByID("test"); !ok {
		t.Fatal("TeamRoleByID(test) missing")
	}
}

func TestApplyCreateTemplate_TestAndPreflight(t *testing.T) {
	for _, id := range []string{"test", "preflight"} {
		ag := Agent{Name: "qa-1", AcpBackend: "cursor"}
		if err := ApplyCreateTemplate(id, &ag); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if ag.Name != "qa-1" {
			t.Fatalf("%s name mutated: %s", id, ag.Name)
		}
		if len(ag.Files) == 0 {
			t.Fatalf("%s: empty files", id)
		}
		if !agentHasFilePath(ag, "rules/role.md") {
			t.Fatalf("%s missing rules/role.md: %v", id, filePaths(ag))
		}
		if !agentHasFilePath(ag, "AGENTS.md") {
			t.Fatalf("%s missing AGENTS.md", id)
		}
	}
	ag := Agent{Name: "x"}
	if err := ApplyCreateTemplate("no-such", &ag); err == nil {
		t.Fatal("expected unknown templateId error")
	}
}

func TestLoadPreflightAgentPack(t *testing.T) {
	ag, err := LoadTeamAgentTemplate("PreflightAgent")
	if err != nil {
		t.Fatal(err)
	}
	if !agentHasFilePath(ag, "skills/preflight-checklist/SKILL.md") {
		t.Fatalf("preflight pack incomplete: %v", filePaths(ag))
	}
}

func TestTeamEmbedPackageNames_IncludesPreflight(t *testing.T) {
	var found bool
	for _, n := range TeamEmbedPackageNames() {
		if n == "PreflightAgent" {
			found = true
		}
	}
	if !found {
		t.Fatal("TeamEmbedPackageNames missing PreflightAgent")
	}
}
