package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/browser"
	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

type publicPreviewTicketBody struct {
	Port    int    `json:"port"`
	Purpose string `json:"purpose"`
}

// PublicPreviewTicket exchanges a share token for a short-lived preview ticket.
// Share token stays in X-Gate-Share-Token; ticket may be used in WS query.
func (h *Handlers) PublicPreviewTicket(c *gin.Context) {
	applyPublicSecurityHeaders(c)
	if !h.publicRateLimit(c, gateshare.RateBucketTicket) {
		return
	}
	if h.GateShare == nil || h.GateShareTickets == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
		return
	}
	if !h.checkPublicCSRF(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "csrf", "message": "请求未通过安全校验"})
		return
	}
	token := strings.TrimSpace(c.GetHeader(headerShareToken))
	if token == "" || !gateshare.ValidTokenShape(token) {
		c.JSON(http.StatusOK, gin.H{"status": "invalid"})
		return
	}
	var body publicPreviewTicketBody
	if err := c.ShouldBindJSON(&body); err != nil || body.Port <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	purpose := strings.TrimSpace(body.Purpose)
	if purpose == "" {
		purpose = gateshare.PreviewPurposeVNC
	}
	lookup, st, err := h.GateShare.LookupByToken(token)
	if err != nil || lookup == nil || st == models.ShareLinkStateNone {
		c.JSON(http.StatusOK, gin.H{"status": "invalid"})
		return
	}
	if st != models.ShareLinkStateActive {
		c.JSON(http.StatusOK, gin.H{"status": st})
		return
	}
	if lookup.Kind != models.ShareLinkKindReview || lookup.Node == nil || lookup.Node.Type != "app_preview" {
		c.JSON(http.StatusForbidden, gin.H{"error": "unsupported", "message": "当前分享链不支持远程预览"})
		return
	}
	ports := h.publicAppPreviewPorts(lookup.Link.RunID, lookup.Link.NodeID)
	var matched *gateshare.PublicPreviewPort
	for i := range ports {
		if ports[i].Port == body.Port {
			matched = &ports[i]
			break
		}
	}
	if matched == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "port_not_registered", "message": "预览端口未注册"})
		return
	}
	wantPurpose := gateshare.PreviewPurposeVNC
	if strings.TrimSpace(matched.DirectURL) != "" {
		wantPurpose = gateshare.PreviewPurposeAPI
	}
	if purpose != wantPurpose {
		purpose = wantPurpose
	}
	ticket, exp, err := h.GateShareTickets.Issue(
		lookup.Link.TokenHash, lookup.Link.RunID, lookup.Link.NodeID, body.Port, purpose,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unavailable"})
		return
	}
	out := gin.H{
		"status":    models.ShareLinkStateActive,
		"ticket":    ticket,
		"expiresAt": exp,
		"port":      body.Port,
		"mode":      purpose,
	}
	if purpose == gateshare.PreviewPurposeVNC {
		out["wsPath"] = "/public/gate-approvals/preview-vnc/ws"
	} else {
		out["iframePath"] = fmt.Sprintf("/public/gate-approvals/preview-api/%s/", ticket)
	}
	c.JSON(http.StatusOK, out)
}

