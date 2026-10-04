package feishu

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
	"github.com/rs/zerolog/log"

	"github.com/cocofhu/grasp/internal/channels"
)

// Adapter is the Feishu/Lark long-connection channel adapter.
type Adapter struct {
	cfg       channels.AdapterConfig
	client    *lark.Client
	baseURL   string
	botOpenID string

	mu        sync.Mutex
	seen      map[string]time.Time
	cancel    context.CancelFunc
	onState   func(state, detail string)
	wsStarted bool
}

// New builds a Feishu adapter from a resolved config. Registered as the
// "feishu" factory in the channels Manager.
func New(cfg channels.AdapterConfig) (channels.Adapter, error) {
	if strings.TrimSpace(cfg.AppID) == "" || strings.TrimSpace(cfg.AppSecret) == "" {
		return nil, fmt.Errorf("feishu: appId 和 appSecret 必填")
	}
	base := OpenBaseURL(cfg.Config)
	client := lark.NewClient(cfg.AppID, cfg.AppSecret, lark.WithOpenBaseUrl(base))
	return &Adapter{
		cfg:     cfg,
		client:  client,
		baseURL: base,
		seen:    map[string]time.Time{},
	}, nil
}

// Type implements channels.Adapter.
func (a *Adapter) Type() string { return "feishu" }

// SetStateHandler implements channels.StatefulAdapter.
func (a *Adapter) SetStateHandler(fn func(state, detail string)) {
	a.mu.Lock()
	a.onState = fn
	a.mu.Unlock()
}

func (a *Adapter) report(state, detail string) {
	a.mu.Lock()
	fn := a.onState
	a.mu.Unlock()
	if fn != nil {
		fn(state, detail)
	}
}

// Start probes tenant_access_token then starts the official long-connection
// in a goroutine. The event callback returns immediately after enqueue.
func (a *Adapter) Start(ctx context.Context, onInbound channels.InboundHandler) error {
	probeCtx, cancelProbe := context.WithTimeout(ctx, 12*time.Second)
	token, err := probeTenantToken(probeCtx, a.baseURL, a.cfg.AppID, a.cfg.AppSecret)
	cancelProbe()
	if err != nil {
		return err
	}
	a.botOpenID = fetchBotOpenID(ctx, a.baseURL, token)

	runCtx, cancel := context.WithCancel(ctx)
	a.mu.Lock()
	a.cancel = cancel
	a.mu.Unlock()

	handler := dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(func(_ context.Context, event *larkim.P2MessageReceiveV1) error {
			// Must return within 3s; enqueue then return.
			go a.handleReceive(runCtx, event, onInbound)
			return nil
		})

	wsOpts := []larkws.ClientOption{larkws.WithEventHandler(handler)}
	if regionOf(a.cfg.Config) == RegionLark {
		wsOpts = append(wsOpts, larkws.WithDomain(lark.LarkBaseUrl))
	} else {
		wsOpts = append(wsOpts, larkws.WithDomain(lark.FeishuBaseUrl))
	}
	ws := larkws.NewClient(a.cfg.AppID, a.cfg.AppSecret, wsOpts...)

	errCh := make(chan error, 1)
	go func() {
		err := ws.Start(runCtx)
		select {
		case errCh <- err:
		default:
		}
		if runCtx.Err() == nil && err != nil {
			a.report(channels.ConnStateDisconnected,
				"长连接已断开。请确认自建应用在线且同一 App ID 无第二条连接互踢。")
		}
	}()

	select {
	case err := <-errCh:
		if err != nil {
			if isAuthish(err) {
				return fmt.Errorf("%w: %v", channels.ErrAdapterAuth, err)
			}
			return err
		}
		return nil
	case <-time.After(3 * time.Second):
		a.mu.Lock()
		a.wsStarted = true
		a.mu.Unlock()
		return nil
	case <-ctx.Done():
		cancel()
		return ctx.Err()
	}
}

func isAuthish(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "99991663") ||
		strings.Contains(s, "99991664") ||
		strings.Contains(s, "app secret") ||
		strings.Contains(s, "app_secret") ||
		strings.Contains(s, "invalid") && strings.Contains(s, "app")
}

// Stop implements channels.Adapter.
func (a *Adapter) Stop() error {
	a.mu.Lock()
	if a.cancel != nil {
		a.cancel()
	}
	a.mu.Unlock()
	return nil
}

