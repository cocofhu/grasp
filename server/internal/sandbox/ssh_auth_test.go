package sandbox

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"encoding/pem"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/ssh"
)

func TestAuthorizeKeyScriptAppendsOnce(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "authorized_keys")
	script := strings.ReplaceAll(authorizeKeyShell, "/root/.ssh/authorized_keys", file)
	script = strings.ReplaceAll(script, "/root/.ssh", dir)
	oldLine := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOldKeyMaterialForScriptTest"
	newLine := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAINewKeyMaterialForScriptTest"
	if err := os.WriteFile(file, []byte(oldLine+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runScript := func(line string) {
		t.Helper()
		cmd := exec.Command("sh", "-c", script)
		cmd.Stdin = strings.NewReader(line + "\n")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("script: %v %s", err, out)
		}
	}
	runScript(newLine)
	runScript(newLine)
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if strings.Count(body, newLine) != 1 {
		t.Fatalf("new key count = %d", strings.Count(body, newLine))
	}
	if strings.Count(body, oldLine) != 1 {
		t.Fatalf("old key count = %d", strings.Count(body, oldLine))
	}
}

func TestRedactSSHTextOmitsSecrets(t *testing.T) {
	secret := "super-secret-token-value"
	msg := "ssh handshake 10.0.0.1:22: " + secret + " -----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----"
	got := redactSSHText(msg, secret)
	if strings.Contains(got, secret) || strings.Contains(got, "PRIVATE KEY") || strings.Contains(got, "abc") {
		t.Fatal("redaction leaked")
	}
}

func TestSSHPasswordRecoversMismatchedKey(t *testing.T) {
	_, oldLine := mustSigner(t)
	newSigner, newLine, newPEM := mustSignerPEM(t)
	const secret = "correct-sandbox-token"
	const wrong = "wrong-sandbox-token-xx"
	srv := startAuthServer(t, secret, []string{oldLine})

	prev := log.Logger
	var logs bytes.Buffer
	log.Logger = zerolog.New(&logs)
	t.Cleanup(func() { log.Logger = prev })

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	okCreds := sshCreds{
		host: srv.host, port: srv.port, signer: newSigner, authKey: newLine,
		password: secret, user: "root", hostKeys: newHostKeyCache(),
	}
	cli, err := okCreds.dial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = cli.Close()
	if got := srv.count(newLine); got != 1 {
		t.Fatalf("appended key count = %d", got)
	}

	again := okCreds
	again.password = ""
	again.hostKeys = okCreds.hostKeys
	cli2, err := again.dial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = cli2.Close()
	if got := srv.count(newLine); got != 1 {
		t.Fatalf("publickey login duplicated the key (%d)", got)
	}
	if strings.Contains(logs.String(), secret) || strings.Contains(logs.String(), "PRIVATE KEY") || strings.Contains(logs.String(), newPEM) {
		t.Fatal("success log contains a secret")
	}

	logs.Reset()
	bad := okCreds
	bad.password = wrong
	bad.authKey = newLine
	bad.hostKeys = newHostKeyCache()
	srv.acceptOnly(oldLine)
	_, err = bad.dial(ctx)
	if err == nil {
		t.Fatal("wrong password should fail")
	}
	if leaked(err.Error(), secret, wrong, newPEM) {
		t.Fatal("wrong password error contains a secret")
	}
	if srv.count(newLine) != 1 {
		t.Fatal("wrong password changed authorized keys")
	}

	empty := okCreds
	empty.password = ""
	empty.hostKeys = newHostKeyCache()
	srv.acceptOnly(oldLine)
	_, err = empty.dial(ctx)
	if err == nil {
		t.Fatal("empty password should fail when the key mismatches")
	}
	if leaked(err.Error(), secret, wrong, newPEM) {
		t.Fatal("empty password error contains a secret")
	}

	logs.Reset()
	srv.setFailHeal(true)
	srv.acceptOnly(oldLine)
	heal := okCreds
	heal.hostKeys = newHostKeyCache()
	kept, err := heal.dial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer kept.Close()
	sess, err := kept.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Run("echo hi"); err != nil {
		t.Fatal(err)
	}
	logged := logs.String()
	if !strings.Contains(logged, "session kept") {
		t.Fatal("heal failure was not logged")
	}
	if strings.Contains(logged, secret) || strings.Contains(logged, "PRIVATE KEY") || strings.Contains(logged, newPEM) {
		t.Fatal("heal failure log contains a secret")
	}
}

