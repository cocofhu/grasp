package wecom

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cocofhu/grasp/internal/channels"
	"github.com/cocofhu/grasp/internal/models"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

const (
	pingInterval    = 30 * time.Second
	minSubscribeGap = 2 * time.Second
	reqIDTTL        = 24 * time.Hour
)

// Adapter is the WeCom intelligent-bot long-connection adapter.
type Adapter struct {
	cfg       channels.AdapterConfig
	online    atomic.Bool
	hasSpoken func(scene channels.Scene, conversationID string) bool

	mu         sync.Mutex
	seen       map[string]time.Time
	reqByMsg   map[string]reqEntry
	reqByConv  map[string]reqEntry
	cancel     context.CancelFunc
	conn       *websocket.Conn
	writeMu    sync.Mutex
	lastSub    time.Time
	httpDo     func(*http.Request) (*http.Response, error)
	pending    map[string]chan resultBody
	ackTimeout time.Duration
}

type reqEntry struct {
	ReqID string
	At    time.Time
}

// New builds a wecom adapter. Registered as models.ChannelTypeWeCom.
func New(cfg channels.AdapterConfig) (channels.Adapter, error) {
	if strings.TrimSpace(cfg.AppID) == "" || strings.TrimSpace(cfg.AppSecret) == "" {
		return nil, fmt.Errorf("wecom: BotID 和 Secret 必填")
	}
	return &Adapter{
		cfg:       cfg,
		hasSpoken: cfg.HasSpoken,
		seen:      map[string]time.Time{},
		reqByMsg:  map[string]reqEntry{},
		reqByConv: map[string]reqEntry{},
		httpDo:    http.DefaultClient.Do,
		pending:   map[string]chan resultBody{},
	}, nil
}

// Type implements channels.Adapter.
func (a *Adapter) Type() string { return models.ChannelTypeWeCom }

// Online reports whether aibot_subscribe succeeded on the current connection.
func (a *Adapter) Online() bool { return a.online.Load() }

// Start implements channels.Adapter (non-blocking; reconnect loop in a goroutine).
func (a *Adapter) Start(ctx context.Context, onInbound channels.InboundHandler) error {
	runCtx, cancel := context.WithCancel(ctx)
	a.mu.Lock()
	a.cancel = cancel
	a.mu.Unlock()
	go a.run(runCtx, onInbound)
	return nil
}

// Stop implements channels.Adapter.
func (a *Adapter) Stop() error {
	a.mu.Lock()
	if a.cancel != nil {
		a.cancel()
	}
	conn := a.conn
	a.conn = nil
	a.mu.Unlock()
	a.online.Store(false)
	if conn != nil {
		_ = conn.Close()
	}
	return nil
}

// Send implements channels.Adapter.
func (a *Adapter) Send(ctx context.Context, out channels.OutboundMessage) error {
	text := TruncateMarkdown(out.Text)
	chatType, ok := sendChatTypeOf(out.Scene)
	if !ok {
		return fmt.Errorf("wecom: 未知会话类型 %q", out.Scene)
	}

	reqID := ""
	if out.ReplyToMessageID != "" {
		reqID = a.lookupReqID(out.ReplyToMessageID)
	}
	if reqID != "" {
		return a.respondMsg(ctx, reqID, out.ReplyToMessageID, text)
	}

	// Active push: local unspoken precheck (no adapter-side retry).
	if a.hasSpoken != nil && !a.hasSpoken(out.Scene, out.ConversationID) {
		return ErrUnspoken
	}
	if !a.online.Load() {
		return ErrOffline
	}

	err := a.sendMsg(ctx, out.ConversationID, chatType, text)
	if err != nil && out.Scene == channels.SceneGroup && !IsRateLimited(err) {
		if fallback := a.lookupConvReqID(out.Scene, out.ConversationID); fallback != "" {
			log.Info().Err(err).Msg("wecom: group send_msg failed; falling back to respond_msg")
			return a.respondMsg(ctx, fallback, "", text)
		}
	}
	return err
}

func sendChatTypeOf(scene channels.Scene) (uint32, bool) {
	switch scene {
	case channels.SceneC2C:
		return sendChatTypeSingle, true
	case channels.SceneGroup:
		return sendChatTypeGroup, true
	default:
		return 0, false
	}
}

// connSession tracks subscribe ACK (by req_id, not cmd) and pre-subscribe callbacks.
type connSession struct {
	subReq     string
	subscribed bool
	buffered   []frame
}

