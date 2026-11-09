package sandbox

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/execution"
	"golang.org/x/sys/unix"
)

func TestWorktreeStopConfirmsOwnSessionAndRetainsRoot(t *testing.T) {
	t.Setenv(execution.EnvMode, execution.ModeWorktree)
	t.Setenv("GRASP_WORKTREE_ROOT", t.TempDir())
	execution.ResetForTest()
	t.Cleanup(execution.ResetForTest)
	t.Cleanup(execution.SetLookPathForTest(func(string) error { return nil }))
	backend := filepath.Join(t.TempDir(), "bin", "backend")
	if err := os.MkdirAll(filepath.Dir(backend), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(filepath.Dir(filepath.Dir(backend)), "share/backend/web"), 0700); err != nil {
		t.Fatal(err)
	}
	useWorktreeGitHelper(t)
	helper, err := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(os.Getenv("GRASP_WORKTREE_BACKEND"))), "scripts/git-credentials.py"))
	if err != nil {
		t.Fatal(err)
	}
	helperDir := filepath.Join(filepath.Dir(filepath.Dir(backend)), "scripts")
	if err := os.MkdirAll(helperDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(helperDir, "git-credentials.py"), helper, 0600); err != nil {
		t.Fatal(err)
	}
	// Python exits after spawning a new process group. Its child is reparented
	// while remaining in our private session; a backend-group kill misses it.
	// Publish readiness only after the complete PID is written and closed.
	script := "#!/bin/sh\npython3 -c 'import os,subprocess; p=subprocess.Popen([\"sleep\",\"30\"],preexec_fn=os.setpgrp); f=open(\"independent.pid.tmp\",\"w\"); f.write(str(p.pid)); f.close(); os.replace(\"independent.pid.tmp\",\"independent.pid\")'\nwhile :; do sleep 30; done\n"
	if err := os.WriteFile(backend, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRASP_WORKTREE_BACKEND", backend)
	manager := NewManager(nil, ManagerOptions{})
	sb, err := manager.Create(t.Context(), Spec{Name: "positive-local-stop", Env: map[string]string{BridgePasswordEnv: "synthetic", envLaunchKind: execution.LaunchSolo}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, confirmed := sb.LocalStopState(); confirmed {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := sb.StopLocal(ctx); err != nil {
			t.Error("owned native fixture did not stop", err)
		}
	})
	marker := filepath.Join(sb.WorkspaceDir, "uncommitted.txt")
	if err := os.WriteFile(marker, []byte("only original files"), 0600); err != nil {
		t.Fatal(err)
	}
	// Same cwd does not make an independently started host process ours.
	unrelated := exec.Command("sleep", "30")
	unrelated.Dir = sb.WorkspaceDir
	if err := unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unrelated.Process.Kill(); _ = unrelated.Wait() })
	var child int
	for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
		if body, err := os.ReadFile(filepath.Join(sb.WorkspaceDir, "independent.pid")); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(body))); err == nil && pid > 0 {
				child = pid
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if child == 0 {
		t.Fatal("independent process group did not start")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := sb.StopLocal(ctx); err != nil {
		t.Fatal("local session stop lacked proof", err)
	}
	if err := unrelated.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("Stop killed an unrelated process sharing cwd", err)
	}
	if requested, confirmed := sb.LocalStopState(); !requested || !confirmed {
		t.Fatal("confirmed stop not retained on original handle")
	}
	if current, err := localProcessStat(child); err == nil && current.state != "Z" && current.state != "X" {
		t.Fatal("independent descendant still executes", current)
	}
	if body, err := os.ReadFile(marker); err != nil || string(body) != "only original files" {
		t.Fatalf("Stop removed original root: %q %v", body, err)
	}
	if err := sb.StopLocal(t.Context()); err != nil {
		t.Fatal("original stop receipt was not idempotent", err)
	}
	sb.Destroy(t.Context()) // Explicit final disposal retains its separate contract.
}

func TestWorktreeStopTimeoutKeepsOriginalHandleAndRetriesOneStop(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	local := &localExec{backendStop: callbackBackendStop(func() { calls.Add(1); close(entered); <-release })}
	sb := &Sandbox{local: local}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := sb.StopLocal(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	<-entered
	if requested, confirmed := sb.LocalStopState(); !requested || confirmed {
		t.Fatal("unknown stop was declared confirmed")
	}
	close(release)
	if err := sb.StopLocal(t.Context()); err != nil || calls.Load() != 1 {
		t.Fatal("retry replaced/repeated original stop", calls.Load(), err)
	}
}

func TestLocalStopKnownDetachedWriterRemainsPendingUntilExplicitlyStopped(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("python3", "-c", `import os,subprocess,time
p=subprocess.Popen(["sleep","30"],start_new_session=True)
with open("detached.pid.tmp","w") as ready:
    ready.write(str(p.pid))
os.replace("detached.pid.tmp","detached.pid")
time.sleep(30)`)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	stop := newLocalProcessStop(cmd.Process.Pid, done)
	root, err := localProcessStat(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	root, err = openLocalProcess(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.PidfdSendSignal(root.fd, unix.SIGKILL, nil, 0); _ = unix.Close(root.fd) })
	var child int
	for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
		if body, err := os.ReadFile(filepath.Join(dir, "detached.pid")); err == nil {
			if pid, err := strconv.Atoi(string(body)); err == nil && pid > 0 {
				child = pid
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if child == 0 {
		t.Fatal("detached writer did not start")
	}
	detached, err := localProcessStat(child)
	if err != nil {
		t.Fatal(err)
	}
	detached, err = openLocalProcess(detached)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.PidfdSendSignal(detached.fd, unix.SIGKILL, nil, 0); _ = unix.Close(detached.fd) })
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := stop(ctx); !errors.Is(err, ErrLocalStopUnconfirmed) {
		t.Fatal("known detached writer was called stopped", err)
	}
	if localProcessExited(detached) {
		t.Fatal("stop expanded its authority to a detached session")
	}
	// Only this fixture's explicitly held pidfd is used for cleanup. Once that
	// external writer is actually gone, the original stop receipt can reconcile.
	if err := unix.PidfdSendSignal(detached.fd, unix.SIGKILL, nil, 0); err != nil {
		t.Fatal(err)
	}
	for !localProcessExited(detached) {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	if err := stop(ctx); err != nil {
		t.Fatal("original receipt did not reconcile", err)
	}
}
