package services

import (
	"context"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

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
	threadID := "thr-reuse-1"
	// SharedToken keeps the async start from failing immediately on a missing
	// ACP_BRIDGE_PASSWORD and flipping the row to error before reuse is checked.
	const token = "sandbox-token"
	row1, reused, err := s.OpenAgentSandbox(ctx, AgentSandboxOpenOpts{
		Profile: "agentA", ThreadID: threadID, RunIDPrefix: "agent", SharedToken: token,
	})
	if err != nil || reused {
		t.Fatalf("first open: row=%+v reused=%v err=%v", row1, reused, err)
	}
	ds.setStatus(row1.Name, "running")

	row2, reused2, err := s.OpenAgentSandbox(ctx, AgentSandboxOpenOpts{
		Profile: "agentA", ThreadID: threadID, Reuse: true, RunIDPrefix: "agent", SharedToken: token,
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

	row1, _, err := s.OpenAgentSandbox(ctx, AgentSandboxOpenOpts{
		Profile: "agentA", ThreadID: "t1", RunIDPrefix: "agent",
	})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	ds.setStatus(row1.Name, "running")
	// Mark first as active in DB so count hits max=2 from newSandboxService.
	db.Model(&models.Sandbox{}).Where("id = ?", row1.ID).Update("status", "running")

	// row1's container start runs in a goroutine that may still rewrite its
	// status, so seed enough independent rows to reach max=2 on their own.
	for _, th := range []string{"t0", "t00"} {
		other := &models.Sandbox{Name: "sb-other-" + th, Purpose: SandboxPurposeAgent, ThreadID: th, Status: "running"}
		if err := db.Create(other).Error; err != nil {
			t.Fatal(err)
		}
	}
	_, _, err = s.OpenAgentSandbox(ctx, AgentSandboxOpenOpts{
		Profile: "agentA", ThreadID: "t2", RunIDPrefix: "agent",
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
	s.SetAgentSandboxDestroyHook(func(threadID, token string) {
		called = true
	})
	row := &models.Sandbox{
		Name: "grasp-sb-hook", Purpose: SandboxPurposeAgent, ThreadID: "thr-hook",
		Status: "running",
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
