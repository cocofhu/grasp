package previewinject

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func scriptPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	p := filepath.Join(filepath.Dir(file), "..", "..", "scripts", "preview-inject.sh")
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func runScript(t *testing.T, env []string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{scriptPath(t)}, args...)...)
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH")}, env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// listenPort opens a TCP listener on all interfaces and returns its port.
func listenPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().(*net.TCPAddr).Port
}

// freePort returns a port nothing listens on.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return p
}

func requireSS(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ss"); err != nil {
		t.Skip("ss (iproute2) not installed")
	}
}

func setUpstreamEnv(t *testing.T, previewPort int) ([]string, string) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "upstream")
	return []string{
		"PREVIEW_DIRECT=1",
		"PREVIEW_PORT=" + strconv.Itoa(previewPort),
		"PREVIEW_INJECT_UPSTREAM_FILE=" + file,
		"GRASP_RUNTIME_RUN_DIR=" + dir,
	}, file
}

func TestPreviewInjectSh_SetUpstreamSwitches(t *testing.T) {
	requireSS(t)
	pp := freePort(t)
	app := listenPort(t)
	env, file := setUpstreamEnv(t, pp)
	out, err := runScript(t, env, "set-upstream", strconv.Itoa(app))
	if err != nil {
		t.Fatalf("exit: %v out=%s", err, out)
	}
	want := fmt.Sprintf("result=ok preview_port=%d upstream=%d", pp, app)
	if strings.TrimSpace(out) != want {
		t.Fatalf("out=%q want %q", out, want)
	}
	b, err := os.ReadFile(file)
	if err != nil || strings.TrimSpace(string(b)) != strconv.Itoa(app) {
		t.Fatalf("upstream file=%q err=%v", b, err)
	}

	out, err = runScript(t, env, "set-upstream", "reset")
	if err != nil || strings.TrimSpace(out) != fmt.Sprintf("result=reset preview_port=%d", pp) {
		t.Fatalf("reset: err=%v out=%q", err, out)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("reset must remove the file: %v", err)
	}
}

func TestPreviewInjectSh_SetUpstreamPreviewPortResets(t *testing.T) {
	pp := freePort(t)
	env, file := setUpstreamEnv(t, pp)
	writeFile(t, file, "5173\n")
	out, err := runScript(t, env, "set-upstream", strconv.Itoa(pp))
	if err != nil || strings.TrimSpace(out) != fmt.Sprintf("result=reset preview_port=%d", pp) {
		t.Fatalf("err=%v out=%q", err, out)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("registering PREVIEW_PORT must clear the switch: %v", err)
	}
}

func TestPreviewInjectSh_SetUpstreamBusyKeepsPreviewPort(t *testing.T) {
	requireSS(t)
	pp := listenPort(t)
	app := listenPort(t)
	env, file := setUpstreamEnv(t, pp)
	out, err := runScript(t, env, "set-upstream", strconv.Itoa(app))
	if err != nil || strings.TrimSpace(out) != fmt.Sprintf("result=busy preview_port=%d", pp) {
		t.Fatalf("err=%v out=%q", err, out)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("busy must not write the file: %v", err)
	}
}

func TestPreviewInjectSh_SetUpstreamNoListener(t *testing.T) {
	requireSS(t)
	pp := freePort(t)
	env, _ := setUpstreamEnv(t, pp)
	out, err := runScript(t, env, "set-upstream", strconv.Itoa(freePort(t)))
	if err != nil || strings.TrimSpace(out) != fmt.Sprintf("result=nolisten preview_port=%d", pp) {
		t.Fatalf("err=%v out=%q", err, out)
	}
}

func TestPreviewInjectSh_SetUpstreamDeniesSandboxPorts(t *testing.T) {
	pp := freePort(t)
	env, file := setUpstreamEnv(t, pp)
	env = append(env, "CODE_SERVER_PORT=8800")
	for _, p := range []string{"22", "5900", "6080", "8744", "8765", "9222", "17980", "8800"} {
		out, err := runScript(t, env, "set-upstream", p)
		if err != nil || strings.TrimSpace(out) != fmt.Sprintf("result=denied preview_port=%d", pp) {
			t.Fatalf("port %s: err=%v out=%q", p, err, out)
		}
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("denied ports must not write the file: %v", err)
	}
}

func TestPreviewInjectSh_SetUpstreamInvalid(t *testing.T) {
	env, _ := setUpstreamEnv(t, freePort(t))
	for _, p := range []string{"", "abc", "0", "70000", "-1"} {
		out, err := runScript(t, env, "set-upstream", p)
		if err == nil || !strings.HasPrefix(strings.TrimSpace(out), "result=invalid") {
			t.Fatalf("port %q: err=%v out=%q", p, err, out)
		}
	}
}

