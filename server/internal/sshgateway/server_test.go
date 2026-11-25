package sshgateway

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func TestParseUser(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want uint
		ok   bool
	}{
		{"sbx-12", 12, true},
		{"sbx-12+root", 12, true},
		{"sbx-12+ubuntu", 0, false},
		{"sbx-0", 0, false},
		{"sbx-", 0, false},
		{"sbx-abc", 0, false},
		{"root", 0, false},
		{"sbx-99999999999", 0, false},
	} {
		got, err := ParseUser(tc.in)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("ParseUser(%q) = %d, %v", tc.in, got, err)
		}
	}
}

func TestExecRelaysOutputStderrAndExitStatus(t *testing.T) {
	h := newHarness(t, Options{})
	cli := h.mustDial(t, "sbx-1")

	sess, err := cli.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	sess.Stdout, sess.Stderr = &stdout, &stderr
	if err := sess.Run("hello"); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "out:hello\n" || stderr.String() != "err:hello\n" {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	sess, err = cli.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	var exit *ssh.ExitError
	if err := sess.Run("exit 3"); !errors.As(err, &exit) || exit.ExitStatus() != 3 {
		t.Fatalf("exit status not relayed: %v", err)
	}

	sess, err = cli.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	sess.Stdin = strings.NewReader("piped input")
	out, err := sess.Output("cat")
	if err != nil || string(out) != "piped input" {
		t.Fatalf("stdin/EOF not relayed: %q %v", out, err)
	}

	if h.sbx.activeFor(1) != 1 || h.srv.Connections(1) != 1 {
		t.Fatalf("connection not tracked: active=%d conns=%d", h.sbx.activeFor(1), h.srv.Connections(1))
	}
	_ = cli.Close()
	waitUntil(t, "release", func() bool { return h.sbx.activeFor(1) == 0 && h.srv.Connections(1) == 0 })
}

func TestShellWithPTYAndBlockedAgentForwarding(t *testing.T) {
	h := newHarness(t, Options{})
	cli := h.mustDial(t, "sbx-1+root")
	sess, err := cli.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.RequestPty("xterm-256color", 40, 120, ssh.TerminalModes{}); err != nil {
		t.Fatal(err)
	}
	if err := sess.Setenv("LANG", "C.UTF-8"); err != nil {
		t.Fatal(err)
	}
	if ok, err := sess.SendRequest("auth-agent-req@openssh.com", true, nil); err != nil || ok {
		t.Fatalf("agent forwarding must be refused: ok=%v err=%v", ok, err)
	}
	if ok, err := sess.SendRequest("x11-req", true, nil); err != nil || ok {
		t.Fatalf("x11 forwarding must be refused: ok=%v err=%v", ok, err)
	}
	stdin, _ := sess.StdinPipe()
	stdout, _ := sess.StdoutPipe()
	if err := sess.Shell(); err != nil {
		t.Fatal(err)
	}
	if err := sess.WindowChange(50, 160); err != nil {
		t.Fatal(err)
	}
	_, _ = stdin.Write([]byte("ping"))
	buf := make([]byte, 4)
	if _, err := io.ReadFull(stdout, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("shell echo = %q %v", buf, err)
	}
	_ = stdin.Close()
	if err := sess.Wait(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"pty-req", "env", "shell", "window-change"} {
		if !h.sandbox.saw(want) {
			t.Errorf("sandbox did not receive %s", want)
		}
	}
	if h.sandbox.saw("auth-agent-req@openssh.com") || h.sandbox.saw("x11-req") {
		t.Fatal("refused requests reached the sandbox")
	}
}

func TestDirectTCPIPAndRefusedChannels(t *testing.T) {
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(c, c); _ = c.Close() }()
		}
	}()
	h := newHarness(t, Options{})
	cli := h.mustDial(t, "sbx-1")
	conn, err := cli.Dial("tcp", echo.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = conn.Write([]byte("forwarded"))
	buf := make([]byte, 9)
	if _, err := io.ReadFull(conn, buf); err != nil || string(buf) != "forwarded" {
		t.Fatalf("port forward echo = %q %v", buf, err)
	}
	_ = conn.Close()

	if _, err := cli.Dial("tcp", "127.0.0.1:1"); err == nil {
		t.Fatal("a refused upstream connection must fail the forward")
	}
	if _, _, err := cli.OpenChannel("x11", nil); err == nil {
		t.Fatal("x11 channel must be refused")
	}
	if ok, _, err := cli.SendRequest("keepalive@openssh.com", true, nil); err != nil || ok {
		t.Fatalf("keepalive must get a reply: ok=%v err=%v", ok, err)
	}
	if _, err := cli.Listen("tcp", "127.0.0.1:0"); err == nil {
		t.Fatal("remote forwarding is not supported")
	}
}

