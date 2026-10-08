package oneshot

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"backend/internal/provider"
)

// bgFake is cursor-agent with a background service: the CLI starts a shell in
// its own session, reports it as a background tool result, and only reports
// its result and exits once that shell has ended.
type bgFake struct{ baseFake }

func (bgFake) Args(_ provider.OpenOptions, _, _ string) []string {
	return []string{"sh", "-c", `setsid sleep 30 </dev/null >/dev/null 2>&1 & pid=$!
sleep 0.2
printf 'use:t1\nbg:t1:%s\ntext:serving\n' "$pid"
wait "$pid"
echo done`}
}

func (bgFake) ParseLine(line []byte) ParseResult {
	s := string(line)
	switch {
	case strings.HasPrefix(s, "use:"):
		return ParseResult{Msgs: []Msg{{Kind: KindToolUse, ToolCallID: strings.TrimPrefix(s, "use:"), ToolTitle: "serve"}}}
	case strings.HasPrefix(s, "bg:"):
		f := strings.Split(s, ":")
		pid, _ := strconv.Atoi(f[2])
		return ParseResult{Msgs: []Msg{{Kind: KindToolResult, ToolCallID: f[1], BackgroundPID: pid}}}
	case strings.HasPrefix(s, "text:"):
		return ParseResult{Msgs: []Msg{{Kind: KindText, Text: strings.TrimPrefix(s, "text:")}}}
	case s == "done":
		return ParseResult{StopReason: "end_turn"}
	}
	return ParseResult{}
}

