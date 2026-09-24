package handlers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// ListNodePreviews returns registered preview ports for a run/node.
func (h *Handlers) ListNodePreviews(c *gin.Context) {
	runID := c.Param("id")
	nodeID := c.Param("nodeId")
	if h.MCP == nil {
		c.JSON(http.StatusOK, gin.H{"ports": []any{}})
		return
	}
	ports := h.MCP.ListPreviewPorts(runID, nodeID)
	c.JSON(http.StatusOK, gin.H{"ports": ports})
}

// Root-absolute attribute references (src/href/action/...) that must be
// re-anchored under the preview sub-path. RE2 has no backreferences, so double-
// and single-quoted attributes are handled by two patterns. `//host` protocol-
// relative and `/x` root-absolute are distinguished by requiring the char after
// the leading slash to not be another slash (or quote).
var (
	reRootAbsDbl = regexp.MustCompile(`(\s(?:src|href|action|poster|data-src)=")/([^/"][^"]*|)"`)
	reRootAbsSgl = regexp.MustCompile(`(\s(?:src|href|action|poster|data-src)=')/([^/'][^']*|)'`)
	reHeadOpen   = regexp.MustCompile(`(?i)<head[^>]*>`)
	reHtmlOpen   = regexp.MustCompile(`(?i)<html[^>]*>`)
	reHasBase    = regexp.MustCompile(`(?i)<base\s`)
)

// PreviewProxy reverse-proxies /preview/:runId/:nodeId/:port/* to the sandbox
// app, transparently (like nginx proxy_pass + sub_filter): it strips the mount
// prefix so the app is dialed at its own root, then rewrites HTML responses so
// the browser re-anchors relative and root-absolute URLs under the sub-path.
// This lets the sandboxed app serve at "/" with zero path/base awareness — the
// only requirement is that it listens on 0.0.0.0:<port>.
//
// The upstream host is read from the persisted registration (RunPreviewPort.Host)
// so this handler does not strictly depend on the co-located sandbox manager and
// can later be split into a standalone proxy service. When the manager is present
// it additionally enforces liveness/health and self-heals a stale host after a
// container restart.
func (h *Handlers) PreviewProxy(c *gin.Context) {
	runID := c.Param("runId")
	nodeID := c.Param("nodeId")
	port, err := strconv.Atoi(c.Param("port"))
	if err != nil || port <= 0 {
		c.String(http.StatusBadRequest, "bad port")
		return
	}
	if h.MCP == nil || h.Preview == nil {
		c.String(http.StatusServiceUnavailable, "preview unavailable")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	host, sandboxName := h.lookupPreviewRegistration(runID, nodeID, port)

	// When co-located with the sandbox manager, enforce liveness + health and
	// refresh a stale/empty upstream host (container IP changes across restarts).
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

	prefix := fmt.Sprintf("/preview/%s/%s/%d/", runID, nodeID, port)

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = 100 * time.Millisecond
	proxy.ModifyResponse = previewModifyResponse(prefix)
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		log.Warn().Err(err).Str("runId", runID).Str("nodeId", nodeID).Int("port", port).
			Msg("preview upstream unreachable")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("preview upstream unreachable"))
	}

	// Strip the mount prefix so the app sees root paths.
	path := c.Param("path")
	if path == "" {
		path = "/"
	}
	c.Request.URL.Path = path
	// Ask upstream for identity encoding so ModifyResponse can rewrite HTML
	// bodies (a gzipped body can't be sub_filtered without decoding first).
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

