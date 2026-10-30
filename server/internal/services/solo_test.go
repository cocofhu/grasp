package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/sandbox/sandboxtest"

	"gorm.io/gorm"
)

func TestProjectValidation(t *testing.T) {
	db := newTestDB(t)
	ps := NewProjectService(db)

	cases := []struct {
		name string
		in   ProjectInput
		code string
	}{
		{"empty name", ProjectInput{RepoURL: "https://x/y.git"}, "name_required"},
		{"bad url", ProjectInput{Name: "a", RepoURL: "ftp://x"}, "repo_url_invalid"},
		{"bad dir", ProjectInput{Name: "a", RepoURL: "https://x/y.git", RepoDirName: "../up"}, "repo_dir_invalid"},
	}
	for _, tc := range cases {
		_, err := ps.Create(tc.in, "alice")
		var se *SoloError
		if !errors.As(err, &se) || se.Code != tc.code {
			t.Fatalf("%s: got %v, want code %s", tc.name, err, tc.code)
		}
	}

	p, err := ps.Create(ProjectInput{
		Name: " demo ", RepoURL: "git@github.com:acme/demo.git", DefaultBranch: "main",
	}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "demo" || p.RepoDirName != "demo" || p.DefaultBranch != "main" || p.RepoURL != "git@github.com:acme/demo.git" {
		t.Fatalf("normalized project: %+v", p)
	}
	if _, err := ps.Create(ProjectInput{Name: "demo", RepoURL: "https://x/y.git"}, "bob"); err == nil {
		t.Fatal("duplicate name should be rejected")
	}
	upd, err := ps.Update(p.ID, ProjectInput{Name: "demo", RepoURL: "https://github.com/acme/demo.git", Description: "kept"})
	if err != nil || upd.RepoURL != "https://github.com/acme/demo.git" || upd.Description != "kept" || upd.RepoDirName != "demo" {
		t.Fatalf("update keeps clone fields: %+v %v", upd, err)
	}
}

func TestSoloRepoSpecs(t *testing.T) {
	if SoloRepoSpecs(nil, "x") != nil {
		t.Fatal("nil project has no repos")
	}
	p := &models.Project{RepoURL: "https://github.com/acme/app.git", DefaultBranch: "main"}
	got := SoloRepoSpecs(p, "")
	if len(got) != 1 || got[0].Name != "app" || got[0].Branch != "main" {
		t.Fatalf("default branch: %+v", got)
	}
	if got := SoloRepoSpecs(p, "feat/x"); got[0].Branch != "feat/x" {
		t.Fatalf("branch override: %+v", got)
	}
}

func newSoloFixture(t *testing.T, acpPort, maxSolo int) (*SoloService, *SandboxService, *sandboxtest.FakeGateway, *gorm.DB) {
	t.Helper()
	db := newTestDB(t)
	if err := db.Create(&models.ModelConfiguration{ID: "sandbox-fixture", Name: "Fixture", Backend: "cursor", State: "active", Binding: modelrouting.Binding{Kind: "platform", CredentialID: "k", ModelID: "fixture-model"}}).Error; err != nil {
		t.Fatal(err)
	}
	fg := sandboxtest.New(t)
	fg.ACPPort = acpPort
	mgr := sandbox.NewManager(fg.Client(), sandbox.ManagerOptions{WorkspaceDir: "/root/workspace"})
	root := t.TempDir()
	skills := NewAgentService(root)
	if err := skills.Save(Agent{
		Name: "agentA", AcpBackend: AcpBackendCursor, ModelBinding: sandboxTestModelBinding(),
		Files: []AgentFile{{Path: "rules/a.md", Content: "# a"}},
		Env:   map[string]string{"AGENT_FLAG": "1"},
	}); err != nil {
		t.Fatal(err)
	}
	sbx := NewSandboxService(db, mgr, skills, mcp.NewHost(NewArtifactService(db)), SandboxOptions{
		ProfilesRoot: root, MCPEndpoint: "http://mcp.local", TTL: time.Minute, Max: 2, MaxSolo: maxSolo,
		ModelBinding: sandboxTestModelResolver,
		GitCredential: func(id string) (runtime.GitCredentialValue, bool) {
			if id == "cred-proj" {
				return runtime.GitCredentialValue{EnvKey: "GITHUB_TOKEN", Secret: "ghp-project", Host: "https://github.com"}, true
			}
			return runtime.GitCredentialValue{}, false
		},
	})
	return NewSoloService(db, sbx, skills, NewProjectService(db)), sbx, fg, db
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestSoloSessionLifecycle(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, sbx, fg, db := newSoloFixture(t, port, 3)
	ctx := context.Background()

	p, err := solo.Projects().Create(ProjectInput{
		Name: "app", RepoURL: "https://github.com/acme/app.git", DefaultBranch: "main",
	}, "alice")
	if err != nil {
		t.Fatal(err)
	}

	v, err := solo.Create(ctx, "alice", SoloCreateInput{AgentName: "agentA", ProjectID: p.ID, Text: "hello there"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if v.Title != "hello there" || v.Branch != "main" || v.ProjectSnapshot == nil {
		t.Fatalf("created view: %+v", v.SoloSession)
	}
	if len(v.Repos) != 1 || v.Repos[0].Name != "app" || v.Repos[0].Branch != "main" || v.Repos[0].RepoDirName != "app" {
		t.Fatalf("single repo snapshot: %+v", v.Repos)
	}
	id := v.ID

	var msgs []models.SoloMessage
	waitFor(t, "assistant reply", func() bool {
		msgs, _ = solo.Messages(id, "alice")
		for _, m := range msgs {
			if m.Role == "assistant" {
				return true
			}
		}
		return false
	})
	var reply models.SoloMessage
	for _, m := range msgs {
		if m.Role == "assistant" {
			reply = m
		}
	}
	if reply.Content != "hi" || reply.Status != "ok" || len(reply.Events) == 0 {
		t.Fatalf("reply: %+v", reply)
	}
	if msgs[0].Role != "user" || msgs[0].Content != "hello there" {
		t.Fatalf("user message: %+v", msgs[0])
	}

	row, _ := solo.Require(id, "alice")
	if row.Status != models.SoloStatusRunning {
		t.Fatalf("status: %s (%s)", row.Status, row.Error)
	}
	var sb models.Sandbox
	db.First(&sb, row.SandboxID)
	if sb.Purpose != SandboxPurposeSolo || sb.DestroyAt != nil {
		t.Fatalf("resident sandbox: purpose=%s destroyAt=%v", sb.Purpose, sb.DestroyAt)
	}
	sbx.sweepOnce(ctx)
	if err := db.First(&models.Sandbox{}, sb.ID).Error; err != nil {
		t.Fatalf("sweeper removed resident sandbox: %v", err)
	}
	sbx.CleanupIdle(ctx)
	if err := db.First(&models.Sandbox{}, sb.ID).Error; err != nil {
		t.Fatalf("cleanup removed resident sandbox: %v", err)
	}

	env := fg.LastCreateEnv()
	if env["GIT_REPOS"] != "app|https://github.com/acme/app.git|main" {
		t.Fatalf("GIT_REPOS = %q", env["GIT_REPOS"])
	}
	if env["APP_ENV"] != "" || env["AGENT_FLAG"] != "1" || env["GITHUB_TOKEN"] != "" {
		t.Fatalf("env layering: APP_ENV=%q AGENT_FLAG=%q GITHUB_TOKEN=%q", env["APP_ENV"], env["AGENT_FLAG"], env["GITHUB_TOKEN"])
	}
	entries, err := solo.Env(id, "alice")
	if err != nil {
		t.Fatal(err)
	}
	src := map[string]string{}
	for _, e := range entries {
		src[e.Key] = e.Source
	}
	if src["APP_ENV"] != "" || src["AGENT_FLAG"] != EnvSourceAgent || src["GITHUB_TOKEN"] != "" || src["GIT_REPOS"] != EnvSourceSystem {
		t.Fatalf("env sources: %+v", src)
	}

	if _, err := solo.Require(id, "bob"); err == nil {
		t.Fatal("other users must not see the session")
	}
	if err := solo.Delete(ctx, id, "alice"); err == nil {
		t.Fatal("deleting a running session should fail")
	}

	if _, err := solo.Close(ctx, id, "alice"); err != nil {
		t.Fatalf("close: %v", err)
	}
	row, _ = solo.Require(id, "alice")
	if row.Status != models.SoloStatusClosed || row.ClosedAt == nil {
		t.Fatalf("closed row: %+v", row)
	}
	if err := db.First(&models.Sandbox{}, sb.ID).Error; err == nil {
		t.Fatal("close should destroy the sandbox")
	}
	if _, err := solo.Send(id, "alice", "again", nil); err == nil {
		t.Fatal("send on closed session should fail")
	}

	if _, err := solo.Restart(ctx, id, "alice"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	waitFor(t, "restart running", func() bool {
		r, _ := solo.Require(id, "alice")
		return r.Status == models.SoloStatusRunning
	})
	row, _ = solo.Require(id, "alice")
	if row.Restarts != 1 || !row.NeedsPreamble {
		t.Fatalf("restart flags: restarts=%d preamble=%v", row.Restarts, row.NeedsPreamble)
	}
	if pre := solo.buildPreamble(id, ""); !strings.Contains(pre, "hello there") {
		t.Fatalf("preamble missing history: %q", pre)
	}

	// Destroying the sandbox outside Close flags the session.
	if err := sbx.Destroy(ctx, row.SandboxID); err != nil {
		t.Fatal(err)
	}
	row, _ = solo.Require(id, "alice")
	if row.Status != models.SoloStatusError {
		t.Fatalf("external destroy should mark error, got %s", row.Status)
	}
	if err := solo.Delete(ctx, id, "alice"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var left int64
	db.Model(&models.SoloMessage{}).Where("session_id = ?", id).Count(&left)
	if left != 0 {
		t.Fatalf("messages left after delete: %d", left)
	}
}

func TestSoloSandboxLimit(t *testing.T) {
	solo, _, _, db := newSoloFixture(t, 0, 1)
	db.Create(&models.Sandbox{Name: "sb-solo-busy", Purpose: SandboxPurposeSolo, Status: "running"})
	_, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	var se *SoloError
	if !errors.As(err, &se) || se.Code != "solo_limit" || se.Status != http.StatusConflict {
		t.Fatalf("want solo_limit, got %v", err)
	}
	var n int64
	db.Model(&models.SoloSession{}).Count(&n)
	if n != 0 {
		t.Fatalf("rejected create must not persist a session, got %d", n)
	}
}

func TestSoloCreateValidation(t *testing.T) {
	solo, _, _, _ := newSoloFixture(t, 0, 3)
	ctx := context.Background()
	if _, err := solo.Create(ctx, "alice", SoloCreateInput{AgentName: "nope"}); err == nil {
		t.Fatal("unknown agent should fail")
	}
	if _, err := solo.Create(ctx, "alice", SoloCreateInput{AgentName: "agentA", ProjectID: "missing"}); err == nil {
		t.Fatal("unknown project should fail")
	}
	if _, err := solo.Create(ctx, "alice", SoloCreateInput{AgentName: "agentA", Branch: "a b"}); err == nil {
		t.Fatal("invalid branch should fail")
	}
}

func TestSoloInitialModelMustBelongToConfiguration(t *testing.T) {
	solo, _, _, db := newSoloFixture(t, 0, 3)
	row := models.ModelConfiguration{
		ID: "sandbox-fixture", Name: "Fixture", Backend: "cursor", State: "active",
		Binding: modelrouting.Binding{Kind: "platform", CredentialID: "k", ModelID: "primary", AuxiliaryModelIDs: []string{"aux"}},
	}
	if err := db.Save(&row).Error; err != nil {
		t.Fatal(err)
	}
	_, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA", InitialModelID: "nope"})
	var se *SoloError
	if !errors.As(err, &se) || se.Code != "model_not_in_configuration" {
		t.Fatalf("outside model: %v", err)
	}
}

func TestSoloMultiRepoCreate(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, fg, db := newSoloFixture(t, port, 4)
	ctx := context.Background()
	ps := solo.Projects()
	app, err := ps.Create(ProjectInput{
		Name: "app", RepoURL: "https://github.com/acme/app.git", DefaultBranch: "main",
	}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	api, err := ps.Create(ProjectInput{
		Name: "api", RepoURL: "https://github.com/acme/api.git", DefaultBranch: "develop",
	}, "alice")
	if err != nil {
		t.Fatal(err)
	}

	v, err := solo.Create(ctx, "alice", SoloCreateInput{
		AgentName: "agentA",
		Repos: []SoloRepoChoice{
			{ProjectID: app.ID},
			{ProjectID: api.ID, Branch: "feat/pay"},
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if v.ProjectID != "" || v.ProjectSnapshot != nil || v.Branch != "" {
		t.Fatalf("multi-repo session should not keep a single project column: %+v", v.SoloSession)
	}
	if len(v.Repos) != 2 || v.Repos[0].ProjectID != app.ID || v.Repos[0].Branch != "main" || v.Repos[1].Branch != "feat/pay" {
		t.Fatalf("snapshot: %+v", v.Repos)
	}
	want := "app|https://github.com/acme/app.git|main,api|https://github.com/acme/api.git|feat/pay"
	waitFor(t, "multi repo running", func() bool {
		row, err := solo.Require(v.ID, "alice")
		return err == nil && row.Status == models.SoloStatusRunning && fg.LastCreateEnv()["GIT_REPOS"] == want
	})
	env := fg.LastCreateEnv()
	if env["APP_ENV"] != "" || env["API_TOKEN"] != "" || env["GITHUB_TOKEN"] != "" {
		t.Fatalf("repository config leaked into env: APP_ENV=%q API_TOKEN=%q GITHUB_TOKEN=%q", env["APP_ENV"], env["API_TOKEN"], env["GITHUB_TOKEN"])
	}

	appRow, _ := ps.Get(app.ID)
	apiRow, _ := ps.Get(api.ID)
	if appRow.LastUsedAt == nil || apiRow.LastUsedAt == nil {
		t.Fatal("every selected repository should record last used")
	}

	active, err := ps.ActiveSessions(api.ID)
	if err != nil || len(active) != 1 || active[0].ID != v.ID {
		t.Fatalf("second repo active sessions: %+v %v", active, err)
	}

	if _, err := ps.Update(app.ID, ProjectInput{
		Name: "app", RepoURL: app.RepoURL, DefaultBranch: "dev",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := solo.Close(ctx, v.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := solo.Restart(ctx, v.ID, "alice"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	waitFor(t, "restart snapshot", func() bool {
		row, err := solo.Require(v.ID, "alice")
		return err == nil && row.Status == models.SoloStatusRunning
	})
	env = fg.LastCreateEnv()
	if env["GIT_REPOS"] != want || env["APP_ENV"] != "" {
		t.Fatalf("restart should keep the clone snapshot only, GIT_REPOS=%q APP_ENV=%q", env["GIT_REPOS"], env["APP_ENV"])
	}

	var n int64
	db.Model(&models.SoloSession{}).Where("id = ?", v.ID).Count(&n)
	if n != 1 {
		t.Fatalf("session missing after restart: %d", n)
	}
}

func TestSoloCreateRejectsConflicts(t *testing.T) {
	solo, _, _, db := newSoloFixture(t, 0, 4)
	ctx := context.Background()
	ps := solo.Projects()
	web, err := ps.Create(ProjectInput{
		Name: "webapp", RepoURL: "https://github.com/acme/webapp.git", RepoDirName: "webapp",
	}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	checkout, err := ps.Create(ProjectInput{
		Name: "checkout", RepoURL: "https://github.com/acme/checkout.git", RepoDirName: "webapp",
	}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	upper, err := ps.Create(ProjectInput{
		Name: "upper", RepoURL: "https://github.com/acme/same.git", RepoDirName: "WebApp",
	}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	lower, err := ps.Create(ProjectInput{
		Name: "lower", RepoURL: "https://github.com/acme/same.git", RepoDirName: "webapp",
	}, "alice")
	if err != nil {
		t.Fatal(err)
	}

	assertRejected := func(in SoloCreateInput, code string, parts ...string) {
		t.Helper()
		beforeS, beforeB := counts(t, db)
		_, err := solo.Create(ctx, "alice", in)
		var se *SoloError
		if !errors.As(err, &se) || se.Code != code {
			t.Fatalf("code %s: got %v", code, err)
		}
		for _, part := range parts {
			if !strings.Contains(se.Msg, part) {
				t.Fatalf("message %q missing %q", se.Msg, part)
			}
		}
		if names, ok := se.Params["repos"].([]string); ok {
			for _, part := range parts {
				if part == "webapp" || part == "SHARED" || part == "APP_ENV" {
					continue
				}
				found := false
				for _, name := range names {
					if name == part {
						found = true
					}
				}
				if !found && (part == "checkout" || part == "billing" || part == "one" || part == "two" || part == "three") {
					t.Fatalf("params repos %+v missing %s", names, part)
				}
			}
		}
		afterS, afterB := counts(t, db)
		if afterS != beforeS || afterB != beforeB {
			t.Fatalf("rejected create left session=%d sandbox=%d (was %d/%d)", afterS, afterB, beforeS, beforeB)
		}
	}

	assertRejected(SoloCreateInput{AgentName: "agentA", Repos: []SoloRepoChoice{{ProjectID: web.ID}, {ProjectID: checkout.ID}}},
		"repo_dir_conflict", "webapp", "checkout")
	assertRejected(SoloCreateInput{AgentName: "agentA", Repos: []SoloRepoChoice{{ProjectID: web.ID, Branch: "bad branch"}}},
		"branch_invalid_repo", "webapp")

	if _, err := solo.Create(ctx, "alice", SoloCreateInput{
		AgentName: "agentA",
		Repos:     []SoloRepoChoice{{ProjectID: upper.ID}, {ProjectID: lower.ID}},
	}); err != nil {
		t.Fatalf("directory names that differ only by case should start: %v", err)
	}
}

func TestSoloLegacySessionRestart(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, fg, db := newSoloFixture(t, port, 3)
	ctx := context.Background()
	p, err := solo.Projects().Create(ProjectInput{
		Name: "app", RepoURL: "https://github.com/acme/app.git", DefaultBranch: "main",
	}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	row := &models.SoloSession{
		ID: "old-session", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusClosed,
		ProjectID: p.ID, ProjectSnapshot: p, Branch: "release",
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := solo.Projects().Update(p.ID, ProjectInput{
		Name: "app", RepoURL: p.RepoURL, DefaultBranch: "dev",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := solo.Restart(ctx, row.ID, "alice"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	want := "app|https://github.com/acme/app.git|release"
	waitFor(t, "legacy clone", func() bool {
		return fg.LastCreateEnv()["GIT_REPOS"] == want && fg.LastCreateEnv()["APP_ENV"] == ""
	})
	got, err := solo.Get(ctx, row.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Repos) != 1 || got.Repos[0].Branch != "release" || got.Repos[0].Name != "app" {
		t.Fatalf("legacy read model: %+v", got.Repos)
	}
}

func TestSoloActiveSessionsCountsEverySnapshotRepo(t *testing.T) {
	db := newTestDB(t)
	ps := NewProjectService(db)
	running := []models.SessionRepo{
		{Position: 0, ProjectID: "first", Name: "first"},
		{Position: 1, ProjectID: "second", Name: "second"},
	}
	if err := db.Create(&models.SoloSession{
		ID: "multi", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusRunning, Repos: running,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SoloSession{
		ID: "legacy", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusStarting, ProjectID: "legacy",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SoloSession{
		ID: "closed", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusClosed,
		Repos: []models.SessionRepo{{Position: 0, ProjectID: "second", Name: "second"}},
	}).Error; err != nil {
		t.Fatal(err)
	}
	second, err := ps.ActiveSessions("second")
	if err != nil || len(second) != 1 || second[0].ID != "multi" {
		t.Fatalf("second: %+v %v", second, err)
	}
	legacy, err := ps.ActiveSessions("legacy")
	if err != nil || len(legacy) != 1 || legacy[0].ID != "legacy" {
		t.Fatalf("legacy: %+v %v", legacy, err)
	}
	if got, err := ps.ActiveSessions("missing"); err != nil || len(got) != 0 {
		t.Fatalf("missing: %+v %v", got, err)
	}
}

func TestSoloEmptyWorkspace(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, fg, _ := newSoloFixture(t, port, 3)
	v, err := solo.Create(context.Background(), "alice", SoloCreateInput{
		AgentName: "agentA", Repos: []SoloRepoChoice{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Repos) != 0 || v.ProjectID != "" {
		t.Fatalf("empty workspace: %+v", v.SoloSession)
	}
	waitFor(t, "empty GIT_REPOS", func() bool {
		env := fg.LastCreateEnv()
		return env != nil && env["GIT_REPOS"] == ""
	})
}

// imageChat records the prompt and attachments one Solo turn hands to the model.
type imageChat struct {
	mu     sync.Mutex
	text   string
	images []models.PromptImage
	calls  int
	done   chan struct{}
}

func (c *imageChat) ChatWithTimeout(_ context.Context, _ uint, text string, images []models.PromptImage, _ time.Duration, _ func(json.RawMessage)) (*models.TokenUsage, models.TokenUsageByModel, error) {
	c.mu.Lock()
	c.calls++
	c.text = text
	c.images = append([]models.PromptImage(nil), images...)
	c.mu.Unlock()
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	return &models.TokenUsage{}, nil, nil
}

func (c *imageChat) Cancel(uint) {}

// deadlineChat records the total limit one Solo turn hands to the chat layer.
type deadlineChat struct {
	timeout     time.Duration
	hasDeadline bool
	done        chan struct{}
}

func (c *deadlineChat) ChatWithTimeout(ctx context.Context, _ uint, _ string, _ []models.PromptImage, timeout time.Duration, _ func(json.RawMessage)) (*models.TokenUsage, models.TokenUsageByModel, error) {
	c.timeout = timeout
	_, c.hasDeadline = ctx.Deadline()
	close(c.done)
	return &models.TokenUsage{}, nil, nil
}

func (c *deadlineChat) Cancel(uint) {}

func TestSoloTurnHasNoTotalDeadline(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, _, _ := newSoloFixture(t, port, 3)
	v, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "sandbox ready", func() bool {
		row, err := solo.Require(v.ID, "alice")
		return err == nil && row.Status == models.SoloStatusRunning
	})
	chat := &deadlineChat{done: make(chan struct{})}
	solo.SetChatterForTest(chat)
	if _, err := solo.Send(v.ID, "alice", "a long coding task", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-chat.done:
	case <-time.After(8 * time.Second):
		t.Fatal("timed out waiting for the Solo turn")
	}
	if chat.hasDeadline || chat.timeout != NoTurnDeadline {
		t.Fatalf("Solo turn got a total limit: deadline=%v timeout=%s", chat.hasDeadline, chat.timeout)
	}
}

func TestSoloSendExternalizesImages(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, _, db := newSoloFixture(t, port, 3)
	ctx := context.Background()
	v, err := solo.Create(ctx, "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "sandbox ready", func() bool {
		row, err := solo.Require(v.ID, "alice")
		return err == nil && row.Status == models.SoloStatusRunning
	})

	chat := &imageChat{done: make(chan struct{})}
	solo.SetChatterForTest(chat)
	raw := []models.PromptImage{{Data: "aGVsbG8=", MimeType: "image/png", Name: "shot.png"}}
	if _, err := solo.Send(v.ID, "alice", "", raw); err == nil || !strings.Contains(err.Error(), "blob store not configured") {
		t.Fatalf("missing blob store: %v", err)
	}
	if chat.calls != 0 {
		t.Fatal("dropped attachment was still sent")
	}

	solo.SetBlobStore(blob.NewMemory())
	if _, err := solo.Send(v.ID, "alice", "", raw); err != nil {
		t.Fatal(err)
	}
	select {
	case <-chat.done:
	case <-time.After(8 * time.Second):
		t.Fatal("timed out waiting for the image turn")
	}

	chat.mu.Lock()
	gotText, gotImages := chat.text, chat.images
	chat.mu.Unlock()
	if gotText != "（见附件）" {
		t.Fatalf("prompt = %q", gotText)
	}
	if len(gotImages) != 1 || !strings.HasPrefix(gotImages[0].Ref, "blob:") || gotImages[0].Data != "" || gotImages[0].Name != "shot.png" || gotImages[0].MimeType != "image/png" {
		t.Fatalf("chat images = %+v", gotImages)
	}

	msgs, err := solo.Messages(v.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	var stored models.SoloMessage
	for _, m := range msgs {
		if m.Role == "user" {
			stored = m
		}
	}
	if stored.ID == "" || stored.Content != "" {
		t.Fatalf("user message = %+v", stored)
	}
	if len(stored.Images) != 1 || stored.Images[0].Ref != gotImages[0].Ref || stored.Images[0].Data != "" {
		t.Fatalf("stored images = %+v", stored.Images)
	}
	var row models.SoloMessage
	if err := db.First(&row, "id = ?", stored.ID).Error; err != nil {
		t.Fatal(err)
	}
	if len(row.Images) != 1 || row.Images[0].Data != "" || row.Images[0].Ref != gotImages[0].Ref {
		t.Fatalf("db images = %+v", row.Images)
	}
}

func counts(t *testing.T, db *gorm.DB) (sessions, sandboxes int64) {
	t.Helper()
	if err := db.Model(&models.SoloSession{}).Count(&sessions).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.Sandbox{}).Count(&sandboxes).Error; err != nil {
		t.Fatal(err)
	}
	return sessions, sandboxes
}
