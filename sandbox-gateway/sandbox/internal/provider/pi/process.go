package pi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"backend/internal/provider"
)

// process is one `pi --mode rpc` child: commands go to stdin as JSON lines,
// responses and events come back on stdout.
type process struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	tail  *provider.OutputTail

	writeMu sync.Mutex
	mu      sync.Mutex
	seq     int
	pending map[string]chan response

	done    chan struct{}
	waitErr error
}

// response answers the command with the same id.
type response struct {
	Success bool            `json:"success"`
	Error   string          `json:"error"`
	Data    json.RawMessage `json:"data"`
}

// startProcess runs the agent and hands each event line to onEvent.
func startProcess(ctx context.Context, bin, dir string, env, args []string, onEvent func(json.RawMessage)) (*process, error) {
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
	tail := &provider.OutputTail{Max: 4096, LogPrefix: "pi stderr", Env: cmd.Env}
	cmd.Stderr = tail
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &process{cmd: cmd, stdin: stdin, tail: tail, pending: map[string]chan response{}, done: make(chan struct{})}
	go p.run(stdout, onEvent)
	return p, nil
}

// run reads stdout until the process exits, routing responses to their
// callers and everything else to onEvent.
func (p *process) run(stdout io.Reader, onEvent func(json.RawMessage)) {
	reader := bufio.NewReader(stdout)
	for {
		// Records end at LF only; JSON strings may hold U+2028/U+2029.
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			p.dispatch(trimRecord(line), onEvent)
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

func trimRecord(line []byte) []byte {
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	return line
}

func (p *process) dispatch(line []byte, onEvent func(json.RawMessage)) {
	if len(line) == 0 {
		return
	}
	var head struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if json.Unmarshal(line, &head) != nil {
		return
	}
	if head.Type == "response" && head.ID != "" {
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
	onEvent(append(json.RawMessage(nil), line...))
}

// errExited is returned for commands the process can no longer answer.
var errExited = errors.New("pi rpc process exited")

// call sends one command and waits for its response.
func (p *process) call(ctx context.Context, cmd map[string]any) (response, error) {
	p.mu.Lock()
	p.seq++
	id := "grasp-" + strconv.Itoa(p.seq)
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
		if !r.Success {
			return r, fmt.Errorf("pi %s: %s", cmd["type"], r.Error)
		}
		return r, nil
	case <-ctx.Done():
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return response{}, ctx.Err()
	}
}

// send writes one command without waiting for its response.
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
		// A closed stdin means the process is going away; report the exit.
		select {
		case <-p.done:
			return errExited
		case <-time.After(5 * time.Second):
			return err
		}
	}
	return nil
}

func (p *process) stop() {
	select {
	case <-p.done:
		return
	default:
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