func TestPreviewInjectSh_SetUpstreamUnavailable(t *testing.T) {
	out, err := runScript(t, []string{"GRASP_RUNTIME_RUN_DIR=" + t.TempDir()}, "set-upstream", "5173")
	if err != nil || strings.TrimSpace(out) != "result=unavailable" {
		t.Fatalf("err=%v out=%q", err, out)
	}
}

func TestPreviewInjectSh_SetUpstreamReadsSavedEnv(t *testing.T) {
	pp := freePort(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "upstream")
	writeFile(t, filepath.Join(dir, "env"), fmt.Sprintf("declare -x PREVIEW_DIRECT=\"1\"\ndeclare -x PREVIEW_PORT=\"%d\"\n", pp))
	out, err := runScript(t, []string{
		"GRASP_RUNTIME_RUN_DIR=" + dir,
		"PREVIEW_INJECT_UPSTREAM_FILE=" + file,
	}, "set-upstream", "reset")
	if err != nil || strings.TrimSpace(out) != fmt.Sprintf("result=reset preview_port=%d", pp) {
		t.Fatalf("SSH callers must get PREVIEW_PORT from the saved env: err=%v out=%q", err, out)
	}
}

func TestPreviewInjectSh_DeclaresSetUpstreamMarker(t *testing.T) {
	b, err := os.ReadFile(scriptPath(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "# grasp:set-upstream") {
		t.Fatal("Grasp gates the subcommand on this marker; old scripts would start a second injector instead")
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPreviewInjectSh_NoDirectExits0(t *testing.T) {
	out, err := runScript(t, nil)
	if err != nil {
		t.Fatalf("exit: %v out=%s", err, out)
	}
	if !strings.Contains(out, "PREVIEW_DIRECT not set") {
		t.Fatalf("out=%s", out)
	}
}

func TestPreviewInjectSh_MissingPortExits0(t *testing.T) {
	out, err := runScript(t, []string{
		"PREVIEW_DIRECT=1",
		"PREVIEW_PICK_SCRIPT_URL=http://x/preview-pick.js",
	})
	if err != nil {
		t.Fatalf("exit: %v out=%s", err, out)
	}
	if !strings.Contains(out, "missing") {
		t.Fatalf("out=%s", out)
	}
}

func TestPreviewInjectSh_StartPassesUpstreamFile(t *testing.T) {
	b, err := os.ReadFile(scriptPath(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `--upstream-file "$UPSTREAM_FILE"`) {
		t.Fatal("start_bin must hand the switch file to the injector")
	}
}

func TestPreviewInjectSh_DryRunRules(t *testing.T) {
	out, err := runScript(t, []string{
		"PREVIEW_DIRECT=1",
		"PREVIEW_PORT=18080",
		"PREVIEW_INJECT_DRY_RUN=1",
	})
	if err != nil {
		t.Fatalf("exit: %v out=%s", err, out)
	}
	for _, want := range []string{
		"APPROVING-PREVIEW",
		"--dport 18080",
		"--to-ports 17980",
		"REDIRECT",
		"grasp-preview-inject",
		"PREROUTING",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestPreviewInjectSh_DryRunIdempotent(t *testing.T) {
	env := []string{
		"PREVIEW_DIRECT=1",
		"PREVIEW_PORT=18111",
		"PREVIEW_PICK_SCRIPT_URL=http://x/preview-pick.js",
		"PREVIEW_INJECT_DRY_RUN=1",
	}
	a, err := runScript(t, env)
	if err != nil {
		t.Fatal(err)
	}
	b, err := runScript(t, env)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("dry-run not stable:\n%s\n---\n%s", a, b)
	}
}

func TestPreviewInjectSh_AutoInjectOff(t *testing.T) {
	out, err := runScript(t, []string{
		"PREVIEW_DIRECT=1",
		"PREVIEW_AUTO_INJECT=0",
		"PREVIEW_PORT=18080",
		"PREVIEW_PICK_SCRIPT_URL=http://x/preview-pick.js",
	})
	if err != nil {
		t.Fatalf("exit: %v out=%s", err, out)
	}
	if !strings.Contains(out, "PREVIEW_AUTO_INJECT off") {
		t.Fatalf("out=%s", out)
	}
}

func TestPreviewInjectSh_SamePortSkip(t *testing.T) {
	out, err := runScript(t, []string{
		"PREVIEW_DIRECT=1",
		"PREVIEW_PORT=17980",
		"PREVIEW_PICK_SCRIPT_URL=http://x/preview-pick.js",
	})
	if err != nil {
		t.Fatalf("exit: %v out=%s", err, out)
	}
	if !strings.Contains(out, "must not equal") {
		t.Fatalf("out=%s", out)
	}
}
