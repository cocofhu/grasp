package services

import (
	"context"
	"errors"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

func wireDefaultBindingServices(t *testing.T, shared *SharedAgentService, agents *AgentService, connections *ModelConnectionService, configurations *ModelConfigurationService) {
	t.Helper()
	resolve := func(binding *modelrouting.Binding) (string, error) {
		if err := modelrouting.ValidateConfigurationReference(binding); err != nil {
			return "", err
		}
		cfg, err := configurations.Get(binding.ConfigurationID)
		if err != nil {
			return "", err
		}
		_, err = connections.Resolve(cfg.Backend, *binding)
		return cfg.Backend, err
	}
	git := func(bindings *models.GitBindings) error {
		_, err := runtime.ResolveGitBindings(bindings, NewCredentialService(configurations.db).ResolveGitValue)
		return err
	}
	shared.SetBindingResolvers(resolve, git)
	agents.SetSharedAgent(shared)
	agents.SetModelConfigurationResolver(resolve)
	agents.SetModelBindingResolver(func(backend string, binding *modelrouting.Binding) error {
		_, err := connections.Resolve(backend, *binding)
		return err
	})
	agents.SetGitBindingResolver(git)
}

func TestExplicitDefaultInheritancePreservesLocalSelectionsAndZIP(t *testing.T) {
	connections, configurations, _, connection, defaultModel := newModelConfigurationFixture(t)
	localModel, err := configurations.Create(modelrouting.Configuration{Name: "Local CodeBuddy", Backend: "codebuddy", Binding: modelrouting.Binding{Kind: "connection", ConnectionID: connection.ID, ModelID: connection.Models[0].ID}})
	if err != nil {
		t.Fatal(err)
	}
	credentials := NewCredentialService(configurations.db)
	defaultGit, err := credentials.Create(CredentialInput{Purpose: "git", Provider: "gitlab", Name: "Default GitLab", Value: "synthetic-default-git"})
	if err != nil {
		t.Fatal(err)
	}
	localGit, err := credentials.Create(CredentialInput{Purpose: "git", Provider: "github", Name: "Local GitHub", Value: "synthetic-local-git"})
	if err != nil {
		t.Fatal(err)
	}
	selection := func(id string) *models.GitBindings {
		return &models.GitBindings{Items: []models.GitCredentialBinding{{CredentialID: id}}, PreferredCredentialID: id}
	}
	shared, agents := NewSharedAgentService(t.TempDir()), NewAgentService(t.TempDir())
	wireDefaultBindingServices(t, shared, agents, connections, configurations)
	defaults := SharedAgentConfig{AcpBackend: "cursor", ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: defaultModel.ID}, GitBindings: selection(defaultGit.ID), Env: map[string]string{runtime.EnvCodeBuddyRegion: "public"}}
	if err := shared.Save(defaults); err != nil {
		t.Fatal(err)
	}
	if shared.Get().AcpBackend != "opencode" {
		t.Fatal("default CLI was not derived from its model")
	}
	local := Agent{Name: "local", AcpBackend: "cursor", ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: localModel.ID}, GitBindings: selection(localGit.ID), Env: map[string]string{runtime.EnvCodeBuddyRegion: "internal"}}
	if err := agents.Save(local); err != nil {
		t.Fatal(err)
	}
	local, _ = agents.Get(local.Name)
	if local.AcpBackend != "codebuddy" || agents.Effective(local).ModelBinding.ConfigurationID != localModel.ID || agents.Effective(local).GitBindings.PreferredCredentialID != localGit.ID {
		t.Fatal("missing flags granted default permissions or CLI was not derived")
	}
	local.InheritModel, local.InheritGit = true, true
	if err := agents.Save(local); err != nil {
		t.Fatal(err)
	}
	stored, _ := agents.Get(local.Name)
	effective := agents.Effective(stored)
	if effective.Env[runtime.EnvCodeBuddyRegion] != "public" || stored.Env[runtime.EnvCodeBuddyRegion] != "internal" {
		t.Fatal("inherited model did not use the default site or overwrote the local site")
	}
	if effective.AcpBackend != "opencode" || effective.ModelBinding.ConfigurationID != defaultModel.ID || effective.GitBindings.PreferredCredentialID != defaultGit.ID || effective.Layout.ConfigRoot != DefaultConfigRootForBackend("opencode") {
		t.Fatal("explicit defaults did not determine the effective route")
	}
	if stored.AcpBackend != "codebuddy" || stored.ModelBinding.ConfigurationID != localModel.ID || stored.GitBindings.PreferredCredentialID != localGit.ID || stored.Layout.ConfigRoot != DefaultConfigRootForBackend("codebuddy") {
		t.Fatal("inheritance overwrote local selections")
	}
	zip, err := agents.ExportZIP(local.Name)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := agents.ImportZIP(zip, "imported", ImportZIPCreate)
	if err != nil || !imported.InheritModel || !imported.InheritGit || imported.ModelBinding.ConfigurationID != localModel.ID {
		t.Fatalf("ZIP lost explicit inheritance/local selections: %v", err)
	}
	stored.InheritModel, stored.InheritGit = false, false
	if err := agents.Save(stored); err != nil {
		t.Fatal(err)
	}
	stored, _ = agents.Get(local.Name)
	effective = agents.Effective(stored)
	if effective.Env[runtime.EnvCodeBuddyRegion] != "internal" {
		t.Fatal("turning inheritance off did not restore the local site")
	}
	if effective.AcpBackend != "codebuddy" || effective.GitBindings.PreferredCredentialID != localGit.ID || effective.Layout.ConfigRoot != DefaultConfigRootForBackend("codebuddy") {
		t.Fatal("turning inheritance off did not restore local configuration")
	}
	defaults.ModelBinding = nil
	if err := shared.Save(defaults); err != nil {
		t.Fatal(err)
	}
	stored.InheritModel = true
	if err := agents.Save(stored); !errors.Is(err, ErrInvalidAgent) {
		t.Fatal("missing default silently fell back to the local model")
	}
}

