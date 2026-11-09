package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"backend/internal/workspacebinding"
	"backend/internal/workspacejob"
	"backend/internal/workspacesnapshot"
)

type rootedBindingSession struct {
	stubSess
	root   string
	closed atomic.Int32
}

func (s *rootedBindingSession) CWD() string    { return s.root }
func (s *rootedBindingSession) FSRoot() string { return s.root }
func (s *rootedBindingSession) Close() error   { s.closed.Add(1); return nil }
func chatBindingFixture(t *testing.T) (*ChatManager, *workspacebinding.Manager) {
	t.Helper()
	base := t.TempDir()
	original, restores := filepath.Join(base, "original"), filepath.Join(base, "restores")
	for _, root := range []string{original, restores} {
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
	}
	controller, err := workspacesnapshot.NewController(workspacesnapshot.ControllerConfig{Engine: workspacesnapshot.Config{Root: original}, Jobs: workspacejob.Config{Root: filepath.Join(base, "jobs"), Resource: "resource-original", Lineage: "lineage-original"}, RestoreRoot: restores, Binding: &workspacebinding.Config{ControlRoot: filepath.Join(base, "control"), OriginalRoot: original, RestoreRoot: restores, Resource: "resource-original", Lineage: "lineage-original"}})
	if err != nil {
		t.Fatal(err)
	}
	bindings := controller.Binding
	t.Cleanup(func() { _ = controller.Close(context.Background()) })
	chats := NewChatManager()
	if err := chats.SetWorkspaceBinding(bindings); err != nil {
		t.Fatal(err)
	}
	if err := bindings.Install(t.Context(), chats.WorkspaceConsumer(), controller.RootConsumer("capture"), controller.RootConsumer("code")); err != nil {
		t.Fatal(err)
	}
	return chats, bindings
}
func TestCanonicalChatRootIgnoresBrowserPathsAndRejectsStaleSession(t *testing.T) {
	chats, bindings := chatBindingFixture(t)
	bridge := chats.Default()
	head, err := bindings.Head()
	if err != nil {
		t.Fatal(err)
	}
	cwd, fsRoot, release, err := bridge.canonicalWorkspace("/browser/arbitrary", "/browser/root")
	if err != nil {
		t.Fatal(err)
	}
	release()
	if cwd != head.Path || fsRoot != head.Path {
		t.Fatal("browser path changed workspace", cwd, fsRoot)
	}
	stale := &rootedBindingSession{root: "/old-root"}
	attach(bridge, stale)
	if _, err := bridge.EnsureAgent("/old-root", "/old-root", nil, nil); !errors.Is(err, ErrWorkspaceChanged) {
		t.Fatal("old tool session reused", err)
	}
	active := &rootedBindingSession{root: head.Path}
	attach(bridge, active)
	if got, err := bridge.EnsureAgent("/browser/arbitrary", "/browser/root", nil, nil); err != nil || got != active {
		t.Fatal(got, err)
	}
}
func TestCanonicalChatSwitchRequiresIdleQueueAndClosesOldSessionOnlyAfterPrepare(t *testing.T) {
	chats, bindings := chatBindingFixture(t)
	head, _ := bindings.Head()
	bridge := chats.Default()
	old := &rootedBindingSession{root: head.Path}
	attach(bridge, old)
	consumer := chats.WorkspaceConsumer()
	bridge.promptQueue = []queuedPrompt{{Text: "accepted user message", OpID: "queued-operation"}}
	if _, err := consumer.Prepare(t.Context(), head.Path, "/next-root"); !errors.Is(err, workspacebinding.ErrBusy) {
		t.Fatal("queued turn lost at switch", err)
	}
	if old.closed.Load() != 0 {
		t.Fatal("prepare mutated active session")
	}
	bridge.promptQueue = nil
	bridge.activeTurn = &promptTurn{opID: "running-operation"}
	if _, err := consumer.Prepare(t.Context(), head.Path, "/next-root"); !errors.Is(err, workspacebinding.ErrBusy) {
		t.Fatal("running turn lost at switch", err)
	}
	bridge.activeTurn = nil
	transition, err := consumer.Prepare(t.Context(), head.Path, "/next-root")
	if err != nil {
		t.Fatal(err)
	}
	if old.closed.Load() != 0 {
		t.Fatal("idle prepare closed session before durable intent")
	}
	if err := transition.Apply(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := transition.Verify(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := transition.Close(); err != nil {
		t.Fatal(err)
	}
	if old.closed.Load() != 1 || bridge.sess != nil {
		t.Fatal("old tool session survived committed consumer switch", old.closed.Load())
	}
	// The admission mutexes are released even after a rejected/finished switch.
	if !bridge.ensureMu.TryLock() {
		t.Fatal("ensure lock leaked")
	}
	bridge.ensureMu.Unlock()
	if !bridge.enqueueMu.TryLock() {
		t.Fatal("enqueue lock leaked")
	}
	bridge.enqueueMu.Unlock()
}
func TestWorkspaceStandbyCannotOpenAgentEvenWithBrowserPaths(t *testing.T) {
	chats := NewChatManager()
	chats.BlockWorkspaceStartup()
	if _, err := chats.Default().EnsureAgent("/user", "/user", nil, nil); !errors.Is(err, workspacebinding.ErrUninstalled) {
		t.Fatal("standby started a tool", err)
	}
}
