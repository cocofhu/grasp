package wecom

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/channels"
	"github.com/cocofhu/grasp/internal/models"
)

func TestTruncateMarkdown(t *testing.T) {
	if got := TruncateMarkdown("短"); got != "短" {
		t.Fatalf("short = %q", got)
	}
	long := strings.Repeat("测", MarkdownMaxBytes)
	got := TruncateMarkdown(long)
	if !strings.HasSuffix(got, truncateSuffix) {
		t.Fatalf("missing suffix: %q", got[len(got)-12:])
	}
	if len(got) > MarkdownMaxBytes {
		t.Fatalf("len=%d want <= %d", len(got), MarkdownMaxBytes)
	}
	if !strings.HasPrefix(got, "测") {
		t.Fatal("should keep prefix")
	}
}

func TestClassifyWeComErrorRateLimit(t *testing.T) {
	err := classifyWeComError(45009, "api freq out of limit")
	if !IsRateLimited(err) {
		t.Fatalf("45009 should be rate limit: %v", err)
	}
	err = classifyWeComError(0, "访问频率过高")
	if !IsRateLimited(err) {
		t.Fatalf("频率文案 should be rate limit: %v", err)
	}
	err = classifyWeComError(40014, "invalid secret")
	if IsRateLimited(err) {
		t.Fatalf("generic should not classify: %v", err)
	}
}

func TestDecryptImageAES256CBC(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	plain := []byte("wecom-image-plain-bytes!!")
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append(plain, bytes.Repeat([]byte{byte(pad)}, pad)...)
	ct := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, key[:16]).CryptBlocks(ct, padded)

	got, err := DecryptImageAES256CBC(ct, hex.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("got %q want %q", got, plain)
	}
}

func TestInboundMapsSingleAndGroup(t *testing.T) {
	a, err := New(channels.AdapterConfig{AppID: "bot", AppSecret: "sec"})
	if err != nil {
		t.Fatal(err)
	}
	ad := a.(*Adapter)
	var got []channels.InboundMessage
	var mu sync.Mutex
	on := func(_ context.Context, in channels.InboundMessage) {
		mu.Lock()
		got = append(got, in)
		mu.Unlock()
	}

	singleBody, _ := json.Marshal(callbackBody{
		MsgID: "m1", ChatType: chatSingle, From: callbackFrom{UserID: "zhangsan"},
		MsgType: msgText, Text: &callbackText{Content: "hello"},
	})
	ad.ApplyCallbackForTest(context.Background(), frame{
		Cmd: cmdCallback, Headers: frameHeaders{ReqID: "req-1"}, Body: singleBody,
	}, on)

	groupBody, _ := json.Marshal(callbackBody{
		MsgID: "m2", ChatType: chatGroup, ChatID: "wr_ops",
		From: callbackFrom{UserID: "lisi"}, MsgType: msgText, Text: &callbackText{Content: "群消息"},
	})
	ad.ApplyCallbackForTest(context.Background(), frame{
		Cmd: cmdCallback, Headers: frameHeaders{ReqID: "req-2"}, Body: groupBody,
	}, on)

	// duplicate msgid ignored
	ad.ApplyCallbackForTest(context.Background(), frame{
		Cmd: cmdCallback, Headers: frameHeaders{ReqID: "req-1b"}, Body: singleBody,
	}, on)

	// group image ignored
	imgBody, _ := json.Marshal(callbackBody{
		MsgID: "m3", ChatType: chatGroup, ChatID: "wr_ops",
		From: callbackFrom{UserID: "lisi"}, MsgType: msgImage,
		Image: &callbackImage{URL: "https://x/a", AESKey: "k"},
	})
	ad.ApplyCallbackForTest(context.Background(), frame{
		Cmd: cmdCallback, Headers: frameHeaders{ReqID: "req-3"}, Body: imgBody,
	}, on)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("inbound count=%d want 2: %+v", len(got), got)
	}
	if got[0].Scene != channels.SceneC2C || got[0].ConversationID != "zhangsan" || got[0].Text != "hello" {
		t.Fatalf("single = %+v", got[0])
	}
	if ad.lookupReqID("m1") != "req-1" {
		t.Fatalf("req_id cache missing")
	}
	if got[1].Scene != channels.SceneGroup || got[1].ConversationID != "wr_ops" {
		t.Fatalf("group = %+v", got[1])
	}
}

func TestTypeConstant(t *testing.T) {
	a, _ := New(channels.AdapterConfig{AppID: "b", AppSecret: "s"})
	if a.Type() != models.ChannelTypeWeCom {
		t.Fatalf("type=%s", a.Type())
	}
	if a.(*Adapter).Online() {
		t.Fatal("should be offline before subscribe")
	}
}

