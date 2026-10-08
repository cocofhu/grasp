package service

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Liveness decides whether a quiet Agent is still working. Besides provider
// events, a sample counts as activity when the Agent's process tree used CPU
// or did IO since the previous sample, so a long build, test run or model call
// is never mistaken for a stuck turn.
const (
	defaultLivenessCPU = 300 * time.Millisecond
	defaultLivenessIO  = 4 << 10

	envLivenessCPU = "SANDBOX_LIVENESS_CPU_MS"
	envLivenessIO  = "SANDBOX_LIVENESS_IO_BYTES"

	// loopRepeatLimit identical tool calls in a row mean the Agent is going
	// round in circles.
	loopRepeatLimit = 8

	// clkTck is USER_HZ, the unit of /proc/<pid>/stat CPU times (100 on every
	// Linux the sandbox images run).
	clkTck = 100
)

// Sampling and heartbeat cadence. Vars so tests can shrink them.
var (
	livenessSampleEvery    = 30 * time.Second
	livenessHeartbeatEvery = 60 * time.Second
)

func livenessThresholdsFromEnv() (cpu time.Duration, io int64) {
	cpu, io = defaultLivenessCPU, defaultLivenessIO
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(envLivenessCPU))); err == nil && n >= 0 {
		cpu = time.Duration(n) * time.Millisecond
	}
	if n, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(envLivenessIO)), 10, 64); err == nil && n >= 0 {
		io = n
	}
	return cpu, io
}

// treeUsage is the cumulative CPU and IO of a process tree at one moment.
type treeUsage struct {
	cpu  time.Duration
	io   int64
	pids map[int]procUsage
	// docker is true when a docker / docker-compose client runs in the tree.
	docker bool
}

// procUsage is one process's cumulative counters. CPU includes reaped
// children (cutime/cstime), so a short command that started and exited
// between two samples is still counted through its parent.
type procUsage struct {
	cpu   time.Duration
	io    int64
	start uint64 // starttime in clock ticks since boot
}

// processSampler reads a process tree's usage. Injectable for tests.
type processSampler interface {
	sample(roots []int) (treeUsage, error)
}

// procSampler reads /proc. While a docker client runs in the tree the work
// happens in dockerd and the containers, so their trees are counted too.
type procSampler struct{ root string }

type procStat struct {
	pid, ppid int
	comm      string
	usage     procUsage
}

func (s procSampler) sample(roots []int) (treeUsage, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return treeUsage{}, err
	}
	all := map[int]procStat{}
	children := map[int][]int{}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		st, ok := s.stat(pid)
		if !ok {
			continue
		}
		all[pid] = st
		children[st.ppid] = append(children[st.ppid], pid)
	}
	out := treeUsage{pids: map[int]procUsage{}}
	var walk func(pid int)
	walk = func(pid int) {
		st, ok := all[pid]
		if !ok {
			return
		}
		if _, seen := out.pids[pid]; seen {
			return
		}
		st.usage.io = s.io(pid)
		out.pids[pid] = st.usage
		if st.comm == "docker" || st.comm == "docker-compose" {
			out.docker = true
		}
		for _, c := range children[pid] {
			walk(c)
		}
	}
	for _, r := range roots {
		if r < 0 {
			for _, c := range children[-r] {
				walk(c)
			}
			continue
		}
		walk(r)
	}
	if out.docker {
		for pid, st := range all {
			if st.comm == "dockerd" || strings.HasPrefix(st.comm, "containerd-shim") {
				walk(pid)
			}
		}
	}
	for _, u := range out.pids {
		out.cpu += u.cpu
		out.io += u.io
	}
	return out, nil
}

func (s procSampler) stat(pid int) (procStat, bool) {
	raw, err := os.ReadFile(filepath.Join(s.root, strconv.Itoa(pid), "stat"))
	if err != nil {
		return procStat{}, false
	}
	return parseProcStat(pid, raw)
}

// parseProcStat reads ppid, comm and CPU times from /proc/<pid>/stat. comm is
// parenthesized and may itself contain spaces or parentheses.
func parseProcStat(pid int, raw []byte) (procStat, bool) {
	open, end := bytes.IndexByte(raw, '('), bytes.LastIndexByte(raw, ')')
	if open < 0 || end < open {
		return procStat{}, false
	}
	f := strings.Fields(string(raw[end+1:]))
	// f[0] is field 3 (state); utime..cstime are fields 14-17, starttime 22.
	if len(f) < 20 {
		return procStat{}, false
	}
	num := func(i int) uint64 {
		n, _ := strconv.ParseUint(f[i], 10, 64)
		return n
	}
	ticks := num(11) + num(12) + num(13) + num(14)
	ppid, _ := strconv.Atoi(f[1])
	return procStat{
		pid: pid, ppid: ppid, comm: string(raw[open+1 : end]),
		usage: procUsage{cpu: time.Duration(ticks) * time.Second / clkTck, start: num(19)},
	}, true
}

