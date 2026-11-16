package desktop

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Messages returned to the agent.
const (
	MsgNoDesktop   = "当前镜像没有桌面（缺少 /etc/grasp-desktop）。请改用桌面镜像，或用命令行与浏览器 MCP 完成任务。"
	MsgUserControl = "用户正在控制桌面，键鼠操作已被拒绝。请稍后重试；截图与列表类工具仍可使用。"
)

const mcpInstructions = `Grasp 云电脑（desktop MCP）。按以下优先顺序完成任务：
1. 命令行优先：先 list_apps，应用有 cli 字段时直接在终端用命令行（如 blender -b、inkscape --export-filename、kicad-cli）完成；
2. 有专属 MCP 的应用（Blender MCP）用它操作运行中的应用；
3. 只有以上都不行时才用本 MCP 的键鼠工具：先 screenshot 看清坐标（单位为屏幕像素），再 click / type_text / key，操作后再截图确认。
用户在 Grasp 中接管电脑时，键鼠类工具会返回「用户正在控制」，请等待后重试；screenshot、list_windows、list_apps 始终可用。`

// MCPServer is the desktop MCP (stdio, newline-delimited JSON-RPC 2.0).
type MCPServer struct {
	Paths    Paths
	Control  *ControlStore
	Runner   Runner
	LookPath func(string) (string, error)
	// Timeout bounds one X11 helper command (default 20s).
	Timeout time.Duration
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// toolResult is the MCP tools/call result.
type toolResult struct {
	Content []map[string]any `json:"content"`
	IsError bool             `json:"isError,omitempty"`
}

func textResult(s string) toolResult {
	return toolResult{Content: []map[string]any{{"type": "text", "text": s}}}
}

func errResult(s string) toolResult {
	r := textResult(s)
	r.IsError = true
	return r
}

// Serve reads requests from in until EOF and writes responses to out.
func (s *MCPServer) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	enc := json.NewEncoder(out)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			if err := enc.Encode(rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: -32700, Message: "parse error"}}); err != nil {
				return err
			}
			continue
		}
		resp, ok := s.handle(ctx, req)
		if !ok {
			continue
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

func (s *MCPServer) handle(ctx context.Context, req rpcRequest) (rpcResponse, bool) {
	if len(req.ID) == 0 {
		return rpcResponse{}, false
	}
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		version := p.ProtocolVersion
		if version == "" {
			version = "2025-06-18"
		}
		resp.Result = map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "grasp-desktop", "version": "1.0.0"},
			"instructions":    mcpInstructions,
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": toolDefs()}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: "invalid params"}
			break
		}
		resp.Result = s.Call(ctx, p.Name, p.Arguments)
	default:
		resp.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
	return resp, true
}

// mutating tools drive the keyboard / mouse and are refused while a user holds control.
var mutating = map[string]bool{
	"click": true, "drag": true, "type_text": true, "key": true,
	"scroll": true, "focus_window": true, "launch_app": true,
}

func obj(props map[string]any, required ...string) map[string]any {
	o := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		o["required"] = required
	}
	return o
}

