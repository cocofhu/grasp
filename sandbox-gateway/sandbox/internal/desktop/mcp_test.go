package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func newMCP(t *testing.T, desktop bool) (*MCPServer, *fakeRunner, *ControlStore) {
	t.Helper()
	p := testPaths(t, desktop)
	fr := &fakeRunner{out: map[string][]byte{}, fail: map[string]error{}}
	ctl := NewControlStore(p, nil)
	return &MCPServer{Paths: p, Control: ctl, Runner: fr, LookPath: lookAll}, fr, ctl
}

func callTool(s *MCPServer, name, args string) toolResult {
	return s.Call(context.Background(), name, json.RawMessage(args))
}

func text(r toolResult) string {
	var b strings.Builder
	for _, c := range r.Content {
		if s, ok := c["text"].(string); ok {
			b.WriteString(s)
		}
	}
	return b.String()
}

func TestMCPServeProtocol(t *testing.T) {
	s, _, _ := newMCP(t, true)
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		``,
		`not json`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"list_apps","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":"bad"}`,
		`{"jsonrpc":"2.0","id":6,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":7,"method":"initialize"}`,
	}, "\n")
	var out bytes.Buffer
	if err := s.Serve(context.Background(), strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 8 {
		t.Fatalf("want 8 responses (no reply to notifications), got %d: %s", len(lines), out.String())
	}
	var init rpcResponse
	_ = json.Unmarshal([]byte(lines[0]), &init)
	res := init.Result.(map[string]any)
	if res["protocolVersion"] != "2025-03-26" || !strings.Contains(res["instructions"].(string), "命令行优先") {
		t.Fatalf("initialize = %v", res)
	}
	if !strings.Contains(lines[1], `"code":-32700`) {
		t.Fatalf("parse error: %s", lines[1])
	}
	var list struct {
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
	}
	_ = json.Unmarshal([]byte(lines[2]), &list)
	if len(list.Result.Tools) != 10 {
		t.Fatalf("tools = %d", len(list.Result.Tools))
	}
	if !strings.Contains(lines[4], "blender -b") {
		t.Fatalf("list_apps: %s", lines[4])
	}
	if !strings.Contains(lines[5], `"code":-32602`) || !strings.Contains(lines[6], `"code":-32601`) {
		t.Fatalf("errors: %s / %s", lines[5], lines[6])
	}
	if !strings.Contains(lines[7], "2025-06-18") {
		t.Fatalf("default protocol: %s", lines[7])
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestMCPServeWriteErrors(t *testing.T) {
	s, _, _ := newMCP(t, true)
	if err := s.Serve(context.Background(), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`+"\n"), failWriter{}); err == nil {
		t.Fatal("write failure must surface")
	}
	if err := s.Serve(context.Background(), strings.NewReader("garbage\n"), failWriter{}); err == nil {
		t.Fatal("write failure on parse error must surface")
	}
}

func TestMCPNoDesktopImage(t *testing.T) {
	s, fr, _ := newMCP(t, false)
	for _, name := range []string{"screenshot", "click", "list_apps"} {
		r := callTool(s, name, `{"x":1,"y":1}`)
		if !r.IsError || text(r) != MsgNoDesktop {
			t.Fatalf("%s: %+v", name, r)
		}
	}
	if len(fr.calls) != 0 {
		t.Fatalf("no X calls expected: %+v", fr.calls)
	}
}

func TestMCPSoftInterlock(t *testing.T) {
	s, fr, ctl := newMCP(t, true)
	fr.out["xdotool getdisplaygeometry"] = []byte("1920 1080\n")
	fr.out["import -silent"] = []byte("PNG")
	fr.out["wmctrl -lG"] = []byte("0x01 0 10 20 300 200 host Mousepad\n")
	if _, err := ctl.Set(HolderUser, "viewer"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, args string }{
		{"click", `{"x":10,"y":10}`},
		{"drag", `{"from_x":1,"from_y":1,"to_x":2,"to_y":2}`},
		{"type_text", `{"text":"你好"}`},
		{"key", `{"keys":"ctrl+s"}`},
		{"scroll", `{"x":1,"y":1,"direction":"down"}`},
		{"focus_window", `{"title":"Mousepad"}`},
		{"launch_app", `{"id":"mousepad"}`},
	} {
		r := callTool(s, tc.name, tc.args)
		if !r.IsError || text(r) != MsgUserControl {
			t.Fatalf("%s must be refused while the user holds control: %+v", tc.name, r)
		}
	}
	if len(fr.started) != 0 {
		t.Fatalf("nothing may start: %+v", fr.started)
	}
	for _, name := range []string{"screenshot", "list_windows", "list_apps"} {
		if r := callTool(s, name, `{}`); r.IsError {
			t.Fatalf("%s must keep working: %+v", name, r)
		}
	}
	if _, err := ctl.Set(HolderAgent, "viewer"); err != nil {
		t.Fatal(err)
	}
	if r := callTool(s, "click", `{"x":10,"y":10}`); r.IsError {
		t.Fatalf("click after return: %+v", r)
	}

	expired := NewControlStore(s.Paths, func() time.Time { return time.Now().Add(-time.Hour) })
	if _, err := expired.Set(HolderUser, "gone"); err != nil {
		t.Fatal(err)
	}
	if r := callTool(s, "click", `{"x":10,"y":10}`); r.IsError {
		t.Fatalf("an expired hold must not block: %+v", r)
	}
}

func TestMCPToolsDriveX(t *testing.T) {
	s, fr, _ := newMCP(t, true)
	fr.out["xdotool getdisplaygeometry"] = []byte("1280 800")
	fr.out["import -silent"] = []byte("PNGDATA")
	fr.out["wmctrl -lG"] = []byte("0x03a00007  0 0 0 1920 1040 host 文本编辑器 - a.txt\nbad line\n0x02 x 0 0 1 1 h t\n0x04 0 1 2 3 4 host\n")

	r := callTool(s, "screenshot", `null`)
	if r.IsError || r.Content[0]["type"] != "image" || r.Content[0]["data"] != "UE5HREFUQQ==" || !strings.Contains(text(r), "1280x800") {
		t.Fatalf("screenshot: %+v", r)
	}
	r = callTool(s, "list_windows", ``)
	var wins []Window
	if err := json.Unmarshal([]byte(text(r)), &wins); err != nil || len(wins) != 2 || wins[0].Title != "文本编辑器 - a.txt" || wins[0].Height != 1040 || wins[1].Title != "" {
		t.Fatalf("windows: %s %v", text(r), err)
	}

	ok := []struct{ name, args, want string }{
		{"click", `{"x":100,"y":200}`, "mousemove --sync 100 200 click 1"},
		{"click", `{"x":1,"y":2,"button":"right","double":true}`, "mousemove --sync 1 2 click --repeat 2 --delay 120 3"},
		{"drag", `{"from_x":1,"from_y":2,"to_x":3,"to_y":4}`, "mousemove --sync 1 2 mousedown 1 mousemove --sync 3 4 mouseup 1"},
		{"type_text", `{"text":"你好 world"}`, "type --delay 12 -- 你好 world"},
		{"key", `{"keys":" ctrl+s Return "}`, "key -- ctrl+s Return"},
		{"scroll", `{"x":5,"y":6,"direction":"up","amount":2}`, "mousemove --sync 5 6 click --repeat 2 4"},
		{"scroll", `{"x":5,"y":6,"direction":"right"}`, "mousemove --sync 5 6 click --repeat 3 7"},
		{"focus_window", `{"window_id":"0x03a00007"}`, "-i -a 0x03a00007"},
		{"focus_window", `{"title":"Mousepad"}`, "-a Mousepad"},
	}
	for _, tc := range ok {
		fr.calls = nil
		r := callTool(s, tc.name, tc.args)
		if r.IsError {
			t.Fatalf("%s %s: %+v", tc.name, tc.args, r)
		}
		got := strings.Join(fr.calls[len(fr.calls)-1].args, " ")
		if got != tc.want {
			t.Fatalf("%s: args %q want %q", tc.name, got, tc.want)
		}
	}
	r = callTool(s, "launch_app", `{"id":"blender"}`)
	if r.IsError || len(fr.started) != 1 || fr.started[0].name != "blender" {
		t.Fatalf("launch: %+v %+v", r, fr.started)
	}
}

func TestMCPArgumentErrors(t *testing.T) {
	s, fr, _ := newMCP(t, true)
	bad := []struct{ name, args, want string }{
		{"nope", `{}`, "未知工具"},
		{"click", `[1]`, "JSON 对象"},
		{"click", `{"y":1}`, "缺少参数 x"},
		{"click", `{"x":1}`, "缺少参数 y"},
		{"click", `{"x":1.5,"y":1}`, "必须是整数"},
		{"click", `{"x":-1,"y":1}`, "超出范围"},
		{"click", `{"x":1,"y":1,"button":"side"}`, "button"},
		{"click", `{"x":1,"y":1,"button":3}`, "必须是字符串"},
		{"click", `{"x":1,"y":1,"double":"yes"}`, "布尔"},
		{"drag", `{"from_x":1,"from_y":1,"to_x":1}`, "缺少参数 to_y"},
		{"type_text", `{}`, "缺少参数 text"},
		{"type_text", `{"text":1}`, "必须是字符串"},
		{"key", `{"keys":"ctrl+s; rm -rf /"}`, "keys 无效"},
		{"key", `{"keys":1}`, "必须是字符串"},
		{"scroll", `{"x":1,"y":1,"direction":"in"}`, "direction"},
		{"scroll", `{"x":1,"y":1,"direction":"up","amount":0}`, "1–20"},
		{"scroll", `{"x":1,"y":1,"direction":"up","amount":"a"}`, "整数"},
		{"scroll", `{"y":1,"direction":"up"}`, "缺少参数 x"},
		{"scroll", `{"x":1,"direction":"up"}`, "缺少参数 y"},
		{"scroll", `{"x":1,"y":1,"direction":2}`, "必须是字符串"},
		{"focus_window", `{}`, "window_id 或 title"},
		{"focus_window", `{"window_id":"1; reboot"}`, "window_id 无效"},
		{"focus_window", `{"window_id":1}`, "必须是字符串"},
		{"focus_window", `{"title":1}`, "必须是字符串"},
		{"launch_app", `{"id":"rm"}`, "未安装的应用"},
		{"launch_app", `{"id":1}`, "必须是字符串"},
	}
	for _, tc := range bad {
		r := callTool(s, tc.name, tc.args)
		if !r.IsError || !strings.Contains(text(r), tc.want) {
			t.Fatalf("%s %s: %q want %q", tc.name, tc.args, text(r), tc.want)
		}
	}
	if len(fr.started) != 0 {
		t.Fatalf("nothing may start: %+v", fr.started)
	}
}

func TestMCPCommandFailures(t *testing.T) {
	s, fr, _ := newMCP(t, true)
	fail := errors.New("exit 1")
	for _, k := range []string{"xdotool mousemove", "xdotool type", "xdotool key", "import -silent", "wmctrl -lG", "wmctrl -i", "wmctrl -a", "xdotool getdisplaygeometry"} {
		fr.fail[k] = fail
	}
	fr.fail["start blender"] = fail
	for _, tc := range []struct{ name, args, want string }{
		{"click", `{"x":1,"y":1}`, "xdotool 失败"},
		{"drag", `{"from_x":1,"from_y":1,"to_x":2,"to_y":2}`, "xdotool 失败"},
		{"type_text", `{"text":"a"}`, "xdotool 失败"},
		{"key", `{"keys":"a"}`, "xdotool 失败"},
		{"scroll", `{"x":1,"y":1,"direction":"up"}`, "xdotool 失败"},
		{"screenshot", `{}`, "截图失败"},
		{"list_windows", `{}`, "列出窗口失败"},
		{"focus_window", `{"window_id":"0x1"}`, "没有找到窗口"},
		{"focus_window", `{"title":"x"}`, "没有找到标题"},
		{"launch_app", `{"id":"blender"}`, "启动 Blender 失败"},
	} {
		r := callTool(s, tc.name, tc.args)
		if !r.IsError || !strings.Contains(text(r), tc.want) {
			t.Fatalf("%s: %q want %q", tc.name, text(r), tc.want)
		}
	}
	delete(fr.fail, "import -silent")
	fr.out["import -silent"] = []byte("P")
	if r := callTool(s, "screenshot", `{}`); r.IsError || !strings.Contains(text(r), "1920x1080") {
		t.Fatalf("geometry fallback: %+v", r)
	}
	delete(fr.fail, "xdotool getdisplaygeometry")
	fr.out["xdotool getdisplaygeometry"] = []byte("a b")
	if r := callTool(s, "screenshot", `{}`); !strings.Contains(text(r), "1920x1080") {
		t.Fatalf("unparsable geometry: %+v", r)
	}
}
