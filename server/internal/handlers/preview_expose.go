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

// PreviewExposeRouter sends preview hosts to their own route-less engine
// before platform matches a route, so platform redirects (trailing slash)
// never rewrite app paths and claimed hosts never reach CORS, static assets,
// API or SPA routes. A ticket host that no configured base claims (a retired
// base_url whose wildcard DNS still points here) gets 404 as well.
func (h *Handlers) PreviewExposeRouter(platform http.Handler) http.Handler {
	previews := gin.New()
	previews.Use(gin.Recovery())
	previews.NoRoute(func(c *gin.Context) {
		inb, key, _ := h.matchPreviewHost(c.Request.Host)
		h.serveExposedPreview(c, inb, key)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, _, claimed := h.matchPreviewHost(r.Host); claimed {
			previews.ServeHTTP(w, r)
			return
		}
		platform.ServeHTTP(w, r)
	})
}

// matchPreviewHost returns the inbound that claims authority and its key.
// A retired ticket host is claimed with no inbound and an empty key.
func (h *Handlers) matchPreviewHost(authority string) (previewexpose.Inbound, string, bool) {
	for _, inb := range h.PreviewExpose.Inbounds() {
		if key, claimed := inb.MatchHost(authority); claimed {
			return inb, key, true
		}
	}
	return nil, "", previewexpose.TicketHost(authority)
}

// exposedSecurityHeaders are set once on every claimed-host response and
// always win over the upstream app's values.
var exposedSecurityHeaders = [...][2]string{
	{"Cache-Control", "no-store"},
	{"Referrer-Policy", "no-referrer"},
	{"X-Content-Type-Options", "nosniff"},
}

func (h *Handlers) serveExposedPreview(c *gin.Context, inb previewexpose.Inbound, key string) {
	for _, kv := range exposedSecurityHeaders {
		c.Header(kv[0], kv[1])
	}

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
		previewexpose.Target{Upstream: reg.Host},
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
			for _, kv := range exposedSecurityHeaders {
				resp.Header.Del(kv[0])
			}
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
// Cookies are edited as text: re-serializing through net/http would drop
// values and attributes that browsers accept.
func sanitizeExposedSetCookies(header http.Header) {
	values := header.Values("Set-Cookie")
	if len(values) == 0 {
		return
	}
	header.Del("Set-Cookie")
	for _, raw := range values {
		parts := strings.Split(raw, ";")
		name, _, _ := strings.Cut(parts[0], "=")
		if strings.TrimSpace(name) == auth.CookieName {
			continue
		}
		kept := append(make([]string, 0, len(parts)), parts[0])
		for _, attr := range parts[1:] {
			attrName, _, _ := strings.Cut(attr, "=")
			if !strings.EqualFold(strings.TrimSpace(attrName), "domain") {
				kept = append(kept, attr)
			}
		}
		header.Add("Set-Cookie", strings.Join(kept, ";"))
	}
}
