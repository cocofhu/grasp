package sandbox

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
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

func TestOpenArtifactFileRejectsFIFOWithoutWaitingForAWriter(t *testing.T) {
	root := t.TempDir()
	pipe := filepath.Join(root, "not-an-image.png")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Fatal(err)
	}
	sb := &Sandbox{WorkspaceDir: root, local: &localExec{}}
	done := make(chan error, 1)
	go func() {
		reader, err := sb.OpenArtifactFile(t.Context(), "not-an-image.png")
		if reader != nil {
			_ = reader.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO was exported as a regular file")
		}
	case <-time.After(time.Second):
		// Unblock an incorrect blocking open before failing the regression.
		writer, err := os.OpenFile(pipe, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			_ = writer.Close()
		}
		<-done
		t.Fatal("FIFO export waited for a writer")
	}
}
