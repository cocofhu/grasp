package execution

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequireToolsMissingCLI(t *testing.T) {
	restore := SetLookPathForTest(func(name string) error {
		return errMissing(name)
	})
	defer restore()
	err := RequireTools("codex", nil)
	if err == nil || !strings.Contains(err.Error(), "codex") || !strings.Contains(err.Error(), "不会改用沙箱容器") {
		t.Fatalf("err=%v", err)
	}
}

func TestRequireToolsPresent(t *testing.T) {
	restore := SetLookPathForTest(func(string) error { return nil })
	defer restore()
	if err := RequireTools("deepseek_harness", nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := CLIForProvider("trae"); !ok {
		t.Fatal("trae")
	}
}

func TestRequireToolsMissingPreview(t *testing.T) {
	restore := SetLookPathForTest(func(name string) error {
		if name == "chrome-devtools-mcp" {
			return errMissing(name)
		}
		return nil
	})
	defer restore()
	err := RequireTools("cursor", map[string]string{"BROWSER_MCP": "1"})
	if err == nil || !strings.Contains(err.Error(), "chrome-devtools-mcp") || !strings.Contains(err.Error(), "不会改用沙箱容器") {
		t.Fatalf("err=%v", err)
	}
}

func TestLocalToolDiscoveryAlsoConfiguresTheChildPath(t *testing.T) {
	t.Setenv(EnvMode, ModeLocal)
	home := t.TempDir()
	t.Setenv("HOME", home)
	ambientPath := t.TempDir()
	t.Setenv("PATH", ambientPath)
	t.Cleanup(SetLookPathForTest(nil))
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(binDir, "codex")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("codex"); err == nil {
		t.Fatal("test CLI unexpectedly exists on the ambient PATH")
	}
	if providers := InstalledProviders(); len(providers) != 1 || providers[0] != "codex" {
		t.Fatalf("host CLI discovery: %v", providers)
	}
	for _, useOverlay := range []bool{false, true} {
		env := map[string]string{}
		prefix := ambientPath
		if useOverlay {
			prefix = t.TempDir()
			env["PATH"] = prefix
		}
		if err := RequireTools("codex", env); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(env["PATH"], prefix+string(os.PathListSeparator)) {
			t.Fatalf("existing PATH precedence changed: %q", env["PATH"])
		}
		cmd := exec.Command("/bin/sh", "-c", "command -v codex")
		cmd.Env = []string{"PATH=" + env["PATH"]}
		if out, err := cmd.Output(); err != nil || strings.TrimSpace(string(out)) != binary {
			t.Fatalf("discovered host CLI is unavailable to the child: %q %v", out, err)
		}
	}
	if os.Getenv("PATH") != ambientPath {
		t.Fatal("local CLI discovery changed the server's PATH")
	}
}

type errMissing string

func (e errMissing) Error() string { return "missing " + string(e) }
