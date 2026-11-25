package sshgateway

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// opensshClient drives the real OpenSSH client against the gateway, the way
// VS Code / Cursor Remote-SSH does.
type opensshClient struct {
	t    *testing.T
	h    *harness
	key  string
	port string
}

func newOpenSSHClient(t *testing.T, h *harness) *opensshClient {
	t.Helper()
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("OpenSSH client not installed")
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "test")
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(key, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	h.auth.add(signer.PublicKey(), "alice")
	_, port, _ := net.SplitHostPort(h.addr)
	return &opensshClient{t: t, h: h, key: key, port: port}
}

func (c *opensshClient) common() []string {
	return []string{
		"-F", "/dev/null", "-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes",
		"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR", "-o", "ConnectTimeout=10", "-i", c.key,
	}
}

func (c *opensshClient) ssh(stdin string, args ...string) (string, string, int) {
	c.t.Helper()
	full := append(c.common(), "-p", c.port)
	full = append(full, args...)
	cmd := exec.Command("ssh", full...)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		c.t.Fatal(err)
	}
	return out.String(), errOut.String(), code
}

func TestOpenSSHClientExecForwardAndSFTP(t *testing.T) {
	h := newHarness(t, Options{})
	c := newOpenSSHClient(t, h)

	out, errOut, code := c.ssh("", "sbx-1@127.0.0.1", "hello")
	if code != 0 || out != "out:hello\n" || !strings.Contains(errOut, "err:hello") {
		t.Fatalf("exec: code=%d out=%q err=%q", code, out, errOut)
	}
	if _, _, code := c.ssh("", "sbx-1@127.0.0.1", "exit 7"); code != 7 {
		t.Fatalf("exit code = %d", code)
	}

	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		for {
			conn, err := echo.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(conn, conn); _ = conn.Close() }()
		}
	}()
	if out, errOut, code := c.ssh("through the tunnel", "-W", echo.Addr().String(), "sbx-2@127.0.0.1"); code != 0 || out != "through the tunnel" {
		t.Fatalf("-W forward: code=%d out=%q err=%q", code, out, errOut)
	}

	if _, err := exec.LookPath("sftp"); err == nil {
		local := filepath.Join(t.TempDir(), "upload.txt")
		if err := os.WriteFile(local, []byte("sftp payload"), 0o600); err != nil {
			t.Fatal(err)
		}
		batch := filepath.Join(t.TempDir(), "batch")
		if err := os.WriteFile(batch, []byte("put "+local+" uploaded.txt\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		args := append(c.common(), "-P", c.port, "-b", batch, "sbx-1@127.0.0.1")
		if out, err := exec.Command("sftp", args...).CombinedOutput(); err != nil {
			t.Fatalf("sftp: %v\n%s", err, out)
		}
		got, err := os.ReadFile(filepath.Join(h.sandbox.dir, "uploaded.txt"))
		if err != nil || string(got) != "sftp payload" {
			t.Fatalf("sftp upload = %q %v", got, err)
		}
	}

	h.sbx.mu.Lock()
	h.sbx.dialErr = errors.New("沙箱已停止")
	h.sbx.mu.Unlock()
	_, errOut, code = c.ssh("", "sbx-1@127.0.0.1", "hello")
	if code != 1 || !strings.Contains(errOut, "grasp: 无法连接沙箱 1：沙箱已停止") {
		t.Fatalf("refusal: code=%d err=%q", code, errOut)
	}
	if _, _, code := c.ssh("", "nobody@127.0.0.1", "hello"); code != 255 {
		t.Fatalf("bad user name must fail authentication, code=%d", code)
	}
}
