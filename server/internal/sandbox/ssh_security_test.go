package sandbox

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestQuoteShellPathRejectsMetacharacters(t *testing.T) {
	t.Parallel()
	bad := []string{
		"",
		"foo;id",
		"foo`id`",
		"foo$(id)",
		"foo && id",
		"foo|id",
		"foo\nid",
		"../etc/passwd",
		"/tmp/foo bar",
		"foo'bar",
	}
	for _, p := range bad {
		if _, err := quoteShellPath(p); err == nil {
			t.Fatalf("quoteShellPath(%q) want error", p)
		}
	}
	ok, err := quoteShellPath("/root/workspace/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if ok != `'/root/workspace/a.txt'` {
		t.Fatalf("got %q", ok)
	}
}

func TestNewSafeCmdRejectsMetacharacters(t *testing.T) {
	t.Parallel()
	if _, err := newSafeCmd(); err == nil {
		t.Fatal("empty argv should fail")
	}
	bad := []string{"foo;id", "foo`id`", "foo$(id)", "a&&b", "a|b", "../x", "a b", "a'b"}
	for _, a := range bad {
		if _, err := newSafeCmd("echo", a); err == nil {
			t.Fatalf("newSafeCmd echo %q want error", a)
		}
	}
	cmd, err := newSafeCmd("cat", "--", "/root/workspace/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	got, err := cmd.render()
	if err != nil {
		t.Fatal(err)
	}
	want := `'cat' '--' '/root/workspace/a.txt'`
	if got != want {
		t.Fatalf("render = %q, want %q", got, want)
	}
}

func TestExecRejectsUnvalidatedArgv(t *testing.T) {
	t.Parallel()
	gw, fg := newInlineGW(t)
	fg.seed("gw-safe", "running")
	m := NewManager(gw, ManagerOptions{WorkspaceDir: "/root/workspace"})
	restore := SetExecHook(func(context.Context, string, int, string, io.Reader) ([]byte, error) {
		t.Fatal("exec hook must not run for rejected argv")
		return nil, nil
	})
	defer restore()
	sb, err := m.Attach(context.Background(), "gw-safe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sb.Exec(context.Background(), time.Second, "echo", "hi;id"); err == nil {
		t.Fatal("Exec with metacharacters should fail before Start")
	}
}

func TestHostKeyTOFUPinAndMismatch(t *testing.T) {
	t.Parallel()
	cache := newHostKeyCache()
	c := sshCreds{host: "127.0.0.1", port: 2222, hostKeys: cache}

	key1 := mustSSHPublicKey(t)
	key2 := mustSSHPublicKey(t)

	cb1 := c.hostKeyCallback()
	if err := cb1("127.0.0.1", dummyAddr{}, key1); err != nil {
		t.Fatalf("first trust: %v", err)
	}
	if got := cache.get(c.addrKey()); got == nil || string(got.Marshal()) != string(key1.Marshal()) {
		t.Fatal("host key not pinned after first trust")
	}

	cb2 := c.hostKeyCallback()
	if err := cb2("127.0.0.1", dummyAddr{}, key1); err != nil {
		t.Fatalf("same key should pass FixedHostKey: %v", err)
	}
	err := cb2("127.0.0.1", dummyAddr{}, key2)
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("mismatch should be rejected, got %v", err)
	}
}

func TestHostKeyReplacedWhenAddressChangesSandbox(t *testing.T) {
	t.Parallel()
	cache := newHostKeyCache()
	first := sshCreds{host: "192.168.2.181", port: 22, sandboxID: "sandbox-a", hostKeys: cache}
	second := first
	second.sandboxID = "sandbox-b"
	keyA := mustSSHPublicKey(t)
	keyB := mustSSHPublicKey(t)
	keyB2 := mustSSHPublicKey(t)

	if err := first.hostKeyCallback()("192.168.2.181", dummyAddr{}, keyA); err != nil {
		t.Fatalf("pin sandbox-a: %v", err)
	}
	if err := second.hostKeyCallback()("192.168.2.181", dummyAddr{}, keyB); err != nil {
		t.Fatalf("reused address should trust sandbox-b: %v", err)
	}
	if got := cache.get(second.addrKey()); got == nil || string(got.Marshal()) != string(keyB.Marshal()) {
		t.Fatal("pin was not replaced with sandbox-b")
	}
	if err := second.hostKeyCallback()("192.168.2.181", dummyAddr{}, keyB2); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("same sandbox must keep its key, got %v", err)
	}
	if got := cache.get(second.addrKey()); string(got.Marshal()) != string(keyB.Marshal()) {
		t.Fatal("rejected key replaced the pin")
	}

	cache.forget("sandbox-b")
	if cache.get(second.addrKey()) != nil {
		t.Fatal("forget left sandbox-b's pin")
	}
	if err := second.hostKeyCallback()("192.168.2.181", dummyAddr{}, keyB2); err != nil {
		t.Fatalf("address is free after forget: %v", err)
	}
}

func mustSSHPublicKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

type dummyAddr struct{}

func (dummyAddr) Network() string { return "tcp" }
func (dummyAddr) String() string  { return "127.0.0.1:2222" }

var _ net.Addr = dummyAddr{}
