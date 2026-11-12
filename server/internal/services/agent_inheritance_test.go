package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
)

func TestAgentMissingInheritanceSwitchesUseCurrentDefaultsWithoutWriting(t *testing.T) {
	connections, configurations, _, _, configuration := newModelConfigurationFixture(t)
	shared, agents := NewSharedAgentService(t.TempDir()), NewAgentService(t.TempDir())
	wireDefaultBindingServices(t, shared, agents, connections, configurations)
	defaults := SharedAgentConfig{ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: configuration.ID}}
	if err := shared.Save(defaults); err != nil {
		t.Fatal(err)
	}
	// Local values do not change the meaning of a missing inheritance switch.
	raw := []byte(`{"acpBackend":"codex","modelBinding":{"kind":"configuration","configurationId":"unavailable-local-model"}}`)
	dir := filepath.Join(agents.root, "default-agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "agent.json")
	if err := os.WriteFile(file, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	agent, ok := agents.Get("default-agent")
	if !ok || !agent.InheritModel {
		t.Fatal("missing switches did not select workspace defaults")
	}
	effective := agents.Effective(agent)
	if effective.AcpBackend != configuration.Backend || effective.ModelBinding.ConfigurationID != configuration.ID {
		t.Fatal("missing switches selected local values instead of current defaults")
	}
	after, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(after, raw) {
		t.Fatal("reading current defaults rewrote the stored config")
	}
	if err := agents.Save(agent); err != nil {
		t.Fatal(err)
	}
	defaults.ModelBinding = nil
	if err := shared.Save(defaults); err != nil {
		t.Fatal(err)
	}
	if err := agents.Save(agent); !errors.Is(err, ErrInvalidAgent) {
		t.Fatal("missing shared model silently used the local model")
	}
}

func TestAgentInheritanceJSONAndZIPPreserveExplicitChoices(t *testing.T) {
	connections, configurations, _, _, configuration := newModelConfigurationFixture(t)
	shared, agents := NewSharedAgentService(t.TempDir()), NewAgentService(t.TempDir())
	wireDefaultBindingServices(t, shared, agents, connections, configurations)
	if err := shared.Save(SharedAgentConfig{ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: configuration.ID}}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		fields map[string]any
		model  bool
	}{
		{name: "omitted", model: true},
		{name: "local-model", fields: map[string]any{"inheritModel": false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			meta := map[string]any{"name": tc.name, "schemaVersion": AgentExportSchemaVersion, "acpBackend": configuration.Backend, "modelBinding": &modelrouting.Binding{Kind: "configuration", ConfigurationID: configuration.ID}}
			for key, value := range tc.fields {
				meta[key] = value
			}
			raw, err := json.Marshal(meta)
			if err != nil {
				t.Fatal(err)
			}
			imported, err := agents.ImportZIP(buildTestZip(t, raw, nil), tc.name, ImportZIPCreate)
			if err != nil || imported.InheritModel != tc.model {
				t.Fatalf("import did not preserve defaults/explicit false: %v", err)
			}
			stored, err := os.ReadFile(filepath.Join(agents.root, tc.name, "agent.json"))
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]any
			if err := json.Unmarshal(stored, &document); err != nil || document["inheritModel"] != tc.model {
				t.Fatal("stored inheritance switches were omitted or changed")
			}
			archive, err := agents.ExportZIP(tc.name)
			if err != nil {
				t.Fatal(err)
			}
			copy, err := agents.ImportZIP(archive, tc.name+"-copy", ImportZIPCreate)
			if err != nil || copy.InheritModel != tc.model {
				t.Fatalf("ZIP roundtrip did not preserve inheritance switches: %v", err)
			}
		})
	}
}
