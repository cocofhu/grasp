package services

import (
	"errors"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
)

func TestModelDefaultsInheritanceIsWholeAndToolSpecific(t *testing.T) {
	s := NewSharedAgentService(t.TempDir())
	defaults := ModelDefaults{BindingsByBackend: map[string]modelrouting.Binding{
		"codex":  {Kind: "connection", ConnectionID: "responses", ModelID: "raw/id", ToolSettings: modelrouting.ToolSettings{ModelRoles: map[string]string{"fast": "small"}}},
		"cursor": {Kind: "platform", CredentialID: "cursor-key"},
	}}
	if err := s.SaveModelDefaults(defaults, func(string, *modelrouting.Binding) error { return nil }); err != nil {
		t.Fatal(err)
	}
	got, err := s.ConcreteModelBinding("codex", &modelrouting.Binding{Kind: "inherit", ModelID: "must-not-overlay"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ConnectionID != "responses" || got.ModelID != "raw/id" || got.ToolSettings.ModelRoles["fast"] != "small" {
		t.Fatalf("incomplete inherited binding: %+v", got)
	}
	if _, err := s.ConcreteModelBinding("trae", &modelrouting.Binding{Kind: "inherit"}); err == nil {
		t.Fatal("fell back to another tool's default")
	}
	if got, err := s.ConcreteModelBinding("codex", nil); err != nil || got != nil {
		t.Fatal("legacy nil binding should remain legacy")
	}
	bad := ModelDefaults{BindingsByBackend: map[string]modelrouting.Binding{"codex": {Kind: "platform", CredentialID: "revoked"}}}
	if err := s.SaveModelDefaults(bad, func(string, *modelrouting.Binding) error { return errors.New("revoked") }); err == nil {
		t.Fatal("invalid default saved")
	}
	stored, err := s.GetModelDefaults()
	if err != nil || stored.BindingsByBackend["codex"].ConnectionID != "responses" {
		t.Fatal("failed validation changed existing defaults")
	}
	if err := s.SaveModelDefaults(ModelDefaults{BindingsByBackend: map[string]modelrouting.Binding{"codex": {Kind: "inherit"}}}, nil); err == nil {
		t.Fatal("recursive default accepted")
	}
}

func TestAgentModelBindingReplacesLegacySelection(t *testing.T) {
	s := NewAgentService(t.TempDir())
	validated := false
	s.SetModelBindingResolver(func(backend string, binding *modelrouting.Binding) error {
		validated = backend == "codex" && binding.ConnectionID == "new-connection"
		return nil
	}, nil)
	if err := s.Save(Agent{Name: "bound-agent", AcpBackend: "codex", AiCredentialID: "old-key", OpenCodeCredentialID: "old-model", ModelBinding: &modelrouting.Binding{Kind: "connection", ConnectionID: "new-connection", ModelID: "raw/model"}, Env: map[string]string{"ACP_BRIDGE_MODEL": "old-model", "OPENAI_BASE_URL": "old-url", "PROJECT": "project"}}); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get("bound-agent")
	if !ok || !validated || got.ModelBinding == nil || got.ModelBinding.ModelID != "raw/model" {
		t.Fatal("model binding was not validated and persisted")
	}
	if got.AiCredentialID != "" || got.OpenCodeCredentialID != "" || got.Env["ACP_BRIDGE_MODEL"] != "" || got.Env["OPENAI_BASE_URL"] != "" || got.Env["PROJECT"] != "project" {
		t.Fatalf("legacy routing survived: %+v", got)
	}
}
