package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"

	"backend/internal/provider"
)

// PermissionChooser resolves session/request_permission when the Agent asks the user.
// If nil, the first allow_once / allow_always option is selected automatically.
type PermissionChooser func(ctx context.Context, rpcID json.RawMessage, rawParams json.RawMessage) (optionID string, err error)

// ConfigOption 对应 ACP SessionConfigOption（select 类型）。
type ConfigOption struct {
	ID           string               `json:"id"`
	CurrentValue string               `json:"currentValue"`
	Category     string               `json:"category,omitempty"`
	Options      []ConfigOptionValue  `json:"options,omitempty"`
	Groups       []ConfigOptionGroup  `json:"groups,omitempty"`
}

// ConfigOptionValue 对应 ACP SessionConfigSelectOption（平铺 options）。
type ConfigOptionValue struct {
	Value       string `json:"value"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// ConfigOptionGroup 对应 ACP SessionConfigSelectGroup（分组 options）。
type ConfigOptionGroup struct {
	ID      string              `json:"id"`
	Name    string              `json:"name,omitempty"`
	Options []ConfigOptionValue `json:"options"`
}

// Panel is an ACP client: one Agent subprocess and one ACP session.
type Panel struct {
	conn *Conn
	term *TerminalManager

	SessionID string
	CWD       string
	FSRoot    string

	AgentName    string
	AgentTitle   string
	AgentVersion string

	// 当前模型名称（由 configOptions 获得或 session/set_config_option 切换后更新）
	ModelID   string
	ModelName string

	// Agent 返回的所有 configOptions（含 model / mode / thinking 等）
	ConfigOptions []ConfigOption

	OnEvent func(ev json.RawMessage)

	Permissions PermissionChooser
}

// NewPanel 启动 argv 子进程（ACP JSON-RPC over stdio），再执行 initialize + session/new。
// procCtx 绑定 exec.CommandContext：必须在整段会话期间保持有效，Connect 返回后仍不得取消（否则会 SIGKILL 子进程）。
// rpcCtx 仅用于握手阶段 Call 的超时/取消（例如 WithTimeout 子 context）。
// mcpServersJSON should be a JSON array (or "null" / empty for none).
// 模型通过 argv 由调用方组装传入，不再在此处切换。
func NewPanel(
	procCtx context.Context,
	rpcCtx context.Context,
	argv, procEnv []string,
	cwd, fsRoot string,
	mcpServersJSON json.RawMessage,
	onEvent func(json.RawMessage),
	perm PermissionChooser,
) (*Panel, error) {
	if fsRoot == "" {
		fsRoot = cwd
	}
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}
	fsRoot, err = filepath.Abs(fsRoot)
	if err != nil {
		return nil, err
	}
	log.Printf("acp: 路径已规范化 cwd=%q fsRoot=%q", cwd, fsRoot)

	conn, err := StartAgent(procCtx, argv, procEnv)
	if err != nil {
		return nil, err
	}
	if proc := conn.Process(); proc != nil {
		log.Printf("acp: 子进程已启动 pid=%d argv0=%q", proc.Pid, argv[0])
	}
	p := &Panel{
		conn:        conn,
		term:        NewTerminalManager(cwd),
		CWD:         cwd,
		FSRoot:      fsRoot,
		OnEvent:     onEvent,
		Permissions: perm,
	}
	conn.SetNotificationHandler(p.onAgentNotification)
	conn.SetRequestHandler(p.onAgentRequest)

	initParams := map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{
			"fs": map[string]any{
				"readTextFile":  true,
				"writeTextFile": true,
			},
			"terminal": true,
		},
		"clientInfo": map[string]any{
			"name":    "agentchat",
			"title":   "AgentChat",
			"version": "0.1.0",
		},
	}
	log.Printf("acp: 等待 JSON-RPC 应答 method=initialize …")
	res, err := conn.Call(rpcCtx, "initialize", initParams)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("initialize: %w", err)
	}
	var initOut struct {
		ProtocolVersion   int             `json:"protocolVersion"`
		AgentCapabilities json.RawMessage `json:"agentCapabilities"`
		AgentInfo         struct {
			Name    string `json:"name"`
			Title   string `json:"title"`
			Version string `json:"version"`
		} `json:"agentInfo"`
	}
	if err := json.Unmarshal(res, &initOut); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("initialize result: %w", err)
	}
	p.AgentName = initOut.AgentInfo.Name
	p.AgentTitle = initOut.AgentInfo.Title
	p.AgentVersion = initOut.AgentInfo.Version
	log.Printf("acp: initialize 完成 agent name=%q version=%q", p.AgentName, p.AgentVersion)

	mcp := json.RawMessage(`[]`)
	if len(mcpServersJSON) > 0 && string(mcpServersJSON) != "null" {
		mcp = mcpServersJSON
	}
	snParams := map[string]any{
		"cwd":        cwd,
		"mcpServers": mcp,
	}
	log.Printf("acp: 等待 JSON-RPC 应答 method=session/new …")
	snRes, err := conn.Call(rpcCtx, "session/new", snParams)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("session/new: %w", err)
	}
	var sn struct {
		SessionID     string          `json:"sessionId"`
		ConfigOptions json.RawMessage `json:"configOptions"`
	}
	if err := json.Unmarshal(snRes, &sn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("session/new result: %w", err)
	}
	if sn.SessionID == "" {
		_ = conn.Close()
		return nil, fmt.Errorf("session/new result: empty sessionId")
	}
	p.SessionID = sn.SessionID
	conn.SetLogSID(p.SessionID)
	log.Printf("acp: session/new 完成 sessionId=%q", p.SessionID)

	p.parseAndSyncConfigOptions(sn.ConfigOptions)

	return p, nil
}

// parseAndSyncConfigOptions 解析 session/new（或 config_option_update）返回的 configOptions，
// 更新 Panel.ConfigOptions / ModelID / ModelName。
func (p *Panel) parseAndSyncConfigOptions(raw json.RawMessage) {
	if len(raw) == 0 || string(raw) == "null" {
		return
	}
	var opts []ConfigOption
	if err := json.Unmarshal(raw, &opts); err != nil {
		log.Printf("acp sid=%s: configOptions 解析失败: %v", p.SessionID, err)
		return
	}
	p.ConfigOptions = opts
	for _, opt := range opts {
		if opt.ID == "model" {
			p.ModelID = opt.CurrentValue
			p.ModelName = findOptionName(opt, opt.CurrentValue)
			if p.ModelName == "" {
				p.ModelName = p.ModelID
			}
			log.Printf("acp sid=%s: 当前模型 id=%q name=%q（共 %d 可选）",
				p.SessionID, p.ModelID, p.ModelName, countOptions(opt))
			break
		}
	}
}

func findOptionName(opt ConfigOption, value string) string {
	for _, o := range opt.Options {
		if o.Value == value {
			return o.Name
		}
	}
	for _, g := range opt.Groups {
		for _, o := range g.Options {
			if o.Value == value {
				return o.Name
			}
		}
	}
	return ""
}

func countOptions(opt ConfigOption) int {
	n := len(opt.Options)
	for _, g := range opt.Groups {
		n += len(g.Options)
	}
	return n
}


func (p *Panel) emit(ev map[string]any) {
	if p.OnEvent == nil {
		return
	}
	b, err := json.Marshal(ev)
	if err != nil {
		log.Printf("acp sid=%s: 事件序列化失败: %v", p.SessionID, err)
		return
	}
	p.OnEvent(b)
}

func (p *Panel) onAgentNotification(raw json.RawMessage) error {
	var env struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		log.Printf("acp sid=%s: 通知 JSON 解析失败: %v", p.SessionID, err)
		return err
	}
	if env.Method != "session/update" {
		return nil
	}
	var params struct {
		SessionID string          `json:"sessionId"`
		Update    json.RawMessage `json:"update"`
	}
	if err := json.Unmarshal(env.Params, &params); err != nil {
		log.Printf("acp sid=%s: session/update params 解析失败: %v", p.SessionID, err)
		return err
	}
	if params.SessionID != p.SessionID {
		return nil
	}
	p.emit(map[string]any{
		"type":      "session_update",
		"sessionId": params.SessionID,
		"update":    params.Update,
	})
	return nil
}

func (p *Panel) onAgentRequest(ctx context.Context, method string, id json.RawMessage, raw json.RawMessage) error {
	switch method {
	case "fs/read_text_file":
		return p.handleFSRead(ctx, id, raw)
	case "fs/write_text_file":
		return p.handleFSWrite(ctx, id, raw)
	case "terminal/create":
		return p.handleTermCreate(ctx, id, raw)
	case "terminal/output":
		return p.handleTermOutput(ctx, id, raw)
	case "terminal/wait_for_exit":
		return p.handleTermWait(ctx, id, raw)
	case "terminal/kill":
		return p.handleTermKill(ctx, id, raw)
	case "terminal/release":
		return p.handleTermRelease(ctx, id, raw)
	case "session/request_permission":
		return p.handlePermission(ctx, id, raw)
	default:
		if err := p.conn.RespondError(id, -32601, "method not implemented: "+method); err != nil {
			log.Printf("acp sid=%s: 回复未实现方法错误失败 method=%q: %v", p.SessionID, method, err)
			return fmt.Errorf("respond error for unimplemented method %q: %w", method, err)
		}
		return nil
	}
}

type fsReadParams struct {
	SessionID string `json:"sessionId"`
	Path      string `json:"path"`
	Line      *int   `json:"line"`
	Limit     *int   `json:"limit"`
}

func (p *Panel) handleFSRead(_ context.Context, id json.RawMessage, raw json.RawMessage) error {
	var env struct {
		Params fsReadParams `json:"params"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return p.conn.RespondError(id, -32602, "invalid params")
	}
	if env.Params.SessionID != p.SessionID {
		return p.conn.RespondError(id, -32602, "session mismatch")
	}
	path := filepath.Clean(env.Params.Path)
	if !filepath.IsAbs(path) {
		return p.conn.RespondError(id, -32602, "path must be absolute")
	}
	if err := EnsurePathAllowed(p.FSRoot, path); err != nil {
		return p.conn.RespondError(id, -32603, err.Error())
	}
	line := 1
	if env.Params.Line != nil && *env.Params.Line > 0 {
		line = *env.Params.Line
	}
	limit := 0
	if env.Params.Limit != nil && *env.Params.Limit > 0 {
		limit = *env.Params.Limit
	}
	content, err := ReadTextFile(path, line, limit)
	if err != nil {
		return p.conn.RespondError(id, -32603, err.Error())
	}
	return p.conn.RespondRaw(id, map[string]any{"content": content})
}

