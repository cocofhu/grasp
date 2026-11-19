package vncws

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func wsURL(srv *httptest.Server, path string) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + path
}

func TestRFBBridge(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = c.Write([]byte("RFB 003.008\n"))
		buf := make([]byte, 5)
		if _, err := io.ReadFull(c, buf); err == nil {
			_, _ = c.Write(bytes.ToUpper(buf))
		}
	}()
	srv := httptest.NewServer((&Server{VNCAddr: ln.Addr().String()}).Handler())
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv, "/websockify"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, msg, err := conn.ReadMessage(); err != nil || string(msg) != "RFB 003.008\n" {
		t.Fatalf("banner %q %v", msg, err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if _, msg, err := conn.ReadMessage(); err != nil || string(msg) != "HELLO" {
		t.Fatalf("echo %q %v", msg, err)
	}
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected close after upstream EOF")
	}
}

func TestRFBUpstreamDown(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	srv := httptest.NewServer((&Server{VNCAddr: addr}).Handler())
	defer srv.Close()
	_, resp, err := websocket.DefaultDialer.Dial(wsURL(srv, "/"), nil)
	if err == nil || resp == nil || resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("want 502, got %v %v", resp, err)
	}
	// Plain HTTP (not an upgrade) also stops at the upgrader.
	ln2, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln2.Close()
	go func() {
		if c, err := ln2.Accept(); err == nil {
			defer c.Close()
			time.Sleep(100 * time.Millisecond)
		}
	}()
	srv2 := httptest.NewServer((&Server{VNCAddr: ln2.Addr().String()}).Handler())
	defer srv2.Close()
	r, err := http.Get(srv2.URL)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("plain GET status %d", r.StatusCode)
	}
}

type fakeAudio struct {
	io.Reader
	closed chan struct{}
}

func (f *fakeAudio) Close() error {
	select {
	case <-f.closed:
	default:
		close(f.closed)
	}
	return nil
}

func TestAudioSkipsSilenceAndEnds(t *testing.T) {
	loud := bytes.Repeat([]byte{1, 2}, FrameBytes/2)
	pcm := append(append(make([]byte, FrameBytes), loud...), 9)
	src := &fakeAudio{Reader: bytes.NewReader(pcm), closed: make(chan struct{})}
	s := &Server{Audio: func(context.Context) (io.ReadCloser, error) { return src, nil }}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv, "/audio"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	typ, msg, err := conn.ReadMessage()
	if err != nil || typ != websocket.BinaryMessage || !bytes.Equal(msg, loud) {
		t.Fatalf("frame type=%d len=%d err=%v", typ, len(msg), err)
	}
	_, _, err = conn.ReadMessage()
	var ce *websocket.CloseError
	if !errors.As(err, &ce) || ce.Code != websocket.CloseGoingAway {
		t.Fatalf("want going-away close, got %v", err)
	}
	select {
	case <-src.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("source not closed")
	}
}

func TestAudioViewerLeaves(t *testing.T) {
	pr, pw := io.Pipe()
	src := &fakeAudio{Reader: pr, closed: make(chan struct{})}
	s := &Server{Audio: func(context.Context) (io.ReadCloser, error) { return src, nil }}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv, "/audio"), nil)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	select {
	case <-src.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("source not closed after viewer left")
	}
	_ = pw.CloseWithError(io.EOF)
}

func TestAudioUnavailable(t *testing.T) {
	for name, s := range map[string]*Server{
		"nil":   {},
		"error": {Audio: func(context.Context) (io.ReadCloser, error) { return nil, errors.New("no pulse") }},
	} {
		srv := httptest.NewServer(s.Handler())
		_, resp, err := websocket.DefaultDialer.Dial(wsURL(srv, "/audio"), nil)
		if err == nil || resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("%s: want 503, got %v %v", name, resp, err)
		}
		srv.Close()
	}
	// A non-upgrade request after a successful capture closes the source.
	src := &fakeAudio{Reader: strings.NewReader(""), closed: make(chan struct{})}
	srv := httptest.NewServer((&Server{Audio: func(context.Context) (io.ReadCloser, error) { return src, nil }}).Handler())
	defer srv.Close()
	r, err := http.Get(srv.URL + "/audio")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	<-src.closed
}

func TestParecSource(t *testing.T) {
	if _, err := ParecSource(func(string) (string, error) { return "", errors.New("missing") })(context.Background()); err == nil {
		t.Fatal("missing parec must fail")
	}
	dir := t.TempDir()
	args := filepath.Join(dir, "args")
	script := filepath.Join(dir, "parec")
	body := "#!/bin/sh\necho \"$@\" >" + args + "\nprintf pcm\nexec sleep 30\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := ParecSource(func(string) (string, error) { return script, nil })(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 3)
	if _, err := io.ReadFull(src, buf); err != nil || string(buf) != "pcm" {
		t.Fatalf("read %q %v", buf, err)
	}
	if err := src.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	got, _ := os.ReadFile(args)
	if !strings.Contains(string(got), "--device=grasp.monitor") || !strings.Contains(string(got), "--rate=48000") {
		t.Fatalf("args %q", got)
	}
	if _, err := ParecSource(func(string) (string, error) { return filepath.Join(dir, "nope"), nil })(context.Background()); err == nil {
		t.Fatal("unstartable parec must fail")
	}
}