func TestSFTPSubsystem(t *testing.T) {
	h := newHarness(t, Options{})
	cli := h.mustDial(t, "sbx-1")
	sc, err := sftp.NewClient(cli)
	if err != nil {
		t.Fatal(err)
	}
	defer sc.Close()
	path := filepath.Join(h.sandbox.dir, "hello.txt")
	f, err := sc.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(bytes.Repeat([]byte("x"), 256<<10)); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	info, err := os.Stat(path)
	if err != nil || info.Size() != 256<<10 {
		t.Fatalf("sftp upload size = %v %v", info, err)
	}
}

func TestAuthenticationFailuresAndRateLimit(t *testing.T) {
	h := newHarness(t, Options{AuthFailLimit: 2, AuthFailWindow: time.Minute})
	stranger := newSigner(t)
	if _, err := h.dial(t, "sbx-1", stranger); err == nil {
		t.Fatal("unregistered key accepted")
	}
	if _, err := h.dial(t, "root", h.userKey); err == nil {
		t.Fatal("user name without a sandbox accepted")
	}
	waitUntil(t, "failures recorded", func() bool { return h.srv.limiter.blocked("127.0.0.1") })
	if _, err := h.dial(t, "sbx-1", h.userKey); err == nil {
		t.Fatal("source address was not blocked after repeated failures")
	}
	if h.sbx.dialCount() != 0 {
		t.Fatal("failed authentication must never reach the sandbox")
	}
}

func TestSandboxUnavailableIsExplained(t *testing.T) {
	h := newHarness(t, Options{})
	h.sbx.mu.Lock()
	h.sbx.dialErr = errors.New("会话已手动停止，请先恢复")
	h.sbx.mu.Unlock()
	cli := h.mustDial(t, "sbx-1")
	if _, err := cli.Dial("tcp", "127.0.0.1:80"); err == nil || !strings.Contains(err.Error(), "会话已手动停止") {
		t.Fatalf("forward must carry the reason: %v", err)
	}
	sess, err := cli.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	_ = sess.RequestPty("xterm", 24, 80, ssh.TerminalModes{})
	var stderr bytes.Buffer
	sess.Stderr = &stderr
	var exit *ssh.ExitError
	if err := sess.Run("ls"); !errors.As(err, &exit) || exit.ExitStatus() != 1 {
		t.Fatalf("refusal exit = %v", err)
	}
	if !strings.Contains(stderr.String(), "无法连接沙箱 1：会话已手动停止") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	h.sbx.mu.Lock()
	acquired := h.sbx.acquired
	h.sbx.mu.Unlock()
	if acquired != 0 {
		t.Fatal("a refused connection must not hold the sandbox")
	}
}

func TestPerSandboxConnectionLimit(t *testing.T) {
	h := newHarness(t, Options{MaxConnsPerSandbox: 1})
	h.mustDial(t, "sbx-1")
	waitUntil(t, "first connection", func() bool { return h.srv.Connections(1) == 1 })
	second := h.mustDial(t, "sbx-1")
	sess, err := second.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	sess.Stderr = &stderr
	if err := sess.Run("true"); err == nil || !strings.Contains(stderr.String(), "连接数已达上限") {
		t.Fatalf("second connection = %v %q", err, stderr.String())
	}
	h.mustDial(t, "sbx-2") // other sandboxes are unaffected
	waitUntil(t, "other sandbox", func() bool { return h.srv.Connections(2) == 1 })
}

