package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

const mcpSessionHeader = "Mcp-Session-Id"

// Session and stream state is protected by Host.mu. A session may reconnect,
// but has only one active notification stream. Different sessions are distinct
// clients even when they share the same run token.
type toolSession struct {
	stream *toolStream
}

type toolStream struct {
	wake chan struct{} // capacity one: notifications invalidate, not describe a delta
	done chan struct{}
}

// ServeRunHTTP serves the run-scoped Streamable HTTP transport. Existing
// sessionless POST clients remain supported; initialize assigns a session ID
// for clients that listen for tools/list_changed notifications.
func (h *Host) ServeRunHTTP(w http.ResponseWriter, r *http.Request, runID, token string) {
	if !h.AuthorizeRun(runID, token) {
		log.Warn().Str("run_id", runID).Str("method", r.Method).Msg("mcp http unauthorized")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// Native MCP clients omit Origin. Browser requests must come from this
	// endpoint's origin rather than an unrelated site (DNS rebinding defense).
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !strings.EqualFold(u.Host, r.Host) || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			http.Error(w, "invalid Origin", http.StatusForbidden)
			return
		}
	}
	sid := r.Header.Get(mcpSessionHeader)
	if sid != "" && !h.hasToolSession(runID, sid) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusOK)
			return
		}
		// Session state is in-memory and lost on restart or run re-registration,
		// while the agent keeps sending its old ID (cursor-agent does not
		// re-initialize on 404). The ID is not a credential — the bearer token
		// already authorized this run — so re-adopt it instead of failing.
		if !h.adoptToolSession(runID, token, sid) {
			http.Error(w, "MCP session not found", http.StatusNotFound)
			return
		}
		log.Warn().Str("run_id", runID).Str("method", r.Method).Msg("mcp http re-adopted unknown session")
	}
	switch r.Method {
	case http.MethodPost:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		status, resp := h.ServeRPC(runID, token, body)
		var req rpcRequest
		if json.Unmarshal(body, &req) == nil && req.Method == "initialize" && status == http.StatusOK {
			id := h.newToolSession(runID, token)
			if id == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.Header().Set(mcpSessionHeader, id)
		}
		if resp != nil {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(status)
		if resp != nil {
			_, _ = w.Write(resp)
		}
	case http.MethodGet, http.MethodDelete:
		if sid == "" {
			http.Error(w, "Mcp-Session-Id required", http.StatusBadRequest)
			return
		}
		if r.Method == http.MethodDelete {
			h.deleteToolSession(runID, sid)
			w.WriteHeader(http.StatusOK)
			return
		}
		if !acceptsEventStream(r.Header.Get("Accept")) {
			http.Error(w, "Accept: text/event-stream required", http.StatusNotAcceptable)
			return
		}
		h.serveToolStream(w, r, runID, sid)
	default:
		w.Header().Set("Allow", "POST, GET, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func acceptsEventStream(accept string) bool {
	for _, item := range strings.Split(accept, ",") {
		media, params, err := mime.ParseMediaType(strings.TrimSpace(item))
		if err == nil && media == "text/event-stream" {
			if q, ok := params["q"]; ok {
				quality, err := strconv.ParseFloat(q, 64)
				if err != nil || quality <= 0 || quality > 1 {
					continue
				}
			}
			return true
		}
	}
	return false
}

func (h *Host) newToolSession(runID, token string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Recheck under the lock so UnregisterRun cannot leave an orphan session
	// after the transport's initial authorization.
	if token == "" || h.tokens[runID] != token {
		return ""
	}
	id := uuid.NewString()
	if h.toolSessions[runID] == nil {
		h.toolSessions[runID] = map[string]*toolSession{}
	}
	h.toolSessions[runID][id] = &toolSession{}
	return id
}

const (
	maxToolSessionIDLen = 128
	// maxClosedToolSessions bounds the per-run tombstones of DELETEd sessions.
	maxClosedToolSessions = 256
)

// adoptToolSession registers a client-supplied session ID for runID when the
// token is still bound to the run. IDs must be short visible ASCII per the
// Streamable HTTP spec. A session the client terminated (DELETE) or one live
// under another run is never adopted.
func (h *Host) adoptToolSession(runID, token, sid string) bool {
	if len(sid) > maxToolSessionIDLen {
		return false
	}
	for _, c := range sid {
		if c < 0x21 || c > 0x7e {
			return false
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if token == "" || h.tokens[runID] != token {
		return false
	}
	if _, closed := h.closedSessions[runID][sid]; closed {
		return false
	}
	for other, sessions := range h.toolSessions {
		if other != runID && sessions[sid] != nil {
			return false
		}
	}
	if h.toolSessions[runID] == nil {
		h.toolSessions[runID] = map[string]*toolSession{}
	}
	if h.toolSessions[runID][sid] == nil {
		h.toolSessions[runID][sid] = &toolSession{}
	}
	return true
}

func (h *Host) hasToolSession(runID, sid string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.toolSessions[runID][sid] != nil
}

func (h *Host) deleteToolSession(runID, sid string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if session := h.toolSessions[runID][sid]; session != nil {
		if session.stream != nil {
			close(session.stream.done)
		}
		delete(h.toolSessions[runID], sid)
		if len(h.toolSessions[runID]) == 0 {
			delete(h.toolSessions, runID)
		}
	}
	if h.closedSessions == nil {
		h.closedSessions = map[string]map[string]uint64{}
	}
	closed := h.closedSessions[runID]
	if closed == nil {
		closed = map[string]uint64{}
		h.closedSessions[runID] = closed
	}
	if _, ok := closed[sid]; !ok && len(closed) >= maxClosedToolSessions {
		var oldest string
		var oldestAt uint64
		for id, at := range closed {
			if oldest == "" || at < oldestAt {
				oldest, oldestAt = id, at
			}
		}
		delete(closed, oldest)
		log.Warn().Str("run_id", runID).Str("evicted_session", oldest).Int("cap", maxClosedToolSessions).
			Msg("mcp closed-session tombstones full; evicted oldest (it may be re-adopted)")
	}
	h.closedSeq++
	closed[sid] = h.closedSeq
}

func (h *Host) closeToolSessionsLocked(runID string) {
	for _, session := range h.toolSessions[runID] {
		if session.stream != nil {
			close(session.stream.done)
		}
	}
	delete(h.toolSessions, runID)
}

// CloseToolStreams ends every open tool-list SSE stream but keeps the
// sessions, so http.Server.Shutdown is not held up by them. Clients reconnect
// and re-read the tool list.
func (h *Host) CloseToolStreams() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sessions := range h.toolSessions {
		for _, session := range sessions {
			if session.stream != nil {
				close(session.stream.done)
				session.stream = nil
			}
		}
	}
}

func (h *Host) notifyToolSessionsLocked(runID string) {
	for _, session := range h.toolSessions[runID] {
		if session.stream != nil {
			select {
			case session.stream.wake <- struct{}{}:
			default: // A queued invalidation already asks the client to read the latest list.
			}
		}
	}
}

func (h *Host) serveToolStream(w http.ResponseWriter, r *http.Request, runID, sid string) {
	stream := &toolStream{wake: make(chan struct{}, 1), done: make(chan struct{})}
	h.mu.Lock()
	session := h.toolSessions[runID][sid]
	if session == nil {
		h.mu.Unlock()
		http.Error(w, "MCP session not found", http.StatusNotFound)
		return
	}
	if session.stream != nil {
		close(session.stream.done)
	}
	session.stream = stream
	gen := h.toolsListGen[runID]
	// Recover changes made before GET or while disconnected. Always invalidate
	// on reconnect after a change; no event IDs or historical replay are promised.
	if gen > 0 {
		stream.wake <- struct{}{}
	}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		if session.stream == stream {
			session.stream = nil
		}
		h.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	controller := http.NewResponseController(w)
	if controller.Flush() != nil {
		return
	}
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	lastSent := -1
	for {
		select {
		case <-r.Context().Done():
			return
		case <-stream.done:
			return
		case <-stream.wake:
			gen := h.ToolsListGeneration(runID)
			if gen == lastSent {
				continue
			}
			if _, err := fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/tools/list_changed\"}\n\n"); err != nil {
				return
			}
			if controller.Flush() != nil {
				return
			}
			lastSent = gen
		case <-heartbeat.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			if controller.Flush() != nil {
				return
			}
		}
	}
}