func TestSharedDefaultsValidateReferencesBeforeWriting(t *testing.T) {
	connections, configurations, key, _, configuration := newModelConfigurationFixture(t)
	shared, agents := NewSharedAgentService(t.TempDir()), NewAgentService(t.TempDir())
	wireDefaultBindingServices(t, shared, agents, connections, configurations)
	valid := SharedAgentConfig{ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: configuration.ID}, Env: map[string]string{"FEATURE": "keep"}}
	if err := shared.Save(valid); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []*modelrouting.Binding{{Kind: "inherit"}, {Kind: "configuration", ConfigurationID: "missing"}} {
		candidate := valid
		candidate.ModelBinding = invalid
		if err := shared.Save(candidate); !errors.Is(err, ErrInvalidAgent) {
			t.Fatalf("invalid model accepted: %v", err)
		}
	}
	candidate := valid
	candidate.GitBindings = &models.GitBindings{Items: []models.GitCredentialBinding{{CredentialID: key.ID}}, PreferredCredentialID: key.ID}
	if err := shared.Save(candidate); !errors.Is(err, ErrInvalidAgent) {
		t.Fatal("model API credential accepted as default Git authorization")
	}
	if err := NewCredentialService(configurations.db).Revoke(key.ID); err != nil {
		t.Fatal(err)
	}
	if err := shared.Save(valid); !errors.Is(err, ErrInvalidAgent) {
		t.Fatal("revoked default model credential accepted")
	}
	if shared.Get().ModelBinding.ConfigurationID != configuration.ID || shared.Get().Env["FEATURE"] != "keep" {
		t.Fatal("rejected changes partially rewrote defaults")
	}
}

func TestBaselineReadinessResolvesInheritedModel(t *testing.T) {
	for _, set := range []string{"zh", "en"} {
		t.Run(set, func(t *testing.T) {
			connections, configurations, _, _, configuration := newModelConfigurationFixture(t)
			shared, agents := NewSharedAgentService(t.TempDir()), NewAgentService(t.TempDir())
			wireDefaultBindingServices(t, shared, agents, connections, configurations)
			if err := shared.Save(SharedAgentConfig{ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: configuration.ID}}); err != nil {
				t.Fatal(err)
			}
			for _, name := range canonicalOnboardingNames(set) {
				if err := agents.Save(Agent{Name: name, InheritModel: true}); err != nil {
					t.Fatal(err)
				}
			}
			onboarding := NewOnboardingService(agents, shared, NewWorkflowService(configurations.db), NewCredentialService(configurations.db))
			if err := onboarding.requireBaselineAgents(set); err != nil {
				t.Fatalf("inherited baseline roles rejected: %v", err)
			}
			if err := shared.Save(SharedAgentConfig{}); err != nil {
				t.Fatal(err)
			}
			if err := onboarding.requireBaselineAgents(set); err == nil {
				t.Fatal("unconfigured inherited baseline roles accepted")
			}
		})
	}
}

