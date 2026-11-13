package api

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"sandbox-gateway/internal/config"
	"sandbox-gateway/internal/service"
	"sandbox-gateway/internal/store"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const tunnelProtocol = "grasp-tcp-v1"

// An unconfigured legacy v1 gateway remains usable locally. A remotely usable
// data channel always requires an explicit server-side API key.
func requireTransportAuth(keys []string) gin.HandlerFunc {
	enabled := false
	for _, key := range keys {
		enabled = enabled || strings.TrimSpace(key) != ""
	}
	return func(c *gin.Context) {
		if !enabled {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "gateway transport requires API key authentication"})
		}
	}
}

func gatewayInfo(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		stopMode, diskMode := "cold", "unlimited"
		switch cfg.Driver {
		case "kubernetes":
			diskMode = "pvc"
		case "e2b":
			stopMode, diskMode = "pause", "template-minimum-free"
		}
		c.JSON(http.StatusOK, gin.H{
			"version": 2, "driver": cfg.Driver, "transport": tunnelProtocol,
			"profiles":     cfg.EnvironmentProfiles(),
			"ports":        map[string]int{"session": cfg.Ports.Session, "ide": cfg.Ports.CodeServer, "ssh": cfg.Ports.SSH, "cdp": cfg.Ports.CDP, "novnc": cfg.Ports.NoVNC},
			"capabilities": gin.H{"stopMode": stopMode, "diskMode": diskMode},
		})
	}
}

// Connect accepts only a sandbox ID and a declared guest port. The target is
// resolved by the owning driver, never from a client-supplied host or URL.
func (h *Handler) Connect(c *gin.Context) {
	port, err := strconv.Atoi(c.Param("port"))
	if err != nil || port < 1 || port > 65535 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid guest port"})
		return
	}
	if c.Request.URL.RawQuery != "" || c.GetHeader("Origin") != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "transport is server-to-server"})
		return
	}
	if !containsProtocol(websocket.Subprotocols(c.Request), tunnelProtocol) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "grasp-tcp-v1 subprotocol required"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	addr, err := h.svc.ConnectionTarget(ctx, c.Param("id"), port)
	if err != nil {
		cancel()
		status := http.StatusBadGateway
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, service.ErrEndpointNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": "sandbox endpoint unavailable"})
		return
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	cancel()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "sandbox endpoint unavailable"})
		return
	}
	defer func() { _ = conn.Close() }()
	upgrader := websocket.Upgrader{Subprotocols: []string{tunnelProtocol}, HandshakeTimeout: 10 * time.Second}
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	relayTunnel(c.Request.Context(), ws, conn)
}

func containsProtocol(protocols []string, want string) bool {
	for _, protocol := range protocols {
		if protocol == want {
			return true
		}
	}
	return false
}

// Wire contract (also implemented by the Grasp client): binary messages are
// TCP bytes; text "eof" half-closes TCP writes. Both directions must finish
// before closing, so stdin EOF cannot truncate a command's remaining output.
func relayTunnel(ctx context.Context, ws *websocket.Conn, tcp net.Conn) {
	defer func() { _ = ws.Close() }()
	defer func() { _ = tcp.Close() }()
	ws.SetReadLimit(64 << 10)
	_ = ws.SetReadDeadline(time.Now().Add(75 * time.Second))
	ws.SetPongHandler(func(string) error { return ws.SetReadDeadline(time.Now().Add(75 * time.Second)) })
	// The reader can report EOF and then a close/error during cancellation.
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
