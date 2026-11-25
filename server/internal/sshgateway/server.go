// Package sshgateway lets local IDEs (VS Code / Cursor Remote-SSH, plain
// OpenSSH) reach sandboxes through one public SSH endpoint. The SSH user name
// selects the sandbox ("sbx-<id>"), the client authenticates with a public key
// registered in Grasp, and the gateway relays the session to the sandbox sshd
// over the platform's own root connection.
package sshgateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/ssh"
)

// Identity is the Grasp account that owns an authenticated key.
type Identity struct {
	Username    string
	Fingerprint string
}

// Authorizer resolves registered public keys.
type Authorizer interface {
	// AuthorizeKey returns the account owning key, or an error when the key
	// is not registered or its account no longer exists.
	AuthorizeKey(key ssh.PublicKey) (Identity, error)
}

// Sandboxes is the sandbox side the gateway relays to.
type Sandboxes interface {
	// AuthorizeSSH reports whether username may enter sandbox id. The error
	// text is shown to the client, so it must not reveal whether id exists.
	AuthorizeSSH(username string, id uint) error
	// DialSSH returns a root SSH client to sandbox id, waking it if needed,
	// and a release that ends its hold on idle reclaim. The hold is taken
	// before idle reclaim can run again, so a fresh connection is never paused.
	DialSSH(ctx context.Context, id uint) (*ssh.Client, func(), error)
	// TouchActivity records access for display.
	TouchActivity(id uint)
}

// Options configures a Server. Zero values take the defaults below.
type Options struct {
	HostKeyPath string
	// MaxConns caps authenticated connections; MaxPendingHandshakes and
	// MaxPendingPerSource cap connections still in the SSH handshake, so
	// unauthenticated clients cannot take the authenticated slots.
	MaxConns             int
	MaxConnsPerSandbox   int
	MaxPendingHandshakes int
	MaxPendingPerSource  int
	// AuthFailLimit failed handshakes per source address within
	// AuthFailWindow block that address until the window passes.
	AuthFailLimit  int
	AuthFailWindow time.Duration
	// DialTimeout bounds waking and dialing the sandbox.
	DialTimeout        time.Duration
	HandshakeTimeout   time.Duration
	RevalidateInterval time.Duration
	TouchInterval      time.Duration
	// RefuseLinger is how long a refused client may take to disconnect.
	RefuseLinger time.Duration
}

func (o *Options) defaults() {
	if o.MaxConns <= 0 {
		o.MaxConns = 200
	}
	if o.MaxConnsPerSandbox <= 0 {
		o.MaxConnsPerSandbox = 10
	}
	if o.MaxPendingHandshakes <= 0 {
		o.MaxPendingHandshakes = 64
	}
	if o.MaxPendingPerSource <= 0 {
		o.MaxPendingPerSource = 8
	}
	if o.AuthFailLimit == 0 {
		o.AuthFailLimit = 20
	}
	if o.AuthFailWindow <= 0 {
		o.AuthFailWindow = 5 * time.Minute
	}
	if o.DialTimeout <= 0 {
		o.DialTimeout = 3 * time.Minute
	}
	if o.HandshakeTimeout <= 0 {
		o.HandshakeTimeout = 30 * time.Second
	}
	if o.RevalidateInterval <= 0 {
		o.RevalidateInterval = time.Minute
	}
	if o.TouchInterval <= 0 {
		o.TouchInterval = time.Minute
	}
	if o.RefuseLinger <= 0 {
		o.RefuseLinger = 30 * time.Second
	}
}

// Permission extension keys carried from authentication to the connection.
const (
	extUser        = "grasp-user"
	extFingerprint = "grasp-fingerprint"
	extKey         = "grasp-key"
	extSandbox     = "grasp-sandbox"
)

// Server is an SSH gateway. Create it with New and run Serve.
type Server struct {
	opts    Options
	auth    Authorizer
	sbx     Sandboxes
	config  *ssh.ServerConfig
	hostKey ssh.Signer
	limiter *failLimiter

	ctx    context.Context
	cancel context.CancelFunc

	mu        sync.Mutex
	listeners []net.Listener
	conns     map[*gatewayConn]struct{}
	total     int
	bySandbox map[uint]int
	pending   int
	bySource  map[string]int
	closed    bool
	wg        sync.WaitGroup
	nextID    atomic.Uint64
}