// Send implements channels.Adapter.
func (a *Adapter) Send(ctx context.Context, out channels.OutboundMessage) error {
	if strings.TrimSpace(out.ConversationID) == "" {
		return fmt.Errorf("feishu: missing conversation id")
	}
	// ACK / progress stay as text; terminal reports with structure use post.
	msgType, content := a.chooseOutbound(out.Text)
	if err := sendIM(ctx, a.client, out.ConversationID, msgType, content); err != nil {
		return err
	}
	for _, u := range out.ImageURLs {
		if err := a.sendImageURL(ctx, out.ConversationID, u); err != nil {
			log.Warn().Err(err).Str("url", u).Msg("feishu: outbound image failed")
		}
	}
	return nil
}

func (a *Adapter) chooseOutbound(text string) (msgType, content string) {
	t := strings.TrimSpace(text)
	if t == "" {
		return "text", textMsgContent("")
	}
	if isProgressOrAck(t) {
		return "text", textMsgContent(t)
	}
	return "post", buildPostContent(t)
}

func isProgressOrAck(t string) bool {
	prefixes := []string{
		"已收到，正在处理", "收到，正在处理", "已收到，排队中",
		"[进度]", "[阻塞]", "[确认]", "进度：", "阻塞：", "需确认：",
		"处理失败：", "队列已满",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

func (a *Adapter) sendImageURL(ctx context.Context, chatID, rawURL string) error {
	data, err := downloadPublic(ctx, rawURL)
	if err != nil {
		return err
	}
	if len(data) > maxInboundImageBytes {
		return errTooLarge
	}
	key, err := uploadImage(ctx, a.client, data)
	if err != nil {
		return err
	}
	return sendIM(ctx, a.client, chatID, "image", imageMsgContent(key))
}

func downloadPublic(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("download image %s: %s", rawURL, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxInboundImageBytes+1))
}

func (a *Adapter) handleReceive(ctx context.Context, event *larkim.P2MessageReceiveV1, onInbound channels.InboundHandler) {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return
	}
	msg := event.Event.Message
	ev := inboundEvent{
		MessageID:   deref(msg.MessageId),
		ChatID:      deref(msg.ChatId),
		ChatType:    deref(msg.ChatType),
		MessageType: deref(msg.MessageType),
		Content:     deref(msg.Content),
	}
	if event.Event.Sender != nil && event.Event.Sender.SenderId != nil {
		ev.SenderID = deref(event.Event.Sender.SenderId.OpenId)
	}
	for _, m := range msg.Mentions {
		if m == nil {
			continue
		}
		item := inboundMention{Key: deref(m.Key), Name: deref(m.Name)}
		if m.Id != nil {
			item.OpenID = deref(m.Id.OpenId)
		}
		ev.Mentions = append(ev.Mentions, item)
	}
	if ev.MessageID == "" || a.isDuplicate(ev.MessageID) {
		return
	}
	if !shouldAccept(ev, a.botOpenID) {
		return
	}
	scene, ok := sceneOf(ev.ChatType)
	if !ok {
		return
	}

	in := channels.InboundMessage{
		Scene:          scene,
		ConversationID: ev.ChatID,
		UserID:         ev.SenderID,
		Text:           extractText(ev.MessageType, ev.Content),
		MessageID:      ev.MessageID,
		Timestamp:      time.Now(),
	}
	if isUnsupportedMedia(ev.MessageType) {
		in.ChannelHint = unsupportedMediaHint
		if in.Text == "" {
			in.Text = unsupportedMediaHint
		}
	}

	var oversized bool
	for _, key := range imageKeys(ev.MessageType, ev.Content) {
		data, mime, err := downloadResource(ctx, a.client, ev.MessageID, key)
		if err != nil {
			if err == errTooLarge {
				oversized = true
				continue
			}
			log.Warn().Err(err).Str("key", key).Msg("feishu: download image failed")
			continue
		}
		in.Images = append(in.Images, channels.Image{Data: data, MimeType: mime, Filename: key})
	}
	if oversized {
		tip := oversizeHint
		if in.Text != "" {
			in.Text = in.Text + "\n" + tip
		} else {
			in.Text = tip
		}
		if len(in.Images) == 0 && strings.TrimSpace(extractText(ev.MessageType, ev.Content)) == "" {
			if err := a.Send(ctx, channels.OutboundMessage{
				Scene: scene, ConversationID: ev.ChatID, ReplyToMessageID: ev.MessageID, Text: tip,
			}); err != nil {
				log.Warn().Err(err).Msg("feishu: oversize tip failed")
			}
			return
		}
	}

	if onInbound != nil {
		onInbound(ctx, in)
	}
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

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
