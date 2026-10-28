package sandbox

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestWorktreeExecAndPreviewEndpoints(t *testing.T) {
	t.Setenv("GRASP_EXECUTION_MODE", "worktree")
	m := NewManager(nil, ManagerOptions{})
	out, err := m.Exec(context.Background(), "local", time.Second, "echo", "ok")
	if err != nil {
		t.Fatal(err)
	}
	if out != "ok" {
		t.Fatalf("exec = %q", out)
	}
	cdp, err := m.EndpointAddr(context.Background(), "local", "cdp")
	if err != nil || cdp != "127.0.0.1:9222" {
		t.Fatalf("cdp = %q err=%v", cdp, err)
	}
	novnc, err := m.EndpointAddr(context.Background(), "local", "novnc")
	if err != nil || novnc != "127.0.0.1:6080" {
		t.Fatalf("novnc = %q err=%v", novnc, err)
	}
}

func TestLocalTerminalEcho(t *testing.T) {
	term, err := OpenLocalTerminal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = term.Close() }()
	if _, err := term.Write([]byte("printf hi\\n\n")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 256)
	_ = term.pty.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := term.Read(buf)
	if n == 0 && err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(buf[:n]), "hi") {
		t.Fatalf("pty output = %q", buf[:n])
	}
}
