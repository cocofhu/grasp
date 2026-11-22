package cursorsdk

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"backend/internal/provider"
)

// process is one bridge: commands go to stdin as JSON lines, responses and
// events come back on stdout.
type process struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.Reader
	tail   *provider.OutputTail

	writeMu sync.Mutex
	mu      sync.Mutex
	seq     int
	pending map[int]chan response

	done    chan struct{}
	waitErr error
}

// response answers the command with the same id.
type response struct {
	OK    bool            `json:"ok"`
	Error string          `json:"error"`
	Data  json.RawMessage `json:"data"`
}

func startProcess(ctx context.Context, bin, dir string, env, args []string) (*process, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = env
	// Its own group, so stopping the agent also stops the shells it runs.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	tail := &provider.OutputTail{Max: 4096, LogPrefix: "cursor sdk stderr", Env: cmd.Env}
	cmd.Stderr = tail
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &process{cmd: cmd, stdin: stdin, stdout: stdout, tail: tail, pending: map[int]chan response{}, done: make(chan struct{})}, nil
}

// serve starts handing each event line to onEvent.
func (p *process) serve(onEvent func(json.RawMessage)) { go p.run(onEvent) }

func (p *process) run(onEvent func(json.RawMessage)) {
	reader := bufio.NewReader(p.stdout)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			p.dispatch(line, onEvent)
		}
		if err != nil {
			break
		}
	}
	p.waitErr = p.cmd.Wait()
	p.mu.Lock()
	for id, ch := range p.pending {
		close(ch)
		delete(p.pending, id)
	}
	p.mu.Unlock()
	close(p.done)
}

func (p *process) dispatch(line []byte, onEvent func(json.RawMessage)) {
	var head struct {
		Type string `json:"type"`
		ID   int    `json:"id"`
	}
	if json.Unmarshal(line, &head) != nil {
		return
	}
	if head.Type == "response" {
		var r response
		_ = json.Unmarshal(line, &r)
		p.mu.Lock()
		ch := p.pending[head.ID]
		delete(p.pending, head.ID)
		p.mu.Unlock()
		if ch != nil {
			ch <- r
		}
		return
	}
	if onEvent != nil {
		onEvent(append(json.RawMessage(nil), line...))
	}
}

// errExited is returned for commands the bridge can no longer answer.
var errExited = errors.New("cursor sdk bridge exited")

// call sends one command and waits for its response.
func (p *process) call(ctx context.Context, cmd map[string]any) (response, error) {
	p.mu.Lock()
	p.seq++
	id := p.seq
	ch := make(chan response, 1)
	p.pending[id] = ch
	p.mu.Unlock()
	cmd["id"] = id
	if err := p.send(cmd); err != nil {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return response{}, err
	}
	select {
	case r, ok := <-ch:
		if !ok {
			return response{}, errExited
		}
		if !r.OK {
			return r, fmt.Errorf("cursor %s: %s", cmd["type"], r.Error)
		}
		return r, nil
	case <-ctx.Done():
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return response{}, ctx.Err()
	}
}

func (p *process) send(cmd map[string]any) error {
	line, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	select {
	case <-p.done:
		return errExited
	default:
	}
	if _, err = p.stdin.Write(append(line, '\n')); err != nil {
		select {
		case <-p.done:
			return errExited
		case <-time.After(5 * time.Second):
			return err
		}
	}
	return nil
}

// closeGrace bounds how long a closing bridge may take to release its agent.
var closeGrace = 3 * time.Second

// stop asks the bridge to close its agent, then kills the group.
func (p *process) stop() {
	select {
	case <-p.done:
		return
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), closeGrace)
	defer cancel()
	_, _ = p.call(ctx, map[string]any{"type": "close"})
	select {
	case <-p.done:
	case <-ctx.Done():
	}
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	<-p.done
}

func (p *process) pid() int { return p.cmd.Process.Pid }

func (p *process) exitText() string {
	text := ""
	if p.waitErr != nil {
		var ee *exec.ExitError
		if errors.As(p.waitErr, &ee) {
			text = fmt.Sprintf("退出码=%d", ee.ExitCode())
		} else {
			text = p.waitErr.Error()
		}
	}
	if out := p.tail.String(); out != "" {
		if text != "" {
			text += "；"
		}
		text += out
	}
	return text
}
