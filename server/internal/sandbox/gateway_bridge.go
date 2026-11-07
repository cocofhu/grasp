package sandbox

import (
	"context"
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
	"github.com/rs/zerolog/log"
)

const tunnelProtocol = "grasp-tcp-v1"

// Local listeners adapt existing HTTP/WS/SSH consumers without exposing the
// gateway key to a browser or teaching business code provider-specific URLs.
// They are process-local; stored ports must be refreshed on attach after boot.
type gatewayBridge struct {
	client    *GatewayClient
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	closed    bool
	listeners map[string]*gatewayListener
	wg        sync.WaitGroup
	infoMu    sync.Mutex
	metadata  *GatewayInfo
}

type gatewayListener struct {
	listener net.Listener
	cancel   context.CancelFunc
	nativeID string
}

func newGatewayBridge(client *GatewayClient) *gatewayBridge {
	ctx, cancel := context.WithCancel(context.Background())
	return &gatewayBridge{client: client, ctx: ctx, cancel: cancel, listeners: map[string]*gatewayListener{}}
}

func (b *gatewayBridge) info(ctx context.Context) (*GatewayInfo, error) {
	b.infoMu.Lock()
	defer b.infoMu.Unlock()
	if b.metadata != nil {
		return b.metadata, nil
	}
	var info GatewayInfo
	if err := b.client.doDirect(ctx, http.MethodGet, "/api/v2/info", nil, &info); err != nil {
		return nil, fmt.Errorf("%w: discovery: %w", ErrGatewayTransport, err)
	}
	if info.Version != 2 || info.Transport != tunnelProtocol || info.Driver == "" {
		return nil, fmt.Errorf("%w: gateway does not support grasp-tcp-v1", ErrGatewayTransport)
	}
	for _, key := range []string{"session", "ide", "ssh", "cdp", "novnc"} {
		if port := info.Ports[key]; port < 1 || port > 65535 {
			return nil, fmt.Errorf("%w: gateway does not declare required %s endpoint", ErrGatewayTransport, key)
		}
	}
	b.metadata = &info
	return b.metadata, nil
}

func (b *gatewayBridge) endpoint(nativeID string, port int) (string, error) {
	key := nativeID + "/" + strconv.Itoa(port)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return "", net.ErrClosed
	}
	if existing := b.listeners[key]; existing != nil {
		return existing.listener.Addr().String(), nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithCancel(b.ctx)
	b.listeners[key] = &gatewayListener{listener: ln, cancel: cancel, nativeID: nativeID}
	b.wg.Add(1)
	go b.accept(ctx, ln, nativeID, port)
	return ln.Addr().String(), nil
}

func (b *gatewayBridge) accept(ctx context.Context, ln net.Listener, nativeID string, port int) {
	defer b.wg.Done()
	for {
		tcp, err := ln.Accept()
		if err != nil {
			return
		}
		b.mu.Lock()
		if b.closed || ctx.Err() != nil {
			b.mu.Unlock()
			_ = tcp.Close()
			return
		}
		b.wg.Add(1)
		b.mu.Unlock()
		go func() {
			defer b.wg.Done()
			defer func() { _ = tcp.Close() }()
			ws, err := b.dial(ctx, nativeID, port)
			if err != nil {
				if ctx.Err() == nil {
					log.Warn().Err(err).Str("gateway", b.client.id).Str("sandbox", nativeID).Int("guest_port", port).Msg("sandbox gateway channel failed")
				}
				return
			}
			relayTunnel(ctx, ws, tcp)
		}()
	}
}

func (b *gatewayBridge) dial(ctx context.Context, nativeID string, port int) (*websocket.Conn, error) {
	u, err := url.Parse(b.client.baseURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/v2/sandboxes/" + nativeID + "/connect/" + strconv.Itoa(port)
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second, Subprotocols: []string{tunnelProtocol}}
	ws, resp, err := dialer.DialContext(ctx, u.String(), http.Header{"Authorization": []string{"Bearer " + b.client.apiKey}})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("gateway TCP channel unavailable: %w", err)
	}
	if ws.Subprotocol() != tunnelProtocol {
		_ = ws.Close()
		return nil, errors.New("gateway TCP subprotocol mismatch")
	}
	return ws, nil
}

func (b *gatewayBridge) remove(nativeID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for key, ln := range b.listeners {
		if ln.nativeID == nativeID {
			ln.cancel()
			_ = ln.listener.Close()
			delete(b.listeners, key)
		}
	}
}

func (b *gatewayBridge) close() {
	b.mu.Lock()
	b.closed = true
	b.cancel()
	for _, ln := range b.listeners {
		ln.cancel()
		_ = ln.listener.Close()
	}
	b.listeners = map[string]*gatewayListener{}
	b.mu.Unlock()
	b.wg.Wait()
}

// Keep this wire implementation compatible with gateway/internal/api/connect.go.
// Binary messages carry TCP bytes; text "eof" means CloseWrite, not Close.
func relayTunnel(ctx context.Context, ws *websocket.Conn, tcp net.Conn) {
	defer func() { _ = ws.Close() }()
	defer func() { _ = tcp.Close() }()
	ws.SetReadLimit(64 << 10)
	_ = ws.SetReadDeadline(time.Now().Add(75 * time.Second))
	ws.SetPongHandler(func(string) error { return ws.SetReadDeadline(time.Now().Add(75 * time.Second)) })
	// The reader can report EOF and then a close/error while still handling
	// control frames. Leave room for all three reports during cancellation.
	done := make(chan error, 3)
	var pumps sync.WaitGroup
	pumps.Add(2)
	go func() {
		defer pumps.Done()
		buf := make([]byte, 32<<10)
		for {
			n, err := tcp.Read(buf)
			if n > 0 {
				_ = ws.SetWriteDeadline(time.Now().Add(30 * time.Second))
				if werr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					done <- werr
					return
				}
			}
			if err != nil {
				if errors.Is(err, io.EOF) {
					_ = ws.SetWriteDeadline(time.Now().Add(30 * time.Second))
					err = ws.WriteMessage(websocket.TextMessage, []byte("eof"))
				}
				done <- err
				return
			}
		}
	}()
	go func() {
		defer pumps.Done()
		readEOF := false
		for {
			kind, data, err := ws.ReadMessage()
			if err != nil {
				done <- err
				return
			}
			if readEOF {
				done <- errors.New("tunnel data after EOF")
				return
			}
			if kind == websocket.TextMessage && string(data) == "eof" {
				readEOF = true
				if cw, ok := tcp.(interface{ CloseWrite() error }); ok {
					done <- cw.CloseWrite()
				} else {
					done <- errors.New("TCP half-close unavailable")
				}
				// Continue reading control frames until both TCP halves finish.
				continue
			}
			if kind != websocket.BinaryMessage {
				done <- errors.New("invalid tunnel frame")
				return
			}
			if _, err := tcp.Write(data); err != nil {
				done <- err
				return
			}
		}
	}()
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	finished := 0
loop:
	for finished < 2 {
		select {
		case err := <-done:
			finished++
			if err != nil {
				break loop
			}
		case <-ticker.C:
			if err := ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
				break loop
			}
		case <-ctx.Done():
			break loop
		}
	}
	_ = ws.Close()
	_ = tcp.Close()
	pumps.Wait()
}
