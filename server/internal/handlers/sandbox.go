package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

func parseUintParam(c *gin.Context, name string) (uint, bool) {
	// bitSize=strconv.IntSize rejects values that cannot fit platform uint (CodeQL #10).
	v, err := strconv.ParseUint(c.Param(name), 10, strconv.IntSize)
	if err != nil {
		return 0, false
	}
	return uint(v), true
}

// testRepoInput is one row from POST /agents/:name/test {repos: [...]}.
type testRepoInput struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Branch string `json:"branch"`
}

// resolveTestRepos picks the clone list for interactive test sandboxes:
// non-empty repos[] (trimmed, deduped by name, skip partial rows) wins over
// legacy repoUrl (ReposFromURL), then an empty list (pure artifact workspace).
func resolveTestRepos(repos []testRepoInput, repoURL string) []sandbox.RepoSpec {
	if len(repos) > 0 {
		seen := map[string]bool{}
		out := make([]sandbox.RepoSpec, 0, len(repos))
		for _, r := range repos {
			name := strings.TrimSpace(r.Name)
			url := strings.TrimSpace(r.URL)
			if name == "" || url == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, sandbox.RepoSpec{
				Name:   name,
				URL:    url,
				Branch: strings.TrimSpace(r.Branch),
			})
		}
		return out
	}
	return sandbox.ReposFromURL(repoURL)
}

// SandboxEvents returns a sandbox's full agent event log, read directly from
// the container's acp-bridge service (no platform-side persistence).
func (h *Handlers) SandboxEvents(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad id"})
		return
	}
	ev, err := h.Sbx.Events(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": ev})
}

// SandboxEventLog returns the raw agent event frames (unaggregated), used by the
// chat tester to rebuild the transcript (incl. original user prompts) when a
// reused sandbox is reopened.
func (h *Handlers) SandboxEventLog(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad id"})
		return
	}
	cp, ok := parseCursorPagination(c)
	if !ok {
		return
	}
	if !cp.Active {
		frames, err := h.Sbx.EventLog(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"events": frames})
		return
	}
	page, err := h.Sbx.EventLogPage(c.Request.Context(), id, cp.Cursor, cp.Limit)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"events":     page.Events,
		"nextCursor": page.NextCursor,
		"hasMore":    page.HasMore,
	})
}

// SandboxLog returns a sandbox container's raw stdout/stderr (docker logs),
// preferring live output and falling back to the archived snapshot captured at
// teardown. Used for post-mortem troubleshooting (e.g. failed git clone).
// Read failures include an `error` field and must not be confused with
// found=false (no log source).
func (h *Handlers) SandboxLog(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad id"})
		return
	}
	content, live, err := h.Sbx.SandboxLogByID(c.Request.Context(), id)
	if err != nil {
		writeSandboxLogErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": content, "live": live, "found": true})
}

// RunNodeSandbox returns the sandbox for a workflow run's node. Responds 404
// when no record exists or the container is not running.
func (h *Handlers) RunNodeSandbox(c *gin.Context) {
	runID := c.Param("id")
	nodeID := c.Param("nodeId")
	v, err := h.Sbx.SandboxViewForRunNode(c.Request.Context(), runID, nodeID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, v)
}

// NodeSandboxLog returns the container logs for a workflow run's node sandbox,
// live if still running, otherwise the archived snapshot. Read failures include
// an `error` field (found=false) so the UI can show an error state instead of
// the empty "no source" placeholder.
func (h *Handlers) NodeSandboxLog(c *gin.Context) {
	runID := c.Param("id")
	nodeID := c.Param("nodeId")
	content, live, err := h.Sbx.NodeSandboxLog(c.Request.Context(), runID, nodeID)
	if err != nil {
		writeSandboxLogErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": content, "live": live, "found": true})
}

// writeSandboxLogErr distinguishes "no log source" from control-plane read
// failures while keeping HTTP 200 so clients can map six UI states without
// treating a logs read error as a global API outage.
func writeSandboxLogErr(c *gin.Context, err error) {
	if isSandboxLogNoSource(err) {
		c.JSON(http.StatusOK, gin.H{"content": "", "live": false, "found": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"content": "",
		"live":    false,
		"found":   false,
		"error":   err.Error(),
	})
}

func isSandboxLogNoSource(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return msg == "not found" || strings.Contains(msg, "record not found")
}

// GetSandbox returns one sandbox with live-derived status. The UI polls this
// to watch a "creating" sandbox flip to running/error after test creation.
func (h *Handlers) GetSandbox(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad id"})
		return
	}
	v, err := h.Sbx.GetView(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, v)
}

// ListSandboxes returns all interactive sandboxes.
func (h *Handlers) ListSandboxes(c *gin.Context) {
	views, err := h.Sbx.List(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, views)
}

// StopSandbox stops a sandbox container (keeps the record).
func (h *Handlers) StopSandbox(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad id"})
		return
	}
	if err := h.Sbx.Stop(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "stopped"})
}