func TestAttachWithTokenSetsDataPlanePassword(t *testing.T) {
	gw, fg := newInlineGW(t)
	fg.seed("gw-a", "running")
	m := NewManager(gw, ManagerOptions{WorkspaceDir: "/ws", SSHUser: "root"})
	const token = "persisted-sandbox-token"
	sb, err := m.AttachWithToken(context.Background(), "gw-a", token)
	if err != nil {
		t.Fatal(err)
	}
	if sb.creds().password != token {
		t.Fatal("attach with token did not set the data-plane password")
	}
	plain, err := m.Attach(context.Background(), "gw-a")
	if err != nil {
		t.Fatal(err)
	}
	if plain.creds().password != "" {
		t.Fatal("attach without a token used a password")
	}

	looked := NewManager(gw, ManagerOptions{DataPlanePassword: func(id string) string {
		if id == "gw-a" {
			return token
		}
		return ""
	}})
	got, err := looked.Attach(context.Background(), "gw-a")
	if err != nil {
		t.Fatal(err)
	}
	if got.creds().password != token {
		t.Fatal("attach lookup did not use the sandbox token")
	}
}

func TestDataPlanePathsCarrySandboxToken(t *testing.T) {
	const token = "dataplane-sandbox-token"
	var mu sync.Mutex
	var seen []string
	restore := SetDataPlaneAuthObserver(func(p string) {
		mu.Lock()
		seen = append(seen, p)
		mu.Unlock()
	})
	defer restore()
	restoreHook := SetExecHook(func(context.Context, string, int, string, io.Reader) ([]byte, error) {
		return []byte("ok\n"), nil
	})
	defer restoreHook()

	sb := &Sandbox{ID: "sb", SSHHost: "h", SSHPort: 22, Password: token, WorkspaceDir: "/root/workspace"}
	if _, err := sb.ReadFile(context.Background(), "a.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := sb.Exec(context.Background(), time.Second, "echo", "hi"); err != nil {
		t.Fatal(err)
	}
	if err := sb.WriteFile(context.Background(), "a.txt", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := (&Manager{}).Reprime(context.Background(), sb, Spec{}, nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !containsString(seen, token) {
		t.Fatal("file, exec, or reprime dial omitted the sandbox token")
	}
	for _, p := range seen {
		if p != "" && p != token {
			t.Fatal("data-plane dial used a different password")
		}
	}
}

func TestEnsureRuntimeCarriesSandboxToken(t *testing.T) {
	const token = "runtime-sandbox-token"
	m, _, _ := newRuntimeManager(t)
	f := &runtimeSSH{replies: map[string]func() ([]byte, error){"version": reply("old\n", nil)}}
	installSSH(t, f)
	var mu sync.Mutex
	var seen []string
	restore := SetDataPlaneAuthObserver(func(p string) {
		mu.Lock()
		seen = append(seen, p)
		mu.Unlock()
	})
	defer restore()
	sb := &Sandbox{ID: "sb-rt", SSHHost: "h", SSHPort: 22, Password: token}
	if err := m.EnsureRuntime(context.Background(), sb); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !containsString(seen, token) {
		t.Fatal("runtime update omitted the sandbox token")
	}
}

func TestManagerExecUsesLookedUpToken(t *testing.T) {
	gw, fg := newInlineGW(t)
	fg.seed("gw-exec", "running")
	const token = "exec-sandbox-token"
	m := NewManager(gw, ManagerOptions{DataPlanePassword: func(id string) string {
		if id == "gw-exec" {
			return token
		}
		return ""
	}})
	var mu sync.Mutex
	var seen []string
	restore := SetDataPlaneAuthObserver(func(p string) {
		mu.Lock()
		seen = append(seen, p)
		mu.Unlock()
	})
	defer restore()
	restoreHook := SetExecHook(func(context.Context, string, int, string, io.Reader) ([]byte, error) {
		return []byte("ok"), nil
	})
	defer restoreHook()
	out, err := m.Exec(context.Background(), "gw-exec", time.Second, "echo", "hi")
	if err != nil || out != "ok" {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !containsString(seen, token) {
		t.Fatal("Exec omitted the looked-up sandbox token")
	}
}

type authServer struct {
	host     string
	port     int
	password string
	failHeal bool

	mu     sync.Mutex
	accept map[string]bool
	lines  []string
}

func startAuthServer(t *testing.T, password string, lines []string) *authServer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	s := &authServer{host: host, port: port, password: password, accept: map[string]bool{}, lines: append([]string{}, lines...)}
	for _, line := range lines {
		s.accept[line] = true
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn, hostSigner)
		}
	}()
	return s
}

