package handlers

import (
	"context"
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
			upstreamPorts := resp.Header.Values("X-Grasp-Upstream")
			stripPrivateReservedHeaders(resp.Header)
			// The injector's public diagnostic contains only one canonical app
			// port, never a platform credential. Keep its existing contract.
			if len(upstreamPorts) == 1 {
				if port, err := strconv.Atoi(upstreamPorts[0]); err == nil && port >= 1 && port <= 65535 && strconv.Itoa(port) == upstreamPorts[0] {
					resp.Header.Set("X-Grasp-Upstream", upstreamPorts[0])
				}
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
