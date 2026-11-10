package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/gin-gonic/gin"
)

// PrivatePreviewMiddleware reserves every host under the explicitly configured
// private-preview base. A capability resolves a registered run/node/port; it
// grants no platform API, static-file, login or arbitrary upstream access.
func (h *Handlers) PrivatePreviewMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !h.PrivatePreviewBase.ContainsHost(c.Request.Host) {
			c.Next()
			return
		}
		c.Abort()
		h.privatePreviewProxy(c)
	}
}

func (h *Handlers) privatePreviewProxy(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
	ticket, ok := h.PrivatePreviewBase.Ticket(c.Request.Host)
	if !ok || h.Preview == nil || h.MCP == nil || !h.MCP.PrivatePreviews() {
		c.String(http.StatusNotFound, "preview not found")
		return
	}
	rec, ok := h.Preview.PrivatePreviewByURL(h.PrivatePreviewBase.URL(ticket))
	if !ok || rec.SandboxName == "" || !rec.Healthy {
		c.String(http.StatusNotFound, "preview not found")
		return
	}
	// Old generations must not survive a Solo restart, node retry or rebinding.
	name, bound := h.Preview.SandboxForRunNode(rec.RunID, rec.NodeID)
	if !bound || name != rec.SandboxName {
		c.String(http.StatusGone, "preview expired")
		return
	}
	closeConsumer, consumerErr := h.Preview.BeginPreviewConsumer(c.Request.Context(), *rec)
	if consumerErr != nil {
		workspaceBindingError(c, consumerErr)
		return
	}
	defer func() { _ = closeConsumer() }()
	if h.Sbx == nil || h.Sbx.Manager() == nil {
		c.String(http.StatusServiceUnavailable, "preview unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	if h.Sbx.Manager().Status(ctx, name) != "running" {
		c.String(http.StatusGone, "sandbox recycled")
		return
	}
	port := rec.InjectorPort
	if port == 0 {
		port = rec.Port
	}
	if port < 1 || port > 65535 {
		c.String(http.StatusBadGateway, "preview upstream unavailable")
		return
	}
	upstream, ok := h.Preview.PreviewUpstream(ctx, name, port)
	if !ok {
		c.String(http.StatusBadGateway, "preview upstream unavailable")
		return
	}
	target, err := url.Parse(upstream)
	if err != nil || target.Scheme != "http" || target.Host == "" || target.User != nil || target.Path != "" || target.RawQuery != "" || target.Fragment != "" {
		c.String(http.StatusBadGateway, "preview upstream unavailable")
		return
	}
	h.Sbx.TouchActivityByName(name)
	publicOrigin := strings.TrimRight(h.PrivatePreviewBase.URL(ticket), "/")
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			// Stock Vite accepts loopback hosts and rejects an arbitrary
			// wildcard preview host by default. Keep the upstream Host local;
			// forwarded headers retain the browser origin for aware apps.
			// Root paths and the browser's import.meta.url remain unchanged.
			pr.Out.Host = target.Host
			stripPrivatePreviewHeaders(pr.Out)
			// Default WebSocket and HTTP same-origin guards compare Origin
			// against Host. Translate only this registration's browser origin;
			// foreign origins retain their mismatch and remain rejectable.
			if pr.Out.Header.Get("Origin") == publicOrigin {
				pr.Out.Header.Set("Origin", target.Scheme+"://"+target.Host)
			}
			pr.Out.Header.Set("X-Forwarded-Host", pr.In.Host)
			pr.Out.Header.Set("X-Forwarded-Proto", strings.SplitN(publicOrigin, ":", 2)[0])
		},
		FlushInterval: 100 * time.Millisecond,
		ModifyResponse: func(resp *http.Response) error {
			upstreamPort := privatePreviewUpstreamHeader(resp.Header)
			// A switched injector can fall back to PREVIEW_PORT when its app
			// stops. Check the response actually being forwarded, including a
			// WebSocket handshake, so this capability never serves another app.
			// The four fixed runtime routes are served by the injector itself
			// and never upgrade to WebSockets.
			runtimeResponse := privatePreviewRuntimePath(c.Request.URL.Path) && resp.StatusCode != http.StatusSwitchingProtocols
			if rec.InjectorPort > 0 && !runtimeResponse && upstreamPort != strconv.Itoa(rec.Port) {
				return errors.New("preview upstream does not match registration")
			}
			stripPrivateReservedHeaders(resp.Header)
			// The injector's public diagnostic contains only one canonical app
			// port, never a platform credential. Keep its existing contract.
			if upstreamPort != "" {
				resp.Header.Set("X-Grasp-Upstream", upstreamPort)
			}
			cookies := resp.Header.Values("Set-Cookie")
			resp.Header.Del("Set-Cookie")
			for _, cookie := range cookies {
				name, _, _ := strings.Cut(cookie, "=")
				if strings.TrimSpace(name) != auth.CookieName {
					resp.Header.Add("Set-Cookie", cookie)
				}
			}
			resp.Header.Set("Cache-Control", "no-store")
			resp.Header.Set("Referrer-Policy", "no-referrer")
			if loc := resp.Header.Get("Location"); loc != "" {
				if parsed, err := url.Parse(loc); err == nil && strings.EqualFold(parsed.Host, target.Host) {
					parsed.Scheme = strings.SplitN(publicOrigin, ":", 2)[0]
					parsed.Host = c.Request.Host
					resp.Header.Set("Location", parsed.String())
				}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			// Provider errors can carry routing credentials; do not relay them
			// into browser responses or log their raw request URL/headers.
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("preview upstream unavailable"))
		},
	}
	// The five-second deadline above bounds control-plane lookup only. The
	// proxied request retains its original context for streams and WebSockets.
	proxy.ServeHTTP(c.Writer, c.Request)
}

func privatePreviewUpstreamHeader(header http.Header) string {
	values := header.Values("X-Grasp-Upstream")
	if len(values) != 1 {
		return ""
	}
	port, err := strconv.Atoi(values[0])
	if err != nil || port < 1 || port > 65535 || strconv.Itoa(port) != values[0] {
		return ""
	}
	return values[0]
}

func privatePreviewRuntimePath(path string) bool {
	switch path {
	case "/__grasp/preview-pick.js", "/__grasp/page-control.js", "/__grasp/live-overlay.js", "/__grasp/embed-origin":
		return true
	}
	return false
}

func stripPrivatePreviewHeaders(req *http.Request) {
	req.Header.Del("Proxy-Authorization")
	req.Header.Del("Forwarded")
	req.Header.Del("X-Forwarded-Prefix")
	stripPrivateReservedHeaders(req.Header)
	cookies := req.Cookies()
	req.Header.Del("Cookie")
	for _, cookie := range cookies {
		if cookie.Name != auth.CookieName {
			req.AddCookie(cookie)
		}
	}
}

func stripPrivateReservedHeaders(header http.Header) {
	for key := range header {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "e2b-") || strings.HasPrefix(lower, "x-e2b-") || strings.HasPrefix(lower, "x-grasp-") || strings.HasPrefix(lower, "x-gate-share-") || lower == "proxy-authorization" {
			header.Del(key)
		}
	}
}
