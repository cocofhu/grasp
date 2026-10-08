package oneshot

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// cursor-agent -p reports its result and exits only after every background
// shell it launched has ended, so a service started as a background task
// (python3 -m http.server, npm run dev) holds the turn open for good, while the
// CLI keeps rewriting its terminal files and looks busy. Once the stream has
// been quiet for the background grace with no foreground tool running, the
// engine ends those shells; the CLI then gets its task notification, wraps up
// and exits by itself.
const (
	defaultBackgroundGrace = 2 * time.Minute
	envBackgroundGrace     = "SANDBOX_BG_TASK_GRACE"
)

// Vars so tests can shrink them or point them at a fake /proc.
var (
	procRoot            = "/proc"
	bgTerminateWait     = time.Second
	resultExitGrace     = 5 * time.Second
	bgCheckEveryCeiling = 5 * time.Second
)

// BackgroundGrace is SANDBOX_BG_TASK_GRACE: a Go duration ("90s") or plain
// seconds; "0" turns the cleanup off.
func BackgroundGrace() time.Duration {
	v := strings.TrimSpace(os.Getenv(envBackgroundGrace))
	if v == "" {
		return defaultBackgroundGrace
	}
	if n, err := strconv.Atoi(v); err == nil {
		return time.Duration(max(n, 0)) * time.Second
	}
	if d, err := time.ParseDuration(v); err == nil && d >= 0 {
		return d
	}
	log.Printf("oneshot: %s=%q 不是合法的时长，使用默认 %s", envBackgroundGrace, v, defaultBackgroundGrace)
	return defaultBackgroundGrace
}

func backgroundCheckEvery(grace time.Duration) time.Duration {
	return min(max(grace/4, 10*time.Millisecond), bgCheckEveryCeiling)
}

// bgShell is a background shell the CLI owns, pinned by its start time so a
// reused pid is never signalled.
type bgShell struct {
	pid   int
	start uint64
}

// backgroundShells tracks one turn's tool calls and the background shells they
// launched.
type backgroundShells struct {
	cliPID int
	grace  time.Duration

	mu        sync.Mutex
	inFlight  map[string]struct{}
	shells    []bgShell
	lastEvent time.Time
}

func newBackgroundShells(cliPID int, grace time.Duration) *backgroundShells {
	return &backgroundShells{cliPID: cliPID, grace: grace, inFlight: map[string]struct{}{}, lastEvent: time.Now()}
}

// event records any output from the CLI.
func (b *backgroundShells) event() {
	b.mu.Lock()
	b.lastEvent = time.Now()
	b.mu.Unlock()
}

func (b *backgroundShells) toolStarted(id string) {
	b.mu.Lock()
	b.inFlight[id] = struct{}{}
	b.mu.Unlock()
}

// toolFinished closes a call; bgPID > 0 means it left a background shell
// running, which is adopted when it provably belongs to the CLI.
func (b *backgroundShells) toolFinished(id string, bgPID int) {
	var sh bgShell
	owned := false
	if bgPID > 0 {
		sh, owned = ownedBackgroundShell(b.cliPID, bgPID)
		if !owned {
			log.Printf("oneshot: 后台 shell pid=%d 不属于本回合的 Agent 进程，不接管", bgPID)
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.inFlight, id)
	if owned {
		b.shells = append(b.shells, sh)
	}
}

// waiting reports whether the CLI is only waiting on background shells: one is
// still running and no foreground tool is.
func (b *backgroundShells) waiting() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked()
	return len(b.shells) > 0 && len(b.inFlight) == 0
}

// due returns the shells to end now: the stream has been quiet for the grace,
// no foreground tool runs and some background shell still does. It restarts
// the quiet period, so the CLI gets a full grace to react before the next
// round.
func (b *backgroundShells) due(now time.Time) []bgShell {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.grace <= 0 || len(b.inFlight) > 0 || now.Sub(b.lastEvent) < b.grace {
		return nil
	}
	b.pruneLocked()
	if len(b.shells) == 0 {
		return nil
	}
	b.lastEvent = now
	return append([]bgShell(nil), b.shells...)
}

func (b *backgroundShells) pruneLocked() {
	kept := b.shells[:0]
	for _, sh := range b.shells {
		if sh.alive() {
			kept = append(kept, sh)
		}
	}
	b.shells = kept
}

// terminate ends each shell's process group: SIGTERM, then SIGKILL for groups
// whose leader outlives bgTerminateWait.
func terminateShells(shells []bgShell) {
	for _, sh := range shells {
		if sh.alive() {
			_ = syscall.Kill(-sh.pid, syscall.SIGTERM)
		}
	}
	deadline := time.Now().Add(bgTerminateWait)
	for _, sh := range shells {
		for sh.alive() && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if sh.alive() {
			_ = syscall.Kill(-sh.pid, syscall.SIGKILL)
		}
	}
}

func (sh bgShell) alive() bool {
	st, ok := readProcStat(sh.pid)
	return ok && !st.zombie && st.start == sh.start && st.pgid == sh.pid
}

// ownedBackgroundShell accepts pid only when it leads its own process group
// (so signalling the group never reaches the CLI) and descends from the CLI.
func ownedBackgroundShell(cliPID, pid int) (bgShell, bool) {
	if cliPID <= 0 || pid <= 0 || pid == cliPID {
		return bgShell{}, false
	}
	cli, ok := readProcStat(cliPID)
	if !ok {
		return bgShell{}, false
	}
	st, ok := readProcStat(pid)
	if !ok || st.zombie || st.pgid != pid || st.pgid == cli.pgid {
		return bgShell{}, false
	}
	cur := st.ppid
	for range 64 {
		if cur == cliPID {
			return bgShell{pid: pid, start: st.start}, true
		}
		if cur <= 1 {
			break
		}
		parent, ok := readProcStat(cur)
		if !ok {
			break
		}
		cur = parent.ppid
	}
	return bgShell{}, false
}

type procStat struct {
	ppid, pgid int
	start      uint64
	zombie     bool
}

// readProcStat reads state, ppid, pgid and starttime from /proc/<pid>/stat;
// comm is parenthesized and may itself contain spaces or parentheses.
func readProcStat(pid int) (procStat, bool) {
	raw, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "stat"))
	if err != nil {
		return procStat{}, false
	}
	s := string(raw)
	end := strings.LastIndexByte(s, ')')
	if end < 0 {
		return procStat{}, false
	}
	f := strings.Fields(s[end+1:])
	// f[0] is field 3 (state); ppid is 4, pgid 5, starttime 22.
	if len(f) < 20 {
		return procStat{}, false
	}
	ppid, err1 := strconv.Atoi(f[1])
	pgid, err2 := strconv.Atoi(f[2])
	start, err3 := strconv.ParseUint(f[19], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return procStat{}, false
	}
	return procStat{ppid: ppid, pgid: pgid, start: start, zombie: f[0] == "Z"}, true
}