func (a *Adapter) run(ctx context.Context, onInbound channels.InboundHandler) {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		a.throttleSubscribe(ctx)
		if err := a.connectOnce(ctx, onInbound); err != nil {
			a.online.Store(false)
			if ctx.Err() != nil {
				return
			}
			log.Warn().Err(err).Dur("backoff", backoff).Msg("wecom: connection ended; retrying")
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			continue
		}
		backoff = time.Second
	}
}

func (a *Adapter) throttleSubscribe(ctx context.Context) {
	a.mu.Lock()
	wait := minSubscribeGap - time.Since(a.lastSub)
	a.mu.Unlock()
	if wait <= 0 {
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(wait):
	}
}

func (a *Adapter) connectOnce(ctx context.Context, onInbound channels.InboundHandler) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.conn = conn
	a.lastSub = time.Now()
	a.mu.Unlock()
	defer func() {
		a.online.Store(false)
		_ = conn.Close()
		a.mu.Lock()
		if a.conn == conn {
			a.conn = nil
		}
		a.mu.Unlock()
	}()

	subReq := newReqID()
	a.mu.Lock()
	if a.pending == nil {
		a.pending = map[string]chan resultBody{}
	}
	a.pending[subReq] = make(chan resultBody, 1)
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.pending, subReq)
		a.mu.Unlock()
	}()

	payload, err := encodeFrame(cmdSubscribe, subReq, subscribeBody{
		BotID: a.cfg.AppID, Secret: a.cfg.AppSecret,
	})
	if err != nil {
		return err
	}
	if err := a.writeRaw(conn, payload); err != nil {
		return err
	}

	hbCtx, stopHB := context.WithCancel(ctx)
	defer stopHB()
	go a.pingLoop(hbCtx, conn)

	sess := &connSession{subReq: subReq}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var f frame
		if err := conn.ReadJSON(&f); err != nil {
			return err
		}
		if err := a.handleIncoming(ctx, f, sess, onInbound); err != nil {
			return err
		}
	}
}

// handleIncoming processes one WS frame. Official subscribe ACK has req_id +
// errcode and typically no cmd=aibot_subscribe; online is set only when errcode==0.
func (a *Adapter) handleIncoming(ctx context.Context, f frame, sess *connSession, onInbound channels.InboundHandler) error {
	reqID := strings.TrimSpace(f.Headers.ReqID)
	if sess != nil && reqID != "" && reqID == sess.subReq && !sess.subscribed {
		code, msg := decodeResult(f)
		a.deliverPending(f)
		if code != 0 {
			a.online.Store(false)
			return classifyWeComError(code, msg)
		}
		a.online.Store(true)
		sess.subscribed = true
		log.Info().Str("bot", a.cfg.AppID).Msg("wecom: subscribed")
		for _, bf := range sess.buffered {
			a.handleCallback(ctx, bf, onInbound)
		}
		sess.buffered = nil
		return nil
	}

	if a.deliverPending(f) {
		return nil
	}

	switch f.Cmd {
	case cmdCallback:
		if sess == nil || !sess.subscribed {
			if sess != nil {
				sess.buffered = append(sess.buffered, f)
			}
			return nil
		}
		a.handleCallback(ctx, f, onInbound)
	case cmdDisconnected:
		a.online.Store(false)
		return fmt.Errorf("wecom: disconnected_event")
	case cmdSubscribe:
		// Fallback if cmd is present but req_id did not match sess.subReq.
		code, msg := decodeResult(f)
		if code != 0 {
			a.online.Store(false)
			return classifyWeComError(code, msg)
		}
		if sess != nil && !sess.subscribed {
			a.online.Store(true)
			sess.subscribed = true
			for _, bf := range sess.buffered {
				a.handleCallback(ctx, bf, onInbound)
			}
			sess.buffered = nil
		}
	case cmdPing, cmdSend, cmdRespond, "":
		// ping reply / unmatched ack: ignore
	default:
		// ignore unknown
	}
	return nil
}

func (a *Adapter) pingLoop(ctx context.Context, conn *websocket.Conn) {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			// Official heartbeat is an application-layer {"cmd":"ping"} frame.
			// WS PingMessage is not recognized and the server will drop the connection.
			payload, err := encodeFrame(cmdPing, newReqID(), nil)
			if err != nil {
				return
			}
			if err := a.writeRaw(conn, payload); err != nil {
				return
			}
		}
	}
}

