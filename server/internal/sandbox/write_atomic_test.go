package sandbox

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileAtomicRemoteRunsScriptOnStdin(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "codex", "auth.json")
	var commands []string
	restore := SetExecHook(func(ctx context.Context, _ string, _ int, command string, stdin io.Reader) ([]byte, error) {
		commands = append(commands, command)
		script, _ := io.ReadAll(stdin)
		cmd := exec.CommandContext(ctx, "bash", "-s")
		cmd.Stdin = bytes.NewReader(script)
		return cmd.CombinedOutput()
	})
	defer restore()
	sb := &Sandbox{ID: "sb", SSHHost: "h", SSHPort: 22, WorkspaceDir: "/root/workspace"}
	secret := []byte(`{"tokens":{"access_token":"secret-at"}}`)
	for i := 0; i < 2; i++ {
		if err := sb.WriteFileAtomic(context.Background(), target, secret); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("content %q %v", got, err)
	}
	info, _ := os.Stat(target)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(target))
	if len(entries) != 1 {
		t.Fatalf("temp file left behind: %v", entries)
	}
	for _, c := range commands {
		if strings.Contains(c, "secret-at") {
			t.Fatalf("content on command line: %q", c)
		}
	}
}

func TestWriteFileAtomicRemoteFailureDoesNotEchoContent(t *testing.T) {
	restore := SetExecHook(func(context.Context, string, int, string, io.Reader) ([]byte, error) {
		return []byte("mv: cannot move"), errors.New("exit 1")
	})
	defer restore()
	sb := &Sandbox{ID: "sb", SSHHost: "h", SSHPort: 22}
	err := sb.WriteFileAtomic(context.Background(), "/root/.codex/auth.json", []byte("secret-at"))
	if err == nil || strings.Contains(err.Error(), "secret-at") || !strings.Contains(err.Error(), "cannot move") {
		t.Fatalf("err %v", err)
	}
	if err := sb.WriteFileAtomic(context.Background(), "/root/../etc/x", []byte("x")); err == nil {
		t.Fatal("unsafe path must be rejected")
	}
}

func TestWriteFileAtomicLocal(t *testing.T) {
	home := t.TempDir()
	sb := &Sandbox{local: &localExec{configRoot: "/root/.codex", configHome: home}}
	if err := sb.WriteFileAtomic(context.Background(), "/root/.codex/auth.json", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := sb.WriteFileAtomic(context.Background(), "/root/.codex/auth.json", []byte("two")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "auth.json")
	got, _ := os.ReadFile(path)
	if string(got) != "two" {
		t.Fatalf("content %q", got)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(home)
	if len(entries) != 1 {
		t.Fatalf("temp file left behind: %v", entries)
	}
}

func TestWriteLocalAtomicFailures(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeLocalAtomic(filepath.Join(blocker, "sub", "auth.json"), []byte("x")); err == nil {
		t.Fatal("mkdir under a file must fail")
	}
	target := filepath.Join(dir, "target")
	if err := os.MkdirAll(filepath.Join(target, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeLocalAtomic(target, []byte("x")); err == nil {
		t.Fatal("renaming over a non-empty directory must fail")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
	if os.Getuid() != 0 {
		ro := filepath.Join(dir, "ro")
		_ = os.MkdirAll(ro, 0o500)
		if err := writeLocalAtomic(filepath.Join(ro, "auth.json"), []byte("x")); err == nil {
			t.Fatal("read-only dir must fail")
		}
	}
}