// DestroySandbox removes a sandbox container and its record.
func (h *Handlers) DestroySandbox(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad id"})
		return
	}
	if h.SbxChats != nil {
		h.SbxChats.Cancel(id)
	}
	if err := h.Sbx.Destroy(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// CleanupSandboxes destroys all idle (non-busy) sandboxes.
func (h *Handlers) CleanupSandboxes(c *gin.Context) {
	destroyed, skipped := h.Sbx.CleanupIdle(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{"destroyed": destroyed, "skipped": skipped})
}

// SandboxChat is the Agent Studio / sandbox-console chat WebSocket. Turns run
// in the sandbox's chatsession FIFO and outlive the connection. On connect the
// server sends a queue_state snapshot and, while busy, replays the active turn;
// then live frames follow: {"type":"session","event":"queue_state"|
// "turn_begin"|"turn_done"|"error",…} and {"type":"acp","data":{…}}. The client
// sends {"type":"chat","id":"…","content":"…","images":[…]} to queue a turn and
// {"type":"cancel"} to stop the running turn and clear the queue.
func (h *Handlers) SandboxChat(c *gin.Context) {
	if h.Auth != nil {
		if _, ok := h.Auth.RequireSession(c); !ok {
			return
		}
	}
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	if h.SbxChats == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "sandbox chat unavailable"})
		return
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Debug().Uint64("sandbox_id", uint64(id)).Err(err).Msg("sandbox websocket upgrade failed")
		return
	}
	defer func() { _ = conn.Close() }()

	var wmu sync.Mutex
	write := func(v any) error {
		wmu.Lock()
		defer wmu.Unlock()
		return conn.WriteJSON(v)
	}

	ch, unsub := h.SbxChats.Subscribe(id, -1)
	defer unsub()
	go func() {
		for ev := range ch {
			if write(ev.Frame()) != nil {
				_ = conn.Close()
				return
			}
		}
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var m struct {
			Type    string               `json:"type"`
			ID      string               `json:"id"`
			Content string               `json:"content"`
			Images  []models.PromptImage `json:"images"`
		}
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		switch m.Type {
		case "cancel":
			h.SbxChats.Cancel(id)
		case "chat", "":
			if m.Content == "" && len(m.Images) == 0 {
				continue
			}
			if _, err := h.SbxChats.Enqueue(id, services.SandboxChatItem{ID: strings.TrimSpace(m.ID), Content: m.Content, Images: m.Images}); err != nil {
				_ = write(gin.H{"type": "session", "event": "error", "message": err.Error(), "interrupted": false})
			}
		}
	}
}

// SandboxTerminal bridges a browser xterm to an interactive shell inside the
// sandbox container over WebSocket. Client frames: {"type":"input","data":"…"}
// and {"type":"resize","cols":N,"rows":M}. Server frames are raw PTY bytes.
func (h *Handlers) SandboxTerminal(c *gin.Context) {
	if h.Auth != nil {
		if _, ok := h.Auth.RequireSession(c); !ok {
			return
		}
	}
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Debug().Uint64("sandbox_id", uint64(id)).Err(err).Msg("sandbox websocket upgrade failed")
		return
	}
	defer func() { _ = conn.Close() }()

	term, err := h.Sbx.OpenTerminal(c.Request.Context(), id)
	if err != nil {
		_ = conn.WriteJSON(gin.H{"type": "error", "data": err.Error()})
		return
	}
	defer func() {
		_ = term.Close()
	}()

	// PTY → browser (binary frames).
	go func() {
		buf := make([]byte, 4096)
		for {
			n, rerr := term.Read(buf)
			if n > 0 {
				if werr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					return
				}
			}
			if rerr != nil {
				_ = conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
		}
	}()

	// browser → PTY (control + input frames).
	for {
		_, data, rerr := conn.ReadMessage()
		if rerr != nil {
			return
		}
		var m struct {
			Type string `json:"type"`
			Data string `json:"data"`
			Cols uint16 `json:"cols"`
			Rows uint16 `json:"rows"`
		}
		if json.Unmarshal(data, &m) == nil && m.Type != "" {
			switch m.Type {
			case "input":
				_, _ = io.WriteString(term, m.Data)
			case "resize":
				_ = term.Resize(m.Rows, m.Cols)
			}
			continue
		}
		// Fallback: treat raw frame as input bytes.
		_, _ = term.Write(data)
	}
}

