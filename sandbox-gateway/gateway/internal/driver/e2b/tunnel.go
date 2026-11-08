package e2b

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const relayPort = 8081

// RelayCredentials never belong in API DTOs or logs. EndpointURL is the private
// port-8081 origin returned by Client.SandboxEndpoint, including cloud domains.
// Empty EndpointURL uses the manager's configured Embed proxy origin.
type RelayCredentials struct {
	SandboxID    string `json:"-"`
	TrafficToken string `json:"-"`
	RelayKey     string `json:"-"`
	EndpointURL  string `json:"-"`
}

type localTunnel struct {
	listener net.Listener
	port     int
	target   int
	active   map[net.Conn]struct{}
	closed   bool
	enabled  bool
	epoch    uint64
	ctx      context.Context
	cancel   context.CancelFunc
}

// TunnelManager publishes private guest TCP endpoints on this host's loopback
// interface only. Persist List's local ports and supply them on restart: an
// occupied preferred port is an error, never an implicit endpoint change.
type TunnelManager struct {
	mu          sync.Mutex
	origin      string
	credentials map[string]RelayCredentials
	tunnels     map[string]map[int]*localTunnel
	closed      bool
	http        *http.Client
	dialer      websocket.Dialer
}

func NewTunnelManager(proxyOrigin string) (*TunnelManager, error) {
	if proxyOrigin != "" {
		if _, err := relayOrigin(proxyOrigin); err != nil {
			return nil, err
		}
	}
	return &TunnelManager{
		origin: proxyOrigin, credentials: make(map[string]RelayCredentials),
		tunnels: make(map[string]map[int]*localTunnel),
		http: &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment, DisableKeepAlives: true,
			DialContext:         (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
		}, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		dialer: websocket.Dialer{Proxy: http.ProxyFromEnvironment, HandshakeTimeout: 20 * time.Second,
			Subprotocols: []string{"binary"}, ReadBufferSize: 65536, WriteBufferSize: 65536},
	}, nil
}

func relayOrigin(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("E2B relay endpoint must be an http(s) origin")
	}
	return u, nil
}

func validTunnelPort(port int) bool {
	return port > 0 && port <= 65535 && port != relayPort && port != 49983
}

func (m *TunnelManager) endpoint(creds RelayCredentials, path string) (*url.URL, http.Header, error) {
	if creds.SandboxID == "" || creds.TrafficToken == "" || creds.RelayKey == "" {
		return nil, nil, errors.New("private E2B relay credentials are incomplete")
	}
	origin := creds.EndpointURL
	if origin == "" {
		origin = m.origin
	}
	u, err := relayOrigin(origin)
	if err != nil {
		return nil, nil, err
	}
	ref, err := url.Parse(path)
	if err != nil || !strings.HasPrefix(path, "/_bridge/") || ref.IsAbs() || ref.Host != "" || ref.Fragment != "" {
		return nil, nil, errors.New("invalid E2B relay control path")
	}
	u.Path, u.RawPath, u.RawQuery = ref.Path, ref.RawPath, ref.RawQuery
	headers := make(http.Header)
	headers.Set("E2b-Sandbox-Id", creds.SandboxID)
	headers.Set("E2b-Sandbox-Port", strconv.Itoa(relayPort))
	headers.Set("e2b-traffic-access-token", creds.TrafficToken)
	headers.Set("X-Grasp-Relay-Key", creds.RelayKey)
	return u, headers, nil
}

// Control makes a bounded authenticated guest-relay request. Error messages
// contain no endpoint URL, request body, token or provider response body.
func (m *TunnelManager) Control(ctx context.Context, creds RelayCredentials, method, path string, body, result any) error {
	return m.control(ctx, creds, method, path, body, result, nil)
}

