package qq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	tokenEndpoint  = "https://bots.qq.com/app/getAppAccessToken"
	prodAPIBase    = "https://api.sgroup.qq.com"
	sandboxAPIBase = "https://sandbox.api.sgroup.qq.com"
)

// client wraps the QQ OpenAPI: token lifecycle + message/media sends.
type client struct {
	appID     string
	appSecret string
	apiBase   string
	http      *http.Client
	markdown  bool // send text as native Markdown (msg_type=2) with plain-text fallback

	tokenMu  sync.Mutex
	token    string
	tokenExp time.Time

	seqMu   sync.Mutex
	seqByID map[string]int

	// mdCap is a negative capability cache: targets known to reject Markdown
	// (e.g. guild channels without 内邀开通) are recorded here with an expiry so
	// we skip the doomed Markdown attempt and its extra round-trip.
	mdCapMu       sync.Mutex
	mdUnsupported map[string]time.Time
}

func newClient(appID, appSecret, apiBase string, markdown bool) *client {
	if apiBase == "" {
		apiBase = prodAPIBase
	}
	return &client{
		appID: appID, appSecret: appSecret, apiBase: strings.TrimRight(apiBase, "/"),
		http:          &http.Client{Timeout: 30 * time.Second},
		markdown:      markdown,
		seqByID:       map[string]int{},
		mdUnsupported: map[string]time.Time{},
	}
}

// accessToken returns a valid app access token, refreshing under lock when
// expired or near expiry.
func (c *client) accessToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.token != "" && time.Now().Before(c.tokenExp.Add(-30*time.Second)) {
		return c.token, nil
	}
	body, _ := json.Marshal(map[string]string{
		"appId":        c.appID,
		"clientSecret": c.appSecret,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token http %d: %s", resp.StatusCode, string(raw))
	}
	var tr tokenResponse
	if err := json.Unmarshal(raw, &tr); err != nil {
		return "", err
	}
	if tr.AccessToken == "" {
		return "", fmt.Errorf("empty access token: %s", string(raw))
	}
	c.token = tr.AccessToken
	c.tokenExp = time.Now().Add(time.Duration(parseExpires(tr.ExpiresIn)) * time.Second)
	return c.token, nil
}

func parseExpires(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return n
	default:
		return 7200
	}
}

func (c *client) authHeader(ctx context.Context) (string, error) {
	tok, err := c.accessToken(ctx)
	if err != nil {
		return "", err
	}
	return "QQBot " + tok, nil
}

// gatewayURL fetches the WS gateway endpoint.
func (c *client) gatewayURL(ctx context.Context) (string, error) {
	var out gatewayResponse
	if err := c.doJSON(ctx, http.MethodGet, "/gateway", nil, &out); err != nil {
		return "", err
	}
	if out.URL == "" {
		return "", fmt.Errorf("empty gateway url")
	}
	return out.URL, nil
}

// apiError is returned by doJSON for non-2xx HTTP responses. It carries the
// status code so callers can tell an outright rejection (4xx) apart from a
// transient failure (network error, 5xx, rate limiting) that should not poison
// the Markdown capability cache.
type apiError struct {
	StatusCode int
	Method     string
	Path       string
	Body       string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("qq api %s %s -> %d: %s", e.Method, e.Path, e.StatusCode, e.Body)
}

// markdownRejected reports whether err indicates QQ rejected the request itself
// (e.g. native Markdown not enabled for the target) rather than a transient or
// network failure. Only rejections should disable Markdown and trigger the
// plain-text fallback; transient errors propagate so the whole send can be
// retried instead of being silently downgraded (and to avoid caching a false
// negative for an hour).
func markdownRejected(err error) bool {
	var ae *apiError
	if !errors.As(err, &ae) {
		return false // network error, context cancellation, etc.
	}
	// A client-side 4xx (other than rate limiting) means the request was
	// rejected outright and was not delivered.
	return ae.StatusCode >= 400 && ae.StatusCode < 500 && ae.StatusCode != http.StatusTooManyRequests
}

// doJSON performs an authenticated JSON request against the API base.
func (c *client) doJSON(ctx context.Context, method, path string, body any, out any) error {
	auth, err := c.authHeader(ctx)
	if err != nil {
		return err
	}
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.apiBase+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("X-Union-Appid", c.appID)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &apiError{StatusCode: resp.StatusCode, Method: method, Path: path, Body: string(raw)}
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode %s: %w (body: %s)", path, err, string(raw))
		}
	}
	return nil
}

