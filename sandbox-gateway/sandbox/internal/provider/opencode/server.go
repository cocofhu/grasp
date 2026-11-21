package opencode

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// server is one `opencode serve` process bound to loopback. Every request
// carries the random password the process was started with, so other
// processes in the sandbox (including the agent's own shell) cannot drive it.
type server struct {
	cmd      *exec.Cmd
	base     string
	password string
	client   *http.Client

	done    chan struct{}
	waitErr error
	tail    *tailBuffer
}

const (
	readyTimeout = 60 * time.Second
	// requestTimeout bounds request/response calls; the event stream is not
	// bounded.
	requestTimeout = 30 * time.Second
)

// startServer runs `opencode serve` on a free loopback port; extra are the
// operator's custom arguments.
func startServer(ctx context.Context, bin, dir string, env, extra []string) (*server, error) {
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	password, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	args := append([]string{"serve", "--hostname", "127.0.0.1", "--port", strconv.Itoa(port)}, extra...)
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = append(append([]string(nil), env...), "OPENCODE_SERVER_PASSWORD="+password)
	// Its own group, so stopping the server also stops the shells it runs.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	tail := &tailBuffer{max: 4096}
	cmd.Stdout, cmd.Stderr = tail, tail
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	s := &server{
		cmd: cmd, base: "http://127.0.0.1:" + strconv.Itoa(port), password: password,
		client: &http.Client{}, done: make(chan struct{}), tail: tail,
	}
	go func() {
		s.waitErr = cmd.Wait()
		close(s.done)
	}()
	return s, nil
}

// ready waits until the server answers its health check and returns the
// version it reports.
func (s *server) ready(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()
	for {
		var health struct {
			Version string `json:"version"`
		}
		if err := s.call(ctx, http.MethodGet, "/global/health", nil, &health); err == nil {
			return health.Version, nil
		}
		select {
		case <-s.done:
			return "", fmt.Errorf("opencode serve exited during startup: %s", s.exitText())
		case <-ctx.Done():
			return "", fmt.Errorf("opencode serve did not become ready: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// httpError is a non-2xx answer.
type httpError struct {
	status int
	body   string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("opencode: HTTP %d: %s", e.status, strings.TrimSpace(e.body))
}

func isNotFound(err error) bool {
	var h *httpError
	return errors.As(err, &h) && h.status == http.StatusNotFound
}

// call sends one request; out, when non-nil, receives the JSON answer.
func (s *server) call(ctx context.Context, method, path string, body, out any) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.base+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.SetBasicAuth("opencode", s.password)
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return &httpError{status: resp.StatusCode, body: truncate(string(raw), 300)}
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// events streams the server's events until ctx ends or the stream breaks.
// connected is closed once the stream is established.
func (s *server) events(ctx context.Context, connected chan<- struct{}, handle func(json.RawMessage)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/event", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth("opencode", s.password)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return &httpError{status: resp.StatusCode}
	}
	close(connected)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 16<<20)
	for scanner.Scan() {
		if data, ok := bytes.CutPrefix(scanner.Bytes(), []byte("data: ")); ok {
			handle(append(json.RawMessage(nil), data...))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return io.EOF
}

func (s *server) stop() {
	select {
	case <-s.done:
		return
	default:
	}
	_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
	<-s.done
}

func (s *server) pid() int { return s.cmd.Process.Pid }

func (s *server) exitText() string {
	var sb strings.Builder
	if s.waitErr != nil {
		var ee *exec.ExitError
		if errors.As(s.waitErr, &ee) {
			fmt.Fprintf(&sb, "退出码=%d", ee.ExitCode())
		} else {
			sb.WriteString(s.waitErr.Error())
		}
	}
	if out := strings.TrimSpace(s.tail.String()); out != "" {
		if sb.Len() > 0 {
			sb.WriteString("；")
		}
		sb.WriteString(out)
	}
	return sb.String()
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// tailBuffer keeps the last max bytes of the server's output for
// diagnostics and mirrors each line to the log.
type tailBuffer struct {
	mu   sync.Mutex
	max  int
	data []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			log.Printf("opencode serve: %s", truncate(line, 500))
		}
	}
	b.data = append(b.data, p...)
	if over := len(b.data) - b.max; over > 0 {
		b.data = b.data[over:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
