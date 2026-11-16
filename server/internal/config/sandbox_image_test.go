package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRetiredSandboxImageSettingsAreReportedAndIgnored(t *testing.T) {
	t.Setenv("GRASP_SANDBOX_IMAGE", "env/img:1")
	t.Setenv("GRASP_SANDBOX_IMAGE_CODEX", "env/codex:1")
	t.Setenv("GRASP_SANDBOX_IMAGE_OPENCODE", "")
	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := "sandbox:\n  image: reg/force:1\n  images:\n    cursor: reg/cursor:pin\n    trae: \"\"\n"
	if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := parse(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"GRASP_SANDBOX_IMAGE", "GRASP_SANDBOX_IMAGE_CODEX", "sandbox.image", "sandbox.images.cursor"}
	if got := retiredSandboxImageKeys(c); !reflect.DeepEqual(got, want) {
		t.Fatalf("retired keys = %v, want %v", got, want)
	}
}

func TestNoRetiredSandboxImageSettings(t *testing.T) {
	t.Setenv("GRASP_SANDBOX_IMAGE", "")
	if got := retiredSandboxImageKeys(&Config{}); len(got) != 0 {
		t.Fatalf("retired keys = %v", got)
	}
}
