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

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/mcp"
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
	// Private registrations are served exclusively on isolated origins. A
	// same-origin path would let sandbox HTML use the platform's login cookie.
	if h.MCP != nil && h.MCP.PrivatePreviews() {
		c.String(http.StatusNotFound, "preview not found")
		return
	}
	runID := c.Param("runId")
	nodeID := c.Param("nodeId")
	port, err := strconv.Atoi(c.Param("port"))
	if err != nil || port <= 0 {
		c.String(http.StatusBadRequest, "bad port")
		return
	}
	writePreviewCORS(c)
	if c.Request.Method == http.MethodOptions && c.GetHeader("Access-Control-Request-Method") != "" {
		c.Status(http.StatusNoContent)
		return
	}
	if h.MCP == nil || h.Preview == nil {
		c.String(http.StatusServiceUnavailable, "preview unavailable")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	reg := h.lookupPreviewRegistration(runID, nodeID, port)
	closeConsumer, consumerErr := h.Preview.BeginPreviewConsumer(c.Request.Context(), reg)
	if consumerErr != nil {
		workspaceBindingError(c, consumerErr)
		return
	}
	defer func() { _ = closeConsumer() }()
	host, sandboxName := reg.Host, reg.SandboxName

	// When co-located with the sandbox manager, enforce liveness + health and
	// refresh a stale/empty upstream host (container IP changes across restarts).
	if h.Sbx != nil {
		if mgr := h.Sbx.Manager(); mgr != nil && sandboxName != "" {
			if mgr.Status(ctx, sandboxName) != "running" {
				c.String(http.StatusGone, "sandbox recycled")
				return
			}
			h.Sbx.TouchActivityByName(sandboxName)
			fresh, healthy := h.Preview.ProbeRegisteredPort(ctx, reg)
			if err := h.Preview.UpdatePreviewHealth(runID, nodeID, port, healthy); err != nil {
				log.Warn().Err(err).Str("runId", runID).Str("nodeId", nodeID).Int("port", port).
					Msg("persist preview health failed")
			}
			if !healthy {
				c.String(http.StatusServiceUnavailable, "preview service unavailable")
				return
			}
			if fresh != host {
				host = fresh
				if err := h.Preview.UpdatePreviewHost(runID, nodeID, port, host); err != nil {
					log.Warn().Err(err).Str("runId", runID).Str("nodeId", nodeID).Int("port", port).
						Msg("persist preview host failed")
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
	rewritePreview := rewritePreviewResponse(prefix, previewStorageShim)
	proxy.ModifyResponse = func(resp *http.Response) error {
		dropSessionSetCookie(resp.Header)
		resp.Header.Del("Access-Control-Allow-Origin")
		resp.Header.Del("Access-Control-Allow-Credentials")
		// App code must never run with the Grasp origin, even as a top-level tab.
		// allow-popups-to-escape-sandbox and allow-same-origin would undo that.
		resp.Header.Add("Content-Security-Policy", previewSandboxCSP)
		return rewritePreview(resp)
	}
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
	forwardCookiesToUpstream(c.Request)

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
	return rewritePreviewResponse(prefix, "")
}

// rewritePreviewResponse is previewModifyResponse plus headHTML placed at the
// start of every HTML document, ahead of the app's own scripts.
func rewritePreviewResponse(prefix, headHTML string) func(*http.Response) error {
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

		head := headHTML
		if !reHasBase.MatchString(html) {
			head = `<base href="` + prefix + `">` + head
		}
		if head != "" {
			if loc := reHeadOpen.FindStringIndex(html); loc != nil {
				html = html[:loc[1]] + head + html[loc[1]:]
			} else if loc := reHtmlOpen.FindStringIndex(html); loc != nil {
				html = html[:loc[1]] + head + html[loc[1]:]
			} else {
				html = head + html
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

const previewSandboxCSP = "sandbox allow-scripts allow-forms allow-popups allow-modals allow-downloads"

// previewStorageShim keeps apps that touch Web Storage or document.cookie at
// startup working in the opaque origin, where those APIs throw. Values live
// for the page only.
const previewStorageShim = `<script>(function(){` +
	`function mem(){var d={};return{get length(){return Object.keys(d).length},` +
	`key:function(i){var k=Object.keys(d);return i<k.length?k[i]:null},` +
	`getItem:function(k){k=String(k);return Object.prototype.hasOwnProperty.call(d,k)?d[k]:null},` +
	`setItem:function(k,v){d[String(k)]=String(v)},removeItem:function(k){delete d[String(k)]},clear:function(){d={}}}}` +
	`["localStorage","sessionStorage"].forEach(function(n){try{window[n]}catch(e){` +
	`try{Object.defineProperty(window,n,{value:mem(),configurable:true})}catch(_){}}});` +
	`try{document.cookie}catch(e){var j={};try{Object.defineProperty(document,"cookie",{configurable:true,` +
	`get:function(){return Object.keys(j).map(function(k){return k+"="+j[k]}).join("; ")},` +
	`set:function(v){v=String(v);var p=v.split(";")[0],i=p.indexOf("=");if(i<1)return;var k=p.slice(0,i).trim();` +
	`if(/;\s*max-age=(0|-)|;\s*expires=thu, 01 jan 1970/i.test(v))delete j[k];else j[k]=p.slice(i+1).trim()}})}catch(_){}}` +
	`})()</script>`

// writePreviewCORS answers for the opaque origin the sandboxed preview runs
// in: the app's requests to its own paths arrive with Origin: null.
func writePreviewCORS(c *gin.Context) {
	header := c.Writer.Header()
	if c.GetHeader("Origin") == "null" {
		header.Set("Access-Control-Allow-Origin", "null")
		header.Set("Access-Control-Allow-Credentials", "true")
		header.Add("Vary", "Origin")
	} else {
		header.Set("Access-Control-Allow-Origin", "*")
	}
	header.Set("Access-Control-Allow-Methods", "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS")
	allowed := c.GetHeader("Access-Control-Request-Headers")
	if allowed == "" {
		allowed = "Content-Type, Authorization"
	}
	header.Set("Access-Control-Allow-Headers", allowed)
}

// dropSessionSetCookie keeps the app's cookies but never lets it set
// cf_session, which would share this host with the workspace.
func dropSessionSetCookie(header http.Header) {
	values := header.Values("Set-Cookie")
	header.Del("Set-Cookie")
	for _, v := range values {
		name, _, _ := strings.Cut(v, "=")
		if strings.TrimSpace(name) != auth.CookieName {
			header.Add("Set-Cookie", v)
		}
	}
}

// isRootAbsolute reports whether p is a root-absolute path ("/x") rather than a
// protocol-relative ("//host") or absolute ("http://") URL.
func isRootAbsolute(p string) bool {
	return strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//")
}

// lookupPreviewRegistration finds the upstream host and sandbox name from MCP
// memory / DB without talking to the sandbox manager.
func (h *Handlers) lookupPreviewRegistration(runID, nodeID string, port int) mcp.PreviewPort {
	reg := mcp.PreviewPort{RunID: runID, NodeID: nodeID, Port: port}
	if h.MCP != nil {
		for _, p := range h.MCP.ListPreviewPorts(runID, nodeID) {
			if p.Port == port {
				reg = p
				break
			}
		}
	}
	if (reg.Host == "" || reg.SandboxName == "") && h.Preview != nil {
		if rec, okRec := h.Preview.GetPreviewPort(runID, nodeID, port); okRec {
			if reg.Host == "" {
				reg.Host, reg.InjectorPort = rec.Host, rec.InjectorPort
			}
			if reg.SandboxName == "" {
				reg.SandboxName = rec.SandboxName
			}
		}
	}
	if reg.SandboxName == "" && h.Preview != nil {
		if name, okName := h.Preview.SandboxForRunNode(runID, nodeID); okName {
			reg.SandboxName = name
		}
	}
	return reg
}