type gatewayConn struct {
	id       uint64
	identity Identity
	key      ssh.PublicKey
	sandbox  uint
	remote   string
	conn     *ssh.ServerConn
	reason   atomic.Value
	// ready is set once access was granted; revalidation skips connections
	// still being checked, which explain their own refusal.
	ready atomic.Bool
}

func (c *gatewayConn) close(reason string) {
	c.reason.CompareAndSwap(nil, reason)
	_ = c.conn.Close()
}

// New loads (or creates) the host key and prepares a Server.
func New(opts Options, auth Authorizer, sbx Sandboxes) (*Server, error) {
	if auth == nil || sbx == nil {
		return nil, errors.New("ssh gateway: authorizer and sandboxes are required")
	}
	opts.defaults()
	hostKey, err := loadOrCreateHostKey(opts.HostKeyPath)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		opts: opts, auth: auth, sbx: sbx, hostKey: hostKey,
		limiter: newFailLimiter(opts.AuthFailLimit, opts.AuthFailWindow),
		ctx:     ctx, cancel: cancel,
		conns: map[*gatewayConn]struct{}{}, bySandbox: map[uint]int{}, bySource: map[string]int{},
	}
	s.config = &ssh.ServerConfig{
		ServerVersion:     "SSH-2.0-GraspGateway",
		MaxAuthTries:      6,
		PublicKeyCallback: s.authenticate,
	}
	s.config.AddHostKey(hostKey)
	s.wg.Add(1)
	go s.revalidateLoop()
	return s, nil
}

// HostKeyFingerprint is the SHA256 fingerprint clients see on first connect.
func (s *Server) HostKeyFingerprint() string {
	return ssh.FingerprintSHA256(s.hostKey.PublicKey())
}

// ParseUser maps an SSH user name to a sandbox id. Accepted forms are
// "sbx-<id>" and "sbx-<id>+root"; other sandbox logins are reserved.
func ParseUser(user string) (uint, error) {
	name, login, hasLogin := strings.Cut(user, "+")
	if hasLogin && login != "root" {
		return 0, fmt.Errorf("only root login is supported, got %q", login)
	}
	raw, ok := strings.CutPrefix(name, "sbx-")
	if !ok {
		return 0, fmt.Errorf("user must be sbx-<sandbox id>, got %q", user)
	}
	id, err := strconv.ParseUint(raw, 10, 32)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("invalid sandbox id in %q", user)
	}
	return uint(id), nil
}

func (s *Server) authenticate(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	id, err := ParseUser(meta.User())
	if err != nil {
		return nil, err
	}
	who, err := s.auth.AuthorizeKey(key)
	if err != nil {
		return nil, err
	}
	return &ssh.Permissions{Extensions: map[string]string{
		extUser:        who.Username,
		extFingerprint: who.Fingerprint,
		extKey:         string(key.Marshal()),
		extSandbox:     strconv.FormatUint(uint64(id), 10),
	}}, nil
}

// Serve accepts connections on ln until Close. It returns nil after Close.
func (s *Server) Serve(ln net.Listener) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = ln.Close()
		return nil
	}
	s.listeners = append(s.listeners, ln)
	s.mu.Unlock()
	for {
		nc, err := ln.Accept()
		if err != nil {
			if s.ctx.Err() != nil {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			return err
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handle(nc)
		}()
	}
}

// Close stops listening and disconnects every client.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.cancel()
	for _, ln := range s.listeners {
		_ = ln.Close()
	}
	for c := range s.conns {
		c.close("gateway shutdown")
	}
	s.mu.Unlock()
	s.wg.Wait()
	return nil
}

// DisconnectFingerprint closes every connection authenticated with the key.
func (s *Server) DisconnectFingerprint(fingerprint string) int {
	return s.disconnect(func(c *gatewayConn) bool { return c.identity.Fingerprint == fingerprint }, "key removed")
}

// DisconnectUser closes every connection owned by username.
func (s *Server) DisconnectUser(username string) int {
	return s.disconnect(func(c *gatewayConn) bool { return c.identity.Username == username }, "account removed")
}

