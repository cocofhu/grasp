package workspacebinding

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/workspacejob"
)

type testConsumer struct {
	name                 string
	calls                *atomic.Int32
	prepareErr, applyErr error
	apply                func()
}

func (c testConsumer) Name() string { return c.name }
func (c testConsumer) Prepare(context.Context, string, string) (Transition, error) {
	if c.prepareErr != nil {
		return nil, c.prepareErr
	}
	return testTransition{c}, nil
}

type testTransition struct{ c testConsumer }

func (t testTransition) Apply(context.Context) error {
	if t.c.calls != nil {
		t.c.calls.Add(1)
	}
	if t.c.apply != nil {
		t.c.apply()
	}
	return t.c.applyErr
}
func (testTransition) Verify(context.Context) error { return nil }
func (testTransition) Close() error                 { return nil }

type bindingFixture struct {
	manager   *Manager
	jobs      *workspacejob.Service
	cfg       Config
	jobCfg    workspacejob.Config
	restoreID string
	calls     atomic.Int32
}

func newFixture(t *testing.T) *bindingFixture {
	t.Helper()
	base := t.TempDir()
	f := &bindingFixture{restoreID: "restored-directory-one"}
	f.cfg = Config{ControlRoot: filepath.Join(base, "private-bindings"), OriginalRoot: filepath.Join(base, "original"), RestoreRoot: filepath.Join(base, "restores"), Resource: "resource-original", Lineage: "lineage-original", ResolveRestore: func(_ context.Context, id string) error {
		if id != "restored-directory-one" {
			return ErrConflict
		}
		return nil
	}}
	for _, name := range []string{f.cfg.OriginalRoot, f.cfg.RestoreRoot, filepath.Join(f.cfg.RestoreRoot, f.restoreID)} {
		if err := os.MkdirAll(name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for name, text := range map[string]string{filepath.Join(f.cfg.OriginalRoot, "marker"): "original dirty", filepath.Join(f.cfg.RestoreRoot, f.restoreID, "marker"): "restored dirty"} {
		if err := os.WriteFile(name, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var err error
	f.manager, err = New(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	f.install(t, nil)
	f.jobCfg = workspacejob.Config{Root: filepath.Join(base, "private-jobs"), Resource: f.cfg.Resource, Lineage: f.cfg.Lineage, MaxLease: 2 * time.Second, MaxDuration: 10 * time.Second, Executors: map[string]workspacejob.Executor{"activate_workspace": f.manager.Execute, "return_workspace": f.manager.Execute}}
	f.jobs, err = workspacejob.New(f.jobCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.jobs.Close(context.Background()); _ = f.manager.Close() })
	return f
}
func (f *bindingFixture) install(t *testing.T, mutate func(*testConsumer)) {
	t.Helper()
	consumers := make([]Consumer, 0, len(nativeConsumers))
	for _, name := range nativeConsumers {
		c := testConsumer{name: name, calls: &f.calls}
		if mutate != nil {
			mutate(&c)
		}
		consumers = append(consumers, c)
	}
	if err := f.manager.Install(context.Background(), consumers...); err != nil {
		t.Fatal(err)
	}
}
func (f *bindingFixture) request(t *testing.T, id, kind string, generation uint64, target Selector) workspacejob.CreateRequest {
	t.Helper()
	view, err := f.manager.Head()
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(Spec{Version: Version, Expected: view.Head.Reference, BindingID: "binding-" + id, Target: target, ReturnSnapshotID: "return-snapshot-ready", Authorization: strings.Repeat("a", 64)})
	spec, hash, err := workspacejob.Fingerprint(workspacejob.Spec{ID: id, Kind: kind, Resource: f.cfg.Resource, Lineage: f.cfg.Lineage, Nonce: id, Deadline: time.Now().Add(5 * time.Second), Body: body})
	if err != nil {
		t.Fatal(err)
	}
	return workspacejob.CreateRequest{Spec: spec, SHA256: hash, Fence: workspacejob.Fence{Owner: "owner-original", Generation: generation, ExpiresAt: time.Now().Add(time.Second)}}
}
func (f *bindingFixture) run(t *testing.T, req workspacejob.CreateRequest) workspacejob.Receipt {
	t.Helper()
	if _, err := f.jobs.Create(req); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		row, err := f.jobs.Get(req.Spec.ID)
		if err != nil {
			t.Fatal(err)
		}
		if row.State == "completed" || row.State == "failed" || row.State == "unknown" {
			return row
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("native binding job did not finish")
	return workspacejob.Receipt{}
}
func TestWorkspaceBindingActivateReturnRetainsBothDirectoriesAndOriginalReceipt(t *testing.T) {
	f := newFixture(t)
	original, _ := f.manager.Head()
	req := f.request(t, "operation-activate", "activate_workspace", 1, Selector{Kind: "restore", ID: f.restoreID})
	row := f.run(t, req)
	if row.State != "completed" {
		t.Fatal(row)
	}
	active, err := f.manager.Head()
	if err != nil || !active.Ready || active.Path != filepath.Join(f.cfg.RestoreRoot, f.restoreID) || active.Head.Reference.Revision != 2 {
		t.Fatal(active, err)
	}
	if _, _, err := f.manager.Acquire(&original.Head.Reference, true); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale capture admitted: %v", err)
	}
	if _, _, err := f.manager.Acquire(nil, true); !errors.Is(err, ErrConflict) {
		t.Fatalf("unbound capture admitted after switch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(active.Path, "later"), []byte("later edit"), 0600); err != nil {
		t.Fatal(err)
	}
	duplicate, err := f.jobs.Create(req)
	if err != nil || duplicate.State != "completed" || f.calls.Load() != 3 {
		t.Fatal("replayed native switch", duplicate, err, f.calls.Load())
	}
	changed := req
	var spec Spec
	_ = json.Unmarshal(changed.Spec.Body, &spec)
	spec.ReturnSnapshotID = "different-return-snapshot"
	changed.Spec.Body, _ = json.Marshal(spec)
	changed.Spec, changed.SHA256, _ = workspacejob.Fingerprint(changed.Spec)
	if _, err := f.jobs.Create(changed); !errors.Is(err, workspacejob.ErrConflict) {
		t.Fatalf("changed original request accepted: %v", err)
	}
	back := f.request(t, "operation-return", "return_workspace", 2, Selector{Kind: "binding", ID: original.Head.Reference.ID})
	if row := f.run(t, back); row.State != "completed" {
		t.Fatal(row)
	}
	returned, _ := f.manager.Head()
	if returned.Path != f.cfg.OriginalRoot || returned.Head.Reference.Revision != 3 {
		t.Fatal(returned)
	}
	for path, want := range map[string]string{filepath.Join(f.cfg.OriginalRoot, "marker"): "original dirty", filepath.Join(active.Path, "marker"): "restored dirty", filepath.Join(active.Path, "later"): "later edit"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatal("switch changed retained files", path, string(got), err)
		}
	}
	op, err := f.manager.Operation(req.Spec.ID)
	if err != nil || op.State != "committed" || op.Result.Active.Reference != active.Head.Reference || op.Identity.SHA256 != req.SHA256 || op.Result.ProcessMemory {
		t.Fatal(op, err)
	}
}
func TestWorkspaceBindingReadPermitAndBusyConsumerRejectBeforeDurableIntent(t *testing.T) {
	f := newFixture(t)
	_, release, err := f.manager.Acquire(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	req := f.request(t, "operation-busy-read", "activate_workspace", 1, Selector{Kind: "restore", ID: f.restoreID})
	if row := f.run(t, req); row.State != "failed" {
		t.Fatal(row)
	}
	if _, err := f.manager.Operation(req.Spec.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("busy read persisted switch intent", err)
	}
	release()
	f.manager.mu.Lock()
	f.manager.consumers[0] = testConsumer{name: "agent", prepareErr: ErrBusy}
	f.manager.mu.Unlock()
	req = f.request(t, "operation-busy-agent", "activate_workspace", 2, Selector{Kind: "restore", ID: f.restoreID})
	if row := f.run(t, req); row.State != "failed" {
		t.Fatal(row)
	}
	view, _ := f.manager.Head()
	if !view.Ready || view.Head.Reference.Revision != 1 || view.PendingOperationID != "" {
		t.Fatal(view)
	}
}
func TestWorkspaceBindingFailedApplySurvivesRestartAsUnknownAndNeverReplays(t *testing.T) {
	f := newFixture(t)
	f.manager.mu.Lock()
	f.manager.consumers[1] = testConsumer{name: "capture", calls: &f.calls, applyErr: errors.New("consumer failed")}
	f.manager.mu.Unlock()
	req := f.request(t, "operation-unknown", "activate_workspace", 1, Selector{Kind: "restore", ID: f.restoreID})
	if row := f.run(t, req); row.State != "failed" {
		t.Fatal(row)
	}
	view, _ := f.manager.Head()
	if view.Ready || view.Path != "" || view.PendingOperationID != req.Spec.ID {
		t.Fatal(view)
	}
	if _, _, err := f.manager.Acquire(nil, false); !errors.Is(err, ErrUnknown) {
		t.Fatal(err)
	}
	if err := f.jobs.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.manager.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	f.manager, err = New(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	f.install(t, nil)
	f.jobCfg.Executors = map[string]workspacejob.Executor{"activate_workspace": f.manager.Execute, "return_workspace": f.manager.Execute}
	f.jobs, err = workspacejob.New(f.jobCfg)
	if err != nil {
		t.Fatal(err)
	}
	op, err := f.manager.Operation(req.Spec.ID)
	if err != nil || op.State != "unknown" || op.Result != nil {
		t.Fatal(op, err)
	}
	before := f.calls.Load()
	if _, err := f.jobs.Create(req); err != nil {
		t.Fatal(err)
	}
	if f.calls.Load() != before {
		t.Fatal("unknown operation replayed")
	}
	view, _ = f.manager.Head()
	if view.Ready || view.PendingOperationID != req.Spec.ID {
		t.Fatal(view)
	}
}
func TestWorkspaceBindingLateConsumerCannotCommitAfterNativeDeadline(t *testing.T) {
	f := newFixture(t)
	f.manager.mu.Lock()
	f.manager.consumers[0] = testConsumer{name: "agent", apply: func() { time.Sleep(100 * time.Millisecond) }}
	f.manager.mu.Unlock()
	req := f.request(t, "operation-expired", "activate_workspace", 1, Selector{Kind: "restore", ID: f.restoreID})
	req.Fence.ExpiresAt = time.Now().Add(30 * time.Millisecond)
	if row := f.run(t, req); row.State != "unknown" {
		t.Fatal(row)
	}
	// Get may observe the independent expiry before the bounded executor has
	// joined. Close waits for it before reopening the persistent state.
	if err := f.jobs.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	op, err := f.manager.Operation(req.Spec.ID)
	if err != nil || op.State != "unknown" {
		t.Fatal("expired owner committed", op, err)
	}
	view, _ := f.manager.Head()
	if view.Ready || view.Head.Reference.Revision != 1 {
		t.Fatal(view)
	}
}
func TestWorkspaceBindingCommittedHeadAndReturnSurviveProcessRestart(t *testing.T) {
	f := newFixture(t)
	req := f.request(t, "operation-persistent", "activate_workspace", 1, Selector{Kind: "restore", ID: f.restoreID})
	if row := f.run(t, req); row.State != "completed" {
		t.Fatal(row)
	}
	active, _ := f.manager.Head()
	if err := f.jobs.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.manager.Close(); err != nil {
		t.Fatal(err)
	}
	root, err := StartupOriginalRoot(f.cfg.ControlRoot, f.cfg.RestoreRoot, active.Path, f.cfg.Resource, f.cfg.Lineage)
	if err != nil || root != f.cfg.OriginalRoot {
		t.Fatal(root, err)
	}
	f.manager, err = New(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	f.install(t, nil)
	reopened, _ := f.manager.Head()
	if reopened != active {
		t.Fatal("canonical head changed at process restart", reopened, active)
	}
	op, err := f.manager.Operation(req.Spec.ID)
	if err != nil || op.State != "committed" || op.Result.ReturnSnapshotID != "return-snapshot-ready" || op.Result.Previous.Reference.Revision != 1 {
		t.Fatal(op, err)
	}
	if _, err := StartupOriginalRoot(f.cfg.ControlRoot, f.cfg.RestoreRoot, t.TempDir(), f.cfg.Resource, f.cfg.Lineage); !errors.Is(err, ErrConflict) {
		t.Fatal("unrelated startup root rebound history", err)
	}
}
func TestWorkspaceBindingRequiresCompleteConsumersAndConfinedRestoredTarget(t *testing.T) {
	f := newFixture(t)
	f.manager.mu.Lock()
	f.manager.installed = false
	f.manager.consumers = nil
	f.manager.mu.Unlock()
	if err := f.manager.Install(context.Background(), testConsumer{name: "agent"}); !errors.Is(err, ErrUninstalled) {
		t.Fatal(err)
	}
	if f.manager.Capabilities().Activate {
		t.Fatal("partial consumers advertised activation")
	}
	f.install(t, nil)
	if err := os.RemoveAll(filepath.Join(f.cfg.RestoreRoot, f.restoreID)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.cfg.OriginalRoot, filepath.Join(f.cfg.RestoreRoot, f.restoreID)); err != nil {
		t.Fatal(err)
	}
	req := f.request(t, "operation-symlink", "activate_workspace", 1, Selector{Kind: "restore", ID: f.restoreID})
	if row := f.run(t, req); row.State != "failed" {
		t.Fatal(row)
	}
	if _, err := f.manager.Operation(req.Spec.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unsafe target persisted intent", err)
	}
}