func TestDownloadDecryptImage(t *testing.T) {
	key := bytes.Repeat([]byte{0x11}, 32)
	plain := []byte("png-bytes-here")
	block, _ := aes.NewCipher(key)
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append(plain, bytes.Repeat([]byte{byte(pad)}, pad)...)
	ct := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, key[:16]).CryptBlocks(ct, padded)

	a, _ := New(channels.AdapterConfig{AppID: "b", AppSecret: "s"})
	ad := a.(*Adapter)
	ad.httpDo = func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewReader(ct)),
		}, nil
	}
	img, hint := ad.downloadDecryptImage(context.Background(), &callbackImage{
		URL: "https://example.com/img", AESKey: hex.EncodeToString(key),
	})
	if hint != "" || img == nil {
		t.Fatalf("hint=%q img=%v", hint, img)
	}
	if !bytes.Equal(img.Data, plain) {
		t.Fatalf("data=%q", img.Data)
	}
}

func TestEncodeSubscribeUsesBotIDKey(t *testing.T) {
	raw, err := encodeFrame(cmdSubscribe, "req-sub-1", subscribeBody{
		BotID: "bot-abc", Secret: "s3cret",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"bot_id"`) {
		t.Fatalf("subscribe payload missing bot_id: %s", s)
	}
	if strings.Contains(s, `"botid"`) {
		t.Fatalf("subscribe payload must not use botid key: %s", s)
	}
	if !strings.Contains(s, `"secret"`) || !strings.Contains(s, "bot-abc") {
		t.Fatalf("subscribe payload incomplete: %s", s)
	}
	var f frame
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if f.Cmd != cmdSubscribe || f.Headers.ReqID != "req-sub-1" {
		t.Fatalf("frame=%+v", f)
	}
	var body map[string]any
	if err := json.Unmarshal(f.Body, &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["botid"]; ok {
		t.Fatalf("body has standalone botid key: %v", body)
	}
	if body["bot_id"] != "bot-abc" {
		t.Fatalf("bot_id=%v", body["bot_id"])
	}
}

func TestEncodeSendMsgChatTypeInt(t *testing.T) {
	for _, tc := range []struct {
		scene channels.Scene
		want  float64
	}{
		{channels.SceneC2C, 1},
		{channels.SceneGroup, 2},
	} {
		code, ok := sendChatTypeOf(tc.scene)
		if !ok {
			t.Fatalf("scene %s not mapped", tc.scene)
		}
		raw, err := encodeFrame(cmdSend, "req-send", sendBody{
			ChatID: "wr_or_userid", ChatType: code, MsgType: "markdown",
			Markdown: markdownBody{Content: "hi"},
		})
		if err != nil {
			t.Fatal(err)
		}
		s := string(raw)
		if strings.Contains(s, `"chattype"`) {
			t.Fatalf("send_msg must not use string chattype: %s", s)
		}
		if !strings.Contains(s, `"chat_type"`) {
			t.Fatalf("send_msg missing chat_type: %s", s)
		}
		var f frame
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.Unmarshal(f.Body, &body); err != nil {
			t.Fatal(err)
		}
		got, ok := body["chat_type"].(float64)
		if !ok {
			t.Fatalf("chat_type type=%T value=%v", body["chat_type"], body["chat_type"])
		}
		if got != tc.want {
			t.Fatalf("chat_type=%v want %v", got, tc.want)
		}
	}
}

func TestEncodePingFrame(t *testing.T) {
	raw, err := encodeFrame(cmdPing, "req-ping-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"cmd":"ping"`) {
		t.Fatalf("ping cmd missing: %s", s)
	}
	if strings.Contains(s, `"body"`) {
		t.Fatalf("ping should omit body: %s", s)
	}
	var f frame
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if f.Cmd != cmdPing || f.Headers.ReqID != "req-ping-1" || len(f.Body) != 0 {
		t.Fatalf("ping frame=%+v body=%s", f, f.Body)
	}
}

func TestDecryptImageOfficialBase64AndPad32(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	plain := []byte("official-wecom-image-bytes")
	pad := 32 - len(plain)%32
	if pad == 0 {
		pad = 32
	}
	padded := append(append([]byte{}, plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	if len(padded)%32 != 0 {
		t.Fatalf("padded len=%d not multiple of 32", len(padded))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	ct := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, key[:16]).CryptBlocks(ct, padded)

	urlKey := base64.RawURLEncoding.EncodeToString(key)
	if len(urlKey) != 43 {
		t.Fatalf("url-safe aeskey len=%d want 43: %s", len(urlKey), urlKey)
	}
	got, err := DecryptImageAES256CBC(ct, urlKey)
	if err != nil {
		t.Fatalf("url-safe base64: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("url-safe got %q want %q", got, plain)
	}

	stdKey := base64.StdEncoding.EncodeToString(key)
	got, err = DecryptImageAES256CBC(ct, stdKey)
	if err != nil {
		t.Fatalf("std base64: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("std got %q want %q", got, plain)
	}
}

func TestWaitOutboundAckTimeout(t *testing.T) {
	ch := make(chan resultBody)
	err := waitOutboundAck(context.Background(), ch, 5*time.Millisecond)
	if !errors.Is(err, ErrOutboundTimeout) {
		t.Fatalf("want timeout, got %v", err)
	}
}

func TestWaitOutboundAckErrorCode(t *testing.T) {
	ch := make(chan resultBody, 1)
	ch <- resultBody{ErrCode: 45009, ErrMsg: "api freq out of limit"}
	err := waitOutboundAck(context.Background(), ch, time.Second)
	if !IsRateLimited(err) {
		t.Fatalf("want rate limit, got %v", err)
	}
}

func TestSubscribeAckWithoutCmdMarksOnline(t *testing.T) {
	a, err := New(channels.AdapterConfig{AppID: "bot", AppSecret: "sec"})
	if err != nil {
		t.Fatal(err)
	}
	ad := a.(*Adapter)
	if ad.Online() {
		t.Fatal("should start offline")
	}

	var got []channels.InboundMessage
	on := func(_ context.Context, in channels.InboundMessage) {
		got = append(got, in)
	}
	sess := &connSession{subReq: "sub-req-1"}

	// Callback arrives before subscribe ACK — must be buffered, not dropped.
	cb, _ := json.Marshal(callbackBody{
		MsgID: "early-1", ChatType: chatSingle, From: callbackFrom{UserID: "u1"},
		MsgType: msgText, Text: &callbackText{Content: "buffered"},
	})
	if err := ad.HandleIncomingForTest(context.Background(), frame{
		Cmd: cmdCallback, Headers: frameHeaders{ReqID: "cb-1"}, Body: cb,
	}, sess, on); err != nil {
		t.Fatal(err)
	}
	if ad.Online() || len(got) != 0 {
		t.Fatalf("callback before ACK must buffer: online=%v got=%d", ad.Online(), len(got))
	}

	// Official ACK: headers.req_id + errcode, no cmd.
	if err := ad.HandleIncomingForTest(context.Background(), frame{
		Headers: frameHeaders{ReqID: "sub-req-1"}, ErrCode: 0,
	}, sess, on); err != nil {
		t.Fatal(err)
	}
	if !ad.Online() || !sess.subscribed {
		t.Fatal("errcode==0 ACK must mark online without cmd")
	}
	if len(got) != 1 || got[0].Text != "buffered" || got[0].ConversationID != "u1" {
		t.Fatalf("buffered callback not flushed: %+v", got)
	}
}

func TestSubscribeAckErrorKeepsOffline(t *testing.T) {
	a, _ := New(channels.AdapterConfig{AppID: "bot", AppSecret: "sec"})
	ad := a.(*Adapter)
	sess := &connSession{subReq: "sub-bad"}
	err := ad.HandleIncomingForTest(context.Background(), frame{
		Headers: frameHeaders{ReqID: "sub-bad"}, ErrCode: 40014, ErrMsg: "invalid secret",
	}, sess, nil)
	if err == nil {
		t.Fatal("want subscribe error")
	}
	if ad.Online() || sess.subscribed {
		t.Fatal("failed subscribe must stay offline")
	}
}

func TestInboundMixedItemsDecryptsC2CImage(t *testing.T) {
	key := bytes.Repeat([]byte{0x22}, 32)
	plain := []byte("mixed-img")
	pad := 32 - len(plain)%32
	padded := append(plain, bytes.Repeat([]byte{byte(pad)}, pad)...)
	block, _ := aes.NewCipher(key)
	ct := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, key[:16]).CryptBlocks(ct, padded)

	a, _ := New(channels.AdapterConfig{AppID: "b", AppSecret: "s"})
	ad := a.(*Adapter)
	ad.httpDo = func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(ct))}, nil
	}

	body, _ := json.Marshal(callbackBody{
		MsgID: "mix-1", ChatType: chatSingle, From: callbackFrom{UserID: "zhang"},
		MsgType: msgMixed,
		Mixed: &callbackMixed{Item: []mixedItem{
			{Type: msgText, Text: &callbackText{Content: "看图"}},
			{Type: msgImage, Image: &callbackImage{
				URL: "https://example.com/m", AESKey: base64.RawURLEncoding.EncodeToString(key),
			}},
		}},
	})
	var got []channels.InboundMessage
	ad.ApplyCallbackForTest(context.Background(), frame{
		Cmd: cmdCallback, Headers: frameHeaders{ReqID: "req-mix"}, Body: body,
	}, func(_ context.Context, in channels.InboundMessage) {
		got = append(got, in)
	})
	if len(got) != 1 {
		t.Fatalf("got=%d", len(got))
	}
	if got[0].Text != "看图" || len(got[0].Images) != 1 {
		t.Fatalf("mixed inbound=%+v", got[0])
	}
	if !bytes.Equal(got[0].Images[0].Data, plain) {
		t.Fatalf("image=%q", got[0].Images[0].Data)
	}
}
