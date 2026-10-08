package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/provider"
)

func writeProc(t *testing.T, root string, pid, ppid int, comm string, ticks, start uint64, rchar int64) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Fields after comm: state ppid pgrp session tty tpgid flags minflt cminflt
	// majflt cmajflt utime stime cutime cstime priority nice threads itreal starttime
	stat := fmt.Sprintf("%d (%s) S %d 1 1 0 -1 0 0 0 0 0 %d 0 0 0 20 0 1 0 %d 0 0\n", pid, comm, ppid, ticks, start)
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0o644); err != nil {
		t.Fatal(err)
	}
	io := fmt.Sprintf("rchar: %d\nwchar: 0\nsyscr: 1\nread_bytes: 0\n", rchar)
	if err := os.WriteFile(filepath.Join(dir, "io"), []byte(io), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseProcStatOddComm(t *testing.T) {
	raw := []byte("42 (node (worker) x) R 7 1 1 0 -1 0 0 0 0 0 150 50 20 30 20 0 1 0 999 0 0")
	st, ok := parseProcStat(42, raw)
	if !ok || st.ppid != 7 || st.comm != "node (worker) x" {
		t.Fatalf("stat = %+v ok=%v", st, ok)
	}
	if st.usage.cpu != 2500*time.Millisecond || st.usage.start != 999 {
		t.Fatalf("usage = %+v", st.usage)
	}
	if _, ok := parseProcStat(1, []byte("garbage")); ok {
		t.Fatal("garbage must not parse")
	}
	if _, ok := parseProcStat(1, []byte("1 (x) S 0 1")); ok {
		t.Fatal("short stat must not parse")
	}
}

func TestProcSamplerTree(t *testing.T) {
	root := t.TempDir()
	writeProc(t, root, 10, 1, "codex", 100, 5, 1000)
	writeProc(t, root, 11, 10, "bash", 50, 6, 10)
	writeProc(t, root, 12, 11, "go", 300, 7, 0)
	writeProc(t, root, 20, 1, "vite", 9999, 3, 99999) // detached service
	writeProc(t, root, 30, 1, "dockerd", 400, 2, 0)
	writeProc(t, root, 31, 1, "containerd-shim-runc-v2", 10, 2, 0)
	writeProc(t, root, 32, 31, "postgres", 70, 2, 0)
	if err := os.WriteFile(filepath.Join(root, "self"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	s := procSampler{root: root}

	u, err := s.sample([]int{10})
	if err != nil {
		t.Fatal(err)
	}
	if len(u.pids) != 3 || u.cpu != 4500*time.Millisecond || u.io != 1010 || u.docker {
		t.Fatalf("agent tree = %+v", u)
	}

	writeProc(t, root, 13, 11, "docker", 1, 8, 0)
	u, _ = s.sample([]int{10})
	if !u.docker || len(u.pids) != 7 {
		t.Fatalf("a docker client must pull in dockerd and the containers: %+v", u)
	}

	u, _ = s.sample([]int{-1})
	if _, ok := u.pids[1]; ok || len(u.pids) < 6 {
		t.Fatalf("negative root samples the children only: %+v", u.pids)
	}

	if _, err := (procSampler{root: filepath.Join(root, "missing")}).sample([]int{10}); err == nil {
		t.Fatal("missing /proc must fail")
	}
}

func TestUsageDelta(t *testing.T) {
	prev := treeUsage{pids: map[int]procUsage{
		1: {cpu: time.Second, io: 100, start: 10},
		2: {cpu: time.Second, io: 100, start: 11},
		3: {cpu: 5 * time.Second, io: 0, start: 12},
	}}
	cur := treeUsage{pids: map[int]procUsage{
		1: {cpu: 1500 * time.Millisecond, io: 600, start: 10}, // grew
		2: {cpu: 200 * time.Millisecond, io: 0, start: 50},    // pid reused by a new process
		4: {cpu: 300 * time.Millisecond, io: 7, start: 40},    // started since the last sample
		5: {cpu: time.Hour, io: 1 << 30, start: 1},            // long-running daemon joining the set
	}}
	cpu, io := usageDelta(prev, cur, maxStart(prev))
	if cpu != time.Second || io != 507 {
		t.Fatalf("delta cpu=%s io=%d", cpu, io)
	}
}

type fakeSampler struct {
	mu   sync.Mutex
	cpu  time.Duration
	io   int64
	err  error
	step time.Duration
}

func (f *fakeSampler) sample([]int) (treeUsage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return treeUsage{}, f.err
	}
	f.cpu += f.step
	return treeUsage{cpu: f.cpu, io: f.io, pids: map[int]procUsage{1: {cpu: f.cpu, io: f.io, start: 1}}}, nil
}

func (f *fakeSampler) setStep(d time.Duration) {
	f.mu.Lock()
	f.step = d
	f.mu.Unlock()
}

func TestLivenessMonitorCheck(t *testing.T) {
	fs := &fakeSampler{step: 500 * time.Millisecond}
	m := &livenessMonitor{sampler: fs, roots: func() []int { return []int{1} }, cpuMin: 300 * time.Millisecond, ioMin: 4096}
	if m.check() {
		t.Fatal("first sample is the baseline")
	}
	if !m.check() {
		t.Fatal("500ms CPU per sample is activity")
	}
	fs.setStep(100 * time.Millisecond)
	if m.check() {
		t.Fatal("100ms CPU per sample is below the threshold")
	}
	fs.io = 10 << 10
	if !m.check() {
		t.Fatal("10 KiB IO per sample is activity")
	}
	active, cpu, io := m.takeBeat()
	if !active || cpu != 700*time.Millisecond || io != 10<<10 {
		t.Fatalf("beat active=%v cpu=%s io=%d", active, cpu, io)
	}
	if a, c, i := m.takeBeat(); a || c != 0 || i != 0 {
		t.Fatal("takeBeat must reset")
	}
	fs.err = errors.New("no /proc")
	if m.check() || !m.disabled {
		t.Fatal("a sampler error disables CPU/IO liveness")
	}
	if (&livenessMonitor{}).check() {
		t.Fatal("no sampler means no CPU/IO liveness")
	}
}

type bgWaitSess struct {
	recordingSess
	waiting atomic.Bool
}

func (s *bgWaitSess) WaitingOnBackground() bool { return s.waiting.Load() }

// While the CLI only waits on background tasks its CPU / IO is not work.
func TestLivenessPausedWhileWaitingOnBackground(t *testing.T) {
	sess := &bgWaitSess{recordingSess: recordingSess{pids: []int{1}}}
	b := newTestBridge(sess)
	fs := &fakeSampler{step: time.Second, io: 0}
	b.sampler = fs
	m := b.newLivenessMonitor(sess)
	m.check()
	if !m.check() {
		t.Fatal("CPU while working is activity")
	}
	sess.waiting.Store(true)
	fs.io = 1 << 20
	if m.check() {
		t.Fatal("CPU / IO while waiting on background tasks must not count")
	}
	_, cpu, io := m.takeBeat()
	if cpu != time.Second || io != 0 {
		t.Fatalf("paused sample leaked into the beat: cpu=%s io=%d", cpu, io)
	}
	sess.waiting.Store(false)
	if !m.check() {
		t.Fatal("activity counts again once the wait is over")
	}
	if _, cpu, io := m.takeBeat(); cpu != time.Second || io != 0 {
		t.Fatalf("baseline not advanced while paused: cpu=%s io=%d", cpu, io)
	}
}

func TestLivenessThresholdsFromEnv(t *testing.T) {
	t.Setenv(envLivenessCPU, "")
	t.Setenv(envLivenessIO, "")
	if cpu, io := livenessThresholdsFromEnv(); cpu != defaultLivenessCPU || io != defaultLivenessIO {
		t.Fatalf("defaults cpu=%s io=%d", cpu, io)
	}
	t.Setenv(envLivenessCPU, "1000")
	t.Setenv(envLivenessIO, "65536")
	if cpu, io := livenessThresholdsFromEnv(); cpu != time.Second || io != 65536 {
		t.Fatalf("env cpu=%s io=%d", cpu, io)
	}
}

func toolEvent(title, input string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"type": "session_update", "update": map[string]any{
		"sessionUpdate": "tool_call", "title": title, "rawInput": json.RawMessage(input),
	}})
	return b
}

