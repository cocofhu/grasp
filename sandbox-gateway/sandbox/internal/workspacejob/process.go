package workspacejob

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Process struct {
	Dir    string
	Env    []string
	Input  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Argv   []string
}

// Run adds binary stdin and an explicit environment to Command's fenced
// process-group supervision. Only trusted executors construct Process values.
// In particular no command, environment or host directory comes from HTTP.
func (h *Handle) Run(process Process) error {
	if len(process.Argv) == 0 || process.Argv[0] == "" {
		return ErrInvalid
	}
	if err := h.Check(); err != nil {
		return err
	}
	cmd := NewSupervisedCommand(h.ctx, process.Argv...)
	cmd.Dir, cmd.Env, cmd.Stdin, cmd.Stdout, cmd.Stderr = process.Dir, process.Env, process.Input, process.Stdout, process.Stderr
	if err := h.Guard(cmd.Start); err != nil {
		// Guard can reject a Start that completed after its deadline. Always
		// reap that process before allowing a successor into this lane.
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			_ = cmd.Wait()
			waitProcessGroupExit(cmd.Process.Pid)
		}
		return err
	}
	err := cmd.Wait()
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	waitProcessGroupExit(cmd.Process.Pid)
	if fenceErr := h.Check(); fenceErr != nil {
		return fenceErr
	}
	return err
}

// NewSupervisedCommand runs trusted argv under the same process-group supervisor
// as durable workspace jobs. The caller must reap the group before releasing
// its physical operation lane; runtime death also terminates its children.
func NewSupervisedCommand(ctx context.Context, argv ...string) *exec.Cmd {
	const supervisor = `exec 3<&0
trap 'kill -KILL -$$' TERM HUP INT
"$@" <&3 &
child=$!
exec 3<&-
wait "$child"
status=$?
trap - TERM HUP INT
exit "$status"`
	args := append([]string{"-c", supervisor, "grasp-workspace-operation"}, argv...)
	cmd := exec.CommandContext(ctx, "/bin/sh", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = time.Second
	return cmd
}

// kill returning is an acknowledgement of delivery, not of child exit. Keep
// the predecessor lane occupied until its whole group can no longer execute.
// An unreadable process table is not a stop proof: Close may time out while
// this supervisor keeps waiting, rather than admitting a concurrent writer.
func waitProcessGroupExit(group int) {
	for processGroupActive(group) {
		time.Sleep(5 * time.Millisecond)
	}
}

// WaitProcessGroupExit preserves physical lane ownership for other trusted
// runtime executors until every member of their own process group has exited.
func WaitProcessGroupExit(group int) { waitProcessGroupExit(group) }

func processGroupActive(group int) bool {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return true
	}
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return true
		}
		end := strings.LastIndexByte(string(data), ')')
		if end < 0 {
			return true
		}
		fields := strings.Fields(string(data)[end+1:])
		if len(fields) < 3 {
			return true
		}
		pidGroup, err := strconv.Atoi(fields[2])
		if err != nil {
			return true
		}
		if pidGroup == group && fields[0] != "Z" && fields[0] != "X" {
			return true
		}
	}
	return false
}
