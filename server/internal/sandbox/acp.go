package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/textutil"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// ErrConnClosed marks a chat/connect failure caused by the ACP WebSocket or its
// sandbox dying (not connected / send failure / closed mid-turn) — as opposed
// to an agent-reported error. Callers use errors.Is to treat it as a retryable
// infrastructure fault.
var ErrConnClosed = errors.New("acp connection closed")

// ErrChatIdle marks a turn aborted because no ACP event frame arrived within
// the idle window: the agent/sandbox is presumed stuck (distinct from a slow
// but productive turn, which keeps emitting events). Also retryable.
var ErrChatIdle = errors.New("acp chat idle timeout")

// newIdleWatch returns a timer channel that fires after the idle window with no
// activity, a reset func to call on each received event, and a stop func for
// cleanup. When idle <= 0 the channel is nil (never fires) and reset/stop are
// no-ops, preserving the original single-deadline behavior.
func newIdleWatch(idle time.Duration) (<-chan time.Time, func(), func()) {
	if idle <= 0 {
		return nil, func() {}, func() {}
	}
	t := time.NewTimer(idle)
	reset := func() {
		if !t.Stop() {
			select {
			case <-t.C:
			default:
			}
		}
		t.Reset(idle)
	}
	return t.C, reset, func() { t.Stop() }
}

// chatMessage builds the {op:"chat"} frame, attaching images only when present
// so the bridge's text-only path stays byte-identical (backward compatible).
// When Name is set it is forwarded so MaterializeAttachments can keep the
// original filename; omitting name preserves old-client compatibility.
func chatMessage(text string, images []models.PromptImage) map[string]any {
	msg := map[string]any{"op": "chat", "content": text}
	if len(images) > 0 {
		imgs := make([]map[string]string, 0, len(images))
		for _, im := range images {
			if im.Data == "" {
				continue
			}
			entry := map[string]string{"data": im.Data, "mimeType": im.MimeType}
			if im.Name != "" {
				entry["name"] = im.Name
			}
			imgs = append(imgs, entry)
		}
		if len(imgs) > 0 {
			msg["images"] = imgs
		}
	}
	return msg
}

// ACPClient is a Go binding for the cursor-acp WebSocket protocol that
// runs inside a sandbox container (port 8765). It mirrors the auto-coder
// client but adds mcpServers/cwd injection on connect so approving can wire
// the per-run artifact-store MCP into the in-container cursor-agent.
type ACPClient struct {
	host string
	port int
	lg   zerolog.Logger

	// ConnectOpts injected into the {op:connect} message.
	cwd        string
	mcpServers json.RawMessage // JSON array, may be nil

	// blobs resolves blob:{id} attachments to base64 for the wire format.
	blobs blob.Store

	// password, when set, is the sandbox token the acp-bridge expects
	// (CURSOR_ACP_PASSWORD). Connect logs in first (POST /api/login) and
	// carries the returned cursor_acp_session cookie on the /ws handshake.
	// Empty means the bridge is unauthenticated (legacy / local dev).
	password string

	// idleTimeout aborts a chat turn when no event frame arrives within the
	// window (0 disables). Lets a stuck agent be detected quickly while a
	// slow-but-working turn (still emitting events) runs to the hard deadline.
	idleTimeout time.Duration

	// bridgeModel is the session ACP_BRIDGE_MODEL string used at ingest to
	// backfill weak usage keys (default/unknown/empty). Empty disables backfill.
	bridgeModel string

	mu        sync.Mutex
	conn      *websocket.Conn
	sessionID string
	connected bool

	eventCh chan json.RawMessage
	done    chan struct{}
}

// NewACPClient builds a client targeting host:port (the published 8765).
func NewACPClient(host string, port int) *ACPClient {
	if host == "" {
		host = "127.0.0.1"
	}
	return &ACPClient{
		host:    host,
		port:    port,
		lg:      log.With().Str("component", "acp").Int("port", port).Logger(),
		eventCh: make(chan json.RawMessage, 512),
		done:    make(chan struct{}),
	}
}

// WithSession sets the working directory and MCP servers used at connect.
func (c *ACPClient) WithSession(cwd string, mcpServers json.RawMessage) *ACPClient {
	c.cwd = cwd
	c.mcpServers = mcpServers
	return c
}

