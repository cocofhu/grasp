package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/provider"

	"github.com/gorilla/websocket"
)

// attach puts a live stub session on b, as if connect had succeeded.
func attach(b *Bridge, sess provider.Session) {
	b.mu.Lock()
	b.sess = sess
	b.agentCtx, b.agentCancel = context.WithCancel(context.Background())
	b.mu.Unlock()
}

func TestChatManagerDefaultChat(t *testing.T) {
	m := NewChatManager()
	def, ok := m.Get("")
	if !ok || def.ID() != DefaultChatID {
		t.Fatalf("empty id should resolve to default, got ok=%v id=%q", ok, def.ID())
	}
	if d2, _ := m.Get(DefaultChatID); d2 != def || m.Default() != def {
		t.Fatal("default lookups disagree")
	}
	if _, ok := m.Get("nope"); ok {
		t.Fatal("unknown chat should not resolve")
	}
	list := m.List()
	if len(list) != 1 || list[0].ID != DefaultChatID || list[0].Connected {
		t.Fatalf("list = %+v", list)
	}
	if err := m.Delete(DefaultChatID); !errors.Is(err, ErrDefaultChatDel) {
		t.Fatalf("delete default err = %v", err)
	}
}

func TestChatManagerCreateRenameDeleteAndCap(t *testing.T) {
	t.Setenv("SANDBOX_MAX_CHATS", "3")
	m := NewChatManager()
	if m.MaxChats() != 3 {
		t.Fatalf("max = %d", m.MaxChats())
	}
	a, err := m.Create("  first  ", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Create("", "gpt-5")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID() == b.ID() || a.ID() == DefaultChatID {
		t.Fatalf("ids not unique: %q %q", a.ID(), b.ID())
	}
	if _, err := m.Create("x", ""); !errors.Is(err, ErrTooManyChats) {
		t.Fatalf("over cap err = %v", err)
	}

	list := m.List()
	if len(list) != 3 || list[0].ID != DefaultChatID || list[1].ID != a.ID() || list[2].ID != b.ID() {
		t.Fatalf("order = %+v", list)
	}
	if list[1].Title != "first" || list[2].Model != "gpt-5" {
		t.Fatalf("meta = %+v", list)
	}

	if _, err := m.Rename(a.ID(), "renamed"); err != nil {
		t.Fatal(err)
	}
	if got := m.Info(a).Title; got != "renamed" {
		t.Fatalf("title = %q", got)
	}
	if _, err := m.Rename("nope", "x"); !errors.Is(err, ErrChatNotFound) {
		t.Fatalf("rename unknown err = %v", err)
	}

	sess := &blockingSess{stubSess: stubSess{id: "s-a"}}
	attach(a, sess)
	c := dialBridge(t, a)
	if err := a.ChatWithOpID("hi", "op-1", "chat", nil); err != nil {
		t.Fatal(err)
	}
	readUntil(t, c, func(f wsFrame) bool { return f.Op == "event" && f.OpID == "op-1" })
	if !m.Info(a).Busy {
		t.Fatal("chat a should be busy")
	}

	if err := m.Delete(a.ID()); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Get(a.ID()); ok {
		t.Fatal("deleted chat still resolvable")
	}
	if a.Session() != nil {
		t.Fatal("deleted chat still holds a session")
	}
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			break
		}
	}
	if err := m.Delete(a.ID()); !errors.Is(err, ErrChatNotFound) {
		t.Fatalf("double delete err = %v", err)
	}
	if _, err := m.Create("after delete", ""); err != nil {
		t.Fatalf("create after delete should fit under cap: %v", err)
	}
}

// A handler that resolved the chat just before Delete must not revive it.
func TestDeletedChatRejectsClientsAndConnect(t *testing.T) {
	m := NewChatManager()
	a, err := m.Create("", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(a.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Connect(t.TempDir(), "", nil, nil); !errors.Is(err, ErrChatNotFound) {
		t.Fatalf("connect on deleted chat err = %v", err)
	}
	if a.Session() != nil {
		t.Fatal("deleted chat got a session")
	}

	registered := make(chan bool, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		registered <- a.RegisterClient(c)
	}))
	defer srv.Close()
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if <-registered {
		t.Fatal("deleted chat accepted a client")
	}
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := c.ReadMessage(); err == nil {
		t.Fatal("connection to deleted chat should be closed")
	}
}

func TestMaxChatsFromEnvInvalid(t *testing.T) {
	for _, v := range []string{"0", "-2", "abc"} {
		t.Setenv("SANDBOX_MAX_CHATS", v)
		if got := maxChatsFromEnv(); got != DefaultMaxChats {
			t.Fatalf("SANDBOX_MAX_CHATS=%q -> %d", v, got)
		}
	}
}

