package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
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

// ErrChatIdle marks a turn this client gave up on because the bridge went
// silent: no frame at all (event or liveness heartbeat) within the watch
// window. The sandbox is presumed lost, so it is retryable.
var ErrChatIdle = errors.New("acp chat idle timeout")

// ErrAgentStuck marks a turn the bridge stopped because the Agent showed no
// activity (no output, CPU or IO) for the no-activity limit even after one
// resume, or kept repeating the same failing command.
var ErrAgentStuck = errors.New("agent stuck")

// stopReasonStuck is the prompt_done stopReason of a turn the bridge stopped
// as stuck.
const stopReasonStuck = "stuck"

// bridgeLostAfter is how long a client that has seen liveness heartbeats
// (sent every minute while a turn runs) waits for any frame before it treats
// the bridge as lost. A var so tests can shrink it.
var bridgeLostAfter = 3 * time.Minute

// legacyWatch is the client-side window for a bridge that does not send
// heartbeats: the bridge resumes once after N and stops after another N, so
// wait for both plus a margin.
func legacyWatch(idle time.Duration) time.Duration {
	if idle <= 0 {
		return 0
	}
	return 2*idle + min(idle, 2*time.Minute)
}

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
// so the bridge's text-only path stays unchanged. When Name is set it is
// forwarded so MaterializeAttachments can keep the original filename.
func chatMessage(text string, images []models.PromptImage, opID string) map[string]any {
	msg := map[string]any{"op": "chat", "content": text}
	if opID != "" {
		msg["opId"] = opID
	}
	attachImages(msg, images)
	return msg
}

// turnLimitFields adds the per-turn limits the bridge enforces: idleSec (the
// no-activity limit N) and deadlineSec (the turn's hard stop, a minute past
// ctx's deadline so the platform reports the node budget first).
func turnLimitFields(msg map[string]any, ctx context.Context, idle time.Duration) {
	if idle > 0 {
		msg["idleSec"] = max(1, int(idle/time.Second))
	}
	if dl, ok := ctx.Deadline(); ok {
		msg["deadlineSec"] = max(1, int(time.Until(dl)/time.Second)) + 60
	}
}

func attachImages(msg map[string]any, images []models.PromptImage) {
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
}

// ACPClient is a Go binding for the acp-bridge WebSocket protocol that
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

	// password is the sandbox token the acp-bridge expects
	// (ACP_BRIDGE_PASSWORD). Connect logs in first (POST /api/login) and
	// carries the returned agentchat_session cookie on the /ws handshake.
	// Required: an empty password fails Connect.
	password string

	// idleTimeout is the Agent no-activity limit N sent to the bridge with each
	// turn (0 disables); idleFn, when set, supplies it per turn instead so a
	// settings change applies from the next turn on.
	idleTimeout time.Duration
	idleFn      func() time.Duration

	// bridgeModel is the session ACP_BRIDGE_MODEL string used at ingest to
	// backfill weak usage keys (default/unknown/empty). Empty disables backfill.
	bridgeModel string

	// chatID selects a bridge chat (/ws?chat=<id>); empty is the default chat.
	chatID string

	mu        sync.Mutex
	conn      *websocket.Conn
	sessionID string
	connected bool

	eventCh chan json.RawMessage
	done    chan struct{}

	// readers counts callers draining eventCh (handshake, turn, cancel wait).
	// With none, frames are not queued: a parked client would otherwise fill
	// the buffer with bridge broadcasts nobody reads.
	readers atomic.Int32
	// dropped counts frames lost to a full eventCh since the last warning.
	dropped     atomic.Int64
	lastDropLog atomic.Int64

	// turnOpID is the opId of the chat currently in flight on this client
	// ("" when none); lastDoneOpID the last one that ended with prompt_done.
	turnOpID     atomic.Value
	lastDoneOpID atomic.Value

	// bridge mirrors the latest queue_state regardless of whether a chat is in
	// flight, plus the local desync flag (cancel never acknowledged).
	stateMu sync.Mutex
	bridge  BridgeState
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

// WithIdleTimeoutFunc reads the no-activity limit at the start of every turn.
func (c *ACPClient) WithIdleTimeoutFunc(f func() time.Duration) *ACPClient {
	c.idleFn = f
	return c
}

