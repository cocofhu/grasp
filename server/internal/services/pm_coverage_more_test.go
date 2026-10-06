package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"github.com/gorilla/websocket"
)

func TestPmMemoryUpdateDeleteAndPagination(t *testing.T) {
	db := setupPmDB(t)
	pm := NewPmService(db, nil)
	ps := NewProjectService(db)
	p, err := ps.Create("CRUDMem", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	item, err := pm.UpsertMemory(p.ID, "agent-a", "标题", "内容一", "admin", "alice")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := pm.UpdateMemoryForAgent(p.ID, "agent-a", item.ID, "新标题", "内容二", "bob")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "新标题" || updated.Content != "内容二" {
		t.Fatalf("updated=%+v", updated)
	}
	if _, err := pm.UpdateMemoryForAgent(p.ID, "agent-a", "missing", "x", "y", "bob"); !errors.Is(err, ErrPmMemoryNotFound) {
		t.Fatalf("update missing: %v", err)
	}
	if err := pm.DeleteMemoryForAgent(p.ID, "agent-a", item.ID); err != nil {
		t.Fatal(err)
	}
	if err := pm.DeleteMemoryForAgent(p.ID, "agent-a", item.ID); !errors.Is(err, ErrPmMemoryNotFound) {
		t.Fatalf("delete twice: %v", err)
	}

	th, err := pm.CreateThread(p.ID, "alice", "", "agent-a", "user")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := pm.AppendMessage(th.ID, "user", "msg"+string(rune('a'+i)), nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	page, total, err := pm.GetMessagesPage(th.ID, 2, 1)
	if err != nil || total != 5 || len(page) != 2 {
		t.Fatalf("page=%v total=%d err=%v", page, total, err)
	}
	page, total, err = pm.GetMessagesPage(th.ID, 0, 0)
	if err != nil || total != 5 || len(page) != 5 {
		t.Fatalf("default page=%d total=%d err=%v", len(page), total, err)
	}
}

func TestPmSearchMessagesAndFailDraft(t *testing.T) {
	db := setupPmDB(t)
	pm := NewPmService(db, nil)
	ps := NewProjectService(db)
	p, err := ps.Create("SearchMsg", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	th, err := pm.CreateThread(p.ID, "alice", "会话", "agent-a", "user")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pm.AppendMessage(th.ID, "user", "查找关键字 alpha", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.SearchMessages(p.ID, "agent-a", "alice", "", 5); err == nil {
		t.Fatal("empty query should fail")
	}
	hits, err := pm.SearchMessages(p.ID, "agent-a", "alice", "alpha", 5)
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits=%v err=%v", hits, err)
	}

	user, err := pm.AppendMessage(th.ID, "user", "draft user", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pm.UpsertDraft(th.ID, user.ID, "partial", PmDraftStreaming, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := pm.FailDraft(th.ID, PmFailConnection); err != nil {
		t.Fatal(err)
	}
	draft, err := pm.GetDraft(th.ID)
	if err != nil || draft == nil || draft.Status != PmDraftFailed || draft.FailKind != PmFailConnection {
		t.Fatalf("failed draft=%+v err=%v", draft, err)
	}
}

func TestPmTurnRunnerStartCancelAndFinish(t *testing.T) {
	db := setupPmDB(t)
	acp, port := fakeACPServer(t)
	defer acp.Close()
	ds := &dockerState{acpPort: port}
	sbx := newSandboxService(t, db, ds)
	pm := NewPmService(db, nil)
	ps := NewProjectService(db)
	p, err := ps.Create("TurnProj", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	th, err := pm.CreateThread(p.ID, "alice", "", "agentA", "user")
	if err != nil {
		t.Fatal(err)
	}
	user, err := pm.AppendMessage(th.ID, "user", "hello", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	runner := NewPmTurnRunner(pm, nil)
	if err := runner.Start(th.ID, user.ID, 1, "hi", nil); !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("nil sbx start: %v", err)
	}

	ctx := context.Background()
	row, err := sbx.Open(ctx, "agentA", nil, p.ID)
	if err != nil {
		t.Fatalf("open sandbox: %v", err)
	}
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		var r models.Sandbox
		db.First(&r, row.ID)
		if r.Status == "running" {
			row = &r
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if row.Status != "running" {
		t.Fatalf("sandbox status=%q", row.Status)
	}

	runner = NewPmTurnRunner(pm, sbx)
	if err := runner.Start(th.ID, user.ID, row.ID, "hello turn", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !runner.Active(th.ID) {
		t.Fatal("turn should be active")
	}
	if err := runner.Start(th.ID, user.ID, row.ID, "again", nil); err == nil {
		t.Fatal("duplicate start should fail")
	}

	deadline = time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if !runner.Active(th.ID) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if runner.Active(th.ID) {
		t.Fatal("turn should finish")
	}
	msgs, _ := pm.ListMessages(th.ID)
	hasAssistant := false
	for _, m := range msgs {
		if m.Role == "assistant" && strings.Contains(m.Content, "hi") {
			hasAssistant = true
		}
	}
	if !hasAssistant {
		t.Fatalf("assistant reply missing: %+v", msgs)
	}

	// Cancel path on a fresh running turn (use a server that hangs until ctx canceled).
	hang := fakeACPServerHang(t)
	defer hang.Close()
	ds2 := &dockerState{acpPort: hang.port}
	sbx2 := newSandboxService(t, db, ds2)
	row2, err := sbx2.Open(ctx, "agentA", nil, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for time.Now().Before(deadline) {
		var r models.Sandbox
		db.First(&r, row2.ID)
		if r.Status == "running" {
			row2 = &r
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	user2, _ := pm.AppendMessage(th.ID, "user", "cancel me", nil, nil, nil)
	runner2 := NewPmTurnRunner(pm, sbx2)
	if err := runner2.Start(th.ID, user2.ID, row2.ID, "block", nil); err != nil {
		t.Fatalf("start hang: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	runner2.Cancel(th.ID)
	time.Sleep(200 * time.Millisecond)
	if runner2.Active(th.ID) {
		t.Fatal("cancelled turn should not stay active")
	}
}

// fakeACPServerHang blocks chat until the client disconnects (ctx cancel).
func fakeACPServerHang(t *testing.T) (srv *httptestHang) {
	t.Helper()
	srv = newHangServer(t)
	return srv
}

type httptestHang struct {
	*httptest.Server
	port int
}

func newHangServer(t *testing.T) *httptestHang {
	t.Helper()
	up := websocket.Upgrader{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "agentchat_session", Value: "test", Path: "/"})
		w.WriteHeader(200)
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			_, msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			var m struct {
				Op string `json:"op"`
			}
			json.Unmarshal(msg, &m)
			if m.Op == "connect" {
				c.WriteJSON(map[string]any{"op": "connected"})
			}
			if m.Op == "chat" {
				time.Sleep(5 * time.Second)
			}
		}
	})
	srv := httptest.NewServer(mux)
	_, portStr, _ := strings.Cut(srv.Listener.Addr().String(), ":")
	port, _ := strconv.Atoi(portStr)
	return &httptestHang{Server: srv, port: port}
}