// WithIdleTimeout sets the per-turn idle (no-activity) window; 0 disables it.
func (c *ACPClient) WithIdleTimeout(d time.Duration) *ACPClient {
	c.idleTimeout = d
	return c
}

// WithBridgeModel sets the ACP_BRIDGE_MODEL string used when parsing
// prompt_done.usage weak keys. Trim-empty disables backfill.
func (c *ACPClient) WithBridgeModel(model string) *ACPClient {
	c.bridgeModel = strings.TrimSpace(model)
	return c
}

// WithBlobs wires a blob.Store so chat turns can resolve blob:{id} refs.
func (c *ACPClient) WithBlobs(store blob.Store) *ACPClient {
	c.blobs = store
	return c
}

func (c *ACPClient) prepareImages(ctx context.Context, images []models.PromptImage) ([]models.PromptImage, error) {
	return blob.ResolveForWire(ctx, c.blobs, images)
}

// BridgeModel returns the configured ACP_BRIDGE_MODEL string (may be empty).
func (c *ACPClient) BridgeModel() string {
	return c.bridgeModel
}

// WithPassword sets the acp-bridge login secret (CURSOR_ACP_PASSWORD). When
// non-empty, Connect authenticates before dialing /ws. Empty is a no-op
// (bridge assumed unauthenticated), preserving the legacy behavior.
func (c *ACPClient) WithPassword(password string) *ACPClient {
	c.password = strings.TrimSpace(password)
	return c
}

func (c *ACPClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

func (c *ACPClient) SessionID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionID
}

func (c *ACPClient) setConnected(sessionID string) {
	c.mu.Lock()
	c.sessionID = sessionID
	c.connected = true
	c.mu.Unlock()
}

func (c *ACPClient) wsURL() string {
	return fmt.Sprintf("ws://%s:%d/ws", c.host, c.port)
}

// acpSessionCookieNames are Set-Cookie names accepted from POST /api/login.
// Deployed universal-sandbox images use agentchat_session; older images and
// local fakes still use cursor_acp_session.
var acpSessionCookieNames = []string{"agentchat_session", "cursor_acp_session"}

// bridgeLogin authenticates against the acp-bridge and returns the session
// cookie ("name=value") to attach on the /ws handshake. The bridge exposes
// POST /api/login accepting a JSON {"password":...} body and replies with a
// Set-Cookie header. Returns ("", nil) when password is empty (bridge
// unauthenticated). Any transport/HTTP error is returned so the caller can
// treat it as a warmup/retry condition.
func bridgeLogin(ctx context.Context, host string, port int, password string) (string, error) {
	password = strings.TrimSpace(password)
	if password == "" {
		return "", nil
	}
	if host == "" {
		host = "127.0.0.1"
	}
	url := fmt.Sprintf("http://%s:%d/api/login", host, port)
	body, _ := json.Marshal(map[string]string{"password": password})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return "", fmt.Errorf("acp login: %w", err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("acp login: status %d", resp.StatusCode)
	}
	cookies := resp.Cookies()
	for _, name := range acpSessionCookieNames {
		for _, ck := range cookies {
			if ck.Name == name && ck.Value != "" {
				return ck.Name + "=" + ck.Value, nil
			}
		}
	}
	// Last resort: any non-empty cookie from a successful login (future renames).
	for _, ck := range cookies {
		if ck.Value != "" && strings.Contains(strings.ToLower(ck.Name), "session") {
			return ck.Name + "=" + ck.Value, nil
		}
	}
	return "", fmt.Errorf("acp login: no session cookie in response")
}

// authWarmupBudget bounds how long Connect keeps re-dialing while the
// in-container cursor-agent is still authenticating. The bridge's WS port
// accepts connections before the agent has finished logging in with
// CURSOR_API_KEY, so an immediate session/new can be rejected with
// "Authentication required" (JSON-RPC -32000) purely as a cold-start race — a
// fresh dial a moment later succeeds. A permanently bad key merely exhausts
// this budget and then fails for real. A var (not const) so tests can shrink it.
var authWarmupBudget = 90 * time.Second

// authWarmupBackoff is the wait between re-dials while the agent warms up.
// A var (not const) so tests can shrink it.
var authWarmupBackoff = 2 * time.Second