func (a *Adapter) handleCallback(ctx context.Context, f frame, onInbound channels.InboundHandler) {
	var body callbackBody
	if err := json.Unmarshal(f.Body, &body); err != nil {
		log.Warn().Err(err).Msg("wecom: decode callback failed")
		return
	}
	if body.MsgID == "" || a.isDuplicate(body.MsgID) {
		return
	}

	var scene channels.Scene
	var convID string
	switch body.ChatType {
	case chatSingle:
		scene = channels.SceneC2C
		convID = strings.TrimSpace(body.From.UserID)
		if convID == "" {
			convID = strings.TrimSpace(body.ChatID)
		}
	case chatGroup:
		scene = channels.SceneGroup
		convID = strings.TrimSpace(body.ChatID)
	default:
		return
	}
	if convID == "" {
		log.Warn().Str("chattype", body.ChatType).Msg("wecom: missing conversation id")
		return
	}

	if reqID := strings.TrimSpace(f.Headers.ReqID); reqID != "" {
		a.rememberReqID(body.MsgID, scene, convID, reqID)
	}

	in := channels.InboundMessage{
		Scene:          scene,
		ConversationID: convID,
		UserID:         strings.TrimSpace(body.From.UserID),
		Text:           "",
		MessageID:      body.MsgID,
		Timestamp:      time.Now(),
	}
	if body.Text != nil {
		in.Text = strings.TrimSpace(body.Text.Content)
	}

	switch body.MsgType {
	case msgText, "":
		// text only
	case msgImage:
		if scene != channels.SceneC2C || body.Image == nil {
			return // group image / missing payload: ignore like QQ unsupported types
		}
		img, hint := a.downloadDecryptImage(ctx, body.Image)
		if img != nil {
			in.Images = []channels.Image{*img}
		}
		in.ChannelHint = hint
		if len(in.Images) == 0 && in.Text == "" && hint == "" {
			return
		}
	case msgMixed:
		a.applyMixedItems(ctx, scene, &body, &in)
	default:
		// voice / file / video: ignore
		return
	}

	if in.Text == "" && len(in.Images) == 0 && in.ChannelHint == "" {
		return
	}
	if in.UserID == "" {
		in.UserID = convID
	}
	if onInbound != nil {
		onInbound(ctx, in)
	}
}

func (a *Adapter) applyMixedItems(ctx context.Context, scene channels.Scene, body *callbackBody, in *channels.InboundMessage) {
	items := body.Mixed.items()
	var texts []string
	if in.Text != "" {
		texts = append(texts, in.Text)
	}
	for _, it := range items {
		kind := strings.TrimSpace(it.Type)
		if kind == "" {
			if it.Image != nil {
				kind = msgImage
			} else {
				kind = msgText
			}
		}
		switch kind {
		case msgText:
			if it.Text != nil {
				if c := strings.TrimSpace(it.Text.Content); c != "" {
					texts = append(texts, c)
				}
			}
		case msgImage:
			if scene != channels.SceneC2C {
				continue
			}
			imgSrc := it.Image
			if imgSrc == nil {
				imgSrc = body.Image
			}
			img, hint := a.downloadDecryptImage(ctx, imgSrc)
			if img != nil {
				in.Images = append(in.Images, *img)
			}
			if hint != "" && in.ChannelHint == "" {
				in.ChannelHint = hint
			}
		}
	}
	// Legacy sibling image (non-official) still accepted for C2C.
	if scene == channels.SceneC2C && len(in.Images) == 0 && body.Image != nil {
		img, hint := a.downloadDecryptImage(ctx, body.Image)
		if img != nil {
			in.Images = append(in.Images, *img)
		}
		if hint != "" && in.ChannelHint == "" {
			in.ChannelHint = hint
		}
	}
	if len(texts) > 0 {
		in.Text = strings.Join(texts, "\n")
	}
}

func (a *Adapter) downloadDecryptImage(ctx context.Context, img *callbackImage) (*channels.Image, string) {
	if img == nil || strings.TrimSpace(img.URL) == "" || strings.TrimSpace(img.AESKey) == "" {
		return nil, "单聊图片缺少 url 或 aeskey，已跳过"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, img.URL, nil)
	if err != nil {
		return nil, "单聊图片下载失败，已跳过"
	}
	resp, err := a.httpDo(req)
	if err != nil {
		log.Warn().Err(err).Msg("wecom: image download failed")
		return nil, "单聊图片下载失败，已跳过"
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return nil, "单聊图片下载失败，已跳过"
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, "单聊图片下载失败，已跳过"
	}
	plain, err := DecryptImageAES256CBC(raw, img.AESKey)
	if err != nil {
		log.Warn().Err(err).Msg("wecom: image decrypt failed")
		return nil, "单聊图片解密失败，已跳过"
	}
	return &channels.Image{Data: plain, MimeType: http.DetectContentType(plain), Filename: "wecom-image"}, ""
}

