package desktop

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// Runner runs X11 helper commands and starts GUI apps on the desktop.
type Runner interface {
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
	// Start launches a detached process in its own session; it outlives the caller.
	Start(name string, args []string) error
}

// ExecRunner runs real processes in the desktop session: the process
// environment, overlaid by SessionEnv (KEY=VALUE lines), then Display.
type ExecRunner struct {
	Display string
	// SessionEnv is the session.env written by desktop-session.sh (D-Bus,
	// input method, locale); missing means the process environment only.
	SessionEnv string
	// LogDir receives one log per started app (default /tmp).
	LogDir string
}

func (r ExecRunner) env() []string {
	vals := map[string]string{}
	var order []string
	set := func(kv string) {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return
		}
		if _, seen := vals[k]; !seen {
			order = append(order, k)
		}
		vals[k] = v
	}
	for _, kv := range os.Environ() {
		set(kv)
	}
	if r.SessionEnv != "" {
		if b, err := os.ReadFile(r.SessionEnv); err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				set(strings.TrimSpace(line))
			}
		}
	}
	switch {
	case r.Display != "":
		set("DISPLAY=" + r.Display)
	case vals["DISPLAY"] == "":
		set("DISPLAY=:99")
	}
	out := make([]string, 0, len(order))
	for _, k := range order {
		out = append(out, k+"="+vals[k])
	}
	return out
}

// Output runs name and returns stdout.
func (r ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = r.env()
	return cmd.Output()
}

// Start launches name detached, logging to LogDir/grasp-desktop-<base>.log.
func (r ExecRunner) Start(name string, args []string) error {
	dir := r.LogDir
	if dir == "" {
		dir = os.TempDir()
	}
	logf, err := os.OpenFile(filepath.Join(dir, "grasp-desktop-"+filepath.Base(name)+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = logf.Close() }()
	cmd := exec.Command(name, args...)
	cmd.Env = r.env()
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// LaunchApp starts a catalog app.
func LaunchApp(r Runner, a App) error {
	return r.Start(a.Exec[0], a.Exec[1:])
}