// Connect dials the bridge and completes the {op:connect} session handshake.
// It transparently re-dials on the transient "authentication not ready yet"
// handshake error while the in-container cursor-agent finishes logging in
// (bounded by authWarmupBudget). All other failures — dial error, a non-auth
// handshake error, context cancellation — return immediately.
func (c *ACPClient) Connect(ctx context.Context) error {
	deadline := time.Now().Add(authWarmupBudget)
	for attempt := 1; ; attempt++ {
		err := c.connectOnce(ctx)
		if err == nil {
			return nil
		}
		if !isAuthWarmupErr(err) || ctx.Err() != nil || time.Now().After(deadline) {
			return err
		}
		c.lg.Warn().Err(err).Int("attempt", attempt).
			Msg("acp session not authenticated yet (agent warming up); re-dialing")
		c.redial()
		select {
		case <-time.After(authWarmupBackoff):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// connectOnce performs a single dial + {op:connect} handshake. It returns nil on
// a "connected" ack, or an error on a handshake error event, timeout, or a
// dropped connection. Each call establishes a fresh WebSocket + read loop
// (tracked by a per-dial done channel) so Connect can safely re-dial.
func (c *ACPClient) connectOnce(ctx context.Context) error {
	url := c.wsURL()
	c.lg.Info().Str("url", url).Msg("acp connecting")

	// Unified auth: log in first (if the bridge requires a password) and carry
	// the session cookie on the WS handshake. A login failure while the bridge
	// is still warming up is surfaced as an auth-warmup error so Connect retries.
	var reqHeader http.Header
	if c.password != "" {
		cookie, err := bridgeLogin(ctx, c.host, c.port, c.password)
		if err != nil {
			return fmt.Errorf("Authentication required: %w", err)
		}
		if cookie != "" {
			reqHeader = http.Header{"Cookie": []string{cookie}}
		}
	}

	dialer := websocket.Dialer{HandshakeTimeout: 30 * time.Second}
	conn, _, err := dialer.DialContext(ctx, url, reqHeader)
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}
	c.mu.Lock()
	c.conn = conn
	c.done = make(chan struct{})
	done := c.done
	c.mu.Unlock()
	go c.readLoop()

	connectMsg := map[string]any{
		"op":             "connect",
		"autoPermission": true,
	}
	if c.cwd != "" {
		connectMsg["cwd"] = c.cwd
	}
	if len(c.mcpServers) > 0 {
		connectMsg["mcpServers"] = c.mcpServers
	}
	if err := c.send(connectMsg); err != nil {
		return fmt.Errorf("send connect: %w", err)
	}

	timer := time.NewTimer(3 * time.Minute)
	defer timer.Stop()
	for {
		select {
		case raw := <-c.eventCh:
			op, sessionID := parseOpAndSession(raw)
			if op == "connected" && sessionID != "" {
				c.setConnected(sessionID)
				c.lg.Info().Str("session", sessionID).Msg("acp connected")
				return nil
			}
			if op == "error" {
				return fmt.Errorf("acp error: %s", parseErrorMessage(raw))
			}
		case <-timer.C:
			return fmt.Errorf("connect timeout (3min)")
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			return fmt.Errorf("connection closed")
		}
	}
}

// redial tears down the current (failed) connection and waits for its read loop
// to exit before draining leftover frames, so the next dial starts from a clean
// slate with no second read loop racing on the shared event channel.
func (c *ACPClient) redial() {
	c.mu.Lock()
	old := c.done
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
	c.connected = false
	c.mu.Unlock()
	if old != nil {
		select {
		case <-old:
		case <-time.After(3 * time.Second):
		}
	}
	c.drainEvents()
}

// isAuthWarmupErr reports whether a handshake error is the transient
// "authentication not ready yet" rejection (cursor-agent still logging in),
// which is worth re-dialing. It matches the agent's auth message and the
// JSON-RPC -32000 code it arrives with.
func isAuthWarmupErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "Authentication required") ||
		strings.Contains(s, "-32000") ||
		strings.Contains(strings.ToLower(s), "authentication")
}

