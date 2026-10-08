package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
)

// Conn is a bidirectional JSON-RPC connection over NDJSON on a subprocess stdio pair.
type Conn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner

	writeMu sync.Mutex
	nextID  atomic.Int64

	// 禁止同连接上并发 session/prompt（与宿主侧 FIFO 队列双保险，避免 JSON-RPC 交错）
	sessionPromptCallMu sync.Mutex

	pendingMu sync.Mutex
	pending   map[int64]chan json.RawMessage

	// 当前进行中的 session/prompt 的 JSON-RPC id（用于 Stop → $/cancel_request）
	promptMu       sync.Mutex
	activePromptID int64

	onNotification func(raw json.RawMessage) error
	onRequest      func(ctx context.Context, method string, id json.RawMessage, raw json.RawMessage) error

	readErr error
	done    chan struct{}
	once    sync.Once

	logMu  sync.RWMutex
	logSID string // sessionId 或握手阶段的 handshake，日志统一 sid= 前缀
}

// pumpAgentStderr 把子进程 stderr 打到标准 log（带时间戳），并原样复制到 os.Stderr，避免「终端里完全看不到 Agent 报错」。
func pumpAgentStderr(r *os.File, pid int) {
	defer func() {
		if err := r.Close(); err != nil {
			log.Printf("agent pid=%d stderr close: %v", pid, err)
		}
	}()
	s := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	s.Buffer(buf, 1024*1024)
	for s.Scan() {
		line := s.Text()
		log.Printf("agent pid=%d stderr | %s", pid, line)
		_, _ = fmt.Fprintln(os.Stderr, line)
	}
	if err := s.Err(); err != nil {
		log.Printf("agent pid=%d stderr reader: %v", pid, err)
	}
}

// StartAgent launches argv[0] with argv[1:] and wires stdio for ACP.
func StartAgent(ctx context.Context, argv []string, env []string) (*Conn, error) {
	if len(argv) == 0 {
		return nil, errors.New("empty agent argv")
	}
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Env = env
	stdin, err := c.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := c.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, err
	}
	c.Stderr = stderrW
	if err := c.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderrW.Close()
		_ = stderrR.Close()
		return nil, err
	}
	// 子进程已 dup stderr；关闭父进程持有的写端，避免管道永不 EOF，并保证子进程退出后读端能结束。
	_ = stderrW.Close()
	pid := 0
	if c.Process != nil {
		pid = c.Process.Pid
	}
	go pumpAgentStderr(stderrR, pid)
	conn := &Conn{
		cmd:     c,
		stdin:   stdin,
		stdout:  bufio.NewScanner(stdout),
		pending: make(map[int64]chan json.RawMessage),
		done:    make(chan struct{}),
	}
	conn.stdout.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	conn.SetLogSID("handshake")
	go conn.readLoop()
	return conn, nil
}

// SetLogSID 设置日志中的 sid= 片段（握手阶段为 handshake，session/new 后为真实 sessionId）。
func (c *Conn) SetLogSID(s string) {
	c.logMu.Lock()
	c.logSID = s
	c.logMu.Unlock()
}

func (c *Conn) logTag() string {
	c.logMu.RLock()
	s := c.logSID
	c.logMu.RUnlock()
	if s == "" {
		return "sid=pending"
	}
	return "sid=" + s
}

func (c *Conn) SetNotificationHandler(fn func(raw json.RawMessage) error) {
	c.onNotification = fn
}

func (c *Conn) SetRequestHandler(fn func(ctx context.Context, method string, id json.RawMessage, raw json.RawMessage) error) {
	c.onRequest = fn
}

func (c *Conn) readLoop() {
	defer c.closeDone()
	defer c.failAllPending()
	for c.stdout.Scan() {
		line := c.stdout.Bytes()
		in, err := classifyInbound(line)
		if err != nil {
			c.readErr = err
			log.Printf("acp %s: stdout 行解析失败（将结束读循环）: %v", c.logTag(), err)
			return
		}
		switch in.Kind {
		case InboundResponse:
			var env struct {
				ID json.RawMessage `json:"id"`
			}
			if err := json.Unmarshal(in.Raw, &env); err != nil {
				log.Printf("acp %s: 响应 ID 解析失败（跳过）: %v", c.logTag(), err)
				continue
			}
			if key := JSONRPCIDKey(env.ID); key != "" {
				if n, err := strconv.ParseInt(key, 10, 64); err == nil {
					ch := c.takePending(n)
					if ch != nil {
						ch <- in.Raw
						close(ch)
					}
				}
			}
		case InboundNotification:
			if c.onNotification != nil {
				if err := c.onNotification(in.Raw); err != nil {
					log.Printf("acp %s: 通知处理失败 method=%q: %v", c.logTag(), in.Method, err)
				}
			}
		case InboundRequest:
			if c.onRequest != nil {
				// Handle in background so read loop keeps draining (permissions, etc.).
				go func(in Inbound) {
					ctx := context.Background()
					if err := c.onRequest(ctx, in.Method, in.RawID, in.Raw); err != nil {
						log.Printf("acp %s: 请求处理失败 method=%q: %v", c.logTag(), in.Method, err)
					}
				}(in)
			}
		default:
			preview := string(line)
			const maxPreviewLen = 200
			if len(preview) > maxPreviewLen {
				preview = preview[:maxPreviewLen] + "…"
			}
			log.Printf("acp %s: 忽略无法分类的 JSON-RPC 行: %s", c.logTag(), preview)
		}
	}
	if err := c.stdout.Err(); err != nil && c.readErr == nil {
		c.readErr = err
		log.Printf("acp %s: stdout 读取错误: %v", c.logTag(), err)
	}
}

