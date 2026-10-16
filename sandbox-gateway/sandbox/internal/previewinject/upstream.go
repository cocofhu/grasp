package previewinject

import (
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// UpstreamHeader names the local port that served a proxied response, so
// Grasp can tell a switched upstream from a stale app still on PREVIEW_PORT.
const UpstreamHeader = "X-Grasp-Upstream"

// reservedPorts are sandbox services that must never sit behind the public,
// unauthenticated PREVIEW_PORT: sshd, VNC, noVNC, code-server, the backend,
// CDP and the injector itself.
var reservedPorts = map[int]bool{22: true, 5900: true, 6080: true, 8744: true, 8765: true, 9222: true, ListenPort: true}

// ValidUpstreamPort reports whether p may be named in the upstream file.
func ValidUpstreamPort(p int) bool {
	return p > 0 && p <= 65535 && !reservedPorts[p]
}

const (
	upstreamFileTTL  = time.Second
	upstreamAliveTTL = time.Second
	upstreamDialWait = 300 * time.Millisecond
)

// upstreamSelector picks the app port per request: the port in file when it
// is valid and accepting connections, else the fixed fallback. An app that
// stops on the switched port therefore never takes PREVIEW_PORT down with it.
type upstreamSelector struct {
	fallback *url.URL
	file     string
	alive    func(hostport string) bool
	now      func() time.Time

	mu        sync.Mutex
	readAt    time.Time
	modTime   time.Time
	size      int64
	port      int
	aliveAt   time.Time
	aliveHost string
	aliveOK   bool
}

func newUpstreamSelector(fallback *url.URL, file string) *upstreamSelector {
	return &upstreamSelector{
		fallback: fallback,
		file:     strings.TrimSpace(file),
		alive:    dialAlive,
		now:      time.Now,
	}
}

func dialAlive(hostport string) bool {
	c, err := net.DialTimeout("tcp", hostport, upstreamDialWait)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func (s *upstreamSelector) pick() *url.URL {
	if s.file == "" {
		return s.fallback
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	port := s.filePortLocked()
	if port == 0 {
		return s.fallback
	}
	host := net.JoinHostPort(s.fallback.Hostname(), strconv.Itoa(port))
	if host == s.fallback.Host || !s.aliveLocked(host) {
		return s.fallback
	}
	u := *s.fallback
	u.Host = host
	return &u
}

// filePortLocked re-reads the file when its stat changed or the cached read
// is older than upstreamFileTTL (mtime granularity can hide a fast rewrite).
func (s *upstreamSelector) filePortLocked() int {
	st, err := os.Stat(s.file)
	if err != nil {
		s.port = 0
		s.readAt = time.Time{}
		return 0
	}
	now := s.now()
	if !s.readAt.IsZero() && st.ModTime().Equal(s.modTime) && st.Size() == s.size && now.Sub(s.readAt) < upstreamFileTTL {
		return s.port
	}
	s.readAt, s.modTime, s.size, s.port = now, st.ModTime(), st.Size(), 0
	raw, err := os.ReadFile(s.file)
	if err != nil {
		return 0
	}
	if p, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && ValidUpstreamPort(p) {
		s.port = p
	}
	return s.port
}

func (s *upstreamSelector) aliveLocked(host string) bool {
	now := s.now()
	if host == s.aliveHost && now.Sub(s.aliveAt) < upstreamAliveTTL {
		return s.aliveOK
	}
	s.aliveHost, s.aliveAt, s.aliveOK = host, now, s.alive(host)
	return s.aliveOK
}