func TestOneShotEndsBackgroundShellAfterQuietGrace(t *testing.T) {
	t.Setenv(envBackgroundGrace, "300ms")
	sess, err := NewProvider(bgFake{}).Open(context.Background(), context.Background(),
		provider.OpenOptions{Cwd: t.TempDir()}, func(json.RawMessage) {}, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sess.Close()

	start := time.Now()
	res, err := sess.Prompt(context.Background(), "serve the page", nil)
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if res.StopReason != "end_turn" {
		t.Fatalf("stop=%q, want end_turn", res.StopReason)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("turn waited on the background shell: %s", d)
	}
	if sess.(*engine).WaitingOnBackground() {
		t.Fatal("still waiting on background after the turn")
	}
}

func TestOneShotKeepsBackgroundShellWhenGraceDisabled(t *testing.T) {
	t.Setenv(envBackgroundGrace, "0")
	sess, err := NewProvider(bgFake{}).Open(context.Background(), context.Background(),
		provider.OpenOptions{Cwd: t.TempDir()}, func(json.RawMessage) {}, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sess.Close()

	e := sess.(*engine)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		_, _ = sess.Prompt(ctx, "serve the page", nil)
		close(done)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for !e.WaitingOnBackground() {
		if time.Now().After(deadline) {
			t.Fatal("never reported waiting on the background shell")
		}
		time.Sleep(20 * time.Millisecond)
	}
	bg := e.bg.Load()
	bg.mu.Lock()
	shells := append([]bgShell(nil), bg.shells...)
	bg.mu.Unlock()
	defer terminateShells(shells)
	select {
	case <-done:
		t.Fatal("turn ended although the background cleanup is disabled")
	case <-time.After(500 * time.Millisecond):
	}
	cancel()
	<-done
}

// lingerFake reports its result and then never exits.
type lingerFake struct{ baseFake }

func (lingerFake) Args(_ provider.OpenOptions, _, _ string) []string {
	return []string{"sh", "-c", `echo text:hi; echo done; sleep 30`}
}

func (lingerFake) ParseLine(line []byte) ParseResult { return bgFake{}.ParseLine(line) }

func TestOneShotKillsCLILingeringAfterResult(t *testing.T) {
	defer func(d time.Duration) { resultExitGrace = d }(resultExitGrace)
	resultExitGrace = 200 * time.Millisecond
	defer func(d time.Duration) { outputDrainGrace = d }(outputDrainGrace)
	outputDrainGrace = 100 * time.Millisecond

	sess, err := NewProvider(lingerFake{}).Open(context.Background(), context.Background(),
		provider.OpenOptions{Cwd: t.TempDir()}, func(json.RawMessage) {}, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sess.Close()
	start := time.Now()
	res, err := sess.Prompt(context.Background(), "hi", nil)
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if res.StopReason != "end_turn" {
		t.Fatalf("stop=%q, want end_turn", res.StopReason)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("lingering CLI was not killed: %s", d)
	}
}

func startProc(t *testing.T, name string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", name, err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	})
	return cmd
}

// childPID waits for the pid a shell wrote to file.
func childPID(t *testing.T, file string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(file); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no pid in %s", file)
	return 0
}

func TestOwnedBackgroundShell(t *testing.T) {
	dir := t.TempDir()
	own, same := filepath.Join(dir, "own"), filepath.Join(dir, "same")
	cli := startProc(t, "sh", "-c", `setsid sleep 30 </dev/null >/dev/null 2>&1 & echo $! >`+own+`
sleep 30 & echo $! >`+same+`
wait`)
	stranger := startProc(t, "sleep", "30")
	ownPID, samePID := childPID(t, own), childPID(t, same)
	cliPID := cli.Process.Pid

	sh, ok := ownedBackgroundShell(cliPID, ownPID)
	if !ok || sh.pid != ownPID || !sh.alive() {
		t.Fatalf("own session child not adopted: %+v ok=%v", sh, ok)
	}
	if _, ok := ownedBackgroundShell(cliPID, samePID); ok {
		t.Fatal("adopted a child in the CLI's own process group")
	}
	if _, ok := ownedBackgroundShell(cliPID, stranger.Process.Pid); ok {
		t.Fatal("adopted a process that is not the CLI's descendant")
	}
	if _, ok := ownedBackgroundShell(cliPID, cliPID); ok {
		t.Fatal("adopted the CLI itself")
	}
	if (bgShell{pid: ownPID, start: sh.start + 1}).alive() {
		t.Fatal("a different start time must not count as the same shell")
	}

	b := newBackgroundShells(cliPID, time.Hour)
	b.toolStarted("a")
	b.toolFinished("a", ownPID)
	if !b.waiting() {
		t.Fatal("not waiting with a live background shell and no tool running")
	}
	b.toolStarted("b")
	if b.waiting() || b.due(time.Now().Add(2*time.Hour)) != nil {
		t.Fatal("a foreground tool is running")
	}
	b.toolFinished("b", 0)
	if b.due(time.Now()) != nil {
		t.Fatal("due before the grace")
	}
	shells := b.due(time.Now().Add(2 * time.Hour))
	if len(shells) != 1 {
		t.Fatalf("due = %+v", shells)
	}
	terminateShells(shells)
	if sh.alive() || b.waiting() {
		t.Fatal("background shell survived terminate")
	}
}

func TestReadProcStat(t *testing.T) {
	root := t.TempDir()
	defer func(r string) { procRoot = r }(procRoot)
	procRoot = root
	write := func(pid int, s string) {
		_ = os.MkdirAll(filepath.Join(root, strconv.Itoa(pid)), 0o755)
		_ = os.WriteFile(filepath.Join(root, strconv.Itoa(pid), "stat"), []byte(s), 0o644)
	}
	tail := " 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 777 0 0\n"
	write(10, "10 (a (b) c) S 1 10 10"+tail)
	write(11, "11 (z) Z 10 11 11"+tail)
	write(12, "12 (short) S 1")
	st, ok := readProcStat(10)
	if !ok || st.ppid != 1 || st.pgid != 10 || st.start != 777 || st.zombie {
		t.Fatalf("got %+v ok=%v", st, ok)
	}
	if st, ok := readProcStat(11); !ok || !st.zombie {
		t.Fatalf("zombie: %+v ok=%v", st, ok)
	}
	for _, pid := range []int{12, 13} {
		if _, ok := readProcStat(pid); ok {
			t.Fatalf("pid %d parsed", pid)
		}
	}
}

func TestBackgroundGraceFromEnv(t *testing.T) {
	for v, want := range map[string]time.Duration{
		"":     defaultBackgroundGrace,
		"0":    0,
		"45":   45 * time.Second,
		"90s":  90 * time.Second,
		"-3":   0,
		"oops": defaultBackgroundGrace,
		"-1m":  defaultBackgroundGrace,
	} {
		t.Setenv(envBackgroundGrace, v)
		if got := backgroundGraceFromEnv(); got != want {
			t.Errorf("%q: got %s, want %s", v, got, want)
		}
	}
}
