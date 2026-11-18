package runtime

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestAlignChatRoutePostsOnTheNextTurnAndRollsBack(t *testing.T) {
	selected := "old"
	effort := "low"
	tier := ""
	var posts []string
	var postedTiers []string
	failNew := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			http.SetCookie(w, &http.Cookie{Name: "agentchat_session", Value: "cookie", Path: "/"})
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		if r.URL.Query().Get("chat") != "default" {
			t.Errorf("chat = %q", r.URL.Query().Get("chat"))
		}
		switch r.URL.Path {
		case "/api/models":
			_ = json.NewEncoder(w).Encode(map[string]any{"selected": selected, "reasoningEffort": effort, "serviceTier": tier})
		case "/api/model":
			var body struct {
				Model           string `json:"model"`
				ReasoningEffort string `json:"reasoningEffort"`
				ServiceTier     string `json:"serviceTier"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			posts = append(posts, body.Model+"/"+body.ReasoningEffort)
			postedTiers = append(postedTiers, body.ServiceTier)
			tier = body.ServiceTier
			if failNew && body.Model == "new" {
				selected, effort = body.Model, body.ReasoningEffort
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"unavailable"}`))
				return
			}
			selected, effort = body.Model, body.ReasoningEffort
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	host, port := splitRouteHost(t, srv.URL)
	acp := sandbox.NewACPClient(host, port).WithPassword("secret").WithBridgeModel("primary").WithExecution(&models.ChatExecution{ModelID: "primary", Backend: "cursor"})
	acp.SeedBridgeForTest(sandbox.BridgeState{}, "", "")
	p := &acpProvider{sessions: map[string]*reactSession{}, backend: BackendCursor}
	req := NodeReq{RunID: "run", NodeID: "clarify", ChatRoute: &ChatRoute{ModelID: "new", ReasoningEffort: "high", ServiceTier: "fast", Set: true}, AppliedRoute: &ChatRoute{ModelID: "old", ReasoningEffort: "low", Set: true}}
	p.sessions[reactKey(req)] = &reactSession{sb: &sandbox.Sandbox{Host: host, Port: port, Password: "secret"}, acp: acp}

	notice, aligned, err := p.AlignChatRoute(context.Background(), req)
	if err != nil || !aligned || notice != visitModelSwitchFailed {
		t.Fatalf("rollback notice: %q aligned=%v err=%v", notice, aligned, err)
	}
	if len(posts) != 2 || posts[0] != "new/high" || posts[1] != "old/low" {
		t.Fatalf("posts = %#v", posts)
	}
	if postedTiers[0] != "fast" || postedTiers[1] != "" || tier != "" {
		t.Fatalf("service tier did not roll back with the route: %q", postedTiers)
	}
	if got := p.sessions[reactKey(req)].acp.Execution(); got == nil || got.ModelID != "old" || got.ReasoningEffort != "low" || got.ServiceTier != "" {
		t.Fatalf("execution = %+v", got)
	}
	posts = nil
	failNew = false
	selected, effort = "old", "low"
	req.ChatRoute.ModelID = "aux"
	req.ChatRoute.ReasoningEffort = ""
	req.ChatRoute.ServiceTier = ""
	notice, aligned, err = p.AlignChatRoute(context.Background(), req)
	if err != nil || notice != "" || !aligned || len(posts) != 1 || posts[0] != "aux/" {
		t.Fatalf("apply: notice=%q posts=%#v err=%v", notice, posts, err)
	}
	if got := p.sessions[reactKey(req)].acp.Execution(); got == nil || got.ModelID != "aux" {
		t.Fatalf("stamped execution = %+v", got)
	}
}

