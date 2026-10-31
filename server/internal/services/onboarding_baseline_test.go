package services

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestDefaultWorkflowEnvelopeUsesSharedSandbox(t *testing.T) {
	env, err := loadDefaultWorkflowEnvelope()
	if err != nil {
		t.Fatal(err)
	}
	d := env.Defaults
	if d == nil || d.Sandbox == nil || d.Sandbox.Mode != models.SandboxModeShared || !d.InheritWorkspace {
		t.Fatalf("template defaults = %+v, want inheritWorkspace + shared sandbox", d)
	}
}

func TestNormalizeBaselineRepos(t *testing.T) {
	got := normalizeBaselineRepos([]BaselineRepo{
		{URL: "  "},
		{URL: "https://github.com/acme/app.git", Name: "", Branch: "main"},
		{URL: "https://gitlab.com/acme/app.git"},
		{URL: "https://github.com/acme/docs.git", Name: "docs"},
	})
	if len(got) != 3 {
		t.Fatalf("len=%d want 3: %#v", len(got), got)
	}
	if got[0].Name != "app" || got[0].Branch != "main" {
		t.Fatalf("first: %#v", got[0])
	}
	if got[1].Name != "app-2" {
		t.Fatalf("second name not disambiguated: %#v", got[1])
	}
	if got[2].Name != "docs" {
		t.Fatalf("third: %#v", got[2])
	}
	if len(normalizeBaselineRepos(nil)) != 0 {
		t.Fatal("empty input should yield no repos")
	}
}
