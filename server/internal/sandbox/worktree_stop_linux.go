package sandbox

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type localProcessIdentity struct {
	pid, parent, session int
	start, state         string
	fd                   int
}

func localProcessStat(pid int) (localProcessIdentity, error) {
	body, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return localProcessIdentity{}, err
	}
	end := strings.LastIndexByte(string(body), ')')
	if end < 0 {
		return localProcessIdentity{}, ErrLocalStopUnconfirmed
	}
	fields := strings.Fields(string(body)[end+1:])
	if len(fields) < 20 {
		return localProcessIdentity{}, ErrLocalStopUnconfirmed
	}
	parent, e1 := strconv.Atoi(fields[1])
	session, e2 := strconv.Atoi(fields[3])
	if e1 != nil || e2 != nil {
		return localProcessIdentity{}, ErrLocalStopUnconfirmed
	}
	return localProcessIdentity{pid: pid, parent: parent, session: session, start: fields[19], state: fields[0], fd: -1}, nil
}

func openLocalProcess(identity localProcessIdentity) (localProcessIdentity, error) {
	fd, err := unix.PidfdOpen(identity.pid, 0)
	if err != nil {
		return localProcessIdentity{}, err
	}
	current, err := localProcessStat(identity.pid)
	if err != nil || current.start != identity.start || current.session != identity.session {
		_ = unix.Close(fd)
		return localProcessIdentity{}, errors.Join(ErrLocalStopUnconfirmed, err)
	}
	current.fd = fd
	return current, nil
}

func localProcessExited(p localProcessIdentity) bool {
	poll := []unix.PollFd{{Fd: int32(p.fd), Events: unix.POLLIN}}
	n, err := unix.Poll(poll, 0)
	return err == nil && n == 1 && poll[0].Revents&unix.POLLIN != 0
}

func localProcessPause(ctx context.Context, p localProcessIdentity) error {
	if localProcessExited(p) {
		return nil
	}
	if err := unix.PidfdSendSignal(p.fd, unix.SIGSTOP, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
		return err
	}
	stable := 0
	for {
		if localProcessExited(p) {
			return nil
		}
		current, err := localProcessStat(p.pid)
		if err != nil || current.start != p.start {
			return errors.Join(ErrLocalStopUnconfirmed, err)
		}
		if current.state == "Z" || current.state == "X" {
			return nil
		}
		paused, err := localThreadsPaused(p.pid)
		if err != nil {
			return err
		}
		if (current.state == "T" || current.state == "t") && paused {
			stable++
			if stable == 2 {
				return nil
			}
		} else {
			stable = 0
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func localThreadsPaused(pid int) (bool, error) {
	threads, err := os.ReadDir("/proc/" + strconv.Itoa(pid) + "/task")
	if err != nil {
		return false, err
	}
	for _, thread := range threads {
		tid, err := strconv.Atoi(thread.Name())
		if err != nil {
			return false, ErrLocalStopUnconfirmed
		}
		state, err := localProcessStat(tid)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		if state.state != "T" && state.state != "t" && state.state != "Z" && state.state != "X" {
			return false, nil
		}
	}
	return true, nil
}

// newLocalProcessStop owns only the session started by realStartWorktreeBackend.
// A private session includes subgroups/reparented helpers. Freeze its members
// before the final census so a still-running member cannot fork past it. A
// known descendant outside this session is deliberately unconfirmed; cwd or
// executable-name matches never authorize killing a different process.
func newLocalProcessStop(pid int, exited <-chan struct{}) localBackendStop {
	root, err := localProcessStat(pid)
	if err == nil && root.session != pid {
		err = ErrLocalStopUnconfirmed
	}
	if err == nil {
		root, err = openLocalProcess(root)
	}
	if err != nil {
		return func(context.Context) error { return errors.Join(ErrLocalStopUnconfirmed, err) }
	}
	gate := make(chan struct{}, 1)
	owned := map[int]localProcessIdentity{pid: root}
	sealed, confirmed := false, false
	return func(ctx context.Context) error {
		select {
		case gate <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		defer func() { <-gate }()
		if confirmed {
			return nil
		}
		if !sealed {
			if localProcessExited(root) {
				return ErrLocalStopUnconfirmed
			} // No post-crash adoption.
			if err := localProcessPause(ctx, root); err != nil {
				return err
			}
			if localProcessExited(root) {
				return ErrLocalStopUnconfirmed
			}
			for {
				if err := ctx.Err(); err != nil {
					return err
				}
				entries, err := os.ReadDir("/proc")
				if err != nil {
					return err
				}
				added := false
				for _, entry := range entries {
					childPID, err := strconv.Atoi(entry.Name())
					if err != nil {
						continue
					}
					current, err := localProcessStat(childPID)
					if errors.Is(err, os.ErrNotExist) {
						continue
					}
					if err != nil {
						return err
					}
					if _, childOfOwned := owned[current.parent]; childOfOwned && current.session != pid && current.state != "Z" && current.state != "X" {
						return ErrLocalStopUnconfirmed
					}
					if current.session != pid {
						continue
					}
					if existing, ok := owned[childPID]; ok {
						if existing.start != current.start {
							return ErrLocalStopUnconfirmed
						}
						if err := localProcessPause(ctx, existing); err != nil {
							return err
						}
						continue
					}
					current, err = openLocalProcess(current)
					if errors.Is(err, unix.ESRCH) || errors.Is(err, os.ErrNotExist) {
						continue
					}
					if err != nil {
						return err
					}
					owned[childPID], added = current, true
					if err := localProcessPause(ctx, current); err != nil {
						return err
					}
				}
				if !added {
					sealed = true
					break
				}
			}
		}
		stop := func(p localProcessIdentity) error {
			if !localProcessExited(p) {
				if err := unix.PidfdSendSignal(p.fd, unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
					return err
				}
			}
			for !localProcessExited(p) {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(5 * time.Millisecond):
				}
			}
			return nil
		}
		// Keep the original root alive/frozen until all recorded descendants
		// exited, preventing an unrelated session from reusing its numeric ID.
		for childPID, identity := range owned {
			if childPID != pid {
				if err := stop(identity); err != nil {
					return err
				}
			}
		}
		if err := stop(root); err != nil {
			return err
		}
		select {
		case <-exited:
		case <-ctx.Done():
			return ctx.Err()
		}
		for _, identity := range owned {
			_ = unix.Close(identity.fd)
		}
		confirmed = true
		return nil
	}
}