type fsWriteParams struct {
	SessionID string `json:"sessionId"`
	Path      string `json:"path"`
	Content   string `json:"content"`
}

func (p *Panel) handleFSWrite(_ context.Context, id json.RawMessage, raw json.RawMessage) error {
	var env struct {
		Params fsWriteParams `json:"params"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return p.conn.RespondError(id, -32602, "invalid params")
	}
	if env.Params.SessionID != p.SessionID {
		return p.conn.RespondError(id, -32602, "session mismatch")
	}
	path := filepath.Clean(env.Params.Path)
	if !filepath.IsAbs(path) {
		return p.conn.RespondError(id, -32602, "path must be absolute")
	}
	if err := EnsurePathAllowed(p.FSRoot, path); err != nil {
		return p.conn.RespondError(id, -32603, err.Error())
	}
	if err := WriteTextFile(path, env.Params.Content); err != nil {
		return p.conn.RespondError(id, -32603, err.Error())
	}
	p.emit(map[string]any{
		"type":      "fs_write",
		"sessionId": p.SessionID,
		"path":      path,
	})
	return p.conn.RespondRaw(id, nil)
}

type termCreateParams struct {
	SessionID string   `json:"sessionId"`
	Command   string   `json:"command"`
	Args      []string `json:"args"`
	Env       []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"env"`
	Cwd             string `json:"cwd"`
	OutputByteLimit int64  `json:"outputByteLimit"`
}

func (p *Panel) handleTermCreate(_ context.Context, id json.RawMessage, raw json.RawMessage) error {
	var env struct {
		Params termCreateParams `json:"params"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return p.conn.RespondError(id, -32602, "invalid params")
	}
	if env.Params.SessionID != p.SessionID {
		return p.conn.RespondError(id, -32602, "session mismatch")
	}
	tid, err := p.term.Create(context.Background(), env.Params.SessionID, env.Params.Command, env.Params.Args, env.Params.Env, env.Params.Cwd, env.Params.OutputByteLimit)
	if err != nil {
		return p.conn.RespondError(id, -32603, err.Error())
	}
	p.emit(map[string]any{
		"type":       "terminal_create",
		"sessionId":  p.SessionID,
		"terminalId": tid,
		"command":    env.Params.Command,
		"args":       env.Params.Args,
	})
	return p.conn.RespondRaw(id, map[string]any{"terminalId": tid})
}

type termIDParams struct {
	SessionID  string `json:"sessionId"`
	TerminalID string `json:"terminalId"`
}

func (p *Panel) handleTermOutput(_ context.Context, id json.RawMessage, raw json.RawMessage) error {
	var env struct {
		Params termIDParams `json:"params"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return p.conn.RespondError(id, -32602, "invalid params")
	}
	if env.Params.SessionID != p.SessionID {
		return p.conn.RespondError(id, -32602, "session mismatch")
	}
	out, truncated, ec, sig, err := p.term.Output(env.Params.SessionID, env.Params.TerminalID)
	if err != nil {
		return p.conn.RespondError(id, -32603, err.Error())
	}
	res := map[string]any{
		"output":    out,
		"truncated": truncated,
	}
	if ec != nil {
		res["exitStatus"] = map[string]any{
			"exitCode": *ec,
			"signal":   sig,
		}
	}
	return p.conn.RespondRaw(id, res)
}

func (p *Panel) handleTermWait(ctx context.Context, id json.RawMessage, raw json.RawMessage) error {
	var env struct {
		Params termIDParams `json:"params"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return p.conn.RespondError(id, -32602, "invalid params")
	}
	if env.Params.SessionID != p.SessionID {
		return p.conn.RespondError(id, -32602, "session mismatch")
	}
	code, sig, err := p.term.WaitForExit(ctx, env.Params.TerminalID)
	if err != nil {
		return p.conn.RespondError(id, -32603, err.Error())
	}
	return p.conn.RespondRaw(id, map[string]any{"exitCode": code, "signal": sig})
}

func (p *Panel) handleTermKill(_ context.Context, id json.RawMessage, raw json.RawMessage) error {
	var env struct {
		Params termIDParams `json:"params"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return p.conn.RespondError(id, -32602, "invalid params")
	}
	if env.Params.SessionID != p.SessionID {
		return p.conn.RespondError(id, -32602, "session mismatch")
	}
	if err := p.term.Kill(env.Params.TerminalID); err != nil {
		return p.conn.RespondError(id, -32603, err.Error())
	}
	return p.conn.RespondRaw(id, nil)
}

func (p *Panel) handleTermRelease(_ context.Context, id json.RawMessage, raw json.RawMessage) error {
	var env struct {
		Params termIDParams `json:"params"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return p.conn.RespondError(id, -32602, "invalid params")
	}
	if env.Params.SessionID != p.SessionID {
		return p.conn.RespondError(id, -32602, "session mismatch")
	}
	if err := p.term.Release(env.Params.TerminalID); err != nil {
		return p.conn.RespondError(id, -32603, err.Error())
	}
	return p.conn.RespondRaw(id, nil)
}

func (p *Panel) handlePermission(ctx context.Context, id json.RawMessage, raw json.RawMessage) error {
	var env struct {
		Params json.RawMessage `json:"params"`
	}
	params := raw
	if err := json.Unmarshal(raw, &env); err == nil && len(env.Params) > 0 {
		params = env.Params
	}
	choose := p.Permissions
	if choose == nil {
		choose = defaultPermissionChoice
	}
	opt, err := choose(ctx, id, params)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return p.conn.RespondRaw(id, map[string]any{
				"outcome": map[string]any{"outcome": "cancelled"},
			})
		}
		return p.conn.RespondError(id, -32603, err.Error())
	}
	return p.conn.RespondRaw(id, map[string]any{
		"outcome": map[string]any{"outcome": "selected", "optionId": opt},
	})
}

// DefaultPermissionForParams picks the first allow_* option (ACP-friendly default).
func DefaultPermissionForParams(rawParams json.RawMessage) (string, error) {
	return defaultPermissionChoice(context.Background(), nil, rawParams)
}

func defaultPermissionChoice(_ context.Context, _ json.RawMessage, rawParams json.RawMessage) (string, error) {
	var p struct {
		Options []struct {
			OptionID string `json:"optionId"`
			Kind     string `json:"kind"`
		} `json:"options"`
	}
	if err := json.Unmarshal(rawParams, &p); err != nil {
		return "", err
	}
	for _, o := range p.Options {
		if o.Kind == "allow_once" || o.Kind == "allow_always" {
			return o.OptionID, nil
		}
	}
	if len(p.Options) > 0 {
		return p.Options[0].OptionID, nil
	}
	return "", fmt.Errorf("no permission options")
}

// PromptImage 图片附件（base64）。为跨传输层统一类型，别名到 provider.PromptImage。
type PromptImage = provider.PromptImage

// Prompt sends a user message (text + optional images) and waits until the Agent completes the turn.
func (p *Panel) Prompt(ctx context.Context, text string, images []PromptImage) (stopReason string, err error) {
	var prompt []map[string]any
	if text != "" {
		prompt = append(prompt, map[string]any{"type": "text", "text": text})
	}
	for _, img := range images {
		prompt = append(prompt, map[string]any{
			"type":     "image",
			"data":     img.Data,
			"mimeType": img.MimeType,
		})
	}
	if len(prompt) == 0 {
		prompt = append(prompt, map[string]any{"type": "text", "text": ""})
	}
	params := map[string]any{
		"sessionId": p.SessionID,
		"prompt":    prompt,
	}
	res, err := p.conn.Call(ctx, "session/prompt", params)
	if err != nil {
		return "", err
	}
	var out struct {
		StopReason string `json:"stopReason"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return "", err
	}
	p.emit(map[string]any{
		"type":       "prompt_done",
		"sessionId":  p.SessionID,
		"stopReason": out.StopReason,
	})
	return out.StopReason, nil
}

// Cancel sends session/cancel (and $/cancel_request for the in-flight prompt id when applicable).
func (p *Panel) Cancel() error {
	if p.conn == nil {
		return nil
	}
	return p.conn.CancelSessionTurn(p.SessionID)
}

func (p *Panel) Close() error {
	if p.conn == nil {
		return nil
	}
	return p.conn.Close()
}

// Conn exposes the underlying connection for advanced use.
func (p *Panel) Conn() *Conn { return p.conn }

// ProcessPIDs lists the agent subprocess and the terminals it started through
// ACP terminal/create (those run as children of the bridge, not the agent).
func (p *Panel) ProcessPIDs() []int {
	var out []int
	if pid := p.conn.PID(); pid > 0 {
		out = append(out, pid)
	}
	return append(out, p.term.PIDs()...)
}
