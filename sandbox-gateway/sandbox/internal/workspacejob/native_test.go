package workspacejob

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func nativeChildAlive(pid int) bool {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	after := strings.LastIndex(string(data), ") ")
	return after < 0 || !strings.HasPrefix(string(data)[after+2:], "Z ")
}

func TestWorkspaceJobProcessCrashHelper(t *testing.T) {
	root := os.Getenv("GRASP_WORKSPACE_JOB_CRASH_ROOT")
	if root == "" {
		t.Skip("subprocess-only runtime crash helper")
	}
	s, err := New(Config{Root: root, Resource: "resource-one", Lineage: "lineage-original", MaxLease: 2 * time.Second, MaxDuration: 10 * time.Second, Executors: map[string]Executor{
		"control_probe": func(_ context.Context, h *Handle, _ json.RawMessage) (json.RawMessage, error) {
			return nil, h.Command(root, nil, nil, "/bin/sh", "-c", "sleep 20 & echo $! > child.pid; wait")
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(jobRequest(t, "job-original", 93, 1500*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	select {} // the parent deliberately SIGKILLs this process without Close
}

func TestWorkspaceJobRuntimeCrashKillsDescendantsAndRestartNeverReplays(t *testing.T) {
	root := t.TempDir()
	child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestWorkspaceJobProcessCrashHelper$")
	child.Env = append(os.Environ(), "GRASP_WORKSPACE_JOB_CRASH_ROOT="+root)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill() }()
	var pid int
	for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
		data, err := os.ReadFile(filepath.Join(root, "child.pid"))
		if err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if pid == 0 || !nativeChildAlive(pid) {
		_ = child.Process.Kill()
		_ = child.Wait()
		t.Fatal("native subprocess did not start")
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	for until := time.Now().Add(time.Second); nativeChildAlive(pid) && time.Now().Before(until); {
		time.Sleep(5 * time.Millisecond)
	}
	if nativeChildAlive(pid) {
		t.Fatal("runtime SIGKILL left native descendants running")
	}
	s, err := New(Config{Root: root, Resource: "resource-one", Lineage: "lineage-original", Executors: map[string]Executor{
		"control_probe": func(context.Context, *Handle, json.RawMessage) (json.RawMessage, error) {
			t.Error("runtime restart replayed a native process")
			return nil, nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close(context.Background()) }()
	if receipt, err := s.Get("job-original"); err != nil || receipt.State != "unknown" || receipt.Generation != 93 {
		t.Fatalf("crash receipt lost: %+v %v", receipt, err)
	}
}

func TestWorkspaceJobNativeProcessGroupExpiresWithoutControlConnection(t *testing.T) {
	dir := t.TempDir()
	done := make(chan error, 1)
	s := jobService(t, func(_ context.Context, h *Handle, _ json.RawMessage) (json.RawMessage, error) {
		err := h.Command(dir, nil, nil, "/bin/sh", "-c", "sleep 20 & echo $! > child.pid; wait")
		done <- err
		return nil, err
	})
	req := jobRequest(t, "job-original", 1, 350*time.Millisecond)
	if _, err := s.Create(req); err != nil {
		t.Fatal(err)
	}
	var pid int
	for until := time.Now().Add(time.Second); time.Now().Before(until); {
		data, err := os.ReadFile(filepath.Join(dir, "child.pid"))
		if err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("native child did not start")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expired native command reported success")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("native command outlived acknowledged expiry")
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	// A dead orphan can briefly remain a zombie until the host reaps it. It
	// cannot execute or write; an S/R/D child here would violate the fence.
	if err == nil {
		after := strings.LastIndex(string(data), ") ")
		if after < 0 || !strings.HasPrefix(string(data)[after+2:], "Z ") {
			t.Fatalf("native descendant still running: %s", data)
		}
	}
	waitJob(t, s, req.Spec.ID, "unknown")
}