func (c *Conn) closeDone() {
	c.once.Do(func() { close(c.done) })
}

// Wait waits for the subprocess to exit after the connection is closed.
func (c *Conn) Wait() error {
	if c.cmd == nil {
		return nil
	}
	return c.cmd.Wait()
}

func (c *Conn) Done() <-chan struct{} { return c.done }

func (c *Conn) ReadErr() error { return c.readErr }

// Process returns the Agent subprocess handle, if started.
func (c *Conn) Process() *os.Process {
	if c.cmd == nil {
		return nil
	}
	return c.cmd.Process
}

func (c *Conn) Close() error {
	c.closeDone()
	var errs []error
	if c.stdin != nil {
		if err := c.stdin.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	return errors.Join(errs...)
}

func (c *Conn) takePending(id int64) chan json.RawMessage {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	ch, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	return ch
}

// failAllPending closes stdout read side: unblock any in-flight Call waiting for a response.
func (c *Conn) failAllPending() {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	for _, ch := range c.pending {
		close(ch)
	}
	c.pending = make(map[int64]chan json.RawMessage)
}

// Call sends a JSON-RPC request and waits for the matching response object (full line).
func (c *Conn) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	trackPrompt := method == "session/prompt"
	if trackPrompt {
		c.sessionPromptCallMu.Lock()
		defer c.sessionPromptCallMu.Unlock()
		c.promptMu.Lock()
		c.activePromptID = id
		c.promptMu.Unlock()
		defer func() {
			c.promptMu.Lock()
			if c.activePromptID == id {
				c.activePromptID = 0
			}
			c.promptMu.Unlock()
		}()
	}
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	ch := make(chan json.RawMessage, 1)
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()

	if err := c.writeLine(body); err != nil {
		c.takePending(id)
		log.Printf("acp %s: Call 写入失败 method=%q id=%d: %v", c.logTag(), method, id, err)
		return nil, err
	}
	select {
	case <-ctx.Done():
		c.takePending(id)
		log.Printf("acp %s: Call 取消 method=%q id=%d: %v", c.logTag(), method, id, ctx.Err())
		return nil, ctx.Err()
	case raw, ok := <-ch:
		if !ok {
			log.Printf("acp %s: Call 连接已关 method=%q id=%d", c.logTag(), method, id)
			return nil, fmt.Errorf("connection closed")
		}
		var env struct {
			Result json.RawMessage `json:"result"`
			Error  *rpcError       `json:"error"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			log.Printf("acp %s: Call 响应 JSON 无效 method=%q id=%d: %v", c.logTag(), method, id, err)
			return nil, err
		}
		if env.Error != nil {
			log.Printf("acp %s: Call RPC 错误 method=%q id=%d code=%d msg=%q", c.logTag(), method, id, env.Error.Code, env.Error.Message)
			return nil, fmt.Errorf("rpc error %d: %s", env.Error.Code, env.Error.Message)
		}
		return env.Result, nil
	}
}

// Notify sends a JSON-RPC notification (no response).
func (c *Conn) Notify(method string, params any) error {
	req := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if err := c.writeLine(body); err != nil {
		log.Printf("acp %s: Notify 写入失败 method=%q: %v", c.logTag(), method, err)
		return err
	}
	return nil
}

// CancelSessionTurn 通知 Agent 停止当前轮次：先 session/cancel，再对进行中的 session/prompt 发 $/cancel_request（ACP 草案）。
func (c *Conn) CancelSessionTurn(sessionID string) error {
	if err := c.Notify("session/cancel", map[string]any{"sessionId": sessionID}); err != nil {
		return err
	}
	c.promptMu.Lock()
	pid := c.activePromptID
	c.promptMu.Unlock()
	if pid == 0 {
		return nil
	}
	if err := c.Notify("$/cancel_request", map[string]any{"id": pid}); err != nil {
		log.Printf("acp %s: $/cancel_request 失败 id=%d（session/cancel 已发）: %v", c.logTag(), pid, err)
	} else {
		log.Printf("acp %s: 已发送 $/cancel_request id=%d", c.logTag(), pid)
	}
	return nil
}

func (c *Conn) RespondRaw(id json.RawMessage, result any) error {
	var idVal any
	if len(id) == 0 || string(id) == "null" {
		return errors.New("missing rpc id")
	}
	if err := json.Unmarshal(id, &idVal); err != nil {
		return err
	}
	resp := map[string]any{"jsonrpc": "2.0", "id": idVal, "result": result}
	body, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	return c.writeLine(body)
}

func (c *Conn) RespondError(id json.RawMessage, code int, message string) error {
	var idVal any
	if len(id) == 0 || string(id) == "null" {
		return errors.New("missing rpc id")
	}
	if err := json.Unmarshal(id, &idVal); err != nil {
		return err
	}
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      idVal,
		"error":   map[string]any{"code": code, "message": message},
	}
	body, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	return c.writeLine(body)
}

func (c *Conn) writeLine(b []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err := c.stdin.Write(append(b, '\n'))
	if err != nil {
		log.Printf("acp %s: stdin 写入失败: %v", c.logTag(), err)
		return fmt.Errorf("agent stdin: %w", err)
	}
	return nil
}

// PID is the agent subprocess's pid (0 when it never started).
func (c *Conn) PID() int {
	if c == nil || c.cmd == nil || c.cmd.Process == nil {
		return 0
	}
	return c.cmd.Process.Pid
}