func (a *Adapter) respondMsg(ctx context.Context, reqID, msgid, text string) error {
	return a.roundTrip(ctx, cmdRespond, reqID, respondBody{
		MsgID: msgid, MsgType: "markdown",
		Markdown: markdownBody{Content: text}, Finish: true,
	})
}

func (a *Adapter) sendMsg(ctx context.Context, chatID string, chatType uint32, text string) error {
	return a.roundTrip(ctx, cmdSend, newReqID(), sendBody{
		ChatID: chatID, ChatType: chatType, MsgType: "markdown",
		Markdown: markdownBody{Content: text},
	})
}

func (a *Adapter) roundTrip(ctx context.Context, cmd, reqID string, body any) error {
	payload, err := encodeFrame(cmd, reqID, body)
	if err != nil {
		return err
	}
	a.mu.Lock()
	conn := a.conn
	if a.pending == nil {
		a.pending = map[string]chan resultBody{}
	}
	ch := make(chan resultBody, 1)
	a.pending[reqID] = ch
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.pending, reqID)
		a.mu.Unlock()
	}()
	if conn == nil {
		return ErrOffline
	}
	if err := a.writeRaw(conn, payload); err != nil {
		return err
	}
	return waitOutboundAck(ctx, ch, a.outboundAckTimeout())
}

func (a *Adapter) outboundAckTimeout() time.Duration {
	if a.ackTimeout > 0 {
		return a.ackTimeout
	}
	return 15 * time.Second
}

func waitOutboundAck(ctx context.Context, ch <-chan resultBody, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ErrOutboundTimeout
	case res := <-ch:
		if res.ErrCode != 0 {
			return classifyWeComError(res.ErrCode, res.ErrMsg)
		}
		return nil
	}
}

func (a *Adapter) deliverPending(f frame) bool {
	reqID := strings.TrimSpace(f.Headers.ReqID)
	if reqID == "" {
		return false
	}
	code, msg := decodeResult(f)
	a.mu.Lock()
	ch, ok := a.pending[reqID]
	a.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- resultBody{ErrCode: code, ErrMsg: msg}:
	default:
	}
	return true
}

func (a *Adapter) writeRaw(conn *websocket.Conn, payload []byte) error {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	return conn.WriteMessage(websocket.TextMessage, payload)
}

func (a *Adapter) isDuplicate(msgID string) bool {
	now := time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.seen[msgID]; ok {
		return true
	}
	if len(a.seen) > 2048 {
		for k, t := range a.seen {
			if now.Sub(t) > 10*time.Minute {
				delete(a.seen, k)
			}
		}
	}
	a.seen[msgID] = now
	return false
}

func (a *Adapter) rememberReqID(msgid string, scene channels.Scene, convID, reqID string) {
	now := time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reqByMsg[msgid] = reqEntry{ReqID: reqID, At: now}
	a.reqByConv[string(scene)+":"+convID] = reqEntry{ReqID: reqID, At: now}
	if len(a.reqByMsg) > 4096 {
		for k, e := range a.reqByMsg {
			if now.Sub(e.At) > reqIDTTL {
				delete(a.reqByMsg, k)
			}
		}
	}
}

func (a *Adapter) lookupReqID(msgid string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	e, ok := a.reqByMsg[msgid]
	if !ok || time.Since(e.At) > reqIDTTL {
		return ""
	}
	return e.ReqID
}

func (a *Adapter) lookupConvReqID(scene channels.Scene, convID string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	e, ok := a.reqByConv[string(scene)+":"+convID]
	if !ok || time.Since(e.At) > reqIDTTL {
		return ""
	}
	return e.ReqID
}

// ApplyCallbackForTest feeds a parsed callback (unit tests).
func (a *Adapter) ApplyCallbackForTest(ctx context.Context, f frame, onInbound channels.InboundHandler) {
	a.handleCallback(ctx, f, onInbound)
}

// SetSpokenCheckerForTest overrides the unspoken precheck.
func (a *Adapter) SetSpokenCheckerForTest(fn func(scene channels.Scene, conversationID string) bool) {
	a.hasSpoken = fn
}

// RememberReqIDForTest seeds msgid→req_id cache.
func (a *Adapter) RememberReqIDForTest(msgid, reqID string, scene channels.Scene, convID string) {
	a.rememberReqID(msgid, scene, convID, reqID)
}

// SetOnlineForTest toggles the subscribe-success flag.
func (a *Adapter) SetOnlineForTest(v bool) { a.online.Store(v) }

// HandleIncomingForTest drives subscribe-ACK / callback dispatch (unit tests).
func (a *Adapter) HandleIncomingForTest(ctx context.Context, f frame, sess *connSession, onInbound channels.InboundHandler) error {
	return a.handleIncoming(ctx, f, sess, onInbound)
}
