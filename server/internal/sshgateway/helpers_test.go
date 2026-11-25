package sshgateway

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// fakeSandbox is a minimal sandbox sshd: exec, shell, sftp and direct-tcpip.
type fakeSandbox struct {
	addr string
	dir  string

	mu       sync.Mutex
	requests []string
}

func (f *fakeSandbox) record(kind string) {
	f.mu.Lock()
	f.requests = append(f.requests, kind)
	f.mu.Unlock()
}

func (f *fakeSandbox) saw(kind string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.requests {
		if r == kind {
			return true
		}
	}
	return false
}

func newSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func startFakeSandbox(t *testing.T) *fakeSandbox {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	f := &fakeSandbox{addr: ln.Addr().String(), dir: t.TempDir()}
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(newSigner(t))
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(nc, cfg)
		}
	}()
	return f
}

func (f *fakeSandbox) serve(nc net.Conn, cfg *ssh.ServerConfig) {
	sconn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		return
	}
	defer func() { _ = sconn.Close() }()
	go ssh.DiscardRequests(reqs)
	for nch := range chans {
		switch nch.ChannelType() {
		case "session":
			ch, reqs, err := nch.Accept()
			if err == nil {
				go f.session(ch, reqs)
			}
		case "direct-tcpip":
			var target struct {
				Host       string
				Port       uint32
				OriginHost string
				OriginPort uint32
			}
			if err := ssh.Unmarshal(nch.ExtraData(), &target); err != nil {
				_ = nch.Reject(ssh.ConnectionFailed, "bad payload")
				continue
			}
			conn, err := net.Dial("tcp", net.JoinHostPort(target.Host, strconv.Itoa(int(target.Port))))
			if err != nil {
				_ = nch.Reject(ssh.ConnectionFailed, err.Error())
				continue
			}
			ch, reqs, err := nch.Accept()
			if err != nil {
				_ = conn.Close()
				continue
			}
			go ssh.DiscardRequests(reqs)
			go func() {
				defer func() { _ = ch.Close() }()
				go func() { _, _ = io.Copy(conn, ch); _ = conn.(*net.TCPConn).CloseWrite() }()
				_, _ = io.Copy(ch, conn)
				_ = conn.Close()
			}()
		default:
			_ = nch.Reject(ssh.UnknownChannelType, "unsupported")
		}
	}
}

func exitStatus(ch ssh.Channel, code uint32) {
	_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{code}))
}

func (f *fakeSandbox) session(ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer func() { _ = ch.Close() }()
	for req := range reqs {
		f.record(req.Type)
		switch req.Type {
		case "exec":
			var p struct{ Command string }
			_ = ssh.Unmarshal(req.Payload, &p)
			_ = req.Reply(true, nil)
			go func() {
				defer func() { _ = ch.Close() }()
				f.exec(ch, p.Command)
			}()
		case "shell":
			_ = req.Reply(true, nil)
			go func() {
				defer func() { _ = ch.Close() }()
				_, _ = io.Copy(ch, ch)
				exitStatus(ch, 0)
			}()
		case "subsystem":
			var p struct{ Name string }
			_ = ssh.Unmarshal(req.Payload, &p)
			if p.Name != "sftp" {
				_ = req.Reply(false, nil)
				continue
			}
			_ = req.Reply(true, nil)
			go func() {
				defer func() { _ = ch.Close() }()
				srv, err := sftp.NewServer(ch, sftp.WithServerWorkingDirectory(f.dir))
				if err != nil {
					return
				}
				_ = srv.Serve()
				exitStatus(ch, 0)
			}()
		default:
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
		}
	}
}

func (f *fakeSandbox) exec(ch ssh.Channel, cmd string) {
	switch {
	case strings.HasPrefix(cmd, "exit "):
		code, _ := strconv.Atoi(strings.TrimPrefix(cmd, "exit "))
		exitStatus(ch, uint32(code))
	case cmd == "cat":
		_, _ = io.Copy(ch, ch)
		exitStatus(ch, 0)
	default:
		_, _ = fmt.Fprintf(ch, "out:%s\n", cmd)
		_, _ = fmt.Fprintf(ch.Stderr(), "err:%s\n", cmd)
		exitStatus(ch, 0)
	}
}