func TestGlobalConnectionLimit(t *testing.T) {
	h := newHarness(t, Options{MaxConns: 1})
	h.mustDial(t, "sbx-1")
	waitUntil(t, "first connection", func() bool { return h.srv.Connections(1) == 1 })
	second := h.mustDial(t, "sbx-2")
	sess, err := second.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	sess.Stderr = &stderr
	if err := sess.Run("true"); err == nil || !strings.Contains(stderr.String(), "网关连接数已达上限") {
		t.Fatalf("global limit not enforced: %v %q", err, stderr.String())
	}
	if h.sbx.dialCount() != 1 {
		t.Fatal("a connection over the global limit reached the sandbox")
	}
}

func TestUnauthenticatedHandshakesCannotTakeAuthenticatedSlots(t *testing.T) {
	h := newHarness(t, Options{MaxConns: 1, MaxPendingPerSource: 3, MaxPendingHandshakes: 3})
	var idle []net.Conn
	for i := 0; i < 2; i++ {
		c, err := net.Dial("tcp", h.addr)
		if err != nil {
			t.Fatal(err)
		}
		idle = append(idle, c)
	}
	waitUntil(t, "pending handshakes", func() bool {
		h.srv.mu.Lock()
		defer h.srv.mu.Unlock()
		return h.srv.pending == 2
	})
	// Two stalled handshakes do not use the single authenticated slot.
	h.mustDial(t, "sbx-1")
	waitUntil(t, "authenticated", func() bool { return h.srv.Connections(1) == 1 })

	third, err := net.Dial("tcp", h.addr)
	if err != nil {
		t.Fatal(err)
	}
	idle = append(idle, third)
	waitUntil(t, "pending full", func() bool {
		h.srv.mu.Lock()
		defer h.srv.mu.Unlock()
		return h.srv.pending == 3
	})
	if _, err := h.dial(t, "sbx-2", h.userKey); err == nil {
		t.Fatal("a source over its pending handshake limit was admitted")
	}
	for _, c := range idle {
		_ = c.Close()
	}
	waitUntil(t, "pending released", func() bool {
		h.srv.mu.Lock()
		defer h.srv.mu.Unlock()
		return h.srv.pending == 0 && len(h.srv.bySource) == 0
	})
}

func TestSandboxAccessIsCheckedBeforeDialAndOnRevalidate(t *testing.T) {
	h := newHarness(t, Options{RevalidateInterval: 20 * time.Millisecond})
	h.sbx.deny(2, true)
	cli := h.mustDial(t, "sbx-2")
	sess, err := cli.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	sess.Stderr = &stderr
	if err := sess.Run("id"); err == nil || !strings.Contains(stderr.String(), "沙箱 2 不存在或无权访问") {
		t.Fatalf("forbidden sandbox = %v %q", err, stderr.String())
	}
	if h.sbx.dialCount() != 0 {
		t.Fatal("a forbidden sandbox was dialed")
	}

	allowed := h.mustDial(t, "sbx-1")
	waitUntil(t, "connected", func() bool { return h.sbx.activeFor(1) == 1 })
	h.sbx.deny(1, true)
	done := make(chan error, 1)
	go func() { done <- allowed.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("losing sandbox access kept the connection")
	}
}

func TestKeyRevokedDuringHandshakeNeverReachesSandbox(t *testing.T) {
	h := newHarness(t, Options{})
	h.auth.mu.Lock()
	h.auth.allowCalls = 1 // the handshake lookup succeeds, the post-register check does not
	h.auth.mu.Unlock()
	cli := h.mustDial(t, "sbx-1")
	sess, err := cli.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	sess.Stderr = &stderr
	if err := sess.Run("id"); err == nil || !strings.Contains(stderr.String(), "key no longer authorized") {
		t.Fatalf("revoked key = %v %q", err, stderr.String())
	}
	if h.sbx.dialCount() != 0 {
		t.Fatal("a key revoked during the handshake reached the sandbox")
	}
}