// nextSeq returns the next msg_seq for a passive reply msg_id. QQ dedups
// replies sharing the same (msg_id, msg_seq), so each reply increments.
func (c *client) nextSeq(msgID string) int {
	c.seqMu.Lock()
	defer c.seqMu.Unlock()
	c.seqByID[msgID]++
	// Bound memory: reset the map opportunistically.
	if len(c.seqByID) > 4096 {
		c.seqByID = map[string]int{msgID: c.seqByID[msgID]}
	}
	return c.seqByID[msgID]
}

// --- message sends ---------------------------------------------------------

const (
	msgTypeText     = 0
	msgTypeMarkdown = 2
	msgTypeMedia    = 7
)

// mdCapTTL bounds how long a target is remembered as not supporting Markdown.
const mdCapTTL = time.Hour

// markdownSupported reports whether Markdown should be attempted for target key.
// Returns false while a fresh negative-cache entry exists.
func (c *client) markdownSupported(key string) bool {
	if !c.markdown {
		return false
	}
	c.mdCapMu.Lock()
	defer c.mdCapMu.Unlock()
	exp, ok := c.mdUnsupported[key]
	if !ok {
		return true
	}
	if time.Now().After(exp) {
		delete(c.mdUnsupported, key)
		return true
	}
	return false
}

// markMarkdownUnsupported records that target key rejected Markdown so we skip
// the extra round-trip until the entry expires.
func (c *client) markMarkdownUnsupported(key string) {
	c.mdCapMu.Lock()
	defer c.mdCapMu.Unlock()
	if len(c.mdUnsupported) > 4096 {
		c.mdUnsupported = map[string]time.Time{}
	}
	c.mdUnsupported[key] = time.Now().Add(mdCapTTL)
}

// markdownContent normalizes assistant text for QQ native Markdown rendering.
// QQ collapses a lone "\n" (soft break), so outside code blocks every newline
// boundary is promoted to a blank-line paragraph break and runs of blank lines
// are compressed to a single one to avoid excessive spacing. Leading whitespace
// is preserved so nested list indentation still renders.
//
// Content inside a fenced code block (``` ... ```) is emitted verbatim: its
// single newlines must NOT be doubled, otherwise the code would be double-spaced
// and its formatting broken. (Indented 4-space code blocks are not detected and
// will still be treated as prose; fenced blocks cover the common model output.)
func markdownContent(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	var b strings.Builder
	inFence := false
	for _, ln := range lines {
		fence := strings.HasPrefix(strings.TrimSpace(ln), "```")
		switch {
		case inFence:
			// Preserve code lines exactly, joined by single newlines.
			b.WriteByte('\n')
			b.WriteString(ln)
			if fence {
				inFence = false
			}
		case fence:
			// Opening fence: separate from any preceding paragraph, then enter
			// verbatim mode.
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(ln)
			inFence = true
		case strings.TrimSpace(ln) == "":
			// Blank line between paragraphs: dropped here; the separator is
			// inserted when the next content line is written.
		default:
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(ln)
		}
	}
	return b.String()
}

// uploadMedia uploads a rich-media image by URL for C2C/group and returns the
// opaque file_info used in a msg_type=7 send. scene is "c2c" or "group".
func (c *client) uploadMedia(ctx context.Context, scene, target, imageURL string) (string, error) {
	var path string
	switch scene {
	case "c2c":
		path = "/v2/users/" + target + "/files"
	case "group":
		path = "/v2/groups/" + target + "/files"
	default:
		return "", fmt.Errorf("uploadMedia: unsupported scene %q", scene)
	}
	body := map[string]any{
		"file_type":    1, // image
		"url":          imageURL,
		"srv_send_msg": false,
	}
	var out fileInfoResponse
	if err := c.doJSON(ctx, http.MethodPost, path, body, &out); err != nil {
		return "", err
	}
	if out.FileInfo == "" {
		return "", fmt.Errorf("empty file_info")
	}
	return out.FileInfo, nil
}

// sendC2C sends a text and/or images to a user openid.
func (c *client) sendC2C(ctx context.Context, openid, replyMsgID, text string, imageURLs []string) error {
	return c.sendUserOrGroup(ctx, "c2c", "/v2/users/"+openid+"/messages", "c2c", openid, replyMsgID, text, imageURLs)
}

// sendGroup sends a text and/or images to a group openid.
func (c *client) sendGroup(ctx context.Context, groupOpenID, replyMsgID, text string, imageURLs []string) error {
	return c.sendUserOrGroup(ctx, "group", "/v2/groups/"+groupOpenID+"/messages", "group", groupOpenID, replyMsgID, text, imageURLs)
}