func TestAlignChatRouteTreatsLostSandboxAsUnaligned(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		refused bool
		connect bool
	}{
		{name: "disconnected acp", status: http.StatusOK, connect: false},
		{name: "gateway 502", status: http.StatusBadGateway, connect: true},
		{name: "gateway 503", status: http.StatusServiceUnavailable, connect: true},
		{name: "gateway 504", status: http.StatusGatewayTimeout, connect: true},
		{name: "connection refused", refused: true, connect: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var posts int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/login" {
					http.SetCookie(w, &http.Cookie{Name: "agentchat_session", Value: "cookie", Path: "/"})
					_, _ = w.Write([]byte(`{"ok":true}`))
					return
				}
				if r.URL.Path == "/api/model" {
					posts++
				}
				if tc.status != http.StatusOK {
					w.WriteHeader(tc.status)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"selected": "old", "reasoningEffort": "low"})
			}))
			host, port := splitRouteHost(t, srv.URL)
			if tc.refused {
				srv.Close()
			} else {
				defer srv.Close()
			}
			acp := sandbox.NewACPClient(host, port).WithPassword("secret").WithBridgeModel("primary").
				WithExecution(&models.ChatExecution{ModelID: "old", ReasoningEffort: "low", Backend: "cursor"})
			if tc.connect {
				acp.SeedBridgeForTest(sandbox.BridgeState{}, "", "")
			}
			p := &acpProvider{sessions: map[string]*reactSession{}, backend: BackendCursor}
			req := NodeReq{RunID: "run", NodeID: "clarify", ChatRoute: &ChatRoute{ModelID: "new", ReasoningEffort: "high", Set: true}, AppliedRoute: &ChatRoute{ModelID: "old", ReasoningEffort: "low", Set: true}}
			p.sessions[reactKey(req)] = &reactSession{sb: &sandbox.Sandbox{Host: host, Port: port, Password: "secret"}, acp: acp}

			notice, aligned, err := p.AlignChatRoute(context.Background(), req)
			if notice != "" || aligned || err != nil {
				t.Fatalf("lost sandbox reported notice=%q aligned=%v err=%v", notice, aligned, err)
			}
			if posts != 0 {
				t.Fatalf("lost sandbox was rolled back with %d posts", posts)
			}
			if _, _, lost, _ := p.alignParkedRoute(context.Background(), req); !lost {
				t.Fatal("lost sandbox was not reported for rebuild")
			}
		})
	}
}

func TestIsSandboxUnreachableKeepsRejectedSwitches(t *testing.T) {
	connected := sandbox.NewACPClient("127.0.0.1", 1)
	connected.SeedBridgeForTest(sandbox.BridgeState{}, "", "")
	sess := &reactSession{acp: connected}
	for _, err := range []error{
		&sandbox.ChatHTTPError{StatusCode: http.StatusBadRequest},
		&sandbox.ChatHTTPError{StatusCode: http.StatusInternalServerError},
		&sandbox.GatewayHTTPError{StatusCode: http.StatusConflict},
		io.ErrUnexpectedEOF,
	} {
		if isSandboxUnreachable(sess, err) {
			t.Fatalf("%v treated as unreachable", err)
		}
	}
	for _, err := range []error{
		sandbox.ErrConnClosed,
		sandbox.ErrGatewayTransport,
		&sandbox.GatewayHTTPError{StatusCode: http.StatusBadGateway},
		&net.OpError{Op: "dial", Err: io.EOF},
	} {
		if !isSandboxUnreachable(sess, err) {
			t.Fatalf("%v not treated as unreachable", err)
		}
	}
}

func TestParkReactSessionRecordsLaunchedModelGroup(t *testing.T) {
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			http.SetCookie(w, &http.Cookie{Name: "agentchat_session", Value: "cookie", Path: "/"})
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		if r.URL.Path == "/api/model" {
			posts++
			w.WriteHeader(http.StatusOK)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"selected": "aux", "reasoningEffort": "high"})
	}))
	defer srv.Close()
	host, port := splitRouteHost(t, srv.URL)
	p := &acpProvider{sessions: map[string]*reactSession{}, live: map[string]*sandbox.Sandbox{}, backend: BackendCursor}
	req := NodeReq{RunID: "run", NodeID: "clarify", ChatRoute: &ChatRoute{ConfigurationID: "grp-b", ModelID: "aux", ReasoningEffort: "high", Set: true}}

	launched := sandbox.NewACPClient(host, port).WithPassword("secret").WithBridgeModel("aux").
		WithExecution(&models.ChatExecution{ConfigurationID: "grp-b", ModelID: "aux", Backend: "cursor"})
	launched.SeedBridgeForTest(sandbox.BridgeState{}, "", "")
	sess := p.parkReactSession(req, &sandbox.Sandbox{Host: host, Port: port, Password: "secret"}, launched, "")
	if sess.routeConfigurationID != "grp-b" {
		t.Fatalf("routeConfigurationID = %q, want grp-b", sess.routeConfigurationID)
	}
	// p has no tool switcher: a needless SwitchTool would fail the alignment.
	notice, aligned, err := p.AlignChatRoute(context.Background(), req)
	if notice != "" || !aligned || err != nil || posts != 0 {
		t.Fatalf("rebuilt session switched again: notice=%q aligned=%v err=%v posts=%d", notice, aligned, err, posts)
	}

	primary := sandbox.NewACPClient(host, port).WithExecution(&models.ChatExecution{Backend: "cursor"})
	if got := p.parkReactSession(req, &sandbox.Sandbox{Host: host, Port: port}, primary, "").routeConfigurationID; got != "" {
		t.Fatalf("default launch recorded group %q", got)
	}
}