func TestRefusedClientMayCloseFirst(t *testing.T) {
	h := newHarness(t, Options{RefuseLinger: 50 * time.Millisecond})
	h.sbx.deny(1, true)
	cli := h.mustDial(t, "sbx-1")
	done := make(chan error, 1)
	go func() { done <- cli.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("an idle refused client was not dropped after the linger")
	}
}

func TestRevokedKeyIsDisconnected(t *testing.T) {
	h := newHarness(t, Options{RevalidateInterval: 20 * time.Millisecond})
	cli := h.mustDial(t, "sbx-1")
	waitUntil(t, "connected", func() bool { return h.sbx.activeFor(1) == 1 })
	h.auth.remove(h.userKey.PublicKey())
	done := make(chan error, 1)
	go func() { done <- cli.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("removed key kept its connection")
	}
	waitUntil(t, "release", func() bool { return h.sbx.activeFor(1) == 0 })
}

func TestDisconnectByFingerprintAndUser(t *testing.T) {
	h := newHarness(t, Options{})
	h.mustDial(t, "sbx-1")
	h.mustDial(t, "sbx-2")
	waitUntil(t, "connected", func() bool { return h.sbx.activeFor(1) == 1 && h.sbx.activeFor(2) == 1 })
	if n := h.srv.DisconnectUser("bob"); n != 0 {
		t.Fatalf("disconnected %d connections of another user", n)
	}
	if n := h.srv.DisconnectFingerprint(ssh.FingerprintSHA256(h.userKey.PublicKey())); n != 2 {
		t.Fatalf("fingerprint disconnect = %d", n)
	}
	waitUntil(t, "closed", func() bool { return h.srv.Connections(1) == 0 && h.srv.Connections(2) == 0 })
	h.mustDial(t, "sbx-1")
	waitUntil(t, "reconnected", func() bool { return h.sbx.activeFor(1) == 1 })
	if n := h.srv.DisconnectUser("alice"); n != 1 {
		t.Fatalf("user disconnect = %d", n)
	}
}

func TestSandboxDisconnectClosesClient(t *testing.T) {
	h := newHarness(t, Options{})
	cli := h.mustDial(t, "sbx-1")
	waitUntil(t, "connected", func() bool { return h.srv.Connections(1) == 1 })
	h.srv.mu.Lock()
	var c *gatewayConn
	for k := range h.srv.conns {
		c = k
	}
	h.srv.mu.Unlock()
	c.close("test")
	if err := cli.Wait(); err == nil {
		t.Log("client saw a clean close")
	}
	waitUntil(t, "release", func() bool { return h.sbx.activeFor(1) == 0 })
}

func TestCloseDisconnectsAndStopsServing(t *testing.T) {
	h := newHarness(t, Options{TouchInterval: 10 * time.Millisecond})
	cli := h.mustDial(t, "sbx-1")
	waitUntil(t, "touch", func() bool {
		h.sbx.mu.Lock()
		defer h.sbx.mu.Unlock()
		return h.sbx.touches >= 2
	})
	if err := h.srv.Close(); err != nil {
		t.Fatal(err)
	}
	_ = h.srv.Close()
	if err := cli.Wait(); err == nil {
		t.Log("client saw a clean close")
	}
	if _, err := h.dial(t, "sbx-1", h.userKey); err == nil {
		t.Fatal("closed server still accepts")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.srv.Serve(ln); err != nil {
		t.Fatalf("Serve after Close = %v", err)
	}
}

func TestDialTimeoutIsExplained(t *testing.T) {
	h := newHarness(t, Options{DialTimeout: 50 * time.Millisecond})
	h.sbx.mu.Lock()
	h.sbx.dialGate = make(chan struct{})
	h.sbx.mu.Unlock()
	cli := h.mustDial(t, "sbx-1")
	sess, err := cli.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	sess.Stderr = &stderr
	if err := sess.Run("true"); err == nil || !strings.Contains(stderr.String(), "deadline exceeded") {
		t.Fatalf("timeout = %v %q", err, stderr.String())
	}
}

func TestHostKeyPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys", "host_key")
	a, err := loadOrCreateHostKey(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("host key mode = %v %v", info, err)
	}
	b, err := loadOrCreateHostKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.PublicKey().Marshal(), b.PublicKey().Marshal()) {
		t.Fatal("host key changed across restarts")
	}
	if _, err := loadOrCreateHostKey(""); err == nil {
		t.Fatal("empty path accepted")
	}
	bad := filepath.Join(dir, "bad")
	if err := os.WriteFile(bad, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrCreateHostKey(bad); err == nil {
		t.Fatal("corrupt host key accepted")
	}
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrCreateHostKey(filepath.Join(blocker, "host_key")); err == nil {
		t.Fatal("unwritable host key dir accepted")
	}
	if _, err := New(Options{}, &fakeAuth{}, &fakeSandboxes{}); err == nil {
		t.Fatal("New without a host key path must fail")
	}
	if _, err := New(Options{HostKeyPath: path}, nil, nil); err == nil {
		t.Fatal("New without dependencies must fail")
	}
	srv, err := New(Options{HostKeyPath: path}, &fakeAuth{}, &fakeSandboxes{})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if srv.HostKeyFingerprint() != ssh.FingerprintSHA256(a.PublicKey()) {
		t.Fatal("fingerprint mismatch")
	}
}

