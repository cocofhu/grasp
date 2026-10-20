package schedulermcp

import (
	"encoding/json"
	"github.com/cocofhu/grasp/internal/platformmcp"
	"strings"
	"testing"
)

func TestSchedulerMCPSessionAndMetaRPC(t *testing.T) {
	db, pm, p := setupSchedDB(t)
	h := NewHost(db, pm)
	tok := platformmcp.NewToken()
	h.Restore(tok, "agent-a", "thr", "u", true)
	h.Restore(tok, "agent-a", "thr", "u", true)
	if _, ok := h.authorize("agent-a", tok); !ok {
		t.Fatal("authorize restored")
	}
	h.Unregister(tok)
	if _, ok := h.authorize("agent-a", tok); ok {
		t.Fatal("unregistered")
	}
	tok = platformmcp.NewToken()
	h.Restore(tok, "agent-a", "thr", "u", true)

	st, _ := h.ServeRPC("agent-b", tok, []byte(`{}`))
	if st != 401 {
		t.Fatalf("wrong agent: %d", st)
	}
	st, _ = h.ServeRPC("agent-a", tok, []byte(`{`))
	if st != 400 {
		t.Fatalf("parse: %d", st)
	}

	initBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
	})
	st, resp := h.ServeRPC("agent-a", tok, initBody)
	if st != 200 || !strings.Contains(string(resp), "task-scheduler") {
		t.Fatalf("init: %d %s", st, resp)
	}
	st, resp = h.ServeRPC("agent-a", tok, []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`))
	if st != 200 || !strings.Contains(string(resp), "list_jobs") {
		t.Fatalf("tools/list: %d %s", st, resp)
	}
	st, resp = h.ServeRPC("agent-a", tok, []byte(`{"jsonrpc":"2.0","id":3,"method":"ping"}`))
	if st != 200 {
		t.Fatalf("ping: %d %s", st, resp)
	}
	_ = p
}