func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "minimum": 0, "description": desc}
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func toolDefs() []map[string]any {
	tool := func(name, desc string, schema map[string]any) map[string]any {
		return map[string]any{"name": name, "description": desc, "inputSchema": schema}
	}
	return []map[string]any{
		tool("screenshot", "截取整个桌面（PNG）。返回图像与屏幕尺寸，点击坐标使用同一像素坐标。", obj(map[string]any{})),
		tool("list_windows", "列出桌面窗口：窗口 id、位置、尺寸与标题。", obj(map[string]any{})),
		tool("list_apps", "列出已安装的桌面应用及其命令行用法（cli）。优先用命令行完成任务。", obj(map[string]any{})),
		tool("click", "在屏幕坐标处点击。用户接管时拒绝。", obj(map[string]any{
			"x": intProp("屏幕 x 像素"), "y": intProp("屏幕 y 像素"),
			"button": map[string]any{"type": "string", "enum": []string{"left", "middle", "right"}},
			"double": map[string]any{"type": "boolean"},
		}, "x", "y")),
		tool("drag", "按住左键从一点拖到另一点。用户接管时拒绝。", obj(map[string]any{
			"from_x": intProp("起点 x"), "from_y": intProp("起点 y"), "to_x": intProp("终点 x"), "to_y": intProp("终点 y"),
		}, "from_x", "from_y", "to_x", "to_y")),
		tool("type_text", "向当前焦点输入文本（支持中文）。用户接管时拒绝。", obj(map[string]any{"text": strProp("要输入的文本")}, "text")),
		tool("key", "按键或组合键，如 ctrl+s、Return、alt+F4；多个用空格分隔。用户接管时拒绝。", obj(map[string]any{"keys": strProp("xdotool 键名")}, "keys")),
		tool("scroll", "在屏幕坐标处滚动。用户接管时拒绝。", obj(map[string]any{
			"x": intProp("屏幕 x"), "y": intProp("屏幕 y"),
			"direction": map[string]any{"type": "string", "enum": []string{"up", "down", "left", "right"}},
			"amount":    map[string]any{"type": "integer", "minimum": 1, "maximum": 20},
		}, "x", "y", "direction")),
		tool("focus_window", "激活窗口（按 list_windows 的 window_id 或标题片段）。用户接管时拒绝。", obj(map[string]any{
			"window_id": strProp("如 0x03a00007"), "title": strProp("窗口标题片段"),
		})),
		tool("launch_app", "按 list_apps 的 id 启动应用。用户接管时拒绝。", obj(map[string]any{"id": strProp("应用 id")}, "id")),
	}
}

const maxCoord = 16384

var (
	keysRe     = regexp.MustCompile(`^[A-Za-z0-9_+\-]+( [A-Za-z0-9_+\-]+)*$`)
	windowIDRe = regexp.MustCompile(`^0x[0-9a-fA-F]{1,10}$`)
)

type toolArgs map[string]json.RawMessage

