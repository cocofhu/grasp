package services

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"

	"github.com/cocofhu/grasp/internal/filestore"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTeamBootstrap_CreatesRoster(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:team_boot_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := filestore.Attach(db, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	skills := NewAgentService(filepath.Join(root, "profiles"))
	if err := db.Create(&models.Workspace{ID: models.WorkspaceID, Name: models.DefaultWorkspaceName}).Error; err != nil {
		t.Fatal(err)
	}
	workspace := NewWorkspaceService(db)
	team := NewTeamService(workspace, skills)
	initTeamModel(t, team, db)
	var alternate models.ModelConfiguration
	if err := db.First(&alternate, "id = ?", "model-team").Error; err != nil {
		t.Fatal(err)
	}
	alternate.ID, alternate.Name = "model-team-alt", "Alternative team model"
	if err := db.Create(&alternate).Error; err != nil {
		t.Fatal(err)
	}

	sess, err := team.Bootstrap(TeamBootstrapRequest{
		Prefix:                "Demo",
		ModelConfigurationID:  "model-team",
		ModelConfigurationIDs: []string{"model-team", "model-team-alt"},
		MCP:                   DefaultPlatformMCP(),
		Env:                   map[string]string{"FEATURE": "on"},
	})
	if err != nil {
		t.Fatal(err)
	}

	var cur TeamBootstrapSession
	for i := 0; i < 80; i++ {
		cur, err = team.GetSession(sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		if cur.Status == "ready" || cur.Status == "failed" {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if cur.Status != "ready" {
		t.Fatalf("status=%s err=%s events=%v", cur.Status, cur.Error, cur.Events)
	}
	if len(cur.AgentNames) != len(TeamEngineerTemplates) {
		t.Fatalf("agents=%d want %d: %v", len(cur.AgentNames), len(TeamEngineerTemplates), cur.AgentNames)
	}
	impl := "Demo实现"
	ag, ok := skills.Get(impl)
	if !ok {
		t.Fatalf("missing %s", impl)
	}
	if len(ag.ModelBinding.ConfigurationIDs) != 2 || ag.ModelBinding.ConfigurationID != "model-team" {
		t.Fatalf("engineer lost model group bindings: %+v", ag.ModelBinding)
	}
	if len(ag.MCP) == 0 || ag.MCP[0].Name != "artifact-store" {
		t.Fatalf("mcp=%+v", ag.MCP)
	}
	for _, r := range cur.Resources {
		if r.Kind != "workspace" && r.Kind != "agent" {
			t.Fatalf("unexpected resource kind %q", r.Kind)
		}
	}
}

func agentHasFilePath(a Agent, path string) bool {
	for _, f := range a.Files {
		if f.Path == path {
			return true
		}
	}
	return false
}

func filePaths(a Agent) []string {
	out := make([]string, 0, len(a.Files))
	for _, f := range a.Files {
		out = append(out, f.Path)
	}
	return out
}

func TestCreateAgentFromTemplate_Conflict(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:team_scope_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := filestore.Attach(db, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	skills := NewAgentService(filepath.Join(root, "profiles"))
	if err := db.Create(&models.Workspace{ID: models.WorkspaceID, Name: models.DefaultWorkspaceName}).Error; err != nil {
		t.Fatal(err)
	}
	workspace := NewWorkspaceService(db)
	team := NewTeamService(workspace, skills)
	initTeamModel(t, team, db)

	_, err = team.CreateAgentFromTemplate(CreateFromTemplateArgs{
		TemplateID:           "implement",
		Name:                 "X实现工程师",
		ModelConfigurationID: "model-team",
		MCP:                  DefaultPlatformMCP(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = team.CreateAgentFromTemplate(CreateFromTemplateArgs{
		TemplateID:           "implement",
		Name:                 "X实现工程师",
		ModelConfigurationID: "model-team",
	})
	if !errors.Is(err, ErrTeamAgentConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
}

func initTeamModel(t *testing.T, team *TeamService, db *gorm.DB) {
	t.Helper()
	setCredentialKey(t)
	team.Credentials = NewCredentialService(db)
	credential, err := team.Credentials.Create(CredentialInput{Purpose: "tool_auth", ToolID: "cursor", Name: "Team key", Value: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	row := models.ModelConfiguration(modelrouting.Configuration{ID: "model-team", Name: "Team model", Backend: "cursor", State: "active", Revision: 1, Binding: modelrouting.Binding{Kind: "platform", CredentialID: credential.ID, ModelID: "composer-2.5"}})
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	team.Skills.SetModelBindingResolver(func(backend string, binding *modelrouting.Binding) error {
		_, err := NewModelConnectionService(db).Resolve(backend, *binding)
		return err
	})
	team.Skills.SetModelConfigurationResolver(func(binding *modelrouting.Binding) (string, error) {
		configuration, err := NewModelConfigurationService(db).Get(binding.ConfigurationID)
		return configuration.Backend, err
	})
}
func TestTeamRequiresSavedModel(t *testing.T) {
	db := newTestDB(t)
	if _, err := NewWorkspaceService(db).CreateFixture("Team", ""); err != nil {
		t.Fatal(err)
	}
	agents := NewAgentService(t.TempDir())
	team := NewTeamService(NewWorkspaceService(db), agents)
	team.Credentials = NewCredentialService(db)
	_, err := team.Bootstrap(TeamBootstrapRequest{Prefix: "Demo"})
	if !errors.Is(err, ErrTeamValidation) {
		t.Fatalf("want model selection error, got %v", err)
	}
	if len(agents.List()) != 0 {
		t.Fatal("missing configuration created agents")
	}
}
