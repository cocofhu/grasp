package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/ssh"
)

// The gateway control plane does not proxy data-plane traffic (exec, files,
// terminal). approving reaches each sandbox directly over SSH (the universal
// image runs sshd on :22). The manager injects one shared public key via the
// SSH_KEY env at create time and authenticates with the matching private key.
// When that key is refused, the dial retries with that sandbox's own token.

// pinnedHostKey is the host key last accepted for one SSH address, plus the
// sandbox that presented it. A later sandbox published on the same address
// replaces the pin; the same sandbox must keep presenting this key.
type pinnedHostKey struct {
	sandboxID string
	key       ssh.PublicKey
}

// hostKeyCache stores TOFU host keys keyed by "host:port" for ephemeral sandboxes.
type hostKeyCache struct {
	mu   sync.Mutex
	keys map[string]pinnedHostKey
}

func newHostKeyCache() *hostKeyCache {
	return &hostKeyCache{keys: make(map[string]pinnedHostKey)}
}

func (c *hostKeyCache) get(addr string) ssh.PublicKey {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.keys[addr].key
}

// accept trusts key for addr. The first key is pinned to sandboxID. A different
// sandbox publishing the same address replaces the pin. The same sandbox (or a
// dial with no sandbox id) must present the pinned key.
func (c *hostKeyCache) accept(addr, sandboxID string, key ssh.PublicKey) error {
	if key == nil {
		return fmt.Errorf("ssh: empty host key from %s", addr)
	}
	if c == nil {
		return fmt.Errorf("ssh: host key cache not configured")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.keys == nil {
		c.keys = make(map[string]pinnedHostKey)
	}
	prev, ok := c.keys[addr]
	if !ok || (sandboxID != "" && prev.sandboxID != "" && prev.sandboxID != sandboxID) {
		c.keys[addr] = pinnedHostKey{sandboxID: sandboxID, key: key}
		return nil
	}
	if !bytes.Equal(prev.key.Marshal(), key.Marshal()) {
		return ssh.FixedHostKey(prev.key)("", nil, key)
	}
	if prev.sandboxID == "" && sandboxID != "" {
		prev.sandboxID = sandboxID
		c.keys[addr] = prev
	}
	return nil
}

// forget drops every pin recorded for sandboxID. The next sandbox published on
// that address can trust its own host key.
func (c *hostKeyCache) forget(sandboxID string) {
	if c == nil || sandboxID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for addr, pinned := range c.keys {
		if pinned.sandboxID == sandboxID {
			delete(c.keys, addr)
		}
	}
}

// sshCreds is the SSH connection material for one sandbox: its reachable
// host:port, the shared signer, and that sandbox's token as a password.
type sshCreds struct {
	host      string
	port      int
	signer    ssh.Signer
	authKey   string // authorized_keys line for the current signer
	password  string // this sandbox's token; empty skips password auth
	user      string
	sandboxID string // gateway sandbox id; empty keeps the pin strict
	hostKeys  *hostKeyCache
}

// authObserver records the password passed into a data-plane dial. Tests only.
var (
	authObserverMu sync.Mutex
	authObserver   func(string)
)

// SetDataPlaneAuthObserver installs a test hook invoked with the password
// handed to a data-plane operation. The hook must not log that password.
func SetDataPlaneAuthObserver(fn func(string)) func() {
	authObserverMu.Lock()
	prev := authObserver
	authObserver = fn
	authObserverMu.Unlock()
	return func() {
		authObserverMu.Lock()
		authObserver = prev
		authObserverMu.Unlock()
	}
}

func (c sshCreds) noteAuth() {
	authObserverMu.Lock()
	fn := authObserver
	authObserverMu.Unlock()
	if fn != nil {
		fn(c.password)
	}
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

func (c sshCreds) addrKey() string {
	return fmt.Sprintf("%s:%d", c.host, c.port)
}

// hostKeyCallback implements TOFU: the first handshake records the key. A
// different sandbox on the same address replaces it. The same sandbox is held
// to the recorded key.
func (c sshCreds) hostKeyCallback() ssh.HostKeyCallback {
	if c.hostKeys == nil {
		return nil
	}
	addr := c.addrKey()
	sandboxID := c.sandboxID
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		return c.hostKeys.accept(addr, sandboxID, key)
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

// authorizeKeyShell appends the public key read from stdin to root's
// authorized_keys. A line that is already present is left as-is.
const authorizeKeyShell = `set -eu
umask 077
mkdir -p /root/.ssh
chmod 700 /root/.ssh
f=/root/.ssh/authorized_keys
touch "$f"
chmod 600 "$f"
IFS= read -r key || true
[ -n "$key" ]
grep -qxF -e "$key" "$f" && exit 0
printf "%s\n" "$key" >> "$f"
`

// redactSSHText removes a sandbox token and any private-key PEM from a
// dial error or log line. Short strings are left alone so ordinary SSH
// wording is not rewritten.
func redactSSHText(msg, secret string) string {
	secret = strings.TrimSpace(secret)
	if len(secret) >= 8 {
		msg = strings.ReplaceAll(msg, secret, "[redacted]")
	}
	if strings.Contains(msg, "PRIVATE KEY") || strings.Contains(msg, "-----BEGIN ") {
		return "ssh authentication failed"
	}
	return msg
}

func (c sshCreds) redactErr(err error) error {
	if err == nil {
		return nil
	}
	msg := redactSSHText(err.Error(), c.password)
	if msg == err.Error() {
		return err
	}
	return errors.New(msg)
}

func sshAuthFailed(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "unable to authenticate") || strings.Contains(msg, "no supported methods")
}

// dial opens an SSH client to the sandbox. Public key is tried first. When
// that is refused and this sandbox has a token, the token is used as the root
// password and the current public key is appended to authorized_keys.
// Callers must Close the client. A failed append does not close it.
func (c sshCreds) dial(ctx context.Context) (*ssh.Client, error) {
	c.noteAuth()
	if c.host == "" || c.port == 0 {
		return nil, fmt.Errorf("ssh: sandbox has no reachable ssh endpoint")
	}
	if c.signer != nil {
		keyOnly := c
		keyOnly.password = ""
		cli, err := keyOnly.dialOnce(ctx)
		if err == nil {
			return cli, nil
		}
		if strings.TrimSpace(c.password) == "" || !sshAuthFailed(err) {
			return nil, c.redactErr(err)
		}
	}
	if strings.TrimSpace(c.password) == "" {
		cli, err := c.dialOnce(ctx)
		if err != nil {
			return nil, c.redactErr(err)
		}
		return cli, nil
	}
	pw := c
	pw.signer = nil
	cli, err := pw.dialOnce(ctx)
	if err != nil {
		return nil, c.redactErr(err)
	}
	if strings.TrimSpace(c.authKey) != "" {
		if herr := c.authorizeCurrentKey(ctx, cli); herr != nil {
			log.Warn().Str("addr", c.addrKey()).Msg("ssh authorized_keys update failed; session kept")
		}
	}
	return cli, nil
}

// dialOnce opens one SSH connection with whatever auth methods clientConfig
// currently includes. Callers must Close the client.
func (c sshCreds) dialOnce(ctx context.Context) (*ssh.Client, error) {
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

// authorizeCurrentKey appends the current public key on an already
// authenticated connection. Failure leaves the connection usable.
func (c sshCreds) authorizeCurrentKey(ctx context.Context, cli *ssh.Client) error {
	key := strings.TrimSpace(c.authKey)
	if key == "" || cli == nil {
		return nil
	}
	sess, err := cli.NewSession()
	if err != nil {
		return fmt.Errorf("append authorized key failed")
	}
	defer func() { _ = sess.Close() }()
	sess.Stdin = strings.NewReader(key + "\n")
	done := make(chan error, 1)
	go func() {
		done <- sess.Run("sh -c " + shellQuote(authorizeKeyShell))
	}()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("append authorized key failed")
		}
		return nil
	case <-ctx.Done():
		_ = sess.Close()
		return fmt.Errorf("append authorized key failed")
	case <-timer.C:
		_ = sess.Close()
		return fmt.Errorf("append authorized key failed")
	}
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
	c.noteAuth()
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
	c.noteAuth()
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