func (m *TunnelManager) control(ctx context.Context, creds RelayCredentials, method, path string, body, result any, extra http.Header) error {
	u, headers, err := m.endpoint(creds, path)
	if err != nil {
		return err
	}
	var data io.Reader
	if body != nil {
		encoded, merr := json.Marshal(body)
		if merr != nil {
			return errors.New("invalid relay control body")
		}
		data = bytes.NewReader(encoded)
		headers.Set("Content-Type", "application/json")
	}
	for name, values := range extra {
		for _, value := range values {
			headers.Add(name, value)
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), data)
	if err != nil {
		return errors.New("invalid relay control request")
	}
	req.Header = headers
	resp, err := m.http.Do(req)
	if err != nil {
		return errors.New("E2B relay control request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("E2B relay control returned HTTP %d", resp.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024+1))
	if err != nil || len(payload) > 4*1024*1024 {
		return errors.New("invalid relay control response")
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(payload, result); err != nil {
		return errors.New("invalid relay control JSON")
	}
	return nil
}

// UpdateCredentials refreshes the handle used by all existing listeners on
// subsequent connections, including a rotated token returned by resume.
func (m *TunnelManager) UpdateCredentials(creds RelayCredentials) error {
	if _, _, err := m.endpoint(creds, "/_bridge/health"); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("E2B tunnel manager is closed")
	}
	m.credentials[creds.SandboxID] = creds
	return nil
}

func (m *TunnelManager) Start(ctx context.Context, creds RelayCredentials, guestPort, preferredLocalPort int) (string, error) {
	return m.StartMapped(ctx, creds, guestPort, guestPort, preferredLocalPort)
}

// StartMapped reserves a stable loopback endpoint for guestPort and forwards to
// targetPort. The driver maps preview ingress 18080 to injector17980 explicitly;
// direct app/CDP loopback targets keep their actual ports.
func (m *TunnelManager) StartMapped(ctx context.Context, creds RelayCredentials, guestPort, targetPort, preferredLocalPort int) (endpoint string, err error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := m.UpdateCredentials(creds); err != nil {
		return "", err
	}
	tunnel, created, err := m.reserveMapped(creds.SandboxID, guestPort, targetPort, preferredLocalPort)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	enabled, epoch := tunnel.enabled, tunnel.epoch
	m.mu.Unlock()
	defer func() {
		if err != nil && created {
			m.mu.Lock()
			defer m.mu.Unlock()
			if m.tunnels[creds.SandboxID][guestPort] == tunnel && !tunnel.enabled && tunnel.epoch == epoch {
				delete(m.tunnels[creds.SandboxID], guestPort)
				tunnel.closed = true
				tunnel.cancel()
				_ = tunnel.listener.Close()
			}
		}
	}()

	// Bind before making HTTP requests: an outbound connection can otherwise
	// claim a persisted listener's port from the same ephemeral port range.
	if !enabled {
		if err := m.Control(ctx, creds, http.MethodPost, "/_bridge/ports", map[string]any{"ports": []int{targetPort}}, nil); err != nil {
			return "", err
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.tunnels[creds.SandboxID][guestPort] != tunnel || tunnel.epoch != epoch {
		return "", errors.New("E2B guest tunnels were closed during publication")
	}
	if _, ok := m.credentials[creds.SandboxID]; !ok {
		return "", errors.New("E2B guest tunnels were closed during publication")
	}
	if !tunnel.enabled {
		tunnel.cancel()
		tunnel.ctx, tunnel.cancel = context.WithCancel(context.Background())
		tunnel.enabled = true
	}
	return tunnel.listener.Addr().String(), nil
}

// reserveMapped keeps a loopback binding without accepting guest traffic or
// making provider requests. Restore reserves every saved port before any I/O.
func (m *TunnelManager) reserveMapped(sandboxID string, guestPort, targetPort, preferredLocalPort int) (*localTunnel, bool, error) {
	if !validTunnelPort(guestPort) || !validTunnelPort(targetPort) || preferredLocalPort < 0 || preferredLocalPort > 65535 {
		return nil, false, errors.New("invalid E2B tunnel port")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, false, errors.New("E2B tunnel manager is closed")
	}
	if existing := m.tunnels[sandboxID][guestPort]; existing != nil {
		port := existing.listener.Addr().(*net.TCPAddr).Port
		if existing.target != targetPort || preferredLocalPort != 0 && preferredLocalPort != port {
			return nil, false, errors.New("existing E2B tunnel endpoint conflicts with requested mapping")
		}
		return existing, false, nil
	}
	listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(preferredLocalPort)))
	if err != nil {
		return nil, false, errors.New("cannot bind required E2B loopback endpoint")
	}
	lifetime, cancel := context.WithCancel(context.Background())
	tunnel := &localTunnel{listener: listener, port: guestPort, target: targetPort,
		active: make(map[net.Conn]struct{}), ctx: lifetime, cancel: cancel}
	if m.tunnels[sandboxID] == nil {
		m.tunnels[sandboxID] = make(map[int]*localTunnel)
	}
	m.tunnels[sandboxID][guestPort] = tunnel
	go m.accept(sandboxID, tunnel)
	return tunnel, true, nil
}

func (m *TunnelManager) Get(sandboxID string, guestPort int) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tunnel := m.tunnels[sandboxID][guestPort]
	if tunnel == nil || !tunnel.enabled {
		return "", false
	}
	return tunnel.listener.Addr().String(), true
}

func (m *TunnelManager) List(sandboxID string) map[int]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[int]string)
	for port, tunnel := range m.tunnels[sandboxID] {
		if tunnel.enabled {
			result[port] = tunnel.listener.Addr().String()
		}
	}
	return result
}

