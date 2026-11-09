package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/execution"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestLocalSoloUnknownStopKeepsQuotaThenConfirmedCloseRetainsRootAndReopens(t *testing.T) {
	backend, err := filepath.Abs(filepath.Join("..", "..", "..", "sandbox-gateway", "sandbox", "bin", "backend"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRASP_WORKTREE_BACKEND", backend)
	t.Setenv(execution.EnvMode, execution.ModeWorktree)
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	execution.ResetForTest()
	t.Cleanup(execution.ResetForTest)
	t.Cleanup(execution.SetLookPathForTest(func(string) error { return nil }))
	acp, port := fakeACPServer(t)
	defer acp.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	var starts, stops atomic.Int32
	t.Cleanup(sandbox.SetWorktreeBackendForTest(func(context.Context, map[string]string, string) (int, func(), error) {
		index := starts.Add(1)
		return port, func() {
			stops.Add(1)
			if index == 1 {
				close(entered)
				<-release
			}
		}, nil
	}))
	solo, service, _, db := newSoloFixture(t, port, 1)
	view, err := solo.Create(t.Context(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "local resident ready", func() bool { row, _ := solo.Require(view.ID, "alice"); return row.Status == models.SoloStatusRunning })
	parent, _ := solo.Require(view.ID, "alice")
	service.mu.Lock()
	live := service.live[parent.SandboxID]
	service.mu.Unlock()
	if live == nil || !live.sb.IsLocal() {
		t.Fatal("fixture did not run a local worktree")
	}
	original := live.sb
	marker := filepath.Join(original.WorkspaceDir, "uncommitted.txt")
	if err := os.WriteFile(marker, []byte("retain the only files"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	_, err = solo.StopEnvironment(ctx, parent.ID, "alice")
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("unknown explicit Stop was claimed successful", err)
	}
	if _, err := solo.ResumeEnvironment(t.Context(), parent.ID, "alice"); err == nil {
		t.Fatal("Resume woke local work before prior Stop was confirmed")
	} else {
		requireSoloFailure(t, err, "environment_stop_pending")
	}
	if starts.Load() != 1 {
		t.Fatal("unknown stop started another process", starts.Load())
	}
	ctx, cancel = context.WithTimeout(t.Context(), 200*time.Millisecond)
	_, err = solo.Close(ctx, parent.ID, "alice")
	cancel()
	requireSoloFailure(t, err, "environment_stop_pending")
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("original local stop was not attempted")
	}
	if count, err := countSoloCompute(db, 0); err != nil || count != 1 {
		t.Fatal("unknown local stop released quota", count, err)
	}
	if _, err := solo.Create(t.Context(), "alice", SoloCreateInput{AgentName: "agentA"}); err == nil {
		t.Fatal("unknown original launch did not retain its slot")
	}
	close(release)
	if _, err := solo.Close(t.Context(), parent.ID, "alice"); err != nil {
		t.Fatal("confirmed local stop remained stuck", err)
	}
	closed, _ := solo.Require(parent.ID, "alice")
	if closed.Status != models.SoloStatusClosed || closed.LifecycleAction != "" || closed.SandboxID != parent.SandboxID {
		t.Fatal("close forgot original retained resource", closed)
	}
	if count, err := countSoloCompute(db, 0); err != nil || count != 0 {
		t.Fatal("proved exited local launch kept quota", count, err)
	}
	if body, err := os.ReadFile(marker); err != nil || string(body) != "retain the only files" {
		t.Fatalf("Close deleted root: %q %v", body, err)
	}
	if _, err := solo.Restart(t.Context(), parent.ID, "alice"); err != nil {
		t.Fatal("same-process retained handle could not reopen", err)
	}
	reopened, _ := solo.Require(parent.ID, "alice")
	if reopened.Status != models.SoloStatusRunning || reopened.SandboxID != parent.SandboxID || starts.Load() != 2 || stops.Load() != 1 {
		t.Fatalf("reopen used another resource or skipped actual start: %+v starts=%d stops=%d", reopened, starts.Load(), stops.Load())
	}
	service.mu.Lock()
	actual := service.live[parent.SandboxID].sb
	service.mu.Unlock()
	if actual != original {
		t.Fatal("reopen replaced original local resource")
	}
	if body, err := os.ReadFile(marker); err != nil || string(body) != "retain the only files" {
		t.Fatalf("Reopen reset original root: %q %v", body, err)
	}
	if _, err := solo.Close(t.Context(), parent.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if count, err := countSoloCompute(db, 0); err != nil || count != 0 {
		t.Fatal("second confirmed Close leaked quota", count, err)
	}
	second, err := solo.Create(t.Context(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal("closed launch permanently consumed MaxSolo slot", err)
	}
	waitFor(t, "new local launch after slot freed", func() bool { row, _ := solo.Require(second.ID, "alice"); return row.Status == models.SoloStatusRunning })
	if _, err := solo.Close(t.Context(), second.ID, "alice"); err != nil {
		t.Fatal(err)
	}
}