func TestToolLoop(t *testing.T) {
	var l toolLoop
	for i := 1; i < loopRepeatLimit; i++ {
		if l.note(toolEvent("go test", `{"cmd":"go test ./..."}`)) {
			t.Fatalf("loop reported after %d calls", i)
		}
	}
	if !l.note(toolEvent("go test", `{"cmd":"go test ./..."}`)) {
		t.Fatal("8 identical calls are a loop")
	}
	if l.tool() != "go test" {
		t.Fatalf("lastTool = %q", l.tool())
	}
	l.reset()
	for i := 0; i < 2*loopRepeatLimit; i++ {
		if l.note(toolEvent("go test", fmt.Sprintf(`{"cmd":"go test ./pkg%d"}`, i%2))) {
			t.Fatal("alternating arguments are not a loop")
		}
	}
	acp := json.RawMessage(`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call","title":"ls"}}}`)
	if title, _, ok := toolCallKey(acp); !ok || title != "ls" {
		t.Fatalf("ACP tool_call: %q %v", title, ok)
	}
	for _, ev := range []string{`{"update":{"sessionUpdate":"agent_message_chunk"}}`, `not json`, `{}`} {
		if _, _, ok := toolCallKey(json.RawMessage(ev)); ok {
			t.Fatalf("%s is not a tool call", ev)
		}
	}
}

