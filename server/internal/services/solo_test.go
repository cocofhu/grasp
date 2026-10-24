package services

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
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
		{"sensitive env", ProjectInput{Name: "a", RepoURL: "https://x/y.git", Env: map[string]string{"NPM_TOKEN": "x"}}, "env_key_sensitive"},
		{"secret preset", ProjectInput{Name: "a", RepoURL: "https://x/y.git", Env: map[string]string{"GITHUB_TOKEN": "x"}}, "env_key_sensitive"},
		{"reserved env", ProjectInput{Name: "a", RepoURL: "https://x/y.git", Env: map[string]string{"GIT_REPOS": "x"}}, "env_key_reserved"},
		{"bad env name", ProjectInput{Name: "a", RepoURL: "https://x/y.git", Env: map[string]string{"1X": "x"}}, "env_key_invalid"},
		{"bad git type", ProjectInput{Name: "a", RepoURL: "https://x/y.git", GitCredentialType: "svn"}, "git_type_invalid"},
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
		Name: " demo ", RepoURL: "git@github.com:acme/demo.git", GitCredentialType: "github_https",
		GitCredentialID: "c1", SshHostsCredentialID: "dropped", Env: map[string]string{"APP_ENV": "dev"},
	}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "demo" || p.RepoDirName != "demo" || p.SshHostsCredentialID != "" || p.Env["APP_ENV"] != "dev" {
		t.Fatalf("normalized project: %+v", p)
	}
	if _, err := ps.Create(ProjectInput{Name: "demo", RepoURL: "https://x/y.git"}, "bob"); err == nil {
		t.Fatal("duplicate name should be rejected")
	}
	upd, err := ps.Update(p.ID, ProjectInput{Name: "demo", RepoURL: "https://github.com/acme/demo.git"})
	if err != nil || upd.GitCredentialType != "" || upd.GitCredentialID != "" {
		t.Fatalf("update without access clears repo creds: %+v %v", upd, err)
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
		GitCredentialType: "github_https", GitCredentialID: "cred-proj",
		Env: map[string]string{"APP_ENV": "dev"},
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
	if env["APP_ENV"] != "dev" || env["AGENT_FLAG"] != "1" || env["GITHUB_TOKEN"] != "" {
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
	if src["APP_ENV"] != EnvSourceProject || src["AGENT_FLAG"] != EnvSourceAgent || src["GITHUB_TOKEN"] != "" || src["GIT_REPOS"] != EnvSourceSystem {
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
