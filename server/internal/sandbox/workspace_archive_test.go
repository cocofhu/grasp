package sandbox

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestWorkspaceArchiveCommands(t *testing.T) {
	var commands []string
	var restored string
	restore := SetExecHook(func(_ context.Context, _ string, _ int, command string, stdin io.Reader) ([]byte, error) {
		commands = append(commands, command)
		if stdin != nil {
			b, _ := io.ReadAll(stdin)
			restored = string(b)
		}
		if strings.Contains(command, "-czf") {
			return []byte("archive"), nil
		}
		return nil, nil
	})
	defer restore()
	sb := &Sandbox{ID: "sb", WorkspaceDir: "/root/workspace"}
	rc, err := sb.ArchiveWorkspace(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(data) != "archive" {
		t.Fatalf("archive: %q", data)
	}
	if err := sb.RestoreWorkspace(context.Background(), strings.NewReader("archive")); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 3 || restored != "archive" {
		t.Fatalf("commands %q restored %q", commands, restored)
	}
	archive := commands[0]
	for _, want := range []string{"tar", "/root/workspace", "-czf", "--exclude=node_modules", "--exclude=.venv"} {
		if !strings.Contains(archive, want) {
			t.Fatalf("archive command %q lacks %q", archive, want)
		}
	}
	if !strings.Contains(commands[1], "mkdir") || !strings.Contains(commands[2], "-xzf") {
		t.Fatalf("restore commands: %q", commands[1:])
	}
}

func TestWorkspaceArchiveFailures(t *testing.T) {
	local := &Sandbox{WorkspaceDir: t.TempDir(), local: &localExec{}}
	if _, err := local.ArchiveWorkspace(context.Background()); err == nil {
		t.Fatal("archived a local worktree")
	}
	if err := local.RestoreWorkspace(context.Background(), strings.NewReader("")); err == nil {
		t.Fatal("restored into a local worktree")
	}
	empty := &Sandbox{}
	if _, err := empty.ArchiveWorkspace(context.Background()); err == nil {
		t.Fatal("archived without a workspace")
	}
	if err := empty.RestoreWorkspace(context.Background(), strings.NewReader("")); err == nil {
		t.Fatal("restored without a workspace")
	}
	bad := &Sandbox{WorkspaceDir: "/root/work space"}
	if _, err := bad.ArchiveWorkspace(context.Background()); err == nil {
		t.Fatal("unsafe workspace path accepted")
	}
	if err := bad.RestoreWorkspace(context.Background(), strings.NewReader("")); err == nil {
		t.Fatal("unsafe workspace path accepted for restore")
	}

	restore := SetExecHook(func(_ context.Context, _ string, _ int, command string, _ io.Reader) ([]byte, error) {
		if strings.Contains(command, "-xzf") {
			return []byte("noise\ntar: unexpected end of file"), errors.New("exit 2")
		}
		if strings.Contains(command, "-czf") {
			return nil, errors.New("tar failed")
		}
		return nil, nil
	})
	defer restore()
	sb := &Sandbox{WorkspaceDir: "/root/workspace"}
	if _, err := sb.ArchiveWorkspace(context.Background()); err == nil {
		t.Fatal("archive error swallowed")
	}
	if err := sb.RestoreWorkspace(context.Background(), strings.NewReader("x")); err == nil || err.Error() != "restore workspace: tar: unexpected end of file" {
		t.Fatalf("restore error: %v", err)
	}
	restoreMkdir := SetExecHook(func(_ context.Context, _ string, _ int, command string, _ io.Reader) ([]byte, error) {
		if strings.Contains(command, "mkdir") {
			return nil, errors.New("read-only file system")
		}
		return nil, errors.New("plain failure")
	})
	defer restoreMkdir()
	if err := sb.RestoreWorkspace(context.Background(), strings.NewReader("x")); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("mkdir error: %v", err)
	}
}

func TestArchiveExitErr(t *testing.T) {
	if archiveExitErr(nil) != nil {
		t.Fatal("nil")
	}
	plain := errors.New("x")
	if archiveExitErr(plain) != plain {
		t.Fatal("plain error changed")
	}
	if archiveExitErr(&ssh.ExitError{Waitmsg: ssh.Waitmsg{}}) == nil {
		t.Fatal("exit 0 ExitError accepted")
	}
}

func TestGatewayDriverAndStop(t *testing.T) {
	var stopped string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			_, _ = io.WriteString(w, `{"status":"ok","driver":"e2b"}`)
		case strings.HasSuffix(r.URL.Path, "/stop"):
			stopped = r.URL.Path
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	m := NewManager(NewGatewayClient(srv.URL, ""), ManagerOptions{})
	if !m.KeepsProcessesOnStop(context.Background(), "sb-1") {
		t.Fatal("e2b driver not detected")
	}
	if err := m.Stop(context.Background(), "sb-1"); err != nil || stopped != "/api/v1/sandboxes/sb-1/stop" {
		t.Fatalf("stop: %v %q", err, stopped)
	}
	if _, err := m.gw.Driver(context.Background(), "gw1_missing_c2I"); err == nil {
		t.Fatal("unknown gateway binding resolved")
	}
	none := &Manager{}
	if none.KeepsProcessesOnStop(context.Background(), "x") || none.Stop(context.Background(), "x") == nil {
		t.Fatal("manager without gateway")
	}
	srv.Close()
	if m.KeepsProcessesOnStop(context.Background(), "sb-1") {
		t.Fatal("unreachable gateway reported e2b")
	}
}
