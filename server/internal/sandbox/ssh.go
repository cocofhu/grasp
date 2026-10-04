package sandbox

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// The gateway control plane does not proxy data-plane traffic (exec, files,
// terminal). approving reaches each sandbox directly over SSH (the universal
// image runs sshd on :22). The manager injects a generated public key via the
// SSH_KEY env at create time and authenticates with the matching private key,
// so no password ever crosses the wire.

// hostKeyCache stores TOFU host keys keyed by "host:port" for ephemeral sandboxes.
type hostKeyCache struct {
	mu   sync.Mutex
	keys map[string]ssh.PublicKey
}

func newHostKeyCache() *hostKeyCache {
	return &hostKeyCache{keys: make(map[string]ssh.PublicKey)}
}

func (c *hostKeyCache) get(addr string) ssh.PublicKey {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.keys[addr]
}

func (c *hostKeyCache) set(addr string, key ssh.PublicKey) {
	if c == nil || key == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.keys == nil {
		c.keys = make(map[string]ssh.PublicKey)
	}
	c.keys[addr] = key
}

// sshCreds is the SSH connection material for one sandbox: its reachable
// host:port plus the shared signer (and an optional password fallback).
type sshCreds struct {
	host     string
	port     int
	signer   ssh.Signer
	password string
	user     string
	hostKeys *hostKeyCache
}

// execHook is a test seam for the sandbox data plane. When non-nil it answers
// every command execution (Sandbox exec/read/write/file-exists and
// Manager.Exec) instead of dialing SSH. host/port identify the target sandbox;
// command is the fully assembled remote shell command; stdin is non-nil for
// writes. Production leaves it nil so real SSH is used.
var execHook func(ctx context.Context, host string, port int, command string, stdin io.Reader) ([]byte, error)

// SetExecHook installs a data-plane exec interceptor (tests only) and returns a
// restore func that reinstates the previous hook. Passing nil clears it.
func SetExecHook(fn func(ctx context.Context, host string, port int, command string, stdin io.Reader) ([]byte, error)) func() {
	prev := execHook
	execHook = fn
	return func() { execHook = prev }
}

// generateSSHKey mints an ephemeral ed25519 keypair for sandbox data-plane
// access. It returns the signer (private half, used to authenticate) and the
// authorized_keys line (public half, injected into the sandbox via SSH_KEY).
func generateSSHKey() (ssh.Signer, string, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", fmt.Errorf("generate ed25519 key: %w", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, "", fmt.Errorf("new signer: %w", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, "", fmt.Errorf("new public key: %w", err)
	}
	authorized := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	return signer, authorized, nil
}

func (c sshCreds) addrKey() string {
	return fmt.Sprintf("%s:%d", c.host, c.port)
}

// hostKeyCallback implements TOFU: first successful handshake records the key;
// later dials use ssh.FixedHostKey and reject mismatches.
func (c sshCreds) hostKeyCallback() ssh.HostKeyCallback {
	if c.hostKeys == nil {
		return nil
	}
	addr := c.addrKey()
	if known := c.hostKeys.get(addr); known != nil {
		return ssh.FixedHostKey(known)
	}
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		if key == nil {
			return fmt.Errorf("ssh: empty host key from %s", addr)
		}
		// Race-safe: if another dial already pinned a key, require a match.
		if known := c.hostKeys.get(addr); known != nil {
			return ssh.FixedHostKey(known)("", nil, key)
		}
		c.hostKeys.set(addr, key)
		return nil
	}
}

func (c sshCreds) clientConfig() *ssh.ClientConfig {
	user := c.user
	if user == "" {
		user = "root"
	}
	var auths []ssh.AuthMethod
	if c.signer != nil {
		auths = append(auths, ssh.PublicKeys(c.signer))
	}
	if c.password != "" {
		auths = append(auths, ssh.Password(c.password))
	}
	cb := c.hostKeyCallback()
	if cb == nil {
		// No shared cache (should not happen in production): refuse rather than
		// fall back to InsecureIgnoreHostKey.
		cb = func(_ string, _ net.Addr, _ ssh.PublicKey) error {
			return fmt.Errorf("ssh: host key cache not configured")
		}
	}
	return &ssh.ClientConfig{
		User:            user,
		Auth:            auths,
		HostKeyCallback: cb,
		Timeout:         15 * time.Second,
	}
}

// dial opens an SSH client to the sandbox. Callers must Close it.
func (c sshCreds) dial(ctx context.Context) (*ssh.Client, error) {
	if c.host == "" || c.port == 0 {
		return nil, fmt.Errorf("ssh: sandbox has no reachable ssh endpoint")
	}
	addr := fmt.Sprintf("%s:%d", c.host, c.port)
	d := net.Dialer{Timeout: 15 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, c.clientConfig())
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ssh handshake %s: %w", addr, err)
	}
	return ssh.NewClient(sshConn, chans, reqs), nil
}

// waitReady polls until sshd accepts an authenticated connection or maxWait
// elapses. Used after create so the first exec doesn't race the sandbox boot.
// When execHook is installed (unit tests), dial is skipped — the hook answers
// subsequent run/runInput immediately.
func (c sshCreds) waitReady(ctx context.Context, maxWait time.Duration) error {
	if execHook != nil {
		return nil
	}
	deadline := time.Now().Add(maxWait)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		cli, err := c.dial(ctx)
		if err == nil {
			_ = cli.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("ssh not ready after %s: %w", maxWait, err)
		}
		time.Sleep(2 * time.Second)
	}
}