// PublicPreviewVNC proxies noVNC over a share-scoped ticket (no Session).
func (h *Handlers) PublicPreviewVNC(c *gin.Context) {
	applyPublicSecurityHeaders(c)
	if h.GateShare == nil || h.GateShareTickets == nil || h.Browser == nil {
		c.String(http.StatusServiceUnavailable, "vnc preview disabled")
		return
	}
	ticket := strings.TrimSpace(c.Query("ticket"))
	claims, ok := h.GateShareTickets.Lookup(ticket)
	if !ok || claims.Purpose != gateshare.PreviewPurposeVNC {
		c.String(http.StatusUnauthorized, "invalid ticket")
		return
	}
	if !h.publicTicketLinkActive(claims.TokenHash) {
		c.String(http.StatusForbidden, "share link inactive")
		return
	}

	rctx, rcancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	bridgeURL, sandboxName, ok := h.resolvePreviewTarget(rctx, claims.RunID, claims.NodeID, claims.Port)
	rcancel()
	if !ok {
		if sandboxName != "" {
			c.String(http.StatusGone, "sandbox recycled")
			return
		}
		c.String(http.StatusNotFound, "preview not registered")
		return
	}
	sandboxIP, err := previewHostIP(bridgeURL)
	if err != nil {
		c.String(http.StatusBadGateway, "preview host invalid")
		return
	}
	navigateURL := fmt.Sprintf("http://127.0.0.1:%d/", claims.Port)

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Debug().Err(err).Msg("public preview-vnc websocket upgrade failed")
		return
	}
	defer conn.Close()

	unregister := func() {}
	if h.GateShareSessions != nil {
		unregister = h.GateShareSessions.Register(claims.TokenHash, conn)
	}
	defer unregister()

	var wmu sync.Mutex
	writeJSON := func(v any) error {
		wmu.Lock()
		defer wmu.Unlock()
		return conn.WriteJSON(v)
	}
	writeMsg := func(msgType int, data []byte) error {
		wmu.Lock()
		defer wmu.Unlock()
		return conn.WriteMessage(msgType, data)
	}
	pushJSON := func(v any) {
		_ = writeJSON(v)
	}

	linkWatchDone := make(chan struct{})
	defer close(linkWatchDone)
	// Share write lock with RFB path — gorilla/websocket forbids concurrent writers.
	go h.watchPublicPreviewLink(claims.TokenHash, writeJSON, conn.Close, linkWatchDone)

	openCtx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()
	sess, err := h.Browser.OpenInSandbox(openCtx, sandboxName, sandboxIP, navigateURL)
	if err != nil {
		_ = writeJSON(gin.H{"type": "error", "message": err.Error()})
		return
	}
	defer sess.Close()

	vncURL, err := sess.VNCWebSocketURL()
	if err != nil {
		_ = writeJSON(gin.H{"type": "error", "message": err.Error()})
		return
	}
	upstream, _, err := websocket.DefaultDialer.Dial(vncURL, nil)
	if err != nil {
		_ = writeJSON(gin.H{"type": "error", "message": "vnc upstream failed"})
		return
	}
	defer upstream.Close()

	done := make(chan struct{})
	defer close(done)

	sess.Page().OnPick(func(p browser.Pick) {
		pushJSON(gin.H{"type": "picked", "pick": p})
	})
	sess.Page().OnInspectCanceled(func() {
		pushJSON(gin.H{"type": "inspect-canceled"})
	})
	sess.Page().OnDescribeFailed(func() {
		pushJSON(gin.H{"type": "describe-failed"})
	})
	pushJSON(gin.H{"type": "ready", "url": navigateURL})

	go func() {
		select {
		case <-sess.Done():
			pushJSON(gin.H{"type": "closed", "reason": sess.Reason()})
			time.Sleep(50 * time.Millisecond)
			_ = conn.Close()
		case <-done:
		}
	}()

	go func() {
		for {
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				_ = upstream.Close()
				return
			}
			if msgType == websocket.TextMessage {
				var m vncClientMsg
				if json.Unmarshal(data, &m) == nil {
					sess.Touch()
					h.applyVncMsg(sess.Page(), m, pushJSON)
					continue
				}
			}
			if err := upstream.WriteMessage(msgType, data); err != nil {
				return
			}
		}
	}()

	for {
		msgType, data, err := upstream.ReadMessage()
		if err != nil {
			return
		}
		if err := writeMsg(msgType, data); err != nil {
			return
		}
	}
}

// PublicPreviewAPIProxy reverse-proxies API ports via opaque ticket path (leak-free).
// Must remain embeddable in same-origin public-page iframes (no DENY / frame-ancestors none).
func (h *Handlers) PublicPreviewAPIProxy(c *gin.Context) {
	applyPublicPreviewAPIHeaders(c)
	if h.GateShare == nil || h.GateShareTickets == nil || h.MCP == nil || h.Preview == nil {
		c.String(http.StatusServiceUnavailable, "preview unavailable")
		return
	}
	ticket := strings.TrimSpace(c.Param("ticket"))
	claims, ok := h.GateShareTickets.Lookup(ticket)
	if !ok || claims.Purpose != gateshare.PreviewPurposeAPI {
		c.String(http.StatusUnauthorized, "invalid ticket")
		return
	}
	if !h.publicTicketLinkActive(claims.TokenHash) {
		c.String(http.StatusForbidden, "share link inactive")
		return
	}

	runID, nodeID, port := claims.RunID, claims.NodeID, claims.Port
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	host, sandboxName := h.lookupPreviewRegistration(runID, nodeID, port)
	if h.Sbx != nil {
		if mgr := h.Sbx.Manager(); mgr != nil && sandboxName != "" {
			if mgr.Status(ctx, sandboxName) != "running" {
				c.String(http.StatusGone, "sandbox recycled")
				return
			}
			healthy := h.Preview.ProbeHTTPPort(ctx, sandboxName, port)
			if err := h.Preview.UpdatePreviewHealth(runID, nodeID, port, healthy); err != nil {
				log.Warn().Err(err).Str("runId", runID).Str("nodeId", nodeID).Int("port", port).
					Msg("persist preview health failed")
			}
			if !healthy {
				c.String(http.StatusServiceUnavailable, "preview service unavailable")
				return
			}
			if fresh, ok := h.Preview.PreviewUpstream(ctx, sandboxName, port); ok && fresh != "" {
				if fresh != host {
					host = fresh
					if err := h.Preview.UpdatePreviewHost(runID, nodeID, port, host); err != nil {
						log.Warn().Err(err).Str("runId", runID).Str("nodeId", nodeID).Int("port", port).
							Msg("persist preview host failed")
					}
				}
			}
		}
	}
	if host == "" {
		c.String(http.StatusNotFound, "preview not registered")
		return
	}
	target, err := url.Parse(host)
	if err != nil || target.Host == "" {
		c.String(http.StatusBadGateway, "bad upstream host")
		return
	}

	prefix := fmt.Sprintf("/public/gate-approvals/preview-api/%s/", ticket)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = 100 * time.Millisecond
	proxy.ModifyResponse = publicPreviewAPIModifyResponse(prefix)
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		log.Warn().Err(err).Int("port", port).Msg("public preview-api upstream unreachable")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("preview upstream unreachable"))
	}

	path := c.Param("path")
	if path == "" {
		path = "/"
	}
	c.Request.URL.Path = path
	c.Request.Header.Set("Accept-Encoding", "identity")

	publicHost := c.Request.Host
	if fh := c.GetHeader("X-Forwarded-Host"); fh != "" {
		publicHost = fh
	}
	c.Request.Header.Set("X-Forwarded-Host", publicHost)
	c.Request.Header.Set("X-Forwarded-Prefix", strings.TrimRight(prefix, "/"))
	proto := c.GetHeader("X-Forwarded-Proto")
	if proto == "" {
		if c.Request.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	c.Request.Header.Set("X-Forwarded-Proto", proto)
	c.Request.Host = target.Host
	if c.Request.Header.Get("Origin") != "" {
		c.Request.Header.Set("Origin", target.Scheme+"://"+target.Host)
	}
	c.Request.Header.Del("Forwarded")
	proxy.ServeHTTP(c.Writer, c.Request)
}

