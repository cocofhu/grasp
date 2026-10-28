package services

import (
	"errors"
	"reflect"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
)

func TestAgentSavesOnlyNamedModelReferencesOrDraft(t *testing.T) {
	s := NewAgentService(t.TempDir())
	if err := s.Save(Agent{Name: "draft", AcpBackend: "codex"}); err != nil {
		t.Fatal(err)
	}
	for _, binding := range []*modelrouting.Binding{
		{Kind: "inherit"}, {Kind: "platform", CredentialID: "key"},
		{Kind: "connection", ConnectionID: "connection", ModelID: "m"},
		{Kind: "configuration", ConfigurationID: "named", ModelID: "override"},
	} {
		if err := s.Save(Agent{Name: "invalid", AcpBackend: "codex", ModelBinding: binding}); !errors.Is(err, ErrInvalidAgent) {
			t.Fatalf("invalid reference accepted: %v", err)
		}
	}
	called := false
	s.SetModelBindingResolver(func(backend string, binding *modelrouting.Binding) error {
		called = backend == "codex" && binding.ConfigurationID == "named"
		return errors.New("model configuration is disabled")
	})
	if err := s.Save(Agent{Name: "rejected", AcpBackend: "codex", ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: "named"}}); !errors.Is(err, ErrInvalidAgent) || !called {
		t.Fatal("named selection did not validate its saved configuration")
	}
	if s.Exists("rejected") {
		t.Fatal("invalid configuration persisted")
	}
	s.SetModelBindingResolver(func(string, *modelrouting.Binding) error { return nil })
	if err := s.Save(Agent{Name: "named", AcpBackend: "codex", ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: "named"}, Env: map[string]string{"ACP_BRIDGE_MODEL": "old", "OPENAI_BASE_URL": "https://old.example", "KEEP": "1"}}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("named")
	if got.ModelBinding == nil || got.ModelBinding.ConfigurationID != "named" || got.Env["ACP_BRIDGE_MODEL"] != "" || got.Env["OPENAI_BASE_URL"] != "" || got.Env["KEEP"] != "1" {
		t.Fatal("configuration reference was not the single source of model routing")
	}
}

func TestAgentBindsDifferentToolsWithoutChangingItsDefault(t *testing.T) {
	s := NewAgentService(t.TempDir())
	backends := map[string]string{"daily": "codex", "review": "claude_code"}
	s.SetModelConfigurationResolver(func(binding *modelrouting.Binding) (string, error) {
		backend := backends[binding.ConfigurationID]
		if backend == "" {
			return "", errors.New("configuration missing")
		}
		return backend, nil
	})
	validated := map[string]string{}
	s.SetModelBindingResolver(func(backend string, binding *modelrouting.Binding) error {
		if backends[binding.ConfigurationID] != backend {
			return errors.New("backend mismatch")
		}
		validated[binding.ConfigurationID] = backend
		return nil
	})
	ref := &modelrouting.Binding{Kind: "configuration", ConfigurationID: "daily", ConfigurationIDs: []string{"daily", "review"}}
	if err := s.Save(Agent{Name: "multi", AcpBackend: "cursor", ModelBinding: ref}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("multi")
	if got.AcpBackend != "codex" || !reflect.DeepEqual(got.ModelBinding, ref) || !reflect.DeepEqual(validated, backends) {
		t.Fatalf("saved route: %+v, validations=%v", got, validated)
	}
	bad := *ref
	bad.ConfigurationIDs = []string{"daily", "missing"}
	if err := s.Save(Agent{Name: "bad-set", AcpBackend: "codex", ModelBinding: &bad}); !errors.Is(err, ErrInvalidAgent) || s.Exists("bad-set") {
		t.Fatalf("invalid alternate was saved: %v", err)
	}
}