// fakeSandboxes dials the fake sandbox for every id it knows.
type fakeSandboxes struct {
	upstream *fakeSandbox

	mu       sync.Mutex
	dialErr  error
	denied   map[uint]bool // sandboxes the user may not enter
	active   map[uint]int
	acquired int
	touches  int
	dials    int
	dialGate chan struct{}
}

func (s *fakeSandboxes) AuthorizeSSH(_ string, id uint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.denied[id] {
		return fmt.Errorf("沙箱 %d 不存在或无权访问", id)
	}
	return nil
}

func (s *fakeSandboxes) deny(id uint, denied bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.denied == nil {
		s.denied = map[uint]bool{}
	}
	s.denied[id] = denied
}

func (s *fakeSandboxes) DialSSH(ctx context.Context, id uint) (*ssh.Client, func(), error) {
	s.mu.Lock()
	s.dials++
	err, gate := s.dialErr, s.dialGate
	s.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	if err != nil {
		return nil, nil, err
	}
	if id != 1 && id != 2 {
		return nil, nil, errors.New("not found")
	}
	cli, err := ssh.Dial("tcp", s.upstream.addr, &ssh.ClientConfig{
		User: "root", HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second,
	})
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	if s.active == nil {
		s.active = map[uint]int{}
	}
	s.active[id]++
	s.acquired++
	s.mu.Unlock()
	return cli, func() {
		s.mu.Lock()
		s.active[id]--
		s.mu.Unlock()
	}, nil
}

func (s *fakeSandboxes) dialCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dials
}

func (s *fakeSandboxes) TouchActivity(uint) {
	s.mu.Lock()
	s.touches++
	s.mu.Unlock()
}

func (s *fakeSandboxes) activeFor(id uint) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active[id]
}

// fakeAuth authorizes registered fingerprints. allowCalls > 0 makes every
// lookup after that many fail, as if the key were deleted mid-connection.
type fakeAuth struct {
	mu         sync.Mutex
	keys       map[string]string // fingerprint -> username
	calls      int
	allowCalls int
}

func (a *fakeAuth) add(key ssh.PublicKey, user string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.keys == nil {
		a.keys = map[string]string{}
	}
	a.keys[ssh.FingerprintSHA256(key)] = user
}

func (a *fakeAuth) remove(key ssh.PublicKey) {
	a.mu.Lock()
	delete(a.keys, ssh.FingerprintSHA256(key))
	a.mu.Unlock()
}

func (a *fakeAuth) AuthorizeKey(key ssh.PublicKey) (Identity, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	fp := ssh.FingerprintSHA256(key)
	user, ok := a.keys[fp]
	if !ok || (a.allowCalls > 0 && a.calls > a.allowCalls) {
		return Identity{}, errors.New("unknown key")
	}
	return Identity{Username: user, Fingerprint: fp}, nil
}

type harness struct {
	srv      *Server
	addr     string
	sandbox  *fakeSandbox
	sbx      *fakeSandboxes
	auth     *fakeAuth
	userKey  ssh.Signer
	hostPath string
}

func newHarness(t *testing.T, opts Options) *harness {
	t.Helper()
	upstream := startFakeSandbox(t)
	h := &harness{sandbox: upstream, sbx: &fakeSandboxes{upstream: upstream}, auth: &fakeAuth{}, userKey: newSigner(t)}
	h.auth.add(h.userKey.PublicKey(), "alice")
	if opts.HostKeyPath == "" {
		opts.HostKeyPath = filepath.Join(t.TempDir(), "host_key")
	}
	h.hostPath = opts.HostKeyPath
	srv, err := New(opts, h.auth, h.sbx)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	h.srv, h.addr = srv, ln.Addr().String()
	return h
}

func (h *harness) dial(t *testing.T, user string, signer ssh.Signer) (*ssh.Client, error) {
	t.Helper()
	return ssh.Dial("tcp", h.addr, &ssh.ClientConfig{
		User: user, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.FixedHostKey(h.srv.hostKey.PublicKey()), Timeout: 5 * time.Second,
	})
}

func (h *harness) mustDial(t *testing.T, user string) *ssh.Client {
	t.Helper()
	cli, err := h.dial(t, user, h.userKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	return cli
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
