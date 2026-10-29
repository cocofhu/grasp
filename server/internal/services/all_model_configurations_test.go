package services

import (
	"errors"
	"reflect"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

func TestAllModelConfigurationsPersistAndFollowSharedInheritance(t *testing.T) {
	connections, configurations, _, connection, primary := newModelConfigurationFixture(t)
	sharedRoot, agentRoot := t.TempDir(), t.TempDir()
	shared, agents := NewSharedAgentService(sharedRoot), NewAgentService(agentRoot)
	wireDefaultBindingServices(t, shared, agents, connections, configurations)
	ref := &modelrouting.Binding{Kind: "configuration", ConfigurationID: primary.ID, AllConfigurations: true}
	if err := shared.Save(SharedAgentConfig{ModelBinding: ref}); err != nil {
		t.Fatal(err)
	}
	for _, inherited := range []bool{false, true} {
		name := "local-all"
		if inherited {
			name = "inherited-all"
		}
		if err := agents.Save(Agent{Name: name, AcpBackend: primary.Backend, InheritModel: inherited, ModelBinding: ref}); err != nil {
			t.Fatal(err)
		}
		archive, err := agents.ExportZIP(name)
		if err != nil {
			t.Fatal(err)
		}
		copy, err := agents.ImportZIP(archive, name+"-copy", ImportZIPCreate)
		if err != nil || !reflect.DeepEqual(copy.ModelBinding, ref) || copy.InheritModel != inherited {
			t.Fatalf("ZIP lost dynamic binding: %+v %v", copy.ModelBinding, err)
		}
	}
	// Re-open both services to prove this is persisted, not an in-memory expansion.
	shared, agents = NewSharedAgentService(sharedRoot), NewAgentService(agentRoot)
	wireDefaultBindingServices(t, shared, agents, connections, configurations)
	if !reflect.DeepEqual(shared.Get().ModelBinding, ref) {
		t.Fatal("shared config lost allConfigurations on disk")
	}
	for _, name := range []string{"local-all", "inherited-all"} {
		agent, ok := agents.Get(name)
		if !ok || !reflect.DeepEqual(agents.Effective(agent).ModelBinding, ref) {
			t.Fatalf("saved Agent lost dynamic model inheritance: %s", name)
		}
	}
	later, err := configurations.Create(modelrouting.Configuration{Name: "Later CodeBuddy", Backend: "codebuddy", Binding: modelrouting.Binding{Kind: "connection", ConnectionID: connection.ID, ModelID: connection.Models[0].ID}})
	if err != nil {
		t.Fatal(err)
	}
	inherited, _ := agents.Get("inherited-all")
	selected, err := selectSoloConfiguration(configurations.db, agents.Effective(inherited), later.ID)
	if err != nil || selected.AcpBackend != later.Backend || selected.ModelBinding.ConfigurationID != later.ID {
		t.Fatalf("later group not available through inherited defaults: %+v %v", selected, err)
	}
	// Editing the shared option takes effect for inheriting Agents only.
	if err := shared.Save(SharedAgentConfig{ModelBinding: modelrouting.ConfigurationReference(primary.ID)}); err != nil {
		t.Fatal(err)
	}
	if _, err := selectSoloConfiguration(configurations.db, agents.Effective(inherited), later.ID); err == nil {
		t.Fatal("inherited Agent retained all groups after shared option was cleared")
	}
	local, _ := agents.Get("local-all")
	if _, err := selectSoloConfiguration(configurations.db, agents.Effective(local), later.ID); err != nil {
		t.Fatal("shared edit changed an explicit local binding", err)
	}
}

func TestAllModelConfigurationsResolveLiveRoutesWithoutDefaultFallback(t *testing.T) {
	connections, configurations, _, _, primary := newModelConfigurationFixture(t)
	agent := Agent{Name: "all", AcpBackend: primary.Backend, ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: primary.ID, AllConfigurations: true}}
	// The Agent exists before the second group and its own credentials are added.
	credentials := NewCredentialService(configurations.db)
	key, err := credentials.Create(CredentialInput{Purpose: "tool_auth", ToolID: "cursor", Name: "Later Cursor", Value: "later-cursor-key"})
	if err != nil {
		t.Fatal(err)
	}
	later, err := configurations.Create(modelrouting.Configuration{Name: "Later Cursor", Backend: "cursor", Binding: modelrouting.Binding{Kind: "platform", CredentialID: key.ID, ModelID: "composer", AuxiliaryModelIDs: []string{"composer-fast"}}})
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(backend string, binding *modelrouting.Binding) (modelrouting.Resolved, error) {
		return connections.Resolve(backend, *binding)
	}
	selected, err := selectSoloConfiguration(configurations.db, agent, later.ID)
	if err != nil || selected.AcpBackend != "cursor" || selected.ModelBinding.AllConfigurations {
		t.Fatalf("dynamic selection did not construct one concrete execution: %+v %v", selected, err)
	}
	env := map[string]string{}
	if err := runtime.ApplyModelBinding(env, selected.AcpBackend, selected.ModelBinding, resolve); err != nil {
		t.Fatal(err)
	}
	if env[runtime.EnvModelConfigurationID] != later.ID || env[runtime.EnvModelCredentialID] != key.ID || env[runtime.EnvACPBridgeModel] != "composer" {
		t.Fatalf("execution did not use the selected group's route: %v", env)
	}
	if agent.ModelBinding.ConfigurationID != primary.ID || !agent.ModelBinding.AllConfigurations || agent.AcpBackend != primary.Backend {
		t.Fatal("execution changed the Agent's default binding")
	}
	if err := configurations.db.Create(&models.ReactConversation{RunID: "all-run", NodeID: "all-node", Iteration: 1}).Error; err != nil {
		t.Fatal(err)
	}
	auxiliary, empty := "composer-fast", ""
	choice, err := SetVisitModel(configurations.db, agent, "all-run", "all-node", 1, &later.ID, &auxiliary, &empty)
	if err != nil || choice.CurrentConfigurationID != later.ID || choice.CurrentModelID != auxiliary || choice.Backend != later.Backend {
		t.Fatalf("workflow visit cannot use a later dynamic group: %+v %v", choice, err)
	}
	invalid := "outside-model"
	if _, err := SetVisitModel(configurations.db, agent, "all-run", "all-node", 1, &later.ID, &invalid, nil); err == nil {
		t.Fatal("all groups expanded a group's own configured models")
	}
	if err := credentials.Revoke(key.ID); err != nil {
		t.Fatal(err)
	}
	if err := runtime.ApplyModelBinding(map[string]string{}, selected.AcpBackend, selected.ModelBinding, resolve); !errors.Is(err, ErrModelBindingInvalid) {
		t.Fatalf("dynamic mode bypassed revoked credentials: %v", err)
	}
	// An unrelated invalid group does not block saving/running the valid default.
	shared, agents := NewSharedAgentService(t.TempDir()), NewAgentService(t.TempDir())
	wireDefaultBindingServices(t, shared, agents, connections, configurations)
	if err := shared.Save(SharedAgentConfig{ModelBinding: agent.ModelBinding}); err != nil {
		t.Fatal("unrelated revoked group blocked shared defaults", err)
	}
	if err := agents.Save(agent); err != nil {
		t.Fatal("unrelated revoked group blocked saving", err)
	}
	if err := runtime.ApplyModelBinding(map[string]string{}, agent.AcpBackend, agent.ModelBinding, resolve); err != nil {
		t.Fatal("unrelated revoked group blocked the default route", err)
	}
	later.State = "disabled"
	if _, err := configurations.Update(later.ID, later); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{later.ID, "never-configured"} {
		if _, err := selectSoloConfiguration(configurations.db, agent, id); err == nil {
			t.Fatalf("unavailable dynamic group was selectable: %s", id)
		}
		if _, err := SetVisitModel(configurations.db, agent, "all-run", "all-node", 1, &id, &empty, nil); err == nil {
			t.Fatalf("workflow accepted unavailable dynamic group: %s", id)
		}
	}
	stored, err := GetVisitModel(configurations.db, "all-run", "all-node", 1)
	if err != nil || stored.CurrentConfigurationID != choice.CurrentConfigurationID || stored.CurrentModelID != choice.CurrentModelID {
		t.Fatalf("rejected dynamic selection changed visit choice: %+v %v", stored, err)
	}
	if err := configurations.Delete(primary.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := selectSoloConfiguration(configurations.db, agent, ""); err == nil {
		t.Fatal("missing default silently switched to another configured group")
	}
	if err := agents.Save(agent); !errors.Is(err, ErrInvalidAgent) {
		t.Fatalf("saving a dynamic binding with a missing default succeeded: %v", err)
	}
}