// SandboxProxy reverse-proxies /sandbox/:id/* to the sandbox's in-container
// code-server (best effort; requires the image to ship code-server).
// Upstream is the runtime-reachable ide host:port from the gateway (never a
// hard-coded 127.0.0.1); Docker loopback endpoints keep working unchanged.
// When the sandbox row has a Token (injected as PASSWORD into the container),
// the proxy auto-logs into code-server so the browser never sees the password
// form — same approach as remote-dev's SandboxProxy.
func (h *Handlers) SandboxProxy(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		c.String(http.StatusBadRequest, "bad id")
		return
	}
	row, err := h.Sbx.Get(id)
	if err != nil || row.CodeServerPort == 0 {
		c.String(http.StatusNotFound, "no code-server for this sandbox")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	dialAddr, uerr := h.Sbx.IDEUpstream(ctx, id)
	if uerr != nil || strings.TrimSpace(dialAddr) == "" {
		c.String(http.StatusNotFound, "no code-server for this sandbox")
		return
	}
	h.serveSandboxUpstream(c, id, "IDE", dialAddr, row.Token)
}

// SandboxACPProxy reverse-proxies /sandbox-bridge/:id/* and /sandbox-acp/:id/*
// to the sandbox's in-container acp-bridge server (port 8765). This exposes the
// native ACP web UI (and its /ws + /api/* endpoints) directly in the browser,
// complementing the platform-mediated chat tester. The mount prefix is stripped
// so acp-bridge sees root paths; its web UI resolves assets/WS against
// document.baseURI, so it works transparently behind this subpath.
func (h *Handlers) SandboxACPProxy(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		c.String(http.StatusBadRequest, "bad id")
		return
	}
	row, err := h.Sbx.Get(id)
	if err != nil || row.ACPPort == 0 {
		c.String(http.StatusNotFound, "no ACP bridge for this sandbox")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	dialAddr, uerr := h.Sbx.ACPUpstream(ctx, id)
	if uerr != nil || strings.TrimSpace(dialAddr) == "" {
		c.String(http.StatusNotFound, "no ACP bridge for this sandbox")
		return
	}
	h.serveSandboxUpstream(c, id, "ACP", dialAddr, row.Token)
}

