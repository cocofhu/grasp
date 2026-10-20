package memorymcp

import (
	"encoding/json"
	"github.com/cocofhu/grasp/internal/platformmcp"
	"strings"
	"testing"
)

func TestMemoryMCPServeRPCMetaMethods(t *testing.T) {
	_, pm, _ := setupMemoryDB(t)
	h := NewHost(pm)
	tok := platformmcp.NewToken()
	h.Restore(tok, "agent-a", "thr", "u", true)
	st, _ := h.ServeRPC("bad", []byte(`{}`))
	if st != 401 {
		t.Fatalf("unauth: %d", st)
	}
	st, _ = h.ServeRPC(tok, []byte(`{`))
	if st != 400 {
		t.Fatalf("parse: %d", st)
	}

	initBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2025-01-01"},
	})
	st, resp := h.ServeRPC(tok, initBody)
	if st != 200 || !strings.Contains(string(resp), "2025-01-01") {
		t.Fatalf("init: %d %s", st, resp)
	}

	st, resp = h.ServeRPC(tok, []byte(`{"jsonrpc":"2.0","id":2,"method":"ping"}`))
	if st != 200 {
		t.Fatalf("ping: %d %s", st, resp)
	}
	st, resp = h.ServeRPC(tok, []byte(`{"jsonrpc":"2.0","method":"notifications/cancelled"}`))
	if st != 202 {
		t.Fatalf("notification: %d", st)
	}
	st, resp = h.ServeRPC(tok, []byte(`{"jsonrpc":"2.0","id":3,"method":"bogus"}`))
	if st != 200 || !strings.Contains(string(resp), "method not found") {
		t.Fatalf("bogus: %d %s", st, resp)
	}
	st, resp = h.ServeRPC(tok, []byte(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":"x"}`))
	if st != 200 || !strings.Contains(string(resp), "invalid tools/call params") {
		t.Fatalf("bad call params: %d %s", st, resp)
	}
}