func (s *authServer) count(line string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, have := range s.lines {
		if have == line {
			n++
		}
	}
	return n
}

func (s *authServer) acceptOnly(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accept = map[string]bool{line: true}
}

func (s *authServer) setFailHeal(v bool) {
	s.mu.Lock()
	s.failHeal = v
	s.mu.Unlock()
}

func (s *authServer) healFailed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failHeal
}

func (s *authServer) serve(conn net.Conn, hostSigner ssh.Signer) {
	defer conn.Close()
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
			s.mu.Lock()
			ok := s.accept[line]
			s.mu.Unlock()
			if ok {
				return &ssh.Permissions{}, nil
			}
			return nil, errRejected
		},
		PasswordCallback: func(_ ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if s.password != "" && subtle.ConstantTimeCompare(pass, []byte(s.password)) == 1 {
				return &ssh.Permissions{}, nil
			}
			return nil, errRejected
		},
	}
	cfg.AddHostKey(hostSigner)
	sconn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	defer sconn.Close()
	go ssh.DiscardRequests(reqs)
	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(ssh.UnknownChannelType, "unknown")
			continue
		}
		ch, reqs, err := newCh.Accept()
		if err != nil {
			continue
		}
		go s.session(ch, reqs)
	}
}

func (s *authServer) session(ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()
	for req := range reqs {
		if req.Type != "exec" {
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
			continue
		}
		var payload struct{ Command string }
		if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
			continue
		}
		if req.WantReply {
			_ = req.Reply(true, nil)
		}
		body, _ := io.ReadAll(ch)
		code := uint32(0)
		if strings.Contains(payload.Command, "authorized_keys") {
			if s.healFailed() {
				code = 1
			} else {
				s.appendLine(strings.TrimSpace(string(body)))
			}
		}
		_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{code}))
		return
	}
}

func (s *authServer) appendLine(line string) {
	if line == "" {
		return
	}
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, have := range s.lines {
		if have == line {
			return
		}
	}
	s.lines = append(s.lines, line)
	s.accept[line] = true
}

var errRejected = errString("rejected")

type errString string

func (e errString) Error() string { return string(e) }

func mustSigner(t *testing.T) (ssh.Signer, string) {
	t.Helper()
	signer, line, _ := mustSignerPEM(t)
	return signer, line
}

func mustSignerPEM(t *testing.T) (ssh.Signer, string, string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	return signer, line, string(pem.EncodeToMemory(block))
}

func leaked(msg, secret, wrong, pemText string) bool {
	if strings.Contains(msg, secret) || strings.Contains(msg, wrong) || strings.Contains(msg, "PRIVATE KEY") {
		return true
	}
	if pemText != "" && strings.Contains(msg, pemText) {
		return true
	}
	return false
}

func containsString(all []string, want string) bool {
	for _, s := range all {
		if s == want {
			return true
		}
	}
	return false
}