func TestInheritedDefaultsChangesBlockLongLivedSandbox(t *testing.T) {
	for _, purpose := range []string{"test", SandboxPurposeAgent, SandboxPurposeSolo} {
		for _, change := range []string{"model", "git", "missing model"} {
			t.Run(purpose+"/"+change, func(t *testing.T) {
				s, row, connections, configurations, _, connection, configuration := boundSandboxFixture(t, purpose, true)
				shared := NewSharedAgentService(t.TempDir())
				resolve := func(binding *modelrouting.Binding) (string, error) {
					cfg, err := configurations.Get(binding.ConfigurationID)
					if err != nil {
						return "", err
					}
					_, err = connections.Resolve(cfg.Backend, *binding)
					return cfg.Backend, err
				}
				shared.SetBindingResolvers(resolve, func(bindings *models.GitBindings) error {
					_, err := runtime.ResolveGitBindings(bindings, serviceGitValue)
					return err
				})
				defaults := SharedAgentConfig{ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: configuration.ID}, GitBindings: serviceGitSelection("gh", "gl")}
				if err := shared.Save(defaults); err != nil {
					t.Fatal(err)
				}
				s.SetSharedAgent(shared)
				s.skills.SetSharedAgent(shared)
				agent, _ := s.skills.Get(row.Profile)
				localModel, err := configurations.Create(modelrouting.Configuration{Name: "Local CLI", Backend: "codebuddy", Binding: modelrouting.Binding{Kind: "connection", ConnectionID: connection.ID, ModelID: connection.Models[0].ID}})
				if err != nil {
					t.Fatal(err)
				}
				agent.AcpBackend = "codebuddy"
				agent.ModelBinding = &modelrouting.Binding{Kind: "configuration", ConfigurationID: localModel.ID}
				agent.Layout.ConfigRoot = DefaultConfigRootForBackend("codebuddy")
				agent.InheritModel, agent.InheritGit = true, true
				if err := s.skills.Save(agent); err != nil {
					t.Fatal(err)
				}
				if _, _, err := s.ChatWithTimeout(context.Background(), row.ID, "unchanged defaults", nil, 0, nil); err != nil {
					t.Fatalf("unchanged defaults blocked: %v", err)
				}
				s.teardownLive(row.ID)
				if _, _, err := s.ChatWithTimeout(context.Background(), row.ID, "reconnect inherited defaults", nil, 0, nil); err != nil {
					t.Fatalf("inherited defaults failed after a cold reconnect: %v", err)
				}
				if s.live[row.ID].sb.ConfigRoot != DefaultConfigRootForBackend("opencode") {
					t.Fatal("cold reconnect restored the inactive local CLI config directory")
				}
				switch change {
				case "model":
					other, err := configurations.Create(modelrouting.Configuration{Name: "Another model", Backend: "opencode", Binding: modelrouting.Binding{Kind: "connection", ConnectionID: connection.ID, ModelID: connection.Models[0].ID}})
					if err != nil {
						t.Fatal(err)
					}
					defaults.ModelBinding = &modelrouting.Binding{Kind: "configuration", ConfigurationID: other.ID}
				case "git":
					defaults.GitBindings = &models.GitBindings{Items: []models.GitCredentialBinding{}}
				case "missing model":
					defaults.ModelBinding = nil
				}
				if err := shared.Save(defaults); err != nil {
					t.Fatal(err)
				}
				if _, _, err := s.ChatWithTimeout(context.Background(), row.ID, "must reopen", nil, 0, nil); err == nil {
					t.Fatal("a new turn ignored changed inherited defaults")
				}
			})
		}
	}
}