func (m *TunnelManager) accept(sandboxID string, tunnel *localTunnel) {
	for {
		conn, err := tunnel.listener.Accept()
		if err != nil {
			return
		}
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetNoDelay(true)
		}
		m.mu.Lock()
		if tunnel.closed || !tunnel.enabled {
			m.mu.Unlock()
			_ = conn.Close()
			continue
		}
		tunnel.active[conn] = struct{}{}
		lifetime := tunnel.ctx
		creds := m.credentials[sandboxID]
		m.mu.Unlock()
		go m.forward(lifetime, creds, tunnel, conn)
	}
}

func (m *TunnelManager) forward(lifetime context.Context, creds RelayCredentials, tunnel *localTunnel, client net.Conn) {
	ctx, cancel := context.WithCancel(lifetime)
	defer cancel()
	defer func() {
		_ = client.Close()
		m.mu.Lock()
		delete(tunnel.active, client)
		m.mu.Unlock()
	}()
	reader := bufio.NewReader(client)
	var prefix []byte
	if tunnel.port == 9222 {
		_ = client.SetReadDeadline(time.Now().Add(15 * time.Second))
		for len(prefix) < 65536 {
			line, err := reader.ReadBytes('\n')
			prefix = append(prefix, line...)
			if err != nil || len(prefix) > 65536 {
				return
			}
			if bytes.HasSuffix(prefix, []byte("\r\n\r\n")) {
				break
			}
		}
		_ = client.SetReadDeadline(time.Time{})
		if !bytes.HasSuffix(prefix, []byte("\r\n\r\n")) {
			return
		}
		req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(prefix)))
		if err != nil {
			return
		}
		if req.Body != nil {
			_ = req.Body.Close()
		}
		if req.Method == http.MethodGet && cdpDiscoveryPath(req.URL.Path) {
			headers := make(http.Header)
			headers.Set("X-Grasp-CDP-Path", req.URL.Path)
			headers.Set("X-Grasp-CDP-Host", tunnel.listener.Addr().String())
			var result any
			if err := m.control(ctx, creds, http.MethodGet, "/_bridge/cdp", nil, &result, headers); err != nil {
				_, _ = io.WriteString(client, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
				return
			}
			rewriteCDPURLs(result, tunnel.listener.Addr().String())
			payload, err := json.Marshal(result)
			if err != nil {
				return
			}
			_, _ = fmt.Fprintf(client, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", len(payload))
			_, _ = client.Write(payload)
			return
		}
	}
	u, headers, err := m.endpoint(creds, "/_bridge/tcp?port="+strconv.Itoa(tunnel.target))
	if err != nil {
		return
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	ws, resp, err := m.dialer.DialContext(ctx, u.String(), headers)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return
	}
	defer func() { _ = ws.Close() }()
	if ws.Subprotocol() != "binary" {
		return
	}
	ws.SetReadLimit(2 * 1024 * 1024)
	if tcp, ok := ws.UnderlyingConn().(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
	}
	m.mu.Lock()
	if tunnel.closed || ctx.Err() != nil {
		m.mu.Unlock()
		return
	}
	tunnel.active[ws.UnderlyingConn()] = struct{}{}
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(tunnel.active, ws.UnderlyingConn())
		m.mu.Unlock()
	}()
	if len(prefix) != 0 {
		if err := ws.WriteMessage(websocket.BinaryMessage, prefix); err != nil {
			return
		}
	}
	uploadDone := make(chan struct{})
	go func() {
		defer close(uploadDone)
		buffer := make([]byte, 65536)
		for {
			n, rerr := reader.Read(buffer)
			if n > 0 {
				if werr := ws.WriteMessage(websocket.BinaryMessage, buffer[:n]); werr != nil {
					_ = ws.Close()
					return
				}
			}
			if rerr != nil {
				if errors.Is(rerr, io.EOF) {
					if err := ws.WriteMessage(websocket.TextMessage, []byte("EOF")); err == nil {
						return
					}
				}
				_ = ws.Close()
				return
			}
		}
	}()
	for {
		kind, stream, err := ws.NextReader()
		if err != nil {
			break
		}
		if kind != websocket.BinaryMessage {
			break
		}
		if _, err := io.Copy(client, stream); err != nil {
			break
		}
	}
	_ = client.Close()
	_ = ws.Close()
	<-uploadDone
}

