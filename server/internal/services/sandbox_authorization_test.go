package services

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func authorizationFixture(t *testing.T) (*SandboxService, Agent, *CredentialService, map[string]string) {
	t.Helper()
	connections, configurations, key, _, configuration := newModelConfigurationFixture(t)
	s := newSandboxService(t, configurations.db, &dockerState{})
	s.modelBinding = func(backend string, binding *modelrouting.Binding) (modelrouting.Resolved, error) {
		return connections.Resolve(backend, *binding)
	}
	credentials := NewCredentialService(s.db)
	ids := map[string]string{"model": key.ID}
	for kind, in := range map[string]CredentialInput{
		"git":   {Purpose: "git", Type: "git", Provider: "github", Name: "Git", EnvKey: "GITHUB_TOKEN", Value: "synthetic-git-secret"},
		"extra": {Purpose: "custom", Type: "custom", Name: "Extra", EnvKey: "EXTRA_TOKEN", Value: "synthetic-extra-secret"},
		"mcp":   {Purpose: "mcp", Type: "mcp", Name: "MCP", EnvKey: "MCP_TOKEN", Value: "synthetic-mcp-secret"},
	} {
		row, err := credentials.Create(in)
		if err != nil {
			t.Fatal(err)
		}
		ids[kind] = row.ID
	}
	s.gitCredential = credentials.ResolveGitValue
	s.selectedCredential = credentials.ResolveSelectedValue
	s.credentialReferences = credentials.ResolveReferences
	extra := ids["extra"]
	agent := Agent{Name: "agentA", AcpBackend: "opencode",
		ModelBinding:       &modelrouting.Binding{Kind: "configuration", ConfigurationID: configuration.ID},
		GitBindings:        &models.GitBindings{Items: []models.GitCredentialBinding{{CredentialID: ids["git"]}}},
		CredentialBindings: map[string]*string{"EXTRA_TOKEN": &extra},
		MCP:                []MCPServer{{Name: "private", URL: "https://example.invalid/mcp", Headers: map[string]string{"Authorization": "Bearer ${credential:" + ids["mcp"] + "}"}}},
		Files:              []AgentFile{{Path: "rules/b.md", Content: "b"}, {Path: "rules/a.md", Content: "a"}},
		Env:                map[string]string{"ORDINARY": "value"}, Layout: AgentLayout{ConfigRoot: "/root/.cursor", WorkspaceDir: "/root/workspace"},
	}
	return s, agent, credentials, ids
}

func TestAuthorizationFingerprintUsesCurrentGrantsWithoutLeakingSecrets(t *testing.T) {
	s, agent, _, _ := authorizationFixture(t)
	hash, err := s.currentAuthorizationFingerprint(context.Background(), agent)
	if err != nil || len(hash) != 64 {
		t.Fatalf("authorization proof unavailable: %v", err)
	}
	agent.Files[0], agent.Files[1] = agent.Files[1], agent.Files[0]
	again, err := s.currentAuthorizationFingerprint(context.Background(), agent)
	if err != nil || again != hash {
		t.Fatal("ordinary file enumeration order changed the grant")
	}
	for _, secret := range []string{"private-model-key", "synthetic-git-secret", "synthetic-extra-secret", "synthetic-mcp-secret"} {
		if strings.Contains(hash, secret) {
			t.Fatal("plaintext escaped through authorization proof")
		}
	}
}

func TestAuthorizationFingerprintDetectsEveryCredentialRevisionAndRevocation(t *testing.T) {
	for _, kind := range []string{"model", "git", "extra", "mcp"} {
		t.Run(kind, func(t *testing.T) {
			s, agent, credentials, ids := authorizationFixture(t)
			before, err := s.currentAuthorizationFingerprint(context.Background(), agent)
			if err != nil {
				t.Fatal(err)
			}
			// A same-value rotation is still a new authorization revision.
			if err := s.db.Model(&models.Credential{}).Where("id = ?", ids[kind]).UpdateColumn("secret_revision", gorm.Expr("secret_revision + 1")).Error; err != nil {
				t.Fatal(err)
			}
			after, err := s.currentAuthorizationFingerprint(context.Background(), agent)
			if err != nil || before == after {
				t.Fatalf("%s revision was not detected: %v", kind, err)
			}
			if err := credentials.Revoke(ids[kind]); err != nil {
				t.Fatal(err)
			}
			if _, err := s.currentAuthorizationFingerprint(context.Background(), agent); err == nil {
				t.Fatalf("revoked %s still authorized provider recovery", kind)
			}
		})
	}
}

func TestAuthorizationFingerprintIncludesCapabilitiesAndExplicitClear(t *testing.T) {
	s, original, _, _ := authorizationFixture(t)
	before, err := s.currentAuthorizationFingerprint(context.Background(), original)
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Agent){
		"clear Git":           func(a *Agent) { a.GitBindings = &models.GitBindings{} },
		"clear extra":         func(a *Agent) { a.CredentialBindings["EXTRA_TOKEN"] = nil },
		"MCP allow-list":      func(a *Agent) { a.MCP = nil },
		"ordinary env":        func(a *Agent) { a.Env["ORDINARY"] = "changed" },
		"layout":              func(a *Agent) { a.Layout.WorkspaceDir = "/root/other" },
		"capabilities":        func(a *Agent) { a.Capabilities = &models.AgentCapabilities{} },
		"configuration files": func(a *Agent) { a.Files[0].Content = "changed" },
	} {
		t.Run(name, func(t *testing.T) {
			body, _ := json.Marshal(original)
			var agent Agent
			if err := json.Unmarshal(body, &agent); err != nil {
				t.Fatal(err)
			}
			change(&agent)
			after, err := s.currentAuthorizationFingerprint(context.Background(), agent)
			if err != nil || before == after {
				t.Fatalf("changed grant ignored: %v", err)
			}
		})
	}
}

func TestAuthorizationResolutionRejectsMissingReferencesAndTransactions(t *testing.T) {
	s, agent, _, _ := authorizationFixture(t)
	for _, template := range []string{"Bearer ${credential:missing}", "Bearer ${credential:}"} {
		agent.MCP[0].Headers["Authorization"] = template
		if _, err := s.currentAuthorizationFingerprint(context.Background(), agent); err == nil || strings.Contains(err.Error(), "synthetic-") {
			t.Fatal("missing MCP credential was accepted or a secret leaked")
		}
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		inside := &SandboxService{db: tx, modelBinding: s.modelBinding}
		if _, err := inside.currentAuthorizationFingerprint(context.Background(), agent); err == nil || !strings.Contains(err.Error(), "transaction-free") {
			t.Fatal("authorization resolvers ran inside a SQL transaction")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.currentAuthorizationFingerprint(ctx, agent); err == nil {
		t.Fatal("cancelled authorization resolution proceeded")
	}
}