// previewModifyResponse re-anchors upstream responses under the preview sub-path
// so the sandboxed app needs no base/path awareness:
//   - 3xx Location headers pointing at a root-absolute path get the prefix
//     prepended (this is what kills the redirect loop when the app redirects "/").
//   - HTML bodies get a <base href="prefix"> injected (relative URLs) and their
//     root-absolute src/href/action attributes rewritten (default SPA builds).
//
// JS/CSS bodies are intentionally left untouched to avoid corrupting bundles;
// once the entry HTML points at the right asset URLs, Vite/webpack chunks load
// relative to their own module URL and resolve correctly.
func previewModifyResponse(prefix string) func(*http.Response) error {
	return func(resp *http.Response) error {
		// Never touch a protocol switch (WebSocket/HMR upgrade).
		if resp.StatusCode == http.StatusSwitchingProtocols {
			return nil
		}

		// Re-anchor redirects to a root-absolute path.
		if loc := resp.Header.Get("Location"); loc != "" {
			if isRootAbsolute(loc) {
				resp.Header.Set("Location", prefix+strings.TrimPrefix(loc, "/"))
			}
		}

		if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
			return nil
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		_ = resp.Body.Close()

		html := string(body)
		// Rewrite root-absolute attribute URLs BEFORE injecting our own <base>
		// (whose href starts with the prefix and would otherwise be double-prefixed).
		dblRepl := "${1}" + prefix + "${2}\""
		sglRepl := "${1}" + prefix + "${2}'"
		html = reRootAbsDbl.ReplaceAllString(html, dblRepl)
		html = reRootAbsSgl.ReplaceAllString(html, sglRepl)

		if !reHasBase.MatchString(html) {
			baseTag := `<base href="` + prefix + `">`
			if loc := reHeadOpen.FindStringIndex(html); loc != nil {
				html = html[:loc[1]] + baseTag + html[loc[1]:]
			} else if loc := reHtmlOpen.FindStringIndex(html); loc != nil {
				html = html[:loc[1]] + baseTag + html[loc[1]:]
			} else {
				html = baseTag + html
			}
		}

		newBody := []byte(html)
		resp.Body = io.NopCloser(bytes.NewReader(newBody))
		resp.ContentLength = int64(len(newBody))
		resp.Header.Set("Content-Length", strconv.Itoa(len(newBody)))
		resp.Header.Del("Content-Encoding")
		return nil
	}
}

// isRootAbsolute reports whether p is a root-absolute path ("/x") rather than a
// protocol-relative ("//host") or absolute ("http://") URL.
func isRootAbsolute(p string) bool {
	return strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//")
}

// lookupPreviewRegistration finds the upstream host and sandbox name from MCP
// memory / DB without talking to the sandbox manager.
func (h *Handlers) lookupPreviewRegistration(runID, nodeID string, port int) (host, sandboxName string) {
	if h.MCP != nil {
		for _, p := range h.MCP.ListPreviewPorts(runID, nodeID) {
			if p.Port == port {
				host, sandboxName = p.Host, p.SandboxName
				break
			}
		}
	}
	if (host == "" || sandboxName == "") && h.Preview != nil {
		if rec, okRec := h.Preview.GetPreviewPort(runID, nodeID, port); okRec {
			if host == "" {
				host = rec.Host
			}
			if sandboxName == "" {
				sandboxName = rec.SandboxName
			}
		}
	}
	if sandboxName == "" && h.Preview != nil {
		if name, okName := h.Preview.SandboxForRunNode(runID, nodeID); okName {
			sandboxName = name
		}
	}
	return host, sandboxName
}

// resolvePreviewTarget returns the bridge-IP upstream URL (with trailing slash)
// and sandbox container name for a registered preview port. Used by PreviewVNC.
// When the co-located manager reports the sandbox is not running, ok is false
// and sandboxName is still set so callers can distinguish "recycled" from
// "not registered".
func (h *Handlers) resolvePreviewTarget(ctx context.Context, runID, nodeID string, port int) (host, sandboxName string, ok bool) {
	host, sandboxName = h.lookupPreviewRegistration(runID, nodeID, port)
	if h.Sbx != nil && sandboxName != "" && h.Preview != nil {
		if mgr := h.Sbx.Manager(); mgr != nil {
			if mgr.Status(ctx, sandboxName) != "running" {
				return "", sandboxName, false
			}
			if fresh, okUp := h.Preview.PreviewUpstream(ctx, sandboxName, port); okUp && fresh != "" {
				host = fresh
				if err := h.Preview.UpdatePreviewHost(runID, nodeID, port, host); err != nil {
					log.Warn().Err(err).Str("runId", runID).Str("nodeId", nodeID).Int("port", port).
						Msg("persist preview host failed")
				}
			}
		}
	}
	if host == "" {
		return "", sandboxName, false
	}
	return host + "/", sandboxName, true
}