// recordingSess blocks every prompt until cancelled and records its text.
type recordingSess struct {
	stubSess
	mu      sync.Mutex
	texts   []string
	pids    []int
	prompts atomic.Int32
}

func (s *recordingSess) Prompt(ctx context.Context, text string, _ []provider.PromptImage) (provider.TurnResult, error) {
	s.mu.Lock()
	s.texts = append(s.texts, text)
	s.mu.Unlock()
	s.prompts.Add(1)
	<-ctx.Done()
	return provider.TurnResult{}, ctx.Err()
}

func (s *recordingSess) AgentPIDs() []int { return s.pids }

func (s *recordingSess) text(i int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i >= len(s.texts) {
		return ""
	}
	return s.texts[i]
}

func shrinkLiveness(t *testing.T, sample, beat time.Duration) {
	t.Helper()
	oldS, oldB := livenessSampleEvery, livenessHeartbeatEvery
	livenessSampleEvery, livenessHeartbeatEvery = sample, beat
	t.Cleanup(func() { livenessSampleEvery, livenessHeartbeatEvery = oldS, oldB })
}

func livenessData(f wsFrame) map[string]any {
	var d map[string]any
	_ = json.Unmarshal(f.Data, &d)
	return d
}

// A quiet Agent whose process tree keeps using CPU is not stopped; once the
// CPU stops it is resumed once, then stopped as stuck.
func TestWatchdogCPUKeepsQuietTurnAlive(t *testing.T) {
	shrinkLiveness(t, 10*time.Millisecond, 25*time.Millisecond)
	sess := &recordingSess{stubSess: stubSess{id: "s1"}, pids: []int{4242}}
	b := newTestBridge(sess)
	fs := &fakeSampler{step: time.Second}
	b.sampler = fs
	b.turnIdle, b.turnMax = 80*time.Millisecond, 0
	c := dialBridge(t, b)

	_ = b.ChatWithLimits("build", "op-A", "chat", nil, 0, 0)
	beat := readUntil(t, c, func(f wsFrame) bool { return f.Op == "liveness" })
	if d := livenessData(beat); beat.OpID != "op-A" || d["active"] != true || d["limitSec"] != float64(0) {
		t.Fatalf("heartbeat = %+v %v", beat, d)
	}
	time.Sleep(250 * time.Millisecond)
	if n := sess.prompts.Load(); n != 1 {
		t.Fatalf("a busy Agent must not be resumed, prompts=%d", n)
	}

	fs.setStep(0)
	done := readUntil(t, c, func(f wsFrame) bool { return f.Op == "event" && dataType(f) == "prompt_done" })
	if stopReasonOf(done) != provider.StopReasonStuck {
		t.Fatalf("stop = %q", stopReasonOf(done))
	}
	if sess.prompts.Load() != 2 || !strings.Contains(sess.text(1), "CPU 或磁盘活动") {
		t.Fatalf("idle must resume once with the stall hint, prompts=%d text=%q", sess.prompts.Load(), sess.text(1))
	}
	waitIdle(t, b)
}