// run executes a validated safeCmd on the sandbox and returns combined
// stdout+stderr. A non-zero exit is returned as an error carrying the output.
func (c sshCreds) run(ctx context.Context, timeout time.Duration, cmd safeCmd) ([]byte, error) {
	command, err := cmd.render()
	if err != nil {
		return nil, err
	}
	if execHook != nil {
		return execHook(ctx, c.host, c.port, command, nil)
	}
	cli, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cli.Close() }()

	sess, err := cli.NewSession()
	if err != nil {
		return nil, fmt.Errorf("ssh session: %w", err)
	}
	defer func() { _ = sess.Close() }()

	var buf bytes.Buffer
	sess.Stdout = &buf
	sess.Stderr = &buf

	done := make(chan error, 1)
	if err := sess.Start(command); err != nil {
		return nil, fmt.Errorf("ssh start: %w", err)
	}
	go func() { done <- sess.Wait() }()

	tctx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		tctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	select {
	case err := <-done:
		return buf.Bytes(), err
	case <-tctx.Done():
		_ = sess.Signal(ssh.SIGKILL)
		_ = sess.Close()
		return buf.Bytes(), tctx.Err()
	}
}

// runInput executes a validated safeCmd feeding stdin from r (used for file writes).
func (c sshCreds) runInput(ctx context.Context, timeout time.Duration, cmd safeCmd, r io.Reader) ([]byte, error) {
	command, err := cmd.render()
	if err != nil {
		return nil, err
	}
	if execHook != nil {
		return execHook(ctx, c.host, c.port, command, r)
	}
	cli, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cli.Close() }()

	sess, err := cli.NewSession()
	if err != nil {
		return nil, fmt.Errorf("ssh session: %w", err)
	}
	defer func() { _ = sess.Close() }()

	var buf bytes.Buffer
	sess.Stdout = &buf
	sess.Stderr = &buf
	sess.Stdin = r

	tctx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		tctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	done := make(chan error, 1)
	go func() { done <- sess.Run(command) }()
	select {
	case err := <-done:
		return buf.Bytes(), err
	case <-tctx.Done():
		_ = sess.Close()
		return buf.Bytes(), tctx.Err()
	}
}

// SSHTerminal is an interactive PTY-backed shell session over SSH. It replaces
// the old docker-exec PTY: the terminal handler reads/writes the combined
// stream and resizes the window; Close/Wait end the session.
type SSHTerminal struct {
	client *ssh.Client
	sess   *ssh.Session
	stdin  io.WriteCloser
	stdout io.Reader
}

// Read returns terminal output (merged stdout/stderr via the PTY).
func (t *SSHTerminal) Read(p []byte) (int, error) { return t.stdout.Read(p) }

// Write sends keystrokes to the shell.
func (t *SSHTerminal) Write(p []byte) (int, error) { return t.stdin.Write(p) }

// Resize adjusts the PTY window size.
func (t *SSHTerminal) Resize(rows, cols uint16) error {
	if t.sess == nil {
		return nil
	}
	return t.sess.WindowChange(int(rows), int(cols))
}

// Wait blocks until the shell exits.
func (t *SSHTerminal) Wait() error {
	if t.sess == nil {
		return nil
	}
	return t.sess.Wait()
}

// Close tears down the session and the underlying connection.
func (t *SSHTerminal) Close() error {
	if t.sess != nil {
		_ = t.sess.Close()
	}
	if t.client != nil {
		return t.client.Close()
	}
	return nil
}

// openTerminal starts an interactive login shell with a PTY over SSH.
// Non-empty argv is validated via newSafeCmd before Session.Start.
func (c sshCreds) openTerminal(ctx context.Context, argv []string) (*SSHTerminal, error) {
	cli, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	sess, err := cli.NewSession()
	if err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("ssh session: %w", err)
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}
	if err := sess.RequestPty("xterm-256color", 40, 120, modes); err != nil {
		_ = sess.Close()
		_ = cli.Close()
		return nil, fmt.Errorf("request pty: %w", err)
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		_ = sess.Close()
		_ = cli.Close()
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		_ = sess.Close()
		_ = cli.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	sess.Stderr = nil // merged into the PTY stdout
	t := &SSHTerminal{client: cli, sess: sess, stdin: stdin, stdout: stdout}
	if len(argv) > 0 {
		cmd, err := newSafeCmd(argv...)
		if err != nil {
			_ = t.Close()
			return nil, fmt.Errorf("start shell: %w", err)
		}
		command, err := cmd.render()
		if err != nil {
			_ = t.Close()
			return nil, fmt.Errorf("start shell: %w", err)
		}
		if err := sess.Start(command); err != nil {
			_ = t.Close()
			return nil, fmt.Errorf("start shell: %w", err)
		}
	} else {
		if err := sess.Shell(); err != nil {
			_ = t.Close()
			return nil, fmt.Errorf("start shell: %w", err)
		}
	}
	return t, nil
}
