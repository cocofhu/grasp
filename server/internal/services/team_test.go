package services

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTeamBootstrap_CreatesRoster(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:team_boot_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
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
	pm := NewPmService(db, skills)
	team := NewTeamService(workspace, skills, pm, nil)
	initTeamModel(t, team, db)

	sess, err := team.Bootstrap(context.Background(), TeamBootstrapRequest{
		Prefix:               "Demo",
		PMName:               "Demo项目经理",
		Background:           "demo background for workflow team",
		ModelConfigurationID: "model-team",
		MCP:                  DefaultPlatformMCP(),
		Env:                  map[string]string{"GIT_REPOS": "${vars.repos}"},
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
	if cur.PMAgent != "Demo项目经理" {
		t.Fatalf("pm=%s", cur.PMAgent)
	}
	if len(cur.AgentNames) != 1+len(TeamEngineerTemplates) {
		t.Fatalf("agents=%d want %d: %v", len(cur.AgentNames), 1+len(TeamEngineerTemplates), cur.AgentNames)
	}
	pmAg, ok := skills.Get("Demo项目经理")
	if !ok {
		t.Fatal("missing PM agent")
	}
	if !agentHasFilePath(pmAg, "rules/role.md") {
		t.Fatalf("PM missing role.md files=%v", filePaths(pmAg))
	}
	if !agentHasFilePath(pmAg, "rules/team-context.md") {
		t.Fatalf("PM missing team-context.md files=%v", filePaths(pmAg))
	}
	if !agentHasFilePath(pmAg, "skills/pm-orchestrate/SKILL.md") {
		t.Fatalf("PM missing orchestrate skill files=%v", filePaths(pmAg))
	}
	ctx := agentFileContent(pmAg, "rules/team-context.md")
	if !strings.Contains(ctx, "demo background for workflow team") {
		t.Fatalf("team-context missing background: %q", ctx)
	}
	if !strings.Contains(ctx, "alwaysApply: true") {
		t.Fatalf("team-context should be alwaysApply: %q", ctx)
	}
	impl := "Demo实现"
	ag, ok := skills.Get(impl)
	if !ok {
		t.Fatalf("missing %s", impl)
	}
	if len(ag.MCP) == 0 || ag.MCP[0].Name != "artifact-store" {
		t.Fatalf("mcp=%+v", ag.MCP)
	}
	if !strings.Contains(ctx, "pm_list_workspace_agents") || strings.Contains(ctx, "组") {
		t.Fatalf("team-context should describe workspace members only: %q", ctx)
	}
	for _, r := range cur.Resources {
		if r.Kind != "workspace" && r.Kind != "agent" {
			t.Fatalf("unexpected resource kind %q", r.Kind)
		}
	}
}

func TestLoadTeamAgentTemplates_AllPackages(t *testing.T) {
	for _, name := range TeamEmbedPackageNames() {
		ag, err := loadTeamAgentTemplate(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(ag.Files) == 0 {
			t.Fatalf("%s: empty workspace files", name)
		}
	}
	pm, err := loadTeamAgentTemplate(TeamPMEmbedName)
	if err != nil {
		t.Fatal(err)
	}
	if !agentHasFilePath(pm, "rules/role.md") || !agentHasFilePath(pm, "skills/pm-orchestrate/SKILL.md") {
		t.Fatalf("PM template incomplete: %v", filePaths(pm))
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

func agentFileContent(a Agent, path string) string {
	for _, f := range a.Files {
		if f.Path == path {
			return f.Content
		}
	}
	return ""
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
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	skills := NewAgentService(filepath.Join(root, "profiles"))
	if err := db.Create(&models.Workspace{ID: models.WorkspaceID, Name: models.DefaultWorkspaceName}).Error; err != nil {
		t.Fatal(err)
	}
	workspace := NewWorkspaceService(db)
	pm := NewPmService(db, skills)
	team := NewTeamService(workspace, skills, pm, nil)
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

type fakeTeamSandbox struct {
	openRow   *models.Sandbox
	openErr   error
	views     []SandboxView
	viewCalls int
	mu        sync.Mutex
}

func (f *fakeTeamSandbox) Open(ctx context.Context, profile string, repos []sandbox.RepoSpec) (*models.Sandbox, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}
	return f.openRow, nil
}

func (f *fakeTeamSandbox) GetView(ctx context.Context, id uint) (*SandboxView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.viewCalls >= len(f.views) {
		last := f.views[len(f.views)-1]
		cp := last
		return &cp, nil
	}
	v := f.views[f.viewCalls]
	f.viewCalls++
	cp := v
	return &cp, nil
}

func TestFinishBootstrap_WaitsForPullingBeforeReady(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:team_pull_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
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
	pm := NewPmService(db, skills)

	fake := &fakeTeamSandbox{
		openRow: &models.Sandbox{ID: 42, Status: "pulling", Name: "sbx-pull"},
		views: []SandboxView{
			{Sandbox: models.Sandbox{ID: 42, Status: "pulling"}},
			{Sandbox: models.Sandbox{ID: 42, Status: "pulling"}},
			{Sandbox: models.Sandbox{ID: 42, Status: "creating"}},
			{Sandbox: models.Sandbox{ID: 42, Status: "running"}},
		},
	}
	team := NewTeamService(workspace, skills, pm, fake)
	initTeamModel(t, team, db)

	sess, err := team.Bootstrap(context.Background(), TeamBootstrapRequest{
		Prefix:               "Pu",
		PMName:               "Pu项目经理",
		Background:           "pull loading coverage for g3.3",
		ModelConfigurationID: "model-team",
		MCP:                  DefaultPlatformMCP(),
	})
	if err != nil {
		t.Fatal(err)
	}

	sawPulling := false
	var cur TeamBootstrapSession
	for i := 0; i < 200; i++ {
		cur, err = team.GetSession(sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		if cur.Status == "pulling" || cur.SandboxStatus == "pulling" {
			sawPulling = true
		}
		if cur.Status == "ready" || cur.Status == "failed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if cur.Status != "ready" {
		t.Fatalf("status=%s err=%s events=%v", cur.Status, cur.Error, cur.Events)
	}
	if !sawPulling {
		t.Fatalf("expected session to report pulling before ready; events=%v sandboxStatus=%s", cur.Events, cur.SandboxStatus)
	}
	if cur.SandboxID != "42" {
		t.Fatalf("sandboxId=%s", cur.SandboxID)
	}
	if cur.SandboxStatus != "running" {
		t.Fatalf("sandboxStatus=%s want running", cur.SandboxStatus)
	}
	hasPullEvent := false
	for _, ev := range cur.Events {
		if strings.Contains(ev.Message, "pulling runtime image") {
			hasPullEvent = true
			break
		}
	}
	if !hasPullEvent {
		t.Fatalf("missing pull event: %v", cur.Events)
	}
}

func TestFinishBootstrap_SandboxErrorFailsSession(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:team_err_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
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
	pm := NewPmService(db, skills)

	fake := &fakeTeamSandbox{
		openRow: &models.Sandbox{ID: 7, Status: "creating", Name: "sbx-err"},
		views: []SandboxView{
			{Sandbox: models.Sandbox{ID: 7, Status: "pulling"}},
			{Sandbox: models.Sandbox{ID: 7, Status: "error", Error: "pull denied"}},
		},
	}
	team := NewTeamService(workspace, skills, pm, fake)
	initTeamModel(t, team, db)

	sess, err := team.Bootstrap(context.Background(), TeamBootstrapRequest{
		Prefix:               "Er",
		PMName:               "Er项目经理",
		Background:           "pull failure should fail bootstrap",
		ModelConfigurationID: "model-team",
		MCP:                  DefaultPlatformMCP(),
	})
	if err != nil {
		t.Fatal(err)
	}

	var cur TeamBootstrapSession
	for i := 0; i < 200; i++ {
		cur, err = team.GetSession(sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		if cur.Status == "ready" || cur.Status == "failed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if cur.Status != "failed" {
		t.Fatalf("status=%s want failed; err=%s", cur.Status, cur.Error)
	}
	if !strings.Contains(cur.Error, "pull denied") {
		t.Fatalf("error=%q", cur.Error)
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
}
func TestTeamRequiresSavedModel(t *testing.T) {
	db := newTestDB(t)
	if _, err := NewWorkspaceService(db).CreateFixture("Team", "", nil); err != nil {
		t.Fatal(err)
	}
	agents := NewAgentService(t.TempDir())
	team := NewTeamService(NewWorkspaceService(db), agents, NewPmService(db, agents), nil)
	team.Credentials = NewCredentialService(db)
	_, err := team.Bootstrap(context.Background(), TeamBootstrapRequest{Prefix: "Demo", Background: "Team background"})
	if !errors.Is(err, ErrTeamValidation) {
		t.Fatalf("want model selection error, got %v", err)
	}
	if len(agents.List()) != 0 {
		t.Fatal("missing configuration created agents")
	}
}
