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
	var posts []string
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
			_ = json.NewEncoder(w).Encode(map[string]any{"selected": selected, "reasoningEffort": effort})
		case "/api/model":
			var body struct {
				Model           string `json:"model"`
				ReasoningEffort string `json:"reasoningEffort"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			posts = append(posts, body.Model+"/"+body.ReasoningEffort)
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
	p := &acpProvider{sessions: map[string]*reactSession{}, backend: BackendCursor}
	req := NodeReq{RunID: "run", NodeID: "clarify", ChatRoute: &ChatRoute{ModelID: "new", ReasoningEffort: "high", Set: true}, AppliedRoute: &ChatRoute{ModelID: "old", ReasoningEffort: "low", Set: true}}
	p.sessions[reactKey(req)] = &reactSession{sb: &sandbox.Sandbox{Host: host, Port: port, Password: "secret"}, acp: acp}

	notice, aligned, err := p.AlignChatRoute(context.Background(), req)
	if err != nil || !aligned || notice != visitModelSwitchFailed {
		t.Fatalf("rollback notice: %q aligned=%v err=%v", notice, aligned, err)
	}
	if len(posts) != 2 || posts[0] != "new/high" || posts[1] != "old/low" {
		t.Fatalf("posts = %#v", posts)
	}
	if got := p.sessions[reactKey(req)].acp.Execution(); got == nil || got.ModelID != "old" || got.ReasoningEffort != "low" {
		t.Fatalf("execution = %+v", got)
	}
	posts = nil
	failNew = false
	selected, effort = "old", "low"
	req.ChatRoute.ModelID = "aux"
	req.ChatRoute.ReasoningEffort = ""
	notice, aligned, err = p.AlignChatRoute(context.Background(), req)
	if err != nil || notice != "" || !aligned || len(posts) != 1 || posts[0] != "aux/" {
		t.Fatalf("apply: notice=%q posts=%#v err=%v", notice, posts, err)
	}
	if got := p.sessions[reactKey(req)].acp.Execution(); got == nil || got.ModelID != "aux" {
		t.Fatalf("stamped execution = %+v", got)
	}
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