func cdpDiscoveryPath(path string) bool {
	return path == "/json/version" || path == "/json/list" || path == "/json" || path == "/json/"
}

func rewriteCDPURLs(value any, localAddress string) {
	switch object := value.(type) {
	case map[string]any:
		for key, entry := range object {
			if text, ok := entry.(string); key == "webSocketDebuggerUrl" && ok {
				if u, err := url.Parse(text); err == nil {
					u.Scheme, u.Host, u.User = "ws", localAddress, nil
					object[key] = u.String()
				}
			} else {
				rewriteCDPURLs(entry, localAddress)
			}
		}
	case []any:
		for _, entry := range object {
			rewriteCDPURLs(entry, localAddress)
		}
	}
}

// Suspend rejects new traffic and closes active streams while retaining the
// guest's stable loopback bindings. StartMapped republishes before re-enabling.
func (m *TunnelManager) Suspend(sandboxID string) {
	m.mu.Lock()
	var connections []net.Conn
	for _, tunnel := range m.tunnels[sandboxID] {
		tunnel.enabled = false
		tunnel.epoch++
		tunnel.cancel()
		for conn := range tunnel.active {
			connections = append(connections, conn)
		}
	}
	m.mu.Unlock()
	for _, conn := range connections {
		_ = conn.Close()
	}
}

// Close closes only this guest's listeners and active streams. It does not
// destroy the E2B guest. CloseAll permanently shuts down the entire manager.
func (m *TunnelManager) Close(sandboxID string) error {
	m.mu.Lock()
	tunnels := m.tunnels[sandboxID]
	delete(m.tunnels, sandboxID)
	delete(m.credentials, sandboxID)
	for _, tunnel := range tunnels {
		tunnel.closed = true
		tunnel.cancel()
	}
	var connections []net.Conn
	for _, tunnel := range tunnels {
		for conn := range tunnel.active {
			connections = append(connections, conn)
		}
	}
	m.mu.Unlock()
	for _, tunnel := range tunnels {
		_ = tunnel.listener.Close()
	}
	for _, conn := range connections {
		_ = conn.Close()
	}
	return nil
}

func (m *TunnelManager) CloseAll() error {
	m.mu.Lock()
	m.closed = true
	ids := make([]string, 0, len(m.tunnels))
	for id := range m.tunnels {
		ids = append(ids, id)
	}
	m.credentials = make(map[string]RelayCredentials)
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Close(id)
	}
	m.http.CloseIdleConnections()
	return nil
}