// Connections reports open connections to sandbox id.
func (s *Server) Connections(id uint) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bySandbox[id]
}

func (s *Server) disconnect(match func(*gatewayConn) bool, reason string) int {
	s.mu.Lock()
	var hit []*gatewayConn
	for c := range s.conns {
		if match(c) {
			hit = append(hit, c)
		}
	}
	s.mu.Unlock()
	for _, c := range hit {
		c.close(reason)
	}
	return len(hit)
}

func (s *Server) revalidateLoop() {
	defer s.wg.Done()
	t := time.NewTicker(s.opts.RevalidateInterval)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			s.revalidate()
		}
	}
}

// revalidate drops connections whose key, account or sandbox access went
// away, including changes made outside the API (config reload, a deleted
// or reassigned session).
func (s *Server) revalidate() {
	s.mu.Lock()
	conns := make([]*gatewayConn, 0, len(s.conns))
	for c := range s.conns {
		if c.ready.Load() {
			conns = append(conns, c)
		}
	}
	s.mu.Unlock()
	for _, c := range conns {
		if err := s.authorized(c); err != nil {
			c.close(err.Error())
		}
	}
}

var errKeyRevoked = errors.New("key no longer authorized")

// authorized re-checks the key and the account's access to the sandbox.
func (s *Server) authorized(c *gatewayConn) error {
	who, err := s.auth.AuthorizeKey(c.key)
	if err != nil || who.Username != c.identity.Username {
		return errKeyRevoked
	}
	return s.sbx.AuthorizeSSH(c.identity.Username, c.sandbox)
}

// reservePending admits one connection into the SSH handshake.
func (s *Server) reservePending(source string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.pending >= s.opts.MaxPendingHandshakes || s.bySource[source] >= s.opts.MaxPendingPerSource {
		return false
	}
	s.pending++
	s.bySource[source]++
	return true
}

func (s *Server) releasePending(source string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending--
	if s.bySource[source] <= 1 {
		delete(s.bySource, source)
	} else {
		s.bySource[source]--
	}
}

func (s *Server) reserveTotal() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.total >= s.opts.MaxConns {
		return false
	}
	s.total++
	return true
}

func (s *Server) releaseTotal() {
	s.mu.Lock()
	s.total--
	s.mu.Unlock()
}

func (s *Server) register(c *gatewayConn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.bySandbox[c.sandbox] >= s.opts.MaxConnsPerSandbox {
		return false
	}
	s.bySandbox[c.sandbox]++
	s.conns[c] = struct{}{}
	return true
}

func (s *Server) unregister(c *gatewayConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.conns[c]; !ok {
		return
	}
	delete(s.conns, c)
	if s.bySandbox[c.sandbox] <= 1 {
		delete(s.bySandbox, c.sandbox)
	} else {
		s.bySandbox[c.sandbox]--
	}
}

func remoteHost(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}

