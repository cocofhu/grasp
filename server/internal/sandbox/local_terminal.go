package sandbox

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

// Terminal is an interactive shell the browser terminal can read, write, and resize.
type Terminal interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Resize(rows, cols uint16) error
	Close() error
}

// LocalTerminal is a PTY shell in this process. Worktree launches have no sshd,
// so the browser terminal talks to this instead of a sandbox container.
type LocalTerminal struct {
	pty *os.File
	cmd *exec.Cmd
}

// OpenLocalTerminal starts an interactive shell in dir.
func OpenLocalTerminal(dir string) (*LocalTerminal, error) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("工作树目录不存在")
	}
	shell := "/bin/bash"
	if _, err := os.Stat(shell); err != nil {
		shell = "/bin/sh"
	}
	pty, tty, err := openPTY()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(shell, "-i")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	cmd.Stdin = tty
	cmd.Stdout = tty
	cmd.Stderr = tty
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		_ = tty.Close()
		_ = pty.Close()
		return nil, fmt.Errorf("start shell: %w", err)
	}
	_ = tty.Close()
	return &LocalTerminal{pty: pty, cmd: cmd}, nil
}

func (t *LocalTerminal) Read(p []byte) (int, error) {
	if t == nil || t.pty == nil {
		return 0, io.EOF
	}
	return t.pty.Read(p)
}

func (t *LocalTerminal) Write(p []byte) (int, error) {
	if t == nil || t.pty == nil {
		return 0, io.ErrClosedPipe
	}
	return t.pty.Write(p)
}

func (t *LocalTerminal) Resize(rows, cols uint16) error {
	if t == nil || t.pty == nil {
		return nil
	}
	return unix.IoctlSetWinsize(int(t.pty.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: rows, Col: cols})
}

func (t *LocalTerminal) Close() error {
	if t == nil {
		return nil
	}
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
		_, _ = t.cmd.Process.Wait()
	}
	if t.pty != nil {
		return t.pty.Close()
	}
	return nil
}

func openPTY() (pty, tty *os.File, err error) {
	pty, err = os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, err
	}
	n, err := unix.IoctlGetInt(int(pty.Fd()), unix.TIOCGPTN)
	if err != nil {
		_ = pty.Close()
		return nil, nil, err
	}
	if err = unix.IoctlSetPointerInt(int(pty.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		_ = pty.Close()
		return nil, nil, err
	}
	name := "/dev/pts/" + strconv.Itoa(n)
	_ = os.Chmod(name, 0o620)
	tty, err = os.OpenFile(name, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		_ = pty.Close()
		return nil, nil, err
	}
	return pty, tty, nil
}