// serveSandboxUpstream reverse-proxies the current request to dialAddr
// (host:port), stripping the gin mount prefix and rewriting Origin / Host so
// code-server and acp-bridge same-origin checks pass. On dial failure the
// iframe receives a dark-theme friendly error page (not a bare edge 502).
// For IDE/ACP channels, password (sandbox Token) triggers upstream auto-login
// before the reverse proxy runs (remote-dev SandboxProxy / VibeCodingProxy).
func (h *Handlers) serveSandboxUpstream(c *gin.Context, sandboxID uint, channel, dialAddr, password string) {
	idStr := strconv.FormatUint(uint64(sandboxID), 10)
	acpMount := "/sandbox-acp/" + idStr
	if strings.HasPrefix(c.Request.URL.Path, "/sandbox-bridge/") {
		acpMount = "/sandbox-bridge/" + idStr
	}
	if strings.TrimSpace(password) != "" {
		switch channel {
		case "IDE":
			if isWebSocketUpgrade(c.Request) {
				cookies, loginErr := forceUpstreamLogin(c.Request.Context(), dialAddr, password)
				if loginErr != nil {
					log.Warn().Err(loginErr).Uint("sandboxId", sandboxID).
						Str("upstream", dialAddr).Msg("code-server WS force login failed")
				} else {
					applyUpstreamLoginCookies(c, sandboxMountPrefix(sandboxID), cookies)
				}
			} else if err := autoLoginCodeServer(c, sandboxID, dialAddr, password); err != nil {
				log.Warn().Err(err).Uint("sandboxId", sandboxID).
					Str("upstream", dialAddr).Msg("code-server auto login failed")
			}
		case "ACP":
			if isWebSocketUpgrade(c.Request) {
				cookies, loginErr := forceVibeLogin(c.Request.Context(), dialAddr, password)
				if loginErr != nil {
					log.Warn().Err(loginErr).Uint("sandboxId", sandboxID).
						Str("upstream", dialAddr).Msg("ACP WS force login failed")
				} else {
					applyUpstreamLoginCookies(c, acpMount, cookies)
				}
			} else if err := autoLoginVibeCoding(c, sandboxID, acpMount, dialAddr, password); err != nil {
				log.Warn().Err(err).Uint("sandboxId", sandboxID).
					Str("upstream", dialAddr).Msg("ACP auto login failed")
			}
		}
	}

	target := &url.URL{Scheme: "http", Host: dialAddr}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = 100 * time.Millisecond
	// Bound dial so a dead upstream fails into ErrorHandler quickly (K8s 502
	// pages should not hang the iframe for the default ~30s dial timeout).
	proxy.Transport = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   3 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   3 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		log.Warn().Err(err).Uint("sandboxId", sandboxID).Str("channel", channel).
			Str("upstream", dialAddr).Msg("sandbox upstream unreachable")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(sandboxProxyErrorHTML(channel, dialAddr, err)))
	}

	clientQ := url.Values{}
	for k, vv := range c.Request.URL.Query() {
		clientQ[k] = append([]string(nil), vv...)
	}
	mountPrefix := ""
	switch channel {
	case "IDE":
		mountPrefix = sandboxMountPrefix(sandboxID)
	case "ACP":
		// Prefer the request's actual mount (/sandbox-bridge or legacy /sandbox-acp).
		idStr := strconv.FormatUint(uint64(sandboxID), 10)
		if strings.HasPrefix(c.Request.URL.Path, "/sandbox-bridge/") {
			mountPrefix = "/sandbox-bridge/" + idStr
		} else {
			mountPrefix = "/sandbox-acp/" + idStr
		}
	}

	orig := proxy.Director
	proxy.Director = func(req *http.Request) {
		orig(req)
		if channel == "IDE" {
			mergeUpstreamQuery(req, clientQ)
		}
		forwardCookiesToUpstream(req)
	}
	if mountPrefix != "" {
		prefix := mountPrefix
		proxy.ModifyResponse = func(resp *http.Response) error {
			resp.Header.Del("X-Frame-Options")
			rewriteUpstreamSetCookiePaths(resp, prefix)
			if resp.StatusCode >= 300 && resp.StatusCode < 400 {
				rewriteRedirectLocation(resp, prefix)
			}
			return nil
		}
	}

	// Strip the /sandbox/:id (or /sandbox-bridge|/sandbox-acp/:id) mount prefix
	// so the upstream sees root paths.
	path := c.Param("path")
	if path == "" {
		path = "/"
	}
	c.Request.URL.Path = path
	c.Request.Host = target.Host
	// code-server / acp-bridge enforce same-origin on WebSocket upgrades. Behind
	// this reverse proxy the browser Origin is the app host; rewrite it to the
	// upstream so the check passes.
	if c.Request.Header.Get("Origin") != "" {
		c.Request.Header.Set("Origin", target.Scheme+"://"+target.Host)
	}
	// code-server's host check prefers Forwarded / X-Forwarded-Host over Host.
	// Strip ingress-injected values so the rewritten Host wins.
	c.Request.Header.Del("Forwarded")
	c.Request.Header.Del("X-Forwarded-Host")
	proxy.ServeHTTP(c.Writer, c.Request)
}

// sandboxProxyErrorHTML renders a console-dark friendly iframe error page that
// surfaces the dial target (host:port) without leaking auth tokens.
func sandboxProxyErrorHTML(channel, dialAddr string, err error) string {
	reason := "connection failed"
	if err != nil {
		reason = err.Error()
	}
	ch := html.EscapeString(channel)
	addr := html.EscapeString(dialAddr)
	why := html.EscapeString(reason)
	return `<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<title>` + ch + ` 不可达</title>` +
		`<style>
body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;
background:#0f1419;color:#e7ecf3;font:14px/1.55 ui-sans-serif,system-ui,sans-serif}
.card{max-width:36rem;padding:1.5rem 1.75rem}
h1{font-size:1.05rem;font-weight:600;margin:0 0 .6rem;color:#f3f6fb}
code{background:#1a2332;color:#9ecbff;padding:.12rem .4rem;border-radius:4px;font-size:12px}
.muted{color:#8b9bb4;margin-top:.85rem;font-size:12px;word-break:break-word}
</style></head><body><div class="card">` +
		`<h1>` + ch + ` 通道不可达</h1>` +
		`<p>平台无法连接到沙箱上游 <code>` + addr + `</code>。</p>` +
		`<p class="muted">` + why + `</p>` +
		`</div></body></html>`
}
