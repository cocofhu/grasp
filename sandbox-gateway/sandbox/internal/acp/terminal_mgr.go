package acp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
)

// TerminalManager implements ACP terminal/* for a session working directory.
type TerminalManager struct {
	mu    sync.Mutex
	terms map[string]*termSession
	seq   atomic.Uint64
	cwd   string
}

type termSession struct {
	cmd        *exec.Cmd
	out        *bytes.Buffer
	truncated  atomic.Bool
	byteLimit  int64
	done       chan struct{}
	exitCode   *int
	exitSignal *string
	mu         sync.Mutex
}

func NewTerminalManager(cwd string) *TerminalManager {
	return &TerminalManager{
		terms: make(map[string]*termSession),
		cwd:   cwd,
	}
}

func (m *TerminalManager) Create(_ context.Context, _ string, command string, args []string, envPairs []struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}, cwd string, outputByteLimit int64) (string, error) {
	if cwd == "" {
		cwd = m.cwd
	}
	if outputByteLimit <= 0 {
		outputByteLimit = 4 << 20
	}
	id := fmt.Sprintf("term_%d", m.seq.Add(1))
	ts := &termSession{
		done:      make(chan struct{}),
		byteLimit: outputByteLimit,
		out:       new(bytes.Buffer),
	}
	cmd := exec.Command(command, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = os.Environ()
	if len(envPairs) > 0 {
		cmd.Env = append(cmd.Env, execEnv(envPairs)...)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	ts.cmd = cmd
	if err := cmd.Start(); err != nil {
		return "", err
	}
	go m.pumpOutput(ts, stdout, stderr)

	m.mu.Lock()
	m.terms[id] = ts
	m.mu.Unlock()
	return id, nil
}

func execEnv(pairs []struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}) []string {
	var e []string
	for _, p := range pairs {
		e = append(e, fmt.Sprintf("%s=%s", p.Name, p.Value))
	}
	return e
}

func (m *TerminalManager) pumpOutput(ts *termSession, stdout, stderr io.Reader) {
	var wg sync.WaitGroup
	copyStream := func(r io.Reader) {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				ts.mu.Lock()
				ts.out.Write(buf[:n])
				if int64(ts.out.Len()) > ts.byteLimit {
					overflow := int64(ts.out.Len()) - ts.byteLimit
					b := ts.out.Bytes()
					cut := trimPrefixUTF8(b, overflow)
					nb := make([]byte, len(b)-cut)
					copy(nb, b[cut:])
					ts.out.Reset()
					ts.out.Write(nb)
					ts.truncated.Store(true)
				}
				ts.mu.Unlock()
			}
			if err != nil {
				break
			}
		}
	}
	wg.Add(2)
	go copyStream(stdout)
	go copyStream(stderr)
	wg.Wait()
	waitErr := ts.cmd.Wait()
	ts.mu.Lock()
	if waitErr != nil {
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			code := ee.ExitCode()
			ts.exitCode = &code
		} else {
			log.Printf("terminal: 子进程 Wait 返回非 ExitError: %v", waitErr)
		}
	} else {
		z := 0
		ts.exitCode = &z
	}
	ts.mu.Unlock()
	close(ts.done)
}

func trimPrefixUTF8(b []byte, cut int64) int {
	if cut <= 0 || int(cut) >= len(b) {
		return len(b)
	}
	start := int(cut)
	for start < len(b) && start > 0 && (b[start]&0xc0) == 0x80 {
		start--
	}
	if start < 0 {
		start = 0
	}
	return start
}

func (m *TerminalManager) Output(_, terminalID string) (output string, truncated bool, exitCode *int, exitSignal *string, err error) {
	ts, err := m.get(terminalID)
	if err != nil {
		return "", false, nil, nil, err
	}
	ts.mu.Lock()
	out := ts.out.String()
	tr := ts.truncated.Load()
	ec, es := ts.exitCode, ts.exitSignal
	ts.mu.Unlock()
	return out, tr, ec, es, nil
}

func (m *TerminalManager) WaitForExit(ctx context.Context, terminalID string) (exitCode int, signal *string, err error) {
	ts, err := m.get(terminalID)
	if err != nil {
		return 0, nil, err
	}
	select {
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case <-ts.done:
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.exitCode == nil {
		return 0, ts.exitSignal, nil
	}
	return *ts.exitCode, ts.exitSignal, nil
}

func (m *TerminalManager) Kill(terminalID string) error {
	ts, err := m.get(terminalID)
	if err != nil {
		return err
	}
	if ts.cmd != nil && ts.cmd.Process != nil {
		return ts.cmd.Process.Kill()
	}
	return nil
}

func (m *TerminalManager) Release(terminalID string) error {
	ts, err := m.get(terminalID)
	if err != nil {
		return err
	}
	if ts.cmd != nil && ts.cmd.Process != nil {
		if err := ts.cmd.Process.Kill(); err != nil {
			log.Printf("terminal: Release kill 失败 id=%q: %v", terminalID, err)
		}
	}
	m.mu.Lock()
	delete(m.terms, terminalID)
	m.mu.Unlock()
	return nil
}

func (m *TerminalManager) get(id string) (*termSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ts, ok := m.terms[id]
	if !ok {
		return nil, fmt.Errorf("unknown terminal %q", id)
	}
	return ts, nil
}

// PIDs lists the processes of terminals that have not exited yet.
func (m *TerminalManager) PIDs() []int {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []int
	for _, ts := range m.terms {
		select {
		case <-ts.done:
			continue
		default:
		}
		if ts.cmd != nil && ts.cmd.Process != nil {
			out = append(out, ts.cmd.Process.Pid)
		}
	}
	return out
}