// ChatStructured sends one prompt (with optional image attachments) and
// aggregates the whole turn's session_update stream into a ChatResult. ctx
// controls the deadline.
func (c *ACPClient) ChatStructured(ctx context.Context, text string, images []models.PromptImage) (*ChatResult, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("%w: not connected", ErrConnClosed)
	}
	var err error
	images, err = c.prepareImages(ctx, images)
	if err != nil {
		return nil, err
	}
	c.drainEvents()
	if err := c.send(chatMessage(text, images)); err != nil {
		return nil, fmt.Errorf("%w: send chat: %v", ErrConnClosed, err)
	}

	result := &ChatResult{}
	idleC, idleReset, idleStop := newIdleWatch(c.idleTimeout)
	defer idleStop()
	for {
		select {
		case raw := <-c.eventCh:
			idleReset()
			op, _ := parseOpAndSession(raw)
			switch op {
			case "event":
				if done := c.dispatchEventData(raw, result); done {
					c.lg.Info().
						Int("narration_bytes", len(result.Narration)).
						Int("tools", len(result.ToolCalls)).
						Msg("acp chat complete")
					return result, nil
				}
			case "queue_state":
				if busy, ok := parseQueueBusy(raw); ok {
					result.Busy, result.BusySet = busy, true
				}
			case "error":
				errMsg := parseErrorMessage(raw)
				c.lg.Warn().Str("err", errMsg).Msg("acp chat error event")
				result.appendErrorText(errMsg)
				if hasContent(result) {
					return result, nil
				}
				return nil, fmt.Errorf("acp error: %s", errMsg)
			}
		case <-idleC:
			c.lg.Warn().Dur("idle", c.idleTimeout).Msg("acp chat idle timeout")
			if hasContent(result) {
				return result, nil
			}
			return nil, fmt.Errorf("%w after %s", ErrChatIdle, c.idleTimeout)
		case <-ctx.Done():
			c.lg.Warn().Err(ctx.Err()).Int("narration_bytes", len(result.Narration)).Msg("acp chat ctx done")
			if hasContent(result) {
				return result, nil
			}
			return nil, ctx.Err()
		case <-c.done:
			if hasContent(result) {
				return result, nil
			}
			return nil, fmt.Errorf("%w during chat", ErrConnClosed)
		}
	}
}

// ChatStream sends one prompt (with optional image attachments) and, in
// addition to aggregating the turn into a ChatResult (like ChatStructured),
// invokes onEvent for every raw ACP event frame as it arrives — enabling live
// streaming of thoughts/messages/tool calls to a connected client. onEvent
// receives the full WS frame ({op:"event", data:{...}}). It returns when the
// turn completes (prompt_done) or errors.
func (c *ACPClient) ChatStream(ctx context.Context, text string, images []models.PromptImage, onEvent func(json.RawMessage)) (*ChatResult, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("%w: not connected", ErrConnClosed)
	}
	var err error
	images, err = c.prepareImages(ctx, images)
	if err != nil {
		return nil, err
	}
	c.drainEvents()
	if err := c.send(chatMessage(text, images)); err != nil {
		return nil, fmt.Errorf("%w: send chat: %v", ErrConnClosed, err)
	}
	result := &ChatResult{}
	idleC, idleReset, idleStop := newIdleWatch(c.idleTimeout)
	defer idleStop()
	for {
		select {
		case raw := <-c.eventCh:
			idleReset()
			op, _ := parseOpAndSession(raw)
			switch op {
			case "event":
				if onEvent != nil {
					onEvent(raw)
				}
				if done := c.dispatchEventData(raw, result); done {
					return result, nil
				}
			case "queue_state":
				if busy, ok := parseQueueBusy(raw); ok {
					result.Busy, result.BusySet = busy, true
				}
				if onEvent != nil {
					onEvent(raw)
				}
			case "error":
				errMsg := parseErrorMessage(raw)
				if onEvent != nil {
					onEvent(raw)
				}
				result.appendErrorText(errMsg)
				if hasContent(result) {
					return result, nil
				}
				return nil, fmt.Errorf("acp error: %s", errMsg)
			}
		case <-idleC:
			c.lg.Warn().Dur("idle", c.idleTimeout).Msg("acp chat idle timeout")
			if hasContent(result) {
				return result, nil
			}
			return nil, fmt.Errorf("%w after %s", ErrChatIdle, c.idleTimeout)
		case <-ctx.Done():
			if hasContent(result) {
				return result, nil
			}
			return nil, ctx.Err()
		case <-c.done:
			if hasContent(result) {
				return result, nil
			}
			return nil, fmt.Errorf("%w during chat", ErrConnClosed)
		}
	}
}

