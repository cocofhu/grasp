package platformmcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRPCHelpers(t *testing.T) {
	req := RPCRequest{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "x"}
	st, body := Ok(req, map[string]any{"ok": true})
	if st != 200 || !strings.Contains(string(body), `"ok"`) {
		t.Fatalf("Ok: %d %s", st, body)
	}
	st, body = Fail(req, -1, "nope")
	if st != 200 || !strings.Contains(string(body), "nope") {
		t.Fatalf("Fail: %s", body)
	}
	st, body = Unauthorized()
	if st != 401 {
		t.Fatalf("Unauthorized: %d", st)
	}
	st, body = ParseError()
	if st != 400 {
		t.Fatalf("ParseError: %d", st)
	}

	if MustJSON(map[string]any{"a": 1}) == nil {
		t.Fatal("MustJSON")
	}
	if MustString("hi") != "hi" || !strings.Contains(MustString(map[string]any{"k": "v"}), "k") {
		t.Fatal("MustString")
	}
	tool := Tool("t", "desc", map[string]any{"x": map[string]any{"type": "string"}})
	if tool["name"] != "t" {
		t.Fatalf("Tool=%v", tool)
	}
	res := ToolResult("out", true)
	if res["isError"] != true {
		t.Fatalf("ToolResult=%v", res)
	}

	args := map[string]any{"s": "v", "n": float64(3), "b": true, "m": map[string]any{"k": "v"}}
	if StrArg(args, "s") != "v" || StrArg(nil, "s") != "" {
		t.Fatal("StrArg")
	}
	if IntArg(args, "n", 0) != 3 || IntArg(args, "missing", 7) != 7 {
		t.Fatal("IntArg")
	}
	if !BoolArg(args, "b") || BoolArg(args, "missing") {
		t.Fatal("BoolArg")
	}
	if MapArg(args, "m")["k"] != "v" || MapArg(args, "missing") != nil {
		t.Fatal("MapArg")
	}
	tok := NewToken()
	if len(tok) != 32 {
		t.Fatalf("NewToken len=%d", len(tok))
	}
	if !IsNotification(RPCRequest{}) || IsNotification(req) {
		t.Fatal("IsNotification")
	}
}

func TestServeTools(t *testing.T) {
	var called []string
	tools := func() []map[string]any { return []map[string]any{Tool("echo", "d", nil)} }
	call := func(name string, args map[string]any) (any, bool) {
		called = append(called, name)
		return map[string]any{"got": args["x"]}, name == "bad"
	}
	serve := func(method, id, params string) (int, string) {
		req := RPCRequest{JSONRPC: "2.0", Method: method, Params: json.RawMessage(params)}
		if id != "" {
			req.ID = json.RawMessage(id)
		}
		st, body := ServeTools(req, "demo-store", tools, call)
		return st, string(body)
	}

	st, body := serve("initialize", "1", `{"protocolVersion":"2025-03-26"}`)
	if st != 200 || !strings.Contains(body, `"protocolVersion":"2025-03-26"`) || !strings.Contains(body, `"name":"demo-store"`) || strings.Contains(body, "listChanged") {
		t.Fatalf("initialize echo: %d %s", st, body)
	}
	if _, body = serve("initialize", "1", `{}`); !strings.Contains(body, `"protocolVersion":"`+ProtocolVersion+`"`) {
		t.Fatalf("initialize default: %s", body)
	}
	for _, m := range []string{"notifications/initialized", "notifications/cancelled"} {
		if st, body = serve(m, "", ``); st != 202 || body != "" {
			t.Fatalf("%s: %d %q", m, st, body)
		}
	}
	if st, body = serve("ping", "2", ``); st != 200 || !strings.Contains(body, `"result":{}`) {
		t.Fatalf("ping: %d %s", st, body)
	}
	if _, body = serve("tools/list", "3", ``); !strings.Contains(body, `"name":"echo"`) {
		t.Fatalf("tools/list: %s", body)
	}
	if _, body = serve("tools/call", "4", `{"name":"echo","arguments":{"x":"v"}}`); !strings.Contains(body, `"isError":false`) || !strings.Contains(body, `\"got\": \"v\"`) {
		t.Fatalf("tools/call: %s", body)
	}
	if _, body = serve("tools/call", "5", `{"name":"bad"}`); !strings.Contains(body, `"isError":true`) {
		t.Fatalf("tools/call error: %s", body)
	}
	if _, body = serve("tools/call", "6", `"nope"`); !strings.Contains(body, `-32602`) {
		t.Fatalf("tools/call params: %s", body)
	}
	if len(called) != 2 || called[0] != "echo" || called[1] != "bad" {
		t.Fatalf("called=%v", called)
	}
	if st, body = serve("notifications/other", "", ``); st != 202 || body != "" {
		t.Fatalf("unknown notification: %d %q", st, body)
	}
	if st, body = serve("nope", "7", ``); st != 200 || !strings.Contains(body, `-32601`) || !strings.Contains(body, "method not found: nope") {
		t.Fatalf("unknown method: %d %s", st, body)
	}
}
