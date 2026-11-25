package handlers

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/previewexpose"
	"github.com/gin-gonic/gin"
)

// PreviewExposeMiddleware intercepts requests targeting any configured inbound
// preview host (e.g. wildcard subdomains) before CORS, static assets, API, and
// SPA fallback routes. Claimed hosts never fall through to platform routes.
func (h *Handlers) PreviewExposeMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.PreviewExpose == nil {
			c.Next()
			return
		}
		for _, inb := range h.PreviewExpose.Inbounds() {
			key, claimed := inb.MatchHost(c.Request.Host)
			if !claimed {
				continue
			}
			c.Abort()
			h.serveExposedPreview(c, inb, key)
			return
		}
		c.Next()
	}
}

func (h *Handlers) serveExposedPreview(c *gin.Context, inb previewexpose.Inbound, key string) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")

	if key == "" || h.MCP == nil || h.Preview == nil || h.PreviewExpose == nil {
		c.String(http.StatusNotFound, "preview not found")
		return
	}
	reg, ok := h.MCP.PreviewByExposeKey(key)
	if !ok || reg.SandboxName == "" {
		c.String(http.StatusNotFound, "preview not found")
		return
	}

	// Ownership verification: the registration's owning connection must currently
	// resolve to the exact Inbound instance that claimed this Host.
	connID, known := h.Preview.PreviewConnection(reg.SandboxName)
	exp := h.PreviewExpose.For(connID, known)
	expInb, isInbound := exp.(previewexpose.Inbound)
	if !isInbound || expInb != inb || reg.ExposeStrategy != exp.Name() {
		c.String(http.StatusNotFound, "preview not found")
		return
	}

	publicOrigin := strings.TrimRight(exp.URL(
		previewexpose.Target{
			RunID: reg.RunID, NodeID: reg.NodeID, SandboxName: reg.SandboxName,
			Port: reg.Port, InjectorPort: reg.InjectorPort, Upstream: reg.Host,
		},
		previewexpose.Binding{Strategy: reg.ExposeStrategy, Key: reg.ExposeKey},
	), "/")
	if publicOrigin == "" {
		c.String(http.StatusNotFound, "preview not found")
		return
	}
	publicScheme, _, _ := strings.Cut(publicOrigin, "://")

	// Subdomain ingress unconditionally enforces generation binding, wakes paused
	// sandboxes, touches activity, and refreshes dynamic bridge ports.
	host, ok := h.livePreviewUpstream(c, reg, true)
	if !ok {
		return
	}
	target, err := url.Parse(host)
	if err != nil || target.Scheme != "http" || target.Host == "" || target.User != nil ||
		target.Path != "" || target.RawQuery != "" || target.Fragment != "" {
		c.String(http.StatusBadGateway, "preview upstream unavailable")
		return
	}

	upstreamPath := c.Request.URL.Path
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			// Keep upstream Host local so dev servers like Vite accept the request.
			pr.Out.Host = target.Host
			stripExposedRequestHeaders(pr.Out)
			// Rewrite Origin only when it matches this registration's public origin
			// (permits HMR WebSockets while preserving CSWSH rejection for foreign origins).
			if pr.Out.Header.Get("Origin") == publicOrigin {
				pr.Out.Header.Set("Origin", target.Scheme+"://"+target.Host)
			}
			pr.Out.Header.Set("X-Forwarded-Host", pr.In.Host)
			pr.Out.Header.Set("X-Forwarded-Proto", publicScheme)
		},
		FlushInterval: 100 * time.Millisecond,
		ModifyResponse: func(resp *http.Response) error {
			if err := checkPrivateInjectorUpstream(reg, upstreamPath, resp); err != nil {
				return err
			}
			stripPrivateReservedHeaders(resp.Header)
			sanitizeExposedSetCookies(resp.Header)
			resp.Header.Set("Cache-Control", "no-store")
			resp.Header.Set("Referrer-Policy", "no-referrer")
			resp.Header.Set("X-Content-Type-Options", "nosniff")
			if loc := resp.Header.Get("Location"); loc != "" {
				if parsed, err := url.Parse(loc); err == nil && strings.EqualFold(parsed.Host, target.Host) {
					parsed.Scheme = publicScheme
					parsed.Host = c.Request.Host
					resp.Header.Set("Location", parsed.String())
				}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("preview upstream unavailable"))
		},
	}
	proxy.ServeHTTP(c.Writer, c.Request)
}

func stripExposedRequestHeaders(req *http.Request) {
	req.Header.Del("Forwarded")
	req.Header.Del("X-Forwarded-Prefix")
	req.Header.Del("Proxy-Authorization")
	stripPrivateReservedHeaders(req.Header)
	forwardCookiesToUpstream(req)
}

// sanitizeExposedSetCookies drops the platform session cookie and strips any
// Domain attribute from app cookies (forcing host-only cookies) so sibling
// preview tickets under the same base domain cannot toss cookies at each other.
func sanitizeExposedSetCookies(header http.Header) {
	values := header.Values("Set-Cookie")
	if len(values) == 0 {
		return
	}
	header.Del("Set-Cookie")
	for _, raw := range values {
		cookie, err := http.ParseSetCookie(raw)
		if err != nil {
			continue
		}
		if strings.TrimSpace(cookie.Name) == auth.CookieName {
			continue
		}
		cookie.Domain = ""
		header.Add("Set-Cookie", cookie.String())
	}
}
