package sandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackConfigHomeTar(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rules", "base.md"), []byte("# base"), 0o644); err != nil {
		t.Fatal(err)
	}

	payload, err := packConfigHomeTar(dir)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	names := tarNames(t, payload)
	if !names["mcp.json"] || !names["rules/base.md"] {
		t.Fatalf("tar missing entries: %v", names)
	}
}

func tarNames(t *testing.T, payload []byte) map[string]bool {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(payload))
	out := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read: %v", err)
		}
		out[strings.TrimSuffix(hdr.Name, "/")] = true
	}
	return out
}

func TestManagerCreateSSHFallbackWhenNoInject(t *testing.T) {
	gw, _ := newInlineGW(t)
	m := NewManager(gw, ManagerOptions{
		Image:          "img:test",
		WorkspaceDir:   "/root/workspace",
		InstallHelpers: true,
		// no InjectStore → Create falls back to SSH EnsureConfigHome
	})

	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	wantMCP := `{"mcpServers":{"artifact-store":{"url":"http://api.example.com/mcp/runs/r1"}}}`
	if err := os.WriteFile(filepath.Join(home, "mcp.json"), []byte(wantMCP), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "rules", "base.md"), []byte("rule"), 0o644); err != nil {
		t.Fatal(err)
	}

	var extractCmd string
	var extractTar []byte
	hasMCP := false
	restore := SetExecHook(func(_ context.Context, _ string, _ int, command string, stdin io.Reader) ([]byte, error) {
		if strings.Contains(command, "test") && strings.Contains(command, "mcp.json") {
			if hasMCP {
				return nil, nil
			}
			return nil, context.DeadlineExceeded
		}
		if strings.Contains(command, "tar") && strings.Contains(command, "-C") && strings.Contains(command, "-xf") {
			extractCmd = command
			if stdin != nil {
				extractTar, _ = io.ReadAll(stdin)
			}
			hasMCP = true
			return nil, nil
		}
		if stdin != nil {
			_, _ = io.Copy(io.Discard, stdin)
		}
		return nil, nil
	})
	defer restore()

	sb, err := m.Create(context.Background(), Spec{
		Name:       "grasp-sb-cfg",
		ConfigHome: home,
		ConfigRoot: "/root/.cursor",
		Env:        map[string]string{BridgePasswordEnv: testBridgePassword},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sb.ConfigRoot != "/root/.cursor" {
		t.Errorf("ConfigRoot = %q", sb.ConfigRoot)
	}
	if extractCmd == "" {
		t.Fatal("expected SSH tar extract fallback when inject unavailable")
	}
	names := tarNames(t, extractTar)
	if !names["mcp.json"] || !names["rules/base.md"] {
		t.Fatalf("seeded tar missing mcp.json/rules: %v", names)
	}
}

func TestEnsureConfigHomeNoopWithoutHelpers(t *testing.T) {
	gw, fg := newInlineGW(t)
	fg.seed("gw-n", "running")
	m := NewManager(gw, ManagerOptions{InstallHelpers: false})
	called := false
	restore := SetExecHook(func(_ context.Context, _ string, _ int, _ string, _ io.Reader) ([]byte, error) {
		called = true
		return nil, nil
	})
	defer restore()
	sb, err := m.Attach(context.Background(), "gw-n")
	if err != nil {
		t.Fatal(err)
	}
	m.EnsureConfigHome(context.Background(), sb, t.TempDir(), "/root/.cursor")
	if called {
		t.Fatal("EnsureConfigHome should no-op when InstallHelpers is off")
	}
}
