package sandbox

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/pkg/sftp"
)

// openSSHSFTP connects to OpenSSH's sftp-server over pipes. Its realpath
// resolves symlinks like the sandbox sshd does, unlike pkg/sftp's server.
func openSSHSFTP(t *testing.T) *sftp.Client {
	t.Helper()
	var bin string
	for _, p := range []string{"/usr/lib/openssh/sftp-server", "/usr/lib/sftp-server", "/usr/libexec/sftp-server", "/usr/libexec/openssh/sftp-server"} {
		if _, err := os.Stat(p); err == nil {
			bin = p
			break
		}
	}
	if bin == "" {
		t.Skip("OpenSSH sftp-server not installed")
	}
	cmd := exec.Command(bin)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	sc, err := sftp.NewClientPipe(stdout, stdin)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sc.Close(); _ = cmd.Wait() })
	return sc
}

func TestOpenSFTPExportConfinesGuestPaths(t *testing.T) {
	sc := openSSHSFTP(t)
	root := t.TempDir()
	for name, body := range map[string]string{"result.zip": "zip", "完整 压缩包(1).zip": "cjk"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "result.zip"), filepath.Join(root, "inside")); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(outside, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	saved := exportRoots
	exportRoots = nil
	t.Cleanup(func() { exportRoots = saved })

	for name, want := range map[string]string{"result.zip": "zip", "完整 压缩包(1).zip": "cjk", "inside": "zip", filepath.Join(root, "result.zip"): "zip"} {
		f, err := openSFTPExport(sc, root, name)
		if err != nil {
			t.Fatalf("open %q: %v", name, err)
		}
		data, err := io.ReadAll(f)
		_ = f.Close()
		if err != nil || string(data) != want {
			t.Fatalf("%q read=%q err=%v", name, data, err)
		}
	}
	rel, err := filepath.Rel(root, outside)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{outside, rel, "escape", ".", "dir", "missing"} {
		if f, err := openSFTPExport(sc, root, name); err == nil {
			_ = f.Close()
			t.Fatalf("exported %q", name)
		}
	}
}

func TestWithinRoot(t *testing.T) {
	cases := map[[2]string]bool{
		{"/root/workspace", "/root/workspace"}:       true,
		{"/root/workspace/a.zip", "/root/workspace"}: true,
		{"/root/workspace2/a", "/root/workspace"}:    false,
		{"/tmp/a", "/"}:         true,
		{"/etc/passwd", "/tmp"}: false,
	}
	for c, want := range cases {
		if got := withinRoot(c[0], c[1]); got != want {
			t.Fatalf("withinRoot(%q,%q)=%v", c[0], c[1], got)
		}
	}
}

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