func TestChatsAreIsolated(t *testing.T) {
	m := NewChatManager()
	def := m.Default()
	other, err := m.Create("", "")
	if err != nil {
		t.Fatal(err)
	}
	sDef := &blockingSess{stubSess: stubSess{id: "s-def"}}
	sOther := &blockingSess{stubSess: stubSess{id: "s-other"}}
	attach(def, sDef)
	attach(other, sOther)
	cDef := dialBridge(t, def)
	cOther := dialBridge(t, other)

	if err := def.ChatWithOpID("to default", "op-def", "chat", nil); err != nil {
		t.Fatal(err)
	}
	readUntil(t, cDef, func(f wsFrame) bool { return f.Op == "event" && f.OpID == "op-def" })

	if err := other.ChatWithOpID("to other", "op-other", "chat", nil); err != nil {
		t.Fatal(err)
	}
	// Other's client sees its own turn begin; it must never see op-def frames.
	readUntil(t, cOther, func(f wsFrame) bool {
		if f.OpID == "op-def" {
			t.Fatal("default chat frame leaked into other chat")
		}
		return f.Op == "event" && f.OpID == "op-other"
	})

	// prompt_begin is broadcast before the turn goroutine calls Prompt.
	deadline := time.Now().Add(2 * time.Second)
	for sDef.prompts.Load() != 1 || sOther.prompts.Load() != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("both chats should run concurrently: def=%d other=%d", sDef.prompts.Load(), sOther.prompts.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if def.activeOpID() != "op-def" || other.activeOpID() != "op-other" {
		t.Fatalf("active turns: def=%q other=%q", def.activeOpID(), other.activeOpID())
	}

	other.CancelPrompt()
	if def.activeOpID() != "op-def" {
		t.Fatal("cancelling other chat must not touch default chat")
	}
	def.CancelPrompt()
}

func TestEffectiveModelPrecedence(t *testing.T) {
	standalone := NewBridge()
	if got := standalone.EffectiveModel(); got != "" {
		t.Fatalf("standalone default = %q", got)
	}

	m := NewChatManager()
	tab, err := m.Create("", "")
	if err != nil {
		t.Fatal(err)
	}
	// No env / -model: auto (empty).
	if got := tab.EffectiveModel(); got != "" {
		t.Fatalf("no default: %q", got)
	}
	// ACP_BRIDGE_MODEL / -model becomes the default for tabs that did not pick one.
	m.SetDefaultModel(" env-model ")
	if got := tab.EffectiveModel(); got != "env-model" {
		t.Fatalf("inherit default: %q", got)
	}
	if got := m.Default().EffectiveModel(); got != "env-model" {
		t.Fatalf("default chat inherit: %q", got)
	}
	// Explicit tab choice overrides the env default.
	tab.SetModel("tab-model")
	if got := tab.EffectiveModel(); got != "tab-model" {
		t.Fatalf("override: %q", got)
	}
	// Empty string goes back to following the default.
	tab.SetModel("")
	if got := tab.EffectiveModel(); got != "env-model" || tab.Model() != "" {
		t.Fatalf("reset: effective=%q selected=%q", got, tab.Model())
	}
}

func TestConnectedPayloadReportsChatAndModel(t *testing.T) {
	m := NewChatManager()
	tab, _ := m.Create("", "")
	sess := &stubSess{id: "s-tab"}
	attach(tab, sess)
	p := tab.ConnectedPayload(sess)
	if p["chatId"] != tab.ID() || p["currentModel"] != "auto" {
		t.Fatalf("payload chatId=%v currentModel=%v", p["chatId"], p["currentModel"])
	}
	tab.SetModel("tab-model")
	if p := tab.ConnectedPayload(sess); p["currentModel"] != "tab-model" {
		t.Fatalf("currentModel = %v", p["currentModel"])
	}
}

func TestRestartOnlyRestartsThatChat(t *testing.T) {
	m := NewChatManager()
	m.SetDefaultModel("env-model")
	def := m.Default()
	tab, _ := m.Create("", "")
	attach(def, &stubSess{id: "s-def"})
	attach(tab, &stubSess{id: "s-tab"})

	var defRestarts atomic.Int32
	def.testConnect = func(string, string, json.RawMessage, *bool) (provider.Session, error) {
		defRestarts.Add(1)
		return nil, errors.New("default must not restart")
	}
	var gotModel string
	tab.testConnect = func(string, string, json.RawMessage, *bool) (provider.Session, error) {
		gotModel = tab.EffectiveModel()
		ns := &stubSess{id: "s-tab-2"}
		attach(tab, ns)
		return ns, nil
	}

	tab.SetModel("tab-model")
	sess, err := tab.RestartAgent()
	if err != nil {
		t.Fatal(err)
	}
	if sess.SessionID() != "s-tab-2" || gotModel != "tab-model" {
		t.Fatalf("restart sid=%q model=%q", sess.SessionID(), gotModel)
	}
	if defRestarts.Load() != 0 || def.Session().SessionID() != "s-def" {
		t.Fatal("default chat was restarted")
	}
}

func TestInheritFromDefault(t *testing.T) {
	m := NewChatManager()
	def := m.Default()
	tab, _ := m.Create("", "")

	// Default not connected yet: nothing to inherit.
	cwd, fs, mcp := tab.inheritFromDefault("", "", nil)
	if cwd != "" || fs != "" || mcp != nil {
		t.Fatalf("got %q %q %s", cwd, fs, mcp)
	}

	attach(def, &stubSess{id: "s-def"})
	def.mu.Lock()
	def.lastMCP = json.RawMessage(`[{"name":"artifact-store"}]`)
	def.mu.Unlock()

	cwd, fs, mcp = tab.inheritFromDefault("", "", json.RawMessage(`null`))
	if cwd != "/tmp" || fs != "/tmp" || string(mcp) != `[{"name":"artifact-store"}]` {
		t.Fatalf("inherit got %q %q %s", cwd, fs, mcp)
	}
	// Explicit values win.
	cwd, _, mcp = tab.inheritFromDefault("/work", "", json.RawMessage(`[{"name":"x"}]`))
	if cwd != "/work" || string(mcp) != `[{"name":"x"}]` {
		t.Fatalf("explicit got %q %s", cwd, mcp)
	}
	// The default chat never inherits from itself.
	cwd, _, mcp = def.inheritFromDefault("", "", nil)
	if cwd != "" || mcp != nil {
		t.Fatalf("default inherit got %q %s", cwd, mcp)
	}
}
