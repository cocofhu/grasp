package services

import (
	"context"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func bindTestAgentHome(t *testing.T, s *SandboxService, name, projectID string) {
	t.Helper()
	ag, ok := s.skills.Get(name)
	if !ok {
		t.Fatalf("agent %q missing", name)
	}
	ag.ProjectID = projectID
	if err := s.skills.Save(ag); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAgentSandboxValidationAndReuse(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	if _, _, err := s.OpenAgentSandbox(ctx, AgentSandboxOpenOpts{}); err == nil {
		t.Fatal("empty opts should fail")
	}
	if _, _, err := s.OpenAgentSandbox(ctx, AgentSandboxOpenOpts{Profile: "missing", ThreadID: "thr-1"}); err == nil {
		t.Fatal("missing agent should fail")
	}
	// Agent whose home project differs cannot open under a project id.
	if _, _, err := s.OpenAgentSandbox(ctx, AgentSandboxOpenOpts{
		Profile: "agentA", ProjectID: "p1", ThreadID: "thr-mismatch",
	}); err == nil {
		t.Fatal("agent of another home project should fail")
	}

	bindTestAgentHome(t, s, "agentA", "p1")
	if _, _, err := s.OpenAgentSandbox(ctx, AgentSandboxOpenOpts{
		Profile: "agentA", ProjectID: "wrong", ThreadID: "thr-mismatch",
	}); err == nil {
		t.Fatal("home project mismatch should fail")
	}

	threadID := "thr-reuse-1"
	row1, reused, err := s.OpenAgentSandbox(ctx, AgentSandboxOpenOpts{
		Profile: "agentA", ProjectID: "p1", ThreadID: threadID, RunIDPrefix: "agent",
	})
	if err != nil || reused {
		t.Fatalf("first open: row=%+v reused=%v err=%v", row1, reused, err)
	}
	ds.setStatus(row1.Name, "running")

	row2, reused2, err := s.OpenAgentSandbox(ctx, AgentSandboxOpenOpts{
		Profile: "agentA", ProjectID: "p1", ThreadID: threadID, Reuse: true, RunIDPrefix: "agent",
	})
	if err != nil || !reused2 || row2.ID != row1.ID {
		t.Fatalf("reuse: row2=%+v reused=%v err=%v", row2, reused2, err)
	}
}

func TestOpenAgentSandboxMaxLimit(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()
	bindTestAgentHome(t, s, "agentA", "p1")

	row1, _, err := s.OpenAgentSandbox(ctx, AgentSandboxOpenOpts{
		Profile: "agentA", ProjectID: "p1", ThreadID: "t1", RunIDPrefix: "agent",
	})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	ds.setStatus(row1.Name, "running")
	// Mark first as active in DB so count hits max=2 from newSandboxService.
	db.Model(&models.Sandbox{}).Where("id = ?", row1.ID).Update("status", "running")

	row2 := &models.Sandbox{Name: "sb-other", Purpose: SandboxPurposeAgent, ThreadID: "t0", Status: "running"}
	if err := db.Create(row2).Error; err != nil {
		t.Fatal(err)
	}
	_, _, err = s.OpenAgentSandbox(ctx, AgentSandboxOpenOpts{
		Profile: "agentA", ProjectID: "p1", ThreadID: "t2", RunIDPrefix: "agent",
	})
	if err == nil {
		t.Fatal("max sandboxes should reject new open")
	}
}

func TestAgentSandboxDestroyHook(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	called := false
	s.SetAgentSandboxDestroyHook(func(projectID, threadID, token string) {
		called = true
	})
	row := &models.Sandbox{
		Name: "grasp-sb-hook", Purpose: SandboxPurposeAgent, ThreadID: "thr-hook",
		Status: "running", ProjectID: "p1",
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	ds.setStatus(row.Name, "running")
	if err := s.Destroy(context.Background(), row.ID); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("destroy hook not invoked")
	}
}
