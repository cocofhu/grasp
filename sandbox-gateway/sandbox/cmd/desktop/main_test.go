package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"backend/internal/desktop"
)

func TestRunUsageAndUnknown(t *testing.T) {
	var errb bytes.Buffer
	if code := run(nil, nil, io.Discard, &errb); code != 2 || !strings.Contains(errb.String(), "usage") {
		t.Fatalf("usage: %d %q", code, errb.String())
	}
	if code := run([]string{"nope"}, nil, io.Discard, io.Discard); code != 2 {
		t.Fatalf("unknown: %d", code)
	}
	if code := run([]string{"launcher", "-bogus"}, nil, io.Discard, io.Discard); code != 2 {
		t.Fatalf("bad flag: %d", code)
	}
	if code := run([]string{"launcher", "-addr", "0.0.0.0:0"}, nil, io.Discard, io.Discard); code != 1 {
		t.Fatalf("non-loopback must be refused: %d", code)
	}
	if code := run([]string{"vnc-ws", "-bogus"}, nil, io.Discard, io.Discard); code != 2 {
		t.Fatalf("vnc-ws bad flag: %d", code)
	}
	if code := run([]string{"vnc-ws", "-addr", "bad"}, nil, io.Discard, io.Discard); code != 1 {
		t.Fatalf("vnc-ws bad addr: %d", code)
	}
}

func TestRunVNCWSServesUntilSignal(t *testing.T) {
	addr := "127.0.0.1:" + strconv.Itoa(freePort(t))
	done := make(chan int, 1)
	go func() { done <- run([]string{"vnc-ws", "-addr", addr, "-vnc", "127.0.0.1:1"}, nil, io.Discard, io.Discard) }()
	var resp *http.Response
	var err error
	for i := 0; i < 50; i++ {
		resp, err = http.Get("http://" + addr + "/")
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("vnc-ws did not stop")
	}
}

func TestRunMCPOnStdio(t *testing.T) {
	t.Setenv("GRASP_DESKTOP_MARKER", filepath.Join(t.TempDir(), "absent"))
	var out bytes.Buffer
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"screenshot"}}` + "\n")
	if code := run([]string{"mcp"}, in, &out, io.Discard); code != 0 {
		t.Fatalf("mcp exit %d", code)
	}
	if !strings.Contains(out.String(), "当前镜像没有桌面") {
		t.Fatalf("out = %s", out.String())
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestServeLauncherLoopback(t *testing.T) {
	dir := t.TempDir()
	paths := desktop.Paths{Marker: filepath.Join(dir, "m"), RunDir: dir, Apps: filepath.Join(dir, "apps.json")}
	_ = os.WriteFile(paths.Apps, []byte(`[]`), 0o644)
	port := freePort(t)
	addr := "127.0.0.1:" + strconv.Itoa(port)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveLauncher(ctx, addr, paths, desktop.ExecRunner{}) }()
	var resp *http.Response
	var err error
	for i := 0; i < 50; i++ {
		resp, err = http.Get("http://" + addr + "/apps")
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.TrimSpace(string(body)) != "[]" {
		t.Fatalf("apps = %q", body)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := serveLauncher(context.Background(), "bad", paths, desktop.ExecRunner{}); err == nil {
		t.Fatal("bad addr")
	}
	if err := serveLauncher(context.Background(), "localhost:x", paths, desktop.ExecRunner{}); err == nil {
		t.Fatal("bad port")
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	if err := serveLauncher(context.Background(), ln.Addr().String(), paths, desktop.ExecRunner{}); err == nil {
		t.Fatal("busy port")
	}
}
