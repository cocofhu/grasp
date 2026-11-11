package sandbox

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenArtifactFileLocalConfinesHostPaths(t *testing.T) {
	root := t.TempDir()
	sb := &Sandbox{WorkspaceDir: root, local: &localExec{}}
	if err := os.WriteFile(filepath.Join(root, "result.txt"), []byte("result"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := sb.OpenArtifactFile(context.Background(), "result.txt")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil || string(data) != "result" {
		t.Fatalf("read=%q err=%v", data, err)
	}
	outside := filepath.Join(t.TempDir(), "private")
	if err = os.WriteFile(outside, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{outside, "../private", "escape", ".", "bad\npath"} {
		if r, err = sb.OpenArtifactFile(context.Background(), name); err == nil {
			_ = r.Close()
			t.Fatalf("escaped with %q", name)
		}
	}
}