// ChatStreamResult is like ChatStructured but invokes onProgress with the
// in-progress aggregated result after each event frame, enabling a live
// preview of the turn (thought/plan/tool calls/narration) as it builds up.
func (c *ACPClient) ChatStreamResult(ctx context.Context, text string, images []models.PromptImage, onProgress func(*ChatResult)) (*ChatResult, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("%w: not connected", ErrConnClosed)
	}
	var err error
	images, err = c.prepareImages(ctx, images)
	if err != nil {
		return nil, err
	}
	c.drainEvents()
	if err := c.send(chatMessage(text, images)); err != nil {
		return nil, fmt.Errorf("%w: send chat: %v", ErrConnClosed, err)
	}
	result := &ChatResult{}
	idleC, idleReset, idleStop := newIdleWatch(c.idleTimeout)
	defer idleStop()
	for {
		select {
		case raw := <-c.eventCh:
			idleReset()
			op, _ := parseOpAndSession(raw)
			switch op {
			case "event":
				done := c.dispatchEventData(raw, result)
				if onProgress != nil {
					onProgress(result)
				}
				if done {
					return result, nil
				}
			case "queue_state":
				if busy, ok := parseQueueBusy(raw); ok {
					result.Busy, result.BusySet = busy, true
					if onProgress != nil {
						onProgress(result)
					}
				}
			case "error":
				errMsg := parseErrorMessage(raw)
				result.appendErrorText(errMsg)
				if hasContent(result) {
					return result, nil
				}
				return nil, fmt.Errorf("acp error: %s", errMsg)
			}
		case <-idleC:
			c.lg.Warn().Dur("idle", c.idleTimeout).Msg("acp chat idle timeout")
			if hasContent(result) {
				return result, nil
			}
			return nil, fmt.Errorf("%w after %s", ErrChatIdle, c.idleTimeout)
		case <-ctx.Done():
			if hasContent(result) {
				return result, nil
			}
			return nil, ctx.Err()
		case <-c.done:
			if hasContent(result) {
				return result, nil
			}
			return nil, fmt.Errorf("%w during chat", ErrConnClosed)
		}
	}
}

func hasContent(r *ChatResult) bool {
	if r == nil {
		return false
	}
	return r.Narration != "" || r.Plan != nil || len(r.ToolCalls) > 0
}

// Cancel sends {op:cancel}; fire-and-forget (the bridge does not ack).
func (c *ACPClient) Cancel() error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected")
	}
	if err := c.send(map[string]any{"op": "cancel"}); err != nil {
		return fmt.Errorf("send cancel: %w", err)
	}
	return nil
}

