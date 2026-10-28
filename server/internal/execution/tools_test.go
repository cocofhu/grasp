package execution

import (
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

type errMissing string

func (e errMissing) Error() string { return "missing " + string(e) }