func (c *ACPClient) agentIdle() time.Duration {
	if c.idleFn != nil {
		return c.idleFn()
	}
	return c.idleTimeout
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

// WithPassword sets the acp-bridge login secret (ACP_BRIDGE_PASSWORD). Connect
// authenticates with it before dialing /ws.
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

// WithChat targets a bridge chat created via CreateChat instead of the default one.
func (c *ACPClient) WithChat(chatID string) *ACPClient {
	c.chatID = strings.TrimSpace(chatID)
	return c
}

func (c *ACPClient) wsURL() string {
	if c.chatID != "" {
		return fmt.Sprintf("ws://%s:%d/ws?chat=%s", c.host, c.port, url.QueryEscape(c.chatID))
	}
	return fmt.Sprintf("ws://%s:%d/ws", c.host, c.port)
}

// acpSessionCookieName is the Set-Cookie name returned by POST /api/login.
const acpSessionCookieName = "agentchat_session"

// errBridgePasswordRequired: every acp-bridge requires ACP_BRIDGE_PASSWORD.
var errBridgePasswordRequired = errors.New("acp login: bridge password is required")

// bridgeLogin authenticates against the acp-bridge and returns the session
// cookie ("name=value") to attach on the /ws handshake. The bridge exposes
// POST /api/login accepting a JSON {"password":...} body and replies with a
// Set-Cookie header. An empty password is an error. Any transport/HTTP error
// is returned so the caller can treat it as a warmup/retry condition.
func bridgeLogin(ctx context.Context, host string, port int, password string) (string, error) {
	password = strings.TrimSpace(password)
	if password == "" {
		return "", errBridgePasswordRequired
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
	for _, ck := range resp.Cookies() {
		if ck.Name == acpSessionCookieName && ck.Value != "" {
			return ck.Name + "=" + ck.Value, nil
		}
	}
	return "", fmt.Errorf("acp login: no %s cookie in response", acpSessionCookieName)
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

	// Unified auth: log in first and carry the session cookie on the WS
	// handshake. A login failure while the bridge is still warming up is
	// surfaced as an auth-warmup error so Connect retries.
	if c.password == "" {
		return errBridgePasswordRequired
	}
	cookie, err := bridgeLogin(ctx, c.host, c.port, c.password)
	if err != nil {
		return fmt.Errorf("Authentication required: %w", err)
	}
	reqHeader := http.Header{"Cookie": []string{cookie}}

	dialer := websocket.Dialer{HandshakeTimeout: 30 * time.Second}
	conn, _, err := dialer.DialContext(ctx, url, reqHeader)
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}
	release := c.acquireReader()
	defer release()
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
	return c.runTurn(ctx, text, images, nil, nil)
}

// ChatStream sends one prompt (with optional image attachments) and, in
// addition to aggregating the turn into a ChatResult (like ChatStructured),
// invokes onEvent for every raw ACP event frame as it arrives — enabling live
// streaming of thoughts/messages/tool calls to a connected client. onEvent
// receives the full WS frame ({op:"event", data:{...}}). It returns when the
// turn completes (prompt_done) or errors.
func (c *ACPClient) ChatStream(ctx context.Context, text string, images []models.PromptImage, onEvent func(json.RawMessage)) (*ChatResult, error) {
	return c.runTurn(ctx, text, images, onEvent, nil)
}

// ChatStreamResult is like ChatStructured but invokes onProgress with the
// in-progress aggregated result after each event frame, enabling a live
// preview of the turn (thought/plan/tool calls/narration) as it builds up.
func (c *ACPClient) ChatStreamResult(ctx context.Context, text string, images []models.PromptImage, onProgress func(*ChatResult)) (*ChatResult, error) {
	return c.runTurn(ctx, text, images, nil, onProgress)
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

	if ev.Type == "turn_segment" {
		if result != nil {
			result.sealSegment()
		}
		return false
	}
	if ev.Type == "prompt_done" {
		// Per-turn usage only — never session CumulativeUsage (cross-node reuse
		// would otherwise bleed prior nodes into this turn).
		if result != nil {
			if u, byModel := parsePromptDoneUsage(ev.Usage, c.bridgeModel); u != nil {
				result.Usage = models.AddTokenUsage(result.Usage, u)
				result.UsageByModel = models.AddTokenUsageByModel(result.UsageByModel, byModel)
			}
			stop := strings.ToLower(strings.TrimSpace(ev.StopReason))
			result.StopReason = stop
			switch stop {
			case "failed":
				result.Failed = true
			case stopReasonTimeout, "cancelled":
				result.Interrupted = true
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
// new prompt starts from a clean slate. The acp-bridge multiplexes every
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
		c.observeQueueState(message)
		if c.readers.Load() == 0 {
			continue
		}
		select {
		case c.eventCh <- json.RawMessage(message):
		default:
			c.noteDropped()
		}
	}
}

// acquireReader marks a caller as draining eventCh until release is called.
func (c *ACPClient) acquireReader() (release func()) {
	c.readers.Add(1)
	var once sync.Once
	return func() { once.Do(func() { c.readers.Add(-1) }) }
}

// dropLogEvery rate-limits the full-channel warning.
const dropLogEvery = time.Minute

func (c *ACPClient) noteDropped() {
	n := c.dropped.Add(1)
	now := time.Now().UnixNano()
	last := c.lastDropLog.Load()
	if last != 0 && time.Duration(now-last) < dropLogEvery {
		return
	}
	if !c.lastDropLog.CompareAndSwap(last, now) {
		return
	}
	c.dropped.Add(-n)
	c.lg.Warn().Int64("dropped", n).Msg("acp event channel full, dropping messages")
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
// Each probe logs in (POST /api/login) and dials /ws with the returned session
// cookie, matching the ACP client's handshake. An empty password is an error.
func WaitForACPReady(ctx context.Context, host string, port int, password string, maxWait time.Duration) error {
	if host == "" {
		host = "127.0.0.1"
	}
	password = strings.TrimSpace(password)
	if password == "" {
		return errBridgePasswordRequired
	}
	deadline := time.Now().Add(maxWait)
	url := fmt.Sprintf("ws://%s:%d/ws", host, port)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if cookie, err := bridgeLogin(ctx, host, port, password); err == nil {
			dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
			conn, _, err := dialer.DialContext(ctx, url, http.Header{"Cookie": []string{cookie}})
			if err == nil {
				_ = conn.Close()
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("ACP not ready after %v", maxWait)
}
