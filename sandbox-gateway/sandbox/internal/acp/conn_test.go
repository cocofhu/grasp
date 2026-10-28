package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// The helper deliberately stays alive after corrupting/closing stdout. This
// reproduces a CLI writing human text into its ACP protocol stream.
func TestConnHelperProcess(t *testing.T) {
	mode := os.Getenv("GRASP_ACP_CONN_HELPER")
	if mode == "" {
		return
	}
	s := bufio.NewScanner(os.Stdin)
	if !s.Scan() {
		os.Exit(1)
	}
	var req struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(s.Bytes(), &req)
	switch mode {
	case "invalid":
		_, _ = fmt.Fprintln(os.Stdout, "Corrupt protocol output")
	case "eof":
		_ = os.Stdout.Close()
	case "response-eof":
		_, _ = fmt.Fprintf(os.Stdout, "{\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"ok\":true}}\n", req.ID)
		_ = os.Stdout.Close()
	}
	for s.Scan() {
		// Keep stdin open until the parent terminates the failed transport.
	}
	os.Exit(0)
}

func startConnHelper(t *testing.T, mode string) *Conn {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c, err := StartAgent(context.Background(), []string{exe, "-test.run=^TestConnHelperProcess$"}, append(os.Environ(), "GRASP_ACP_CONN_HELPER="+mode))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(); _ = c.Wait() })
	return c
}

func TestStartAgentProgramIsArgvZero(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	const modelArg = "request-body-not-a-program"
	c, err := StartAgent(context.Background(), []string{exe, "-test.run=^TestConnHelperProcess$", modelArg}, append(os.Environ(), "GRASP_ACP_CONN_HELPER=eof"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(); _ = c.Wait() })
	if c.cmd == nil || len(c.cmd.Args) == 0 || c.cmd.Args[0] != exe {
		t.Fatalf("program = %#v", c.cmd)
	}
	if c.cmd.Path == "sh" || strings.HasSuffix(c.cmd.Path, "/sh") || strings.Contains(c.cmd.Path, "bash") {
		t.Fatalf("StartAgent used a shell: %q", c.cmd.Path)
	}
	if c.cmd.Args[len(c.cmd.Args)-1] != modelArg {
		t.Fatalf("model text became the program: %q", c.cmd.Args)
	}
}

func TestConnReaderFailureStopsLiveAgent(t *testing.T) {
	for _, mode := range []string{"invalid", "eof"} {
		t.Run(mode, func(t *testing.T) {
			c := startConnHelper(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if _, err := c.Call(ctx, "session/prompt", nil); err == nil || !strings.Contains(err.Error(), "connection closed") {
				t.Fatalf("in-flight Call: %v", err)
			}
			if mode == "invalid" && (c.ReadErr() == nil || !strings.Contains(c.ReadErr().Error(), "invalid character 'C'")) {
				t.Fatalf("lost protocol error: %v", c.ReadErr())
			}
			if _, err := c.Call(ctx, "session/prompt", nil); err == nil || !strings.Contains(err.Error(), "connection closed") {
				t.Fatalf("subsequent Call: %v", err)
			}
			if err := c.Notify("session/cancel", nil); err == nil || !strings.Contains(err.Error(), "connection closed") {
				t.Fatalf("subsequent Notify: %v", err)
			}
			reaped := make(chan struct{})
			go func() { _ = c.Wait(); close(reaped) }()
			select {
			case <-reaped:
			case <-ctx.Done():
				t.Fatal("failed transport left its Agent alive / Wait blocked")
			}
		})
	}
}

func TestConnKeepsResponseBeforeEOF(t *testing.T) {
	c := startConnHelper(t, "response-eof")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	raw, err := c.Call(ctx, "initialize", nil)
	if err != nil || string(raw) != `{"ok":true}` {
		t.Fatalf("response before EOF: %s, %v", raw, err)
	}
}

type discardWriteCloser struct{ io.Writer }

func (discardWriteCloser) Close() error { return nil }

func TestConnCallRegistrationConcurrentWithClose(t *testing.T) {
	for range 40 {
		c := &Conn{stdin: discardWriteCloser{io.Discard}, pending: make(map[int64]chan json.RawMessage), done: make(chan struct{})}
		start := make(chan struct{})
		var wg sync.WaitGroup
		errs := make(chan error, 32)
		for range 32 {
			wg.Go(func() {
				<-start
				_, err := c.Call(context.Background(), "initialize", nil)
				errs <- err
			})
		}
		wg.Go(func() { <-start; _ = c.Close() })
		close(start)
		finished := make(chan struct{})
		go func() { wg.Wait(); close(finished) }()
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			t.Fatal("a Call registered after shutdown and never returned")
		}
		close(errs)
		for err := range errs {
			if err == nil || !strings.Contains(err.Error(), "connection closed") {
				t.Fatalf("concurrent Call: %v", err)
			}
		}
		c.pendingMu.Lock()
		pending := len(c.pending)
		c.pendingMu.Unlock()
		if pending != 0 {
			t.Fatalf("shutdown retained %d pending calls", pending)
		}
	}
}
