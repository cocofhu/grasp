package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/pagebridge"

	"github.com/gorilla/websocket"
)

func dialPublicEvents(t *testing.T, srv *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, "/public/gate-approvals/events"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	_ = conn.WriteJSON(map[string]string{"token": token})
	for {
		var m map[string]any
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		if err := conn.ReadJSON(&m); err != nil {
			t.Fatalf("ready: %v", err)
		}
		if m["type"] == "ready" {
			return conn
		}
	}
}

func readFrame(t *testing.T, conn *websocket.Conn, typ string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var m map[string]any
		_ = conn.SetReadDeadline(deadline)
		if err := conn.ReadJSON(&m); err != nil {
			break
		}
		if m["type"] == typ {
			return m
		}
	}
	t.Fatalf("no %s frame", typ)
	return nil
}

func waitStatus(t *testing.T, hub *pagebridge.Hub, key pagebridge.Key, want pagebridge.Status) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.Status(key) == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("status = %s, want %s", hub.Status(key), want)
}

func TestDrawerRegistersPageControlAndRelaysCommands(t *testing.T) {
	hn := newHarness(t)
	hub := pagebridge.NewHub()
	hn.h.PageBridge = hub
	seedAppPreviewReview(t, hn, "run-page", "ap1")
	seedDirectPreview(t, hn, "run-page", "ap1")
	token, _ := redeem(t, hn, issueSessionTicket(t, hn, "run-page", "ap1"))["token"].(string)

	srv := httptest.NewServer(hn.r)
	t.Cleanup(srv.Close)
	drawer := dialPublicEvents(t, srv, token)
	key := pagebridge.Key{RunID: "run-page", NodeID: "ap1", Owner: pagebridge.UserOwner("admin")}

	_ = drawer.WriteJSON(map[string]any{"type": "page_control", "on": true, "visible": true})
	st := readFrame(t, drawer, "page_control_state")
	if st["state"] != "online" || st["active"] != true || st["nodeId"] != nil {
		t.Fatalf("state frame = %v", st)
	}
	waitStatus(t, hub, key, pagebridge.StatusOnline)

	type out struct {
		res pagebridge.Result
		err error
	}
	done := make(chan out, 1)
	go func() {
		res, err := hub.Do(context.Background(), key, pagebridge.Command{Action: "click", Args: map[string]any{"index": 2}})
		done <- out{res, err}
	}()
	cmd := readFrame(t, drawer, "page_cmd")
	if cmd["action"] != "click" {
		t.Fatalf("cmd = %v", cmd)
	}
	_ = drawer.WriteJSON(map[string]any{"type": "page_result", "id": cmd["id"], "ok": true, "state": map[string]any{"stateId": "p:2"}})
	select {
	case o := <-done:
		if o.err != nil || !o.res.OK || o.res.State["stateId"] != "p:2" {
			t.Fatalf("got %+v %v", o.res, o.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("command did not return")
	}

	// The same user's workbench sees the status, with the node id.
	bench, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, "/api/runs/run-page/events"), hn.wsHeader())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { bench.Close() })
	if st := readFrame(t, bench, "page_control_state"); st["state"] != "online" || st["nodeId"] != "ap1" {
		t.Fatalf("workbench frame = %v", st)
	}
	drawer.Close()
	waitStatus(t, hub, key, pagebridge.StatusOffline)
	if st := readFrame(t, bench, "page_control_state"); st["state"] != "offline" {
		t.Fatalf("workbench frame = %v", st)
	}
}

type sessionTurns map[string]string

func (m sessionTurns) PageTurn(_, _, sessionID string) (string, <-chan struct{}, bool) {
	return m[sessionID], nil, m[sessionID] != ""
}

func TestMCPPageToolRoutesBySessionID(t *testing.T) {
	hn := newHarness(t)
	seedAppPreviewReview(t, hn, "run-page-sid", "ap1")
	seedDirectPreview(t, hn, "run-page-sid", "ap1")
	hub := pagebridge.NewHub()
	hn.host.SetPageBridge(&pagebridge.Router{Hub: hub, Turns: sessionTurns{"ps_a": "embed:a", "ps_b": "embed:b"}})
	tok := hn.host.RegisterRun("run-page-sid")
	hn.host.SetActiveNode("run-page-sid", "ap1", testPreviewCaps)
	hn.host.SetActiveReview("run-page-sid", true)

	frames := map[string]chan map[string]any{"embed:a": make(chan map[string]any, 4), "embed:b": make(chan map[string]any, 4)}
	conns := map[string]*pagebridge.Conn{}
	for owner, ch := range frames {
		ch := ch
		c := hub.Attach(pagebridge.Key{RunID: "run-page-sid", NodeID: "ap1", Owner: owner}, func(b []byte) error {
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			if m["type"] == "page_cmd" {
				ch <- m
			}
			return nil
		})
		c.SetControl(true, true)
		conns[owner] = c
	}

	callPage := func(sid string) string {
		req := httptest.NewRequest(http.MethodPost, "/mcp/runs/run-page-sid", strings.NewReader(
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"page_state","arguments":{"session_id":"`+sid+`"}}}`))
		req.Header.Set("Authorization", "Bearer "+tok)
		w := httptest.NewRecorder()
		hn.r.ServeHTTP(w, req)
		return w.Body.String()
	}
	done := make(chan string, 1)
	go func() { done <- callPage("ps_a") }()
	select {
	case cmd := <-frames["embed:a"]:
		conns["embed:a"].Deliver(cmd["id"].(string), pagebridge.Result{OK: true, State: map[string]any{"stateId": "a:1"}})
	case <-time.After(3 * time.Second):
		t.Fatal("ps_a never reached embed:a")
	}
	if body := <-done; !strings.Contains(body, "a:1") {
		t.Fatalf("result = %s", body)
	}
	select {
	case f := <-frames["embed:b"]:
		t.Fatalf("ps_a reached embed:b: %v", f)
	default:
	}
	if body := callPage(""); !strings.Contains(body, "session_id") {
		t.Fatalf("missing session id should be refused: %s", body)
	}
}

func TestShareTokenCannotOfferPage(t *testing.T) {
	hn := newHarness(t)
	hub := pagebridge.NewHub()
	hn.h.PageBridge = hub
	seedAppPreviewReview(t, hn, "run-page-share", "ap1")
	seedDirectPreview(t, hn, "run-page-share", "ap1")
	created := parseJSON(t, hn.do(http.MethodPost, "/api/runs/run-page-share/reviews/ap1/share-link", map[string]any{"ttlTier": "24h"}))
	url, _ := created["url"].(string)
	share := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")

	srv := httptest.NewServer(hn.r)
	t.Cleanup(srv.Close)
	conn := dialPublicEvents(t, srv, share)
	_ = conn.WriteJSON(map[string]any{"type": "page_control", "on": true, "visible": true})
	time.Sleep(100 * time.Millisecond)
	if st := hub.Status(pagebridge.Key{RunID: "run-page-share", NodeID: "ap1", Owner: pagebridge.TokenOwner("share", share)}); st != pagebridge.StatusOffline {
		t.Fatalf("share-link page registered: %s", st)
	}
}
