package services_test

import (
	"reflect"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestOnboardingCreatesEmptySoloOutsideWorkflow(t *testing.T) {
	for _, language := range []string{"zh-CN", "en"} {
		t.Run(language, func(t *testing.T) {
			svc := newOnboardingHarness(t)
			key, err := svc.Credentials.Create(services.CredentialInput{Purpose: "git", Type: "git", Provider: "github", Name: "Solo GitHub", EnvKey: "GITHUB_TOKEN", Value: "test-git-secret"})
			if err != nil {
				t.Fatal(err)
			}
			bindings := &models.GitBindings{Items: []models.GitCredentialBinding{{CredentialID: key.ID}}}
			result, err := svc.Bootstrap(services.OnboardingBootstrapRequest{
				ModelConfigurationID: "model-onboarding", Language: language, GitBindings: bindings,
				RepoURL: "https://github.com/example/app.git",
				Agents: []services.OnboardingAgentChoice{
					{TemplateID: "clarify", ModelConfigurationID: "model-onboarding-alt"},
					{TemplateID: "implement"}, {TemplateID: "test_review"}, {TemplateID: "deliver"},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.SoloAgentID != services.OnboardingSoloAgentName || len(result.AgentIDs) != 4 {
				t.Fatalf("Solo must be separate from workflow team: %+v", result)
			}
			solo, ok := svc.Skills.Get(result.SoloAgentID)
			if !ok {
				t.Fatal("Solo Agent is missing")
			}
			if len(solo.Files) != 0 || len(solo.MCP) != 0 || solo.Capabilities != nil || len(solo.Env) != 0 || len(solo.CredentialBindings) != 0 {
				t.Fatalf("Solo must have no role instructions, skills or workflow settings: %+v", solo)
			}
			if solo.AcpBackend != "cursor" || solo.InheritModel || solo.InheritGit || solo.ModelBinding == nil || solo.ModelBinding.Kind != "configuration" || solo.ModelBinding.ConfigurationID != "model-onboarding" {
				t.Fatalf("Solo must use the selected default model: %+v", solo)
			}
			if !reflect.DeepEqual(solo.GitBindings, bindings) {
				t.Fatalf("Solo Git bindings = %+v, want %+v", solo.GitBindings, bindings)
			}
			if solo.Layout.ConfigRoot != services.DefaultConfigRootForBackend("cursor") || solo.Layout.WorkspaceDir != services.DefaultWorkspaceDir {
				t.Fatalf("Solo layout is not runnable: %+v", solo.Layout)
			}
			workflow, ok := svc.WF.Get(result.WorkflowID)
			if !ok {
				t.Fatal("default workflow is missing")
			}
			for _, node := range workflow.Graph.Nodes {
				if models.AgentProfile(node.Config) == services.OnboardingSoloAgentName {
					t.Fatal("Solo must not become a workflow node")
				}
			}
		})
	}
}

func TestOnboardingKeepsExistingSolo(t *testing.T) {
	svc := newOnboardingHarness(t)
	if err := svc.Skills.Save(services.Agent{
		Name: services.OnboardingSoloAgentName, AcpBackend: "cursor",
		ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: "model-onboarding-alt"},
		Files:        []services.AgentFile{{Path: "AGENTS.md", Content: "My customized Solo instructions"}},
		Env:          map[string]string{"PROJECT_LANGUAGE": "Go"},
	}); err != nil {
		t.Fatal(err)
	}
	before, _ := svc.Skills.Get(services.OnboardingSoloAgentName)
	for range 2 {
		result, err := svc.Bootstrap(services.OnboardingBootstrapRequest{ModelConfigurationID: "model-onboarding"})
		if err != nil {
			t.Fatal(err)
		}
		if result.SoloAgentID != services.OnboardingSoloAgentName {
			t.Fatalf("existing Solo identity = %q", result.SoloAgentID)
		}
	}
	after, ok := svc.Skills.Get(services.OnboardingSoloAgentName)
	if !ok || !reflect.DeepEqual(after, before) {
		t.Fatalf("existing Solo changed: before=%+v after=%+v", before, after)
	}
	if len(svc.Skills.List()) != 5 {
		t.Fatalf("bootstrap should create one Solo and four workflow Agents: %d", len(svc.Skills.List()))
	}
}
