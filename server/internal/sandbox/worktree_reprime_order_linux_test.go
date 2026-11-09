package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/execution"
	"golang.org/x/sys/unix"
)

func reprimeOrderFixture(t *testing.T) (*Manager, Spec) {
	t.Helper()
	useWorktreeGitHelper(t)
	t.Setenv(execution.EnvMode, execution.ModeWorktree)
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	execution.ResetForTest()
	t.Cleanup(execution.ResetForTest)
	t.Cleanup(execution.SetLookPathForTest(func(string) error { return nil }))
	return NewManager(nil, ManagerOptions{}), Spec{Name: "same-journal-owner", ConfigHome: t.TempDir(),
		Env: map[string]string{BridgePasswordEnv: "synthetic", "AGENT_PROVIDER": "codex", envRunID: "same-journal-owner", envLaunchKind: execution.LaunchSolo}}
}

func TestWorktreeReprimeReleasesOriginalJournalLockBeforeStartingReplacement(t *testing.T) {
	m, spec := reprimeOrderFixture(t)
	var starts atomic.Int32
	t.Cleanup(SetWorktreeBackendForTest(func(_ context.Context, _ map[string]string, workspace string) (int, func(), error) {
		// Use a real exclusive kernel lock, as the retained native journals do.
		// A replacement dispatched before old stop fails here deterministically.
		fd, err := os.OpenFile(filepath.Join(workspace, "native-journal.lock"), os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return 0, nil, err
		}
		if err := unix.Flock(int(fd.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
			_ = fd.Close()
			return 0, nil, errors.New("workspace operation journal is already open")
		}
		index := starts.Add(1)
		return 17000 + int(index), func() { _ = fd.Close() }, nil
	}))
	sb, err := m.Create(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sb.Destroy(context.Background()) })
	root, identity := sb.WorkspaceDir, sb.ID
	if err := os.WriteFile(filepath.Join(root, "dirty.bin"), []byte{0, 255, 1}, 0600); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := m.Reprime(t.Context(), sb, spec, nil); err != nil {
			t.Fatal("replacement overlapped the original journal owner", err)
		}
	}
	if starts.Load() != 4 || sb.WorkspaceDir != root || sb.ID != identity {
		t.Fatal("replacement recreated the retained workspace", starts.Load())
	}
	if body, err := os.ReadFile(filepath.Join(root, "dirty.bin")); err != nil || string(body) != string([]byte{0, 255, 1}) {
		t.Fatal("replacement changed retained files", err)
	}
}

func TestWorktreeReprimeUnknownOldStopDoesNotDispatchReplacement(t *testing.T) {
	m, spec := reprimeOrderFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var starts, oldStops atomic.Int32
	t.Cleanup(SetWorktreeBackendForTest(func(context.Context, map[string]string, string) (int, func(), error) {
		index := starts.Add(1)
		if index == 1 {
			return 17101, func() { oldStops.Add(1); close(entered); <-release }, nil
		}
		return 17100 + int(index), func() {}, nil
	}))
	sb, err := m.Create(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sb.Destroy(context.Background()) })
	var releaseOnce sync.Once
	releaseStop := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseStop)
	root := sb.WorkspaceDir
	if err := os.WriteFile(filepath.Join(root, "progress"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- m.Reprime(ctx, sb, spec, nil) }()
	<-entered
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) || !errors.Is(err, ErrLocalStopUnconfirmed) {
		t.Fatal("unknown stop was accepted", err)
	}
	if starts.Load() != 1 || sb.Port != 17101 || sb.WorkspaceDir != root {
		t.Fatal("unknown stop dispatched or rebound a replacement")
	}
	if _, confirmed := sb.LocalStopState(); confirmed {
		t.Fatal("unconfirmed process exit was marked stopped")
	}
	releaseStop()
	if err := m.Reprime(t.Context(), sb, spec, nil); err != nil || starts.Load() != 2 || oldStops.Load() != 1 {
		t.Fatal("retry did not reconcile the original stop receipt", err, starts.Load(), oldStops.Load())
	}
	if body, err := os.ReadFile(filepath.Join(root, "progress")); err != nil || string(body) != "keep" {
		t.Fatal("unknown stop discarded files", err)
	}
}

func TestWorktreeReprimeWaitingForTransitionHonorsDeadline(t *testing.T) {
	m, spec := reprimeOrderFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var starts atomic.Int32
	t.Cleanup(SetWorktreeBackendForTest(func(context.Context, map[string]string, string) (int, func(), error) {
		index := starts.Add(1)
		if index == 1 {
			return 17201, func() { close(entered); <-release }, nil
		}
		return 17200 + int(index), func() {}, nil
	}))
	sb, err := m.Create(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sb.Destroy(context.Background()) })
	var releaseOnce sync.Once
	releaseStop := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseStop)
	done := make(chan error, 1)
	go func() { done <- m.Reprime(t.Context(), sb, spec, nil) }()
	<-entered
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := m.Reprime(ctx, sb, spec, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("second replacement bypassed the owned transition", err)
	}
	if starts.Load() != 1 {
		t.Fatal("second replacement started while original stop was pending")
	}
	releaseStop()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 2 {
		t.Fatal("original transition did not dispatch exactly one replacement")
	}
}