func (s *Server) handle(nc net.Conn) {
	source := remoteHost(nc.RemoteAddr())
	if s.limiter.blocked(source) || !s.reservePending(source) {
		_ = nc.Close()
		return
	}
	_ = nc.SetDeadline(time.Now().Add(s.opts.HandshakeTimeout))
	sconn, chans, reqs, err := ssh.NewServerConn(nc, s.config)
	s.releasePending(source)
	if err != nil {
		s.limiter.fail(source)
		_ = nc.Close()
		return
	}
	_ = nc.SetDeadline(time.Time{})
	defer func() { _ = sconn.Close() }()
	go replyGlobalRequests(reqs)

	ext := sconn.Permissions.Extensions
	key, err := ssh.ParsePublicKey([]byte(ext[extKey]))
	sandboxID, perr := strconv.ParseUint(ext[extSandbox], 10, 32)
	if err != nil || perr != nil {
		return
	}
	c := &gatewayConn{
		id:       s.nextID.Add(1),
		identity: Identity{Username: ext[extUser], Fingerprint: ext[extFingerprint]},
		key:      key, sandbox: uint(sandboxID), remote: source, conn: sconn,
	}
	logger := log.With().Uint64("conn", c.id).Str("user", c.identity.Username).
		Str("fingerprint", c.identity.Fingerprint).Uint("sandbox", c.sandbox).Str("remote", source).Logger()
	if !s.reserveTotal() {
		logger.Warn().Msg("ssh gateway: connection limit reached")
		s.refuse(sconn, chans, "Grasp SSH 网关连接数已达上限，请稍后重试")
		return
	}
	defer s.releaseTotal()
	if !s.register(c) {
		s.refuse(sconn, chans, fmt.Sprintf("沙箱 %d 的连接数已达上限（%d）", c.sandbox, s.opts.MaxConnsPerSandbox))
		return
	}
	defer s.unregister(c)
	// Registered first, so a key deleted from here on also closes this
	// connection; checked again here for one deleted during the handshake.
	if err := s.authorized(c); err != nil {
		logger.Warn().Err(err).Msg("ssh gateway: access denied")
		// Unregistered so revalidation does not cut the explanation short.
		s.unregister(c)
		s.refuse(sconn, chans, err.Error())
		return
	}
	c.ready.Store(true)

	ctx, cancel := context.WithTimeout(s.ctx, s.opts.DialTimeout)
	upstream, release, err := s.sbx.DialSSH(ctx, c.sandbox)
	cancel()
	if err != nil {
		logger.Warn().Err(err).Msg("ssh gateway: sandbox unavailable")
		s.refuse(sconn, chans, fmt.Sprintf("无法连接沙箱 %d：%v", c.sandbox, err))
		return
	}
	defer release()
	defer func() { _ = upstream.Close() }()
	s.sbx.TouchActivity(c.sandbox)
	started := time.Now()
	logger.Info().Msg("ssh gateway: connected")

	done := make(chan struct{})
	defer close(done)
	go func() {
		_ = upstream.Wait()
		c.close("sandbox connection closed")
	}()
	go s.touchLoop(c.sandbox, done)

	var channels sync.WaitGroup
	for nch := range chans {
		if !forwardedChannels[nch.ChannelType()] {
			_ = nch.Reject(ssh.Prohibited, "channel type not supported by the Grasp gateway")
			continue
		}
		channels.Add(1)
		go func() {
			defer channels.Done()
			proxyChannel(nch, upstream)
		}()
	}
	_ = upstream.Close()
	channels.Wait()
	reason, _ := c.reason.Load().(string)
	if reason == "" {
		reason = "client disconnected"
	}
	logger.Info().Dur("duration", time.Since(started)).Str("reason", reason).Msg("ssh gateway: disconnected")
}

func (s *Server) touchLoop(id uint, done <-chan struct{}) {
	t := time.NewTicker(s.opts.TouchInterval)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			s.sbx.TouchActivity(id)
		}
	}
}

// replyGlobalRequests answers client global requests locally. keepalive and
// no-more-sessions only need a reply; tcpip-forward is not supported.
func replyGlobalRequests(reqs <-chan *ssh.Request) {
	for req := range reqs {
		if req.WantReply {
			_ = req.Reply(false, nil)
		}
	}
}

// refuse explains why the connection cannot proceed. Session channels get the
// message on stderr and exit status 1 so OpenSSH and IDE logs show it. The
// client closes the connection itself; closing it here first makes OpenSSH
// report a broken pipe and exit 255 instead of 1. The timer bounds the wait.
func (s *Server) refuse(conn *ssh.ServerConn, chans <-chan ssh.NewChannel, msg string) {
	timer := time.AfterFunc(s.opts.RefuseLinger, func() { _ = conn.Close() })
	defer timer.Stop()
	for nch := range chans {
		if nch.ChannelType() != "session" {
			_ = nch.Reject(ssh.ConnectionFailed, msg)
			continue
		}
		ch, reqs, err := nch.Accept()
		if err != nil {
			continue
		}
		go explain(ch, reqs, msg)
	}
}

func explain(ch ssh.Channel, reqs <-chan *ssh.Request, msg string) {
	defer func() { _ = ch.Close() }()
	for req := range reqs {
		switch req.Type {
		case "shell", "exec", "subsystem":
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
			_, _ = fmt.Fprintf(ch.Stderr(), "grasp: %s\r\n", msg)
			_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{1}))
			return
		default:
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
		}
	}
}