func TestHostKeyConcurrentFirstStart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "host_key")
	const replicas = 8
	keys := make(chan []byte, replicas)
	errs := make(chan error, replicas)
	var start sync.WaitGroup
	start.Add(1)
	for i := 0; i < replicas; i++ {
		go func() {
			start.Wait()
			signer, err := loadOrCreateHostKey(path)
			if err != nil {
				errs <- err
				return
			}
			keys <- signer.PublicKey().Marshal()
		}()
	}
	start.Done()
	var first []byte
	for i := 0; i < replicas; i++ {
		select {
		case err := <-errs:
			t.Fatalf("replica failed to start: %v", err)
		case k := <-keys:
			if first == nil {
				first = k
			} else if !bytes.Equal(first, k) {
				t.Fatal("replicas serve different host keys")
			}
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temporary key files left behind: %d entries", len(entries))
	}
}

func TestFailLimiterWindow(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newFailLimiter(2, time.Minute)
	l.now = func() time.Time { return now }
	l.fail("a")
	if l.blocked("a") {
		t.Fatal("blocked below the limit")
	}
	l.fail("a")
	if !l.blocked("a") || l.blocked("b") {
		t.Fatal("limit not per address")
	}
	now = now.Add(61 * time.Second)
	if l.blocked("a") {
		t.Fatal("window did not expire")
	}
	if len(l.hits) != 0 {
		t.Fatal("expired entries kept")
	}
	off := newFailLimiter(0, time.Minute)
	off.fail("a")
	if off.blocked("a") {
		t.Fatal("disabled limiter blocked")
	}
	for i := 0; i < 4200; i++ {
		l.fail(net.IPv4(10, byte(i>>16), byte(i>>8), byte(i)).String())
	}
	now = now.Add(2 * time.Minute)
	l.fail("fresh")
	if len(l.hits) != 1 {
		t.Fatalf("stale addresses not pruned: %d", len(l.hits))
	}
}

func TestRemoteHost(t *testing.T) {
	if remoteHost(nil) != "" {
		t.Fatal("nil addr")
	}
	if got := remoteHost(&net.TCPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 5}); got != "1.2.3.4" {
		t.Fatal(got)
	}
	if got := remoteHost(&net.UnixAddr{Name: "sock", Net: "unix"}); got != "sock" {
		t.Fatal(got)
	}
}

func TestHostKeyFallsBackWhenHardLinksFail(t *testing.T) {
	prev := linkFile
	linkFile = func(string, string) error { return &os.LinkError{Op: "link", Err: errors.ErrUnsupported} }
	defer func() { linkFile = prev }()
	dir := t.TempDir()
	path := filepath.Join(dir, "host_key")
	signer, err := loadOrCreateHostKey(path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := loadOrCreateHostKey(path)
	if err != nil || !bytes.Equal(signer.PublicKey().Marshal(), again.PublicKey().Marshal()) {
		t.Fatalf("fallback key not reused: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("temporary key files left behind: %d entries", len(entries))
	}
}