func TestVisitToolSwitchPreservesSandboxAndSkipsNodeInitialization(t *testing.T) {
	h := newSharedHarness(t, nil)
	req := h.req("n1")
	req.KeepAliveForReview = true
	h.run(t, req)
	h.p.mu.Lock()
	sess := h.p.sessions[reactKey(req)]
	h.p.mu.Unlock()
	if sess == nil || sess.sb == nil {
		t.Fatal("missing parked visit session")
	}
	container, workspace := sess.sb, sess.sb.WorkspaceDir
	for i := 0; i < 2; i++ {
		want, err := h.p.spec(req)
		if err != nil {
			t.Fatal(err)
		}
		// A user switch must work even when the node's configured repo is
		// missing/unreachable; only the shared-node path may prepare it.
		want.Env["GIT_REPOS"] = "missing|https://unreachable.invalid/team/repo.git|"
		if err := h.p.switchSessionTool(context.Background(), req, sess, want); err != nil {
			t.Fatal(err)
		}
	}
	if h.mgr.createCount() != 1 || h.mgr.reprimeCount() != 0 || len(h.mgr.toolSwitches) != 2 || sess.sb != container || sess.sb.WorkspaceDir != workspace {
		t.Fatalf("user switch initialized a sandbox: creates=%d reprimes=%d switches=%d", h.mgr.createCount(), h.mgr.reprimeCount(), len(h.mgr.toolSwitches))
	}
	h.p.RetireSession(req.RunID, req.NodeID)
}

func TestResetLiveChatModelUsesTheSessionChat(t *testing.T) {
	selected := "aux"
	var chats []string
	var posts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			http.SetCookie(w, &http.Cookie{Name: "agentchat_session", Value: "cookie", Path: "/"})
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		chats = append(chats, r.URL.Query().Get("chat"))
		switch r.URL.Path {
		case "/api/models":
			_ = json.NewEncoder(w).Encode(map[string]string{"selected": selected})
		case "/api/model":
			var body struct {
				Model string `json:"model"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			posts = append(posts, body.Model)
			selected = body.Model
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	host, port := splitRouteHost(t, srv.URL)
	acp := sandbox.NewACPClient(host, port).WithPassword("secret").WithChat("node-chat").WithBridgeModel("primary")
	p := &acpProvider{backend: BackendCursor}
	if err := p.resetLiveChatModel(context.Background(), &sandbox.Sandbox{Host: host, Port: port, Password: "secret"}, acp); err != nil {
		t.Fatal(err)
	}
	if len(posts) != 1 || posts[0] != "" || !containsAll(chats, "node-chat") {
		t.Fatalf("chats=%v posts=%v", chats, posts)
	}
	posts = nil
	if err := p.resetLiveChatModel(context.Background(), &sandbox.Sandbox{Host: host, Port: port, Password: "secret"}, acp); err != nil {
		t.Fatal(err)
	}
	if len(posts) != 0 {
		t.Fatalf("default chat was posted again: %#v", posts)
	}
	req := NodeReq{RunID: "run", NodeID: "auto"}
	if notice, aligned, err := p.AlignChatRoute(context.Background(), req); notice != "" || aligned || err != nil {
		t.Fatalf("missing session: %q %v %v", notice, aligned, err)
	}
}

func TestResetLiveChatModelErrorsWhenSettingsUnread(t *testing.T) {
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			http.SetCookie(w, &http.Cookie{Name: "agentchat_session", Value: "cookie", Path: "/"})
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		if r.URL.Path == "/api/model" {
			posts++
			t.Errorf("posted a reset without reading settings")
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	host, port := splitRouteHost(t, srv.URL)
	acp := sandbox.NewACPClient(host, port).WithPassword("secret").WithChat("node-chat").WithBridgeModel("primary")
	p := &acpProvider{backend: BackendCursor}
	err := p.resetLiveChatModel(context.Background(), &sandbox.Sandbox{Host: host, Port: port, Password: "secret"}, acp)
	if err == nil || posts != 0 {
		t.Fatalf("unread settings treated as primary: err=%v posts=%d", err, posts)
	}
}

func containsAll(values []string, want string) bool {
	for _, value := range values {
		if value != want {
			return false
		}
	}
	return len(values) > 0
}

func splitRouteHost(t *testing.T, raw string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(strings.TrimPrefix(raw, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	return host, port
}