func (c *ACPClient) dispatchEventData(raw json.RawMessage, result *ChatResult) bool {
	var envelope struct {
		Op   string          `json:"op"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		c.lg.Warn().Err(err).Str("raw", textutil.TruncateBytes(string(raw), 200, "...")).Msg("malformed envelope")
		return false
	}
	if envelope.Op != "event" || len(envelope.Data) == 0 {
		return false
	}

	var ev struct {
		Type       string          `json:"type"`
		Update     json.RawMessage `json:"update"`
		Usage      json.RawMessage `json:"usage"`
		Text       string          `json:"text"`
		StopReason string          `json:"stopReason"`
	}
	if err := json.Unmarshal(envelope.Data, &ev); err != nil {
		c.lg.Warn().Err(err).Msg("malformed event data")
		return false
	}

	if result != nil {
		dup := make(json.RawMessage, len(envelope.Data))
		copy(dup, envelope.Data)
		result.RawEvents = append(result.RawEvents, dup)
	}

	if ev.Type == "prompt_done" {
		// Per-turn usage only — never session CumulativeUsage (cross-node reuse
		// would otherwise bleed prior nodes into this turn).
		if result != nil {
			if u, byModel := parsePromptDoneUsage(ev.Usage, c.bridgeModel); u != nil {
				result.Usage = models.AddTokenUsage(result.Usage, u)
				result.UsageByModel = models.AddTokenUsageByModel(result.UsageByModel, byModel)
			}
			if strings.EqualFold(strings.TrimSpace(ev.StopReason), "failed") {
				result.Failed = true
			}
		}
		return true
	}
	if ev.Type == "error_text" {
		// oneshot provider surfaces model/CLI failures as raw error_text frames
		// before prompt_done{stopReason:failed}. Capture the body so React can
		// show it instead of an empty agent bubble.
		if result != nil {
			result.appendErrorText(ev.Text)
		}
		return false
	}
	if ev.Type == "session_update" && len(ev.Update) > 0 {
		kind, flat := normalizeSessionUpdate(ev.Update)
		dispatchSessionUpdate(kind, flat, result)
	}
	return false
}

// Close shuts down the WebSocket. Safe to call multiple times.
func (c *ACPClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected = false
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

// drainEvents discards any buffered frames left over from a previous turn so a
// new prompt starts from a clean slate. The cursor-acp bridge multiplexes every
// turn of a reused session over one connection and one buffered channel;
// trailing/meta frames (or a stale prompt_done) from a prior turn would
// otherwise bleed into and scramble the next turn's aggregated narration.
func (c *ACPClient) drainEvents() {
	for {
		select {
		case <-c.eventCh:
		default:
			return
		}
	}
}

func (c *ACPClient) send(msg any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return fmt.Errorf("no connection")
	}
	return c.conn.WriteJSON(msg)
}

func (c *ACPClient) readLoop() {
	defer close(c.done)
	// A panic in the read/decode path (e.g. a malformed frame tripping a
	// dependency) must not take down the whole process; log it and let done
	// close so waiters unblock and the session is treated as disconnected.
	defer func() {
		if r := recover(); r != nil {
			c.lg.Error().Interface("panic", r).Bytes("stack", debug.Stack()).Msg("acp readLoop panicked")
			c.mu.Lock()
			c.connected = false
			c.mu.Unlock()
		}
	}()
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return
	}
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if !strings.Contains(err.Error(), "use of closed network connection") {
				c.lg.Warn().Err(err).Msg("acp read error")
			}
			c.mu.Lock()
			c.connected = false
			c.mu.Unlock()
			return
		}
		select {
		case c.eventCh <- json.RawMessage(message):
		default:
			c.lg.Warn().Msg("acp event channel full, dropping message")
		}
	}
}

func parseOpAndSession(raw json.RawMessage) (string, string) {
	var m struct {
		Op        string `json:"op"`
		SessionID string `json:"sessionId"`
	}
	_ = json.Unmarshal(raw, &m)
	return m.Op, m.SessionID
}

func parseErrorMessage(raw json.RawMessage) string {
	var m struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &m)
	return m.Message
}

// parseQueueBusy extracts the busy flag from a {op:"queue_state", busy, ...}
// frame. ok is false when the frame carries no busy field, so callers can
// ignore malformed/partial snapshots rather than treating them as idle.
func parseQueueBusy(raw json.RawMessage) (busy, ok bool) {
	var m struct {
		Busy *bool `json:"busy"`
	}
	if err := json.Unmarshal(raw, &m); err != nil || m.Busy == nil {
		return false, false
	}
	return *m.Busy, true
}

// WaitForACPReady polls the bridge WebSocket until it accepts a connection.
// When password is non-empty the bridge requires auth, so each probe logs in
// (POST /api/login) and dials /ws with the returned session cookie — matching
// the ACP client's handshake so a password-protected bridge is still detected
// as ready. Empty password preserves the legacy unauthenticated probe.
func WaitForACPReady(ctx context.Context, host string, port int, password string, maxWait time.Duration) error {
	if host == "" {
		host = "127.0.0.1"
	}
	password = strings.TrimSpace(password)
	deadline := time.Now().Add(maxWait)
	url := fmt.Sprintf("ws://%s:%d/ws", host, port)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var reqHeader http.Header
		ok := true
		if password != "" {
			if cookie, err := bridgeLogin(ctx, host, port, password); err != nil {
				ok = false
			} else if cookie != "" {
				reqHeader = http.Header{"Cookie": []string{cookie}}
			}
		}
		if ok {
			dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
			conn, _, err := dialer.DialContext(ctx, url, reqHeader)
			if err == nil {
				_ = conn.Close()
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("ACP not ready after %v", maxWait)
}