// io returns rchar+wchar: every byte read or written, sockets included, so a
// CLI streaming a long model response counts as active.
func (s procSampler) io(pid int) int64 {
	raw, err := os.ReadFile(filepath.Join(s.root, strconv.Itoa(pid), "io"))
	if err != nil {
		return 0
	}
	var total int64
	for _, line := range strings.Split(string(raw), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok || (k != "rchar" && k != "wchar") {
			continue
		}
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		total += n
	}
	return total
}

// usageDelta is the CPU and IO a tree used between two samples. Processes
// that already existed but only now joined the tree (e.g. dockerd once a
// docker client starts) contribute from this sample on, not their lifetime.
func usageDelta(prev, cur treeUsage, prevMaxStart uint64) (cpu time.Duration, io int64) {
	for pid, u := range cur.pids {
		p, ok := prev.pids[pid]
		switch {
		case ok && p.start == u.start:
			cpu += max(0, u.cpu-p.cpu)
			io += max(0, u.io-p.io)
		case u.start > prevMaxStart:
			cpu += u.cpu
			io += u.io
		}
	}
	return cpu, io
}

func maxStart(u treeUsage) uint64 {
	var m uint64
	for _, p := range u.pids {
		m = max(m, p.start)
	}
	return m
}

// agentPIDReporter is implemented by sessions that know their CLI processes.
type agentPIDReporter interface {
	AgentPIDs() []int
}

// livenessMonitor samples one turn's Agent and reports heartbeats.
type livenessMonitor struct {
	sampler   processSampler
	roots     func() []int
	cpuMin    time.Duration
	ioMin     int64
	disabled  bool
	prev      treeUsage
	prevStart uint64
	havePrev  bool

	// Accumulated since the last heartbeat.
	beatCPU time.Duration
	beatIO  int64
	active  bool
}

var procWarnOnce sync.Once

// check samples the tree and reports whether it was active since the last
// sample. A sampler failure disables CPU/IO liveness for the turn, leaving
// provider events as the only signal.
func (m *livenessMonitor) check() bool {
	if m.disabled || m.sampler == nil {
		return false
	}
	cur, err := m.sampler.sample(m.roots())
	if err != nil {
		m.disabled = true
		procWarnOnce.Do(func() {
			log.Printf("bridge: 无法读取进程 CPU/IO（%v），Agent 活性只看输出事件", err)
		})
		return false
	}
	if !m.havePrev {
		m.prev, m.prevStart, m.havePrev = cur, maxStart(cur), true
		return false
	}
	cpu, io := usageDelta(m.prev, cur, m.prevStart)
	m.prev, m.prevStart = cur, max(m.prevStart, maxStart(cur))
	m.beatCPU += cpu
	m.beatIO += io
	active := cpu >= m.cpuMin || io >= m.ioMin
	m.active = m.active || active
	return active
}

// takeBeat returns and resets what accumulated since the last heartbeat.
func (m *livenessMonitor) takeBeat() (active bool, cpu time.Duration, io int64) {
	active, cpu, io = m.active, m.beatCPU, m.beatIO
	m.active, m.beatCPU, m.beatIO = false, 0, 0
	return active, cpu, io
}

// toolLoop tracks consecutive identical tool calls.
type toolLoop struct {
	mu       sync.Mutex
	last     string
	repeats  int
	lastTool string
}

// note records one event and reports whether it completed a loop.
func (l *toolLoop) note(ev json.RawMessage) bool {
	title, key, ok := toolCallKey(ev)
	if !ok {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lastTool = title
	if key == l.last {
		l.repeats++
	} else {
		l.last, l.repeats = key, 1
	}
	return l.repeats >= loopRepeatLimit
}

func (l *toolLoop) reset() {
	l.mu.Lock()
	l.last, l.repeats = "", 0
	l.mu.Unlock()
}

func (l *toolLoop) tool() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lastTool
}

// toolCallKey extracts a tool_call start from a session_update frame (oneshot
// envelope or ACP session/update notification). The key is the title plus the
// raw input, so the same command with different arguments is not a loop.
func toolCallKey(ev json.RawMessage) (title, key string, ok bool) {
	var f struct {
		Update *toolUpdate `json:"update"`
		Params *struct {
			Update *toolUpdate `json:"update"`
		} `json:"params"`
	}
	if json.Unmarshal(ev, &f) != nil {
		return "", "", false
	}
	u := f.Update
	if u == nil && f.Params != nil {
		u = f.Params.Update
	}
	if u == nil || u.SessionUpdate != "tool_call" {
		return "", "", false
	}
	return u.Title, u.Title + "\x00" + string(u.RawInput), true
}

type toolUpdate struct {
	SessionUpdate string          `json:"sessionUpdate"`
	Title         string          `json:"title"`
	RawInput      json.RawMessage `json:"rawInput"`
}