func (a toolArgs) intArg(name string, required bool, def int) (int, error) {
	raw, ok := a[name]
	if !ok {
		if required {
			return 0, fmt.Errorf("缺少参数 %s", name)
		}
		return def, nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil || f != float64(int(f)) {
		return 0, fmt.Errorf("参数 %s 必须是整数", name)
	}
	n := int(f)
	if n < 0 || n > maxCoord {
		return 0, fmt.Errorf("参数 %s 超出范围 0–%d", name, maxCoord)
	}
	return n, nil
}

func (a toolArgs) strArg(name string) (string, bool, error) {
	raw, ok := a[name]
	if !ok {
		return "", false, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", true, fmt.Errorf("参数 %s 必须是字符串", name)
	}
	return s, true, nil
}

func (a toolArgs) boolArg(name string) (bool, error) {
	raw, ok := a[name]
	if !ok {
		return false, nil
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return false, fmt.Errorf("参数 %s 必须是布尔值", name)
	}
	return b, nil
}

// Call runs one tool. Errors are tool results (isError), never RPC errors.
func (s *MCPServer) Call(ctx context.Context, name string, rawArgs json.RawMessage) toolResult {
	known := false
	for _, d := range toolDefs() {
		if d["name"] == name {
			known = true
			break
		}
	}
	if !known {
		return errResult("未知工具：" + name)
	}
	args := toolArgs{}
	if len(rawArgs) > 0 && string(rawArgs) != "null" {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return errResult("参数必须是 JSON 对象")
		}
	}
	if !s.Paths.Available() {
		return errResult(MsgNoDesktop)
	}
	if mutating[name] && s.Control != nil && s.Control.Read().Holder == HolderUser {
		return errResult(MsgUserControl)
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := s.dispatch(ctx, name, args)
	if err != nil {
		return errResult(err.Error())
	}
	return res
}

func (s *MCPServer) xdo(ctx context.Context, args ...string) error {
	if out, err := s.Runner.Output(ctx, "xdotool", args...); err != nil {
		return fmt.Errorf("xdotool 失败：%v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (s *MCPServer) dispatch(ctx context.Context, name string, a toolArgs) (toolResult, error) {
	switch name {
	case "screenshot":
		return s.screenshot(ctx)
	case "list_windows":
		return s.listWindows(ctx)
	case "list_apps":
		apps := InstalledApps(LoadApps(s.Paths.Apps), s.LookPath)
		b, _ := json.MarshalIndent(apps, "", "  ")
		return textResult(string(b)), nil
	case "click":
		x, err := a.intArg("x", true, 0)
		if err != nil {
			return toolResult{}, err
		}
		y, err := a.intArg("y", true, 0)
		if err != nil {
			return toolResult{}, err
		}
		btn, _, err := a.strArg("button")
		if err != nil {
			return toolResult{}, err
		}
		code := map[string]string{"": "1", "left": "1", "middle": "2", "right": "3"}[btn]
		if code == "" {
			return toolResult{}, fmt.Errorf("参数 button 只能是 left、middle 或 right")
		}
		double, err := a.boolArg("double")
		if err != nil {
			return toolResult{}, err
		}
		cmd := []string{"mousemove", "--sync", strconv.Itoa(x), strconv.Itoa(y), "click"}
		if double {
			cmd = append(cmd, "--repeat", "2", "--delay", "120")
		}
		if err := s.xdo(ctx, append(cmd, code)...); err != nil {
			return toolResult{}, err
		}
		return textResult(fmt.Sprintf("已点击 (%d, %d)", x, y)), nil
	case "drag":
		var c [4]int
		for i, k := range []string{"from_x", "from_y", "to_x", "to_y"} {
			n, err := a.intArg(k, true, 0)
			if err != nil {
				return toolResult{}, err
			}
			c[i] = n
		}
		if err := s.xdo(ctx, "mousemove", "--sync", strconv.Itoa(c[0]), strconv.Itoa(c[1]), "mousedown", "1",
			"mousemove", "--sync", strconv.Itoa(c[2]), strconv.Itoa(c[3]), "mouseup", "1"); err != nil {
			return toolResult{}, err
		}
		return textResult(fmt.Sprintf("已从 (%d, %d) 拖到 (%d, %d)", c[0], c[1], c[2], c[3])), nil
	case "type_text":
		text, ok, err := a.strArg("text")
		if err != nil {
			return toolResult{}, err
		}
		if !ok || text == "" {
			return toolResult{}, fmt.Errorf("缺少参数 text")
		}
		if err := s.xdo(ctx, "type", "--delay", "12", "--", text); err != nil {
			return toolResult{}, err
		}
		return textResult(fmt.Sprintf("已输入 %d 个字符", len([]rune(text)))), nil
	case "key":
		keys, _, err := a.strArg("keys")
		if err != nil {
			return toolResult{}, err
		}
		keys = strings.TrimSpace(keys)
		if !keysRe.MatchString(keys) {
			return toolResult{}, fmt.Errorf("参数 keys 无效：用 xdotool 键名，如 ctrl+s 或 Return")
		}
		if err := s.xdo(ctx, append([]string{"key", "--"}, strings.Fields(keys)...)...); err != nil {
			return toolResult{}, err
		}
		return textResult("已按键 " + keys), nil
	case "scroll":
		x, err := a.intArg("x", true, 0)
		if err != nil {
			return toolResult{}, err
		}
		y, err := a.intArg("y", true, 0)
		if err != nil {
			return toolResult{}, err
		}
		dir, _, err := a.strArg("direction")
		if err != nil {
			return toolResult{}, err
		}
		btn := map[string]string{"up": "4", "down": "5", "left": "6", "right": "7"}[dir]
		if btn == "" {
			return toolResult{}, fmt.Errorf("参数 direction 只能是 up、down、left 或 right")
		}
		amount, err := a.intArg("amount", false, 3)
		if err != nil {
			return toolResult{}, err
		}
		if amount < 1 || amount > 20 {
			return toolResult{}, fmt.Errorf("参数 amount 超出范围 1–20")
		}
		if err := s.xdo(ctx, "mousemove", "--sync", strconv.Itoa(x), strconv.Itoa(y), "click", "--repeat", strconv.Itoa(amount), btn); err != nil {
			return toolResult{}, err
		}
		return textResult(fmt.Sprintf("已在 (%d, %d) 向 %s 滚动 %d 格", x, y, dir, amount)), nil
	case "focus_window":
		id, hasID, err := a.strArg("window_id")
		if err != nil {
			return toolResult{}, err
		}
		title, hasTitle, err := a.strArg("title")
		if err != nil {
			return toolResult{}, err
		}
		switch {
		case hasID && windowIDRe.MatchString(id):
			if out, err := s.Runner.Output(ctx, "wmctrl", "-i", "-a", id); err != nil {
				return toolResult{}, fmt.Errorf("没有找到窗口 %s：%v %s", id, err, strings.TrimSpace(string(out)))
			}
			return textResult("已激活窗口 " + id), nil
		case hasID:
			return toolResult{}, fmt.Errorf("参数 window_id 无效，应形如 0x03a00007")
		case hasTitle && strings.TrimSpace(title) != "":
			if out, err := s.Runner.Output(ctx, "wmctrl", "-a", title); err != nil {
				return toolResult{}, fmt.Errorf("没有找到标题含「%s」的窗口：%v %s", title, err, strings.TrimSpace(string(out)))
			}
			return textResult("已激活窗口「" + title + "」"), nil
		default:
			return toolResult{}, fmt.Errorf("需要 window_id 或 title")
		}
	case "launch_app":
		id, _, err := a.strArg("id")
		if err != nil {
			return toolResult{}, err
		}
		app, ok := FindApp(InstalledApps(LoadApps(s.Paths.Apps), s.LookPath), id)
		if !ok {
			return toolResult{}, fmt.Errorf("未安装的应用：%s（用 list_apps 查看可用 id）", id)
		}
		if err := LaunchApp(s.Runner, app); err != nil {
			return toolResult{}, fmt.Errorf("启动 %s 失败：%v", app.Name, err)
		}
		return textResult("已启动 " + app.Name + "，稍后用 screenshot 确认窗口"), nil
	}
	return toolResult{}, fmt.Errorf("未知工具：%s", name)
}

func (s *MCPServer) screenSize(ctx context.Context) (int, int) {
	out, err := s.Runner.Output(ctx, "xdotool", "getdisplaygeometry")
	if err == nil {
		f := strings.Fields(string(out))
		if len(f) == 2 {
			w, e1 := strconv.Atoi(f[0])
			h, e2 := strconv.Atoi(f[1])
			if e1 == nil && e2 == nil {
				return w, h
			}
		}
	}
	return 1920, 1080
}

func (s *MCPServer) screenshot(ctx context.Context) (toolResult, error) {
	png, err := s.Runner.Output(ctx, "import", "-silent", "-window", "root", "png:-")
	if err != nil || len(png) == 0 {
		return toolResult{}, fmt.Errorf("截图失败：%v", err)
	}
	w, h := s.screenSize(ctx)
	return toolResult{Content: []map[string]any{
		{"type": "image", "data": base64.StdEncoding.EncodeToString(png), "mimeType": "image/png"},
		{"type": "text", "text": fmt.Sprintf("屏幕 %dx%d 像素；click 等工具使用同一坐标。", w, h)},
	}}, nil
}

// Window is one list_windows row.
type Window struct {
	ID      string `json:"id"`
	Desktop int    `json:"desktop"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Title   string `json:"title"`
}

// ParseWmctrl parses `wmctrl -lG` output.
func ParseWmctrl(out string) []Window {
	wins := []Window{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 7 {
			continue
		}
		var n [5]int
		ok := true
		for i := 0; i < 5; i++ {
			v, err := strconv.Atoi(f[i+1])
			if err != nil {
				ok = false
				break
			}
			n[i] = v
		}
		if !ok {
			continue
		}
		title := ""
		if len(f) > 7 {
			title = strings.Join(f[7:], " ")
		}
		wins = append(wins, Window{ID: f[0], Desktop: n[0], X: n[1], Y: n[2], Width: n[3], Height: n[4], Title: title})
	}
	return wins
}

func (s *MCPServer) listWindows(ctx context.Context) (toolResult, error) {
	out, err := s.Runner.Output(ctx, "wmctrl", "-lG")
	if err != nil {
		return toolResult{}, fmt.Errorf("列出窗口失败：%v", err)
	}
	b, _ := json.MarshalIndent(ParseWmctrl(string(out)), "", "  ")
	return textResult(string(b)), nil
}