func (c *client) sendUserOrGroup(ctx context.Context, scene, path, uploadScene, target, replyMsgID, text string, imageURLs []string) error {
	// Text first (if any), then each image as its own media message.
	if strings.TrimSpace(text) != "" {
		if err := c.sendUserOrGroupText(ctx, scene, path, target, replyMsgID, text); err != nil {
			return err
		}
	}
	for _, u := range imageURLs {
		fileInfo, err := c.uploadMedia(ctx, uploadScene, target, u)
		if err != nil {
			return err
		}
		payload := map[string]any{
			"content":  " ",
			"msg_type": msgTypeMedia,
			"media":    map[string]any{"file_info": fileInfo},
		}
		if replyMsgID != "" {
			payload["msg_id"] = replyMsgID
			payload["msg_seq"] = c.nextSeq(replyMsgID)
		}
		if err := c.doJSON(ctx, http.MethodPost, path, payload, nil); err != nil {
			return err
		}
	}
	return nil
}

// sendGuildText sends the text body for a guild channel message. Guild Markdown
// still requires 内邀开通, so a Markdown attempt (markdown.content) falls back to
// a plain-text content message and caches the target as unsupported.
func (c *client) sendGuildText(ctx context.Context, path, channelID, replyMsgID, text string) error {
	capKey := "guild:" + channelID
	if c.markdownSupported(capKey) {
		payload := map[string]any{
			"markdown": map[string]any{"content": markdownContent(text)},
		}
		if replyMsgID != "" {
			payload["msg_id"] = replyMsgID
		}
		err := c.doJSON(ctx, http.MethodPost, path, payload, nil)
		if err == nil {
			return nil
		}
		// Only downgrade on an outright rejection; propagate transient errors so
		// we neither cache a false negative nor risk double-posting a message
		// that may have actually been delivered.
		if !markdownRejected(err) {
			return err
		}
		c.markMarkdownUnsupported(capKey)
		log.Warn().Err(err).Str("scene", "guild").Msg("qq: markdown rejected; falling back to plain text")
	}
	payload := map[string]any{"content": text}
	if replyMsgID != "" {
		payload["msg_id"] = replyMsgID
	}
	return c.doJSON(ctx, http.MethodPost, path, payload, nil)
}

// sendUserOrGroupText sends the text body for a C2C/group message. When Markdown
// is enabled and the target isn't known to reject it, the text is sent as a
// native Markdown message (msg_type=2); on failure it falls back to plain text
// (msg_type=0) and remembers the target as Markdown-unsupported.
func (c *client) sendUserOrGroupText(ctx context.Context, scene, path, target, replyMsgID, text string) error {
	capKey := scene + ":" + target
	// Reserve a single msg_seq up front and reuse it for both the Markdown
	// attempt and the plain-text fallback. QQ dedups on (msg_id, msg_seq), so if
	// the Markdown message was actually delivered the fallback is dropped rather
	// than double-posted; if Markdown was rejected (4xx, not delivered) the seq
	// was never consumed server-side and the fallback goes through.
	var seq int
	if replyMsgID != "" {
		seq = c.nextSeq(replyMsgID)
	}
	if c.markdownSupported(capKey) {
		payload := map[string]any{
			"msg_type": msgTypeMarkdown,
			"markdown": map[string]any{"content": markdownContent(text)},
		}
		if replyMsgID != "" {
			payload["msg_id"] = replyMsgID
			payload["msg_seq"] = seq
		}
		err := c.doJSON(ctx, http.MethodPost, path, payload, nil)
		if err == nil {
			return nil
		}
		// Only downgrade on an outright rejection; propagate transient errors.
		if !markdownRejected(err) {
			return err
		}
		c.markMarkdownUnsupported(capKey)
		log.Warn().Err(err).Str("scene", scene).Msg("qq: markdown rejected; falling back to plain text")
	}
	payload := map[string]any{
		"content":  text,
		"msg_type": msgTypeText,
	}
	if replyMsgID != "" {
		payload["msg_id"] = replyMsgID
		payload["msg_seq"] = seq
	}
	return c.doJSON(ctx, http.MethodPost, path, payload, nil)
}

// sendGuild sends to a guild channel. Guild channel messages carry text plus an
// optional image URL (public-domain guilds cannot receive inbound rich media,
// but can be sent an image URL).
func (c *client) sendGuild(ctx context.Context, channelID, replyMsgID, text string, imageURLs []string) error {
	path := "/channels/" + channelID + "/messages"
	if strings.TrimSpace(text) != "" || len(imageURLs) == 0 {
		if err := c.sendGuildText(ctx, path, channelID, replyMsgID, text); err != nil {
			return err
		}
	}
	for _, u := range imageURLs {
		payload := map[string]any{"image": u}
		if replyMsgID != "" {
			payload["msg_id"] = replyMsgID
		}
		if err := c.doJSON(ctx, http.MethodPost, path, payload, nil); err != nil {
			return err
		}
	}
	return nil
}