func (h *Handlers) publicAppPreviewPorts(runID, nodeID string) []gateshare.PublicPreviewPort {
	if h.MCP == nil {
		return nil
	}
	raw := h.MCP.ListPreviewPorts(runID, nodeID)
	out := make([]gateshare.PublicPreviewPort, 0, len(raw))
	for _, p := range raw {
		if p.Port <= 0 && strings.TrimSpace(p.URL) == "" {
			continue
		}
		label := strings.TrimSpace(p.Label)
		entry := gateshare.PublicPreviewPort{
			Port:      p.Port,
			Label:     label,
			Kind:      strings.TrimSpace(p.Kind),
			URL:       strings.TrimSpace(p.URL),
			Mode:      gateshare.InferPreviewMode(label),
			DirectURL: strings.TrimSpace(p.DirectURL),
		}
		if entry.Kind == "" {
			if entry.URL != "" {
				entry.Kind = "url"
			} else {
				entry.Kind = "port"
			}
		}
		if entry.Kind == "url" && entry.URL != "" {
			entry.DirectURL = entry.URL
			entry.Mode = "url"
		}
		out = append(out, entry)
	}
	return out
}

func (h *Handlers) publicTicketLinkActive(tokenHash string) bool {
	if h.GateShare == nil || strings.TrimSpace(tokenHash) == "" {
		return false
	}
	link, err := h.GateShare.LoadLinkByTokenHash(tokenHash)
	if err != nil || link == nil {
		return false
	}
	return link.UsedAt == nil && link.RevokedAt == nil && time.Now().Before(link.ExpiresAt)
}

func (h *Handlers) watchPublicPreviewLink(
	tokenHash string,
	writeJSON func(any) error,
	closeFn func() error,
	done <-chan struct{},
) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if h.publicTicketLinkActive(tokenHash) {
				continue
			}
			if writeJSON != nil {
				_ = writeJSON(gin.H{"type": "closed", "reason": "share_link_inactive"})
			}
			if closeFn != nil {
				_ = closeFn()
			}
			return
		}
	}
}

// applyPublicPreviewAPIHeaders sets cache/referrer/nosniff only and clears any
// framing deny headers that middleware may have applied. Unlike
// applyPublicSecurityHeaders, this path must remain embeddable same-origin.
func applyPublicPreviewAPIHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Writer.Header().Del("X-Frame-Options")
	c.Writer.Header().Del("Content-Security-Policy")
	c.Writer.Header().Del("Access-Control-Allow-Origin")
	c.Writer.Header().Del("Access-Control-Allow-Methods")
	c.Writer.Header().Del("Access-Control-Allow-Headers")
}

// publicPreviewAPIModifyResponse rewrites HTML under the opaque ticket prefix and
// strips upstream framing headers that would block same-origin iframe embedding.
func publicPreviewAPIModifyResponse(prefix string) func(*http.Response) error {
	inner := previewModifyResponse(prefix)
	return func(resp *http.Response) error {
		if err := inner(resp); err != nil {
			return err
		}
		resp.Header.Del("X-Frame-Options")
		csp := resp.Header.Get("Content-Security-Policy")
		cleaned := stripCSPDirective(csp, "frame-ancestors")
		if cleaned == "" {
			resp.Header.Set("Content-Security-Policy", "frame-ancestors 'self'")
		} else {
			resp.Header.Set("Content-Security-Policy", cleaned+"; frame-ancestors 'self'")
		}
		return nil
	}
}

func stripCSPDirective(csp, name string) string {
	parts := strings.Split(csp, ";")
	out := make([]string, 0, len(parts))
	prefix := strings.ToLower(strings.TrimSpace(name))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		lower := strings.ToLower(p)
		if lower == prefix || strings.HasPrefix(lower, prefix+" ") {
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, "; ")
}