// The same tool call over and over is resumed once with a change-approach
// hint, and stopped as stuck when it happens again.
func TestWatchdogToolLoop(t *testing.T) {
	sess := &recordingSess{stubSess: stubSess{id: "s1"}}
	b := newTestBridge(sess)
	b.sampler = nil
	b.turnIdle, b.turnMax = time.Hour, 0
	c := dialBridge(t, b)

	_ = b.ChatWithLimits("fix", "op-A", "chat", nil, 0, 0)
	loop := func() {
		for i := 0; i < loopRepeatLimit; i++ {
			b.touchActiveTurn(toolEvent("npm test", `{"cmd":"npm test"}`))
		}
	}
	waitPrompts(t, sess, 1)
	loop()
	waitPrompts(t, sess, 2)
	if !strings.Contains(sess.text(1), "反复执行同一个工具调用") {
		t.Fatalf("loop resume hint = %q", sess.text(1))
	}
	loop()
	et := readUntil(t, c, func(f wsFrame) bool { return f.Op == "event" && errorText(f) != "" })
	if !strings.Contains(errorText(et), "同一个工具调用（npm test）") || !strings.Contains(errorText(et), "卡住") {
		t.Fatalf("stuck explanation = %q", errorText(et))
	}
	done := readUntil(t, c, func(f wsFrame) bool { return f.Op == "event" && dataType(f) == "prompt_done" })
	if stopReasonOf(done) != provider.StopReasonStuck {
		t.Fatalf("stop = %q", stopReasonOf(done))
	}
	waitIdle(t, b)
}

// The chat frame's idleSec replaces the bridge default for that turn.
func TestTurnLimitsFromChat(t *testing.T) {
	b := &Bridge{turnIdle: 10 * time.Minute, turnMax: time.Hour}
	idle, max := b.turnLimits(queuedPrompt{IdleTimeout: 20 * time.Minute, MaxDuration: 3 * time.Hour})
	if idle != 20*time.Minute || max != 3*time.Hour {
		t.Fatalf("idle=%s max=%s", idle, max)
	}
	idle, max = b.turnLimits(queuedPrompt{})
	if idle != 10*time.Minute || max != time.Hour {
		t.Fatalf("defaults idle=%s max=%s", idle, max)
	}
}

func TestLivenessRoots(t *testing.T) {
	if got := livenessRoots(&recordingSess{pids: []int{7, 8}})(); len(got) != 2 || got[0] != 7 {
		t.Fatalf("reported roots = %v", got)
	}
	if got := livenessRoots(&stubSess{})(); len(got) != 1 || got[0] != -os.Getpid() {
		t.Fatalf("fallback roots = %v", got)
	}
}

func TestTimeoutStopReason(t *testing.T) {
	if provider.TimeoutStopReason(fmt.Errorf("%w: x", provider.ErrTurnStuck)) != provider.StopReasonStuck {
		t.Fatal("stuck cause")
	}
	if provider.TimeoutStopReason(fmt.Errorf("%w: x", provider.ErrTurnTimeout)) != provider.StopReasonTimeout {
		t.Fatal("timeout cause")
	}
	if !errors.Is(provider.ErrTurnStuck, provider.ErrTurnTimeout) {
		t.Fatal("stuck must still be a timeout for transports")
	}
}

func waitPrompts(t *testing.T, s *recordingSess, n int32) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for s.prompts.Load() < n {
		if time.Now().After(deadline) {
			t.Fatalf("prompts = %d, want %d", s.prompts.Load(), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
