package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type call struct {
	name string
	args []string
}

type fakeRunner struct {
	mu      sync.Mutex
	calls   []call
	started []call
	out     map[string][]byte
	fail    map[string]error
}

func (f *fakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call{name, args})
	key := name
	if len(args) > 0 {
		key = name + " " + args[0]
	}
	if err := f.fail[key]; err != nil {
		return []byte("boom"), err
	}
	return f.out[key], nil
}

func (f *fakeRunner) Start(name string, args []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail["start "+name]; err != nil {
		return err
	}
	f.started = append(f.started, call{name, args})
	return nil
}

func lookAll(string) (string, error) { return "/usr/bin/x", nil }

func lookOnly(names ...string) func(string) (string, error) {
	return func(n string) (string, error) {
		for _, m := range names {
			if m == n {
				return "/usr/bin/" + n, nil
			}
		}
		return "", errors.New("not found")
	}
}

const appsJSON = `[
 {"id":"mousepad","name":"Mousepad","nameZh":"文本编辑器","exec":["mousepad"],"cli":"mousepad FILE","category":"office"},
 {"id":"blender","name":"Blender","exec":["blender","--online-mode"],"cli":"blender -b -P script.py","category":"3d","icon":"ICON"},
 {"id":"mousepad","name":"Dup","exec":["x"],"category":"x"},
 {"id":"","name":"NoID","exec":["x"],"category":"x"},
 {"id":"noexec","name":"NoExec","exec":[],"category":"x"}
]`

func testPaths(t *testing.T, desktop bool) Paths {
	t.Helper()
	dir := t.TempDir()
	p := Paths{Marker: filepath.Join(dir, "grasp-desktop"), RunDir: filepath.Join(dir, "run"), Apps: filepath.Join(dir, "apps.json")}
	if desktop {
		if err := os.WriteFile(p.Marker, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	icon := filepath.Join(dir, "blender.svg")
	if err := os.WriteFile(icon, []byte("<svg/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Apps, []byte(strings.Replace(appsJSON, "ICON", icon, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeState(t *testing.T, p Paths, body string) {
	t.Helper()
	if err := os.MkdirAll(p.RunDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.statePath(), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultPathsHonourEnv(t *testing.T) {
	t.Setenv("GRASP_DESKTOP_MARKER", "/m")
	t.Setenv("GRASP_DESKTOP_RUN_DIR", "/r")
	t.Setenv("GRASP_DESKTOP_APPS", "")
	p := DefaultPaths()
	if p.Marker != "/m" || p.RunDir != "/r" || p.Apps != "/opt/grasp-desktop/apps.json" {
		t.Fatalf("paths = %+v", p)
	}
}

func TestReadShellModes(t *testing.T) {
	browser := testPaths(t, false)
	if s := browser.ReadShell(); s.Mode != ModeBrowser || s.ShellReady {
		t.Fatalf("no marker: %+v", s)
	}
	p := testPaths(t, true)
	if s := p.ReadShell(); s.Mode != ModeDesktop || s.ShellReady {
		t.Fatalf("starting: %+v", s)
	}
	writeState(t, p, `{"mode":"desktop","shellReady":true}`)
	if s := p.ReadShell(); s.Mode != ModeDesktop || !s.ShellReady {
		t.Fatalf("ready: %+v", s)
	}
	writeState(t, p, `{"mode":"browser","shellReady":true,"fallbackReason":"openbox failed"}`)
	if s := p.ReadShell(); s.Mode != ModeBrowser || s.ShellReady || s.FallbackReason != "openbox failed" {
		t.Fatalf("fallback: %+v", s)
	}
	writeState(t, p, `{"mode":"weird"}`)
	if s := p.ReadShell(); s.Mode != ModeDesktop {
		t.Fatalf("garbage: %+v", s)
	}
}

func TestLoadAppsFiltersAndInstalled(t *testing.T) {
	p := testPaths(t, true)
	apps := LoadApps(p.Apps)
	if len(apps) != 2 || apps[0].ID != "mousepad" || apps[1].ID != "blender" {
		t.Fatalf("apps = %+v", apps)
	}
	if got := InstalledApps(apps, lookOnly("blender")); len(got) != 1 || got[0].ID != "blender" {
		t.Fatalf("installed = %+v", got)
	}
	if _, ok := FindApp(apps, " blender "); !ok {
		t.Fatal("FindApp trims")
	}
	if _, ok := FindApp(apps, "gimp"); ok {
		t.Fatal("unknown app found")
	}
	if LoadApps(filepath.Join(t.TempDir(), "none")) != nil {
		t.Fatal("missing file must be empty")
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	_ = os.WriteFile(bad, []byte("{"), 0o644)
	if LoadApps(bad) != nil {
		t.Fatal("broken file must be empty")
	}
}

func TestBuildStatus(t *testing.T) {
	p := testPaths(t, true)
	writeState(t, p, `{"mode":"desktop","shellReady":true}`)
	exp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	st := BuildStatus(p, Control{Holder: HolderUser, ExpiresAt: exp}, true, lookAll)
	if st.Mode != ModeDesktop || !st.ShellReady || !st.AgentActive || st.Holder != HolderUser || st.ExpiresAt != "2026-01-02T03:04:05Z" || len(st.Apps) != 2 {
		t.Fatalf("status = %+v", st)
	}
	st = BuildStatus(testPaths(t, false), Control{Holder: HolderAgent}, false, lookAll)
	if st.Mode != ModeBrowser || st.ExpiresAt != "" || len(st.Apps) != 0 || st.Apps == nil {
		t.Fatalf("browser status = %+v", st)
	}
}

func TestControlStoreRenewExpireAndMultipleViewers(t *testing.T) {
	p := testPaths(t, true)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := NewControlStore(p, func() time.Time { return now })
	if c := s.Read(); c.Holder != HolderAgent {
		t.Fatalf("default = %+v", c)
	}
	c, err := s.Set("user", "a")
	if err != nil || c.Holder != HolderUser || !c.ExpiresAt.Equal(now.Add(ControlTTL)) {
		t.Fatalf("take = %+v %v", c, err)
	}
	if _, err := s.Set("user", "b"); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.Set("agent", "a"); c.Holder != HolderUser {
		t.Fatalf("b still holds: %+v", c)
	}
	now = now.Add(20 * time.Second)
	if _, err := s.Set("user", "b"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(20 * time.Second)
	if c := s.Read(); c.Holder != HolderUser {
		t.Fatalf("renewed hold expired early: %+v", c)
	}
	now = now.Add(11 * time.Second)
	if c := s.Read(); c.Holder != HolderAgent {
		t.Fatalf("hold must lapse 30s after the last renewal: %+v", c)
	}
	if _, err := s.Set("owner", ""); !errors.Is(err, ErrBadHolder) {
		t.Fatalf("bad holder err = %v", err)
	}
	if c, err := s.Set("user", strings.Repeat("x", 300)); err != nil || c.Holder != HolderUser {
		t.Fatalf("long client: %+v %v", c, err)
	}
	if c, _ := s.Set("agent", strings.Repeat("x", 300)); c.Holder != HolderAgent {
		t.Fatalf("long client id must be truncated the same way: %+v", c)
	}
	if c, _ := s.Set("user", ""); c.Holder != HolderUser {
		t.Fatalf("default client: %+v", c)
	}
}

func TestControlStoreBrokenFileAndUnwritableDir(t *testing.T) {
	p := testPaths(t, true)
	_ = os.MkdirAll(p.RunDir, 0o755)
	_ = os.WriteFile(p.controlPath(), []byte("not json"), 0o644)
	s := NewControlStore(p, nil)
	if c := s.Read(); c.Holder != HolderAgent {
		t.Fatalf("broken file = %+v", c)
	}
	blocker := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(blocker, nil, 0o644)
	bad := NewControlStore(Paths{RunDir: filepath.Join(blocker, "run")}, nil)
	if _, err := bad.Set("user", "a"); err == nil {
		t.Fatal("unwritable run dir must fail")
	}
}

func TestExecRunner(t *testing.T) {
	dir := t.TempDir()
	r := ExecRunner{Display: ":42", LogDir: dir}
	out, err := r.Output(context.Background(), "sh", "-c", "printf %s \"$DISPLAY\"")
	if err != nil || string(out) != ":42" {
		t.Fatalf("Output = %q %v", out, err)
	}
	if err := LaunchApp(r, App{Exec: []string{"sh", "-c", "echo started"}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		b, _ := os.ReadFile(filepath.Join(dir, "grasp-desktop-sh.log"))
		if strings.Contains(string(b), "started") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("app log = %q", b)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := r.Start("/nonexistent/app", nil); err == nil {
		t.Fatal("missing binary must fail")
	}
	if err := (ExecRunner{LogDir: filepath.Join(dir, "missing")}).Start("sh", nil); err == nil {
		t.Fatal("missing log dir must fail")
	}
	t.Setenv("DISPLAY", ":7")
	out, _ = ExecRunner{}.Output(context.Background(), "sh", "-c", "printf %s \"$DISPLAY\"")
	if string(out) != ":7" {
		t.Fatalf("inherited DISPLAY = %q", out)
	}
	t.Setenv("DISPLAY", "")
	if err := (ExecRunner{}).Start("true", nil); err != nil {
		t.Fatal(err)
	}
	out, _ = ExecRunner{}.Output(context.Background(), "sh", "-c", "printf %s \"$DISPLAY\"")
	if string(out) != ":99" {
		t.Fatalf("default DISPLAY = %q", out)
	}
}

func TestExecRunnerSessionEnv(t *testing.T) {
	p := Paths{RunDir: t.TempDir()}
	if err := os.WriteFile(p.SessionEnv(), []byte("DISPLAY=:5\nDBUS_SESSION_BUS_ADDRESS=unix:path=/tmp/bus,guid=x\nGTK_IM_MODULE=fcitx\n\nnot-a-pair\n=skip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GTK_IM_MODULE", "ibus")
	t.Setenv("GRASP_KEEP", "1")
	show := `printf '%s|%s|%s|%s' "$DISPLAY" "$DBUS_SESSION_BUS_ADDRESS" "$GTK_IM_MODULE" "$GRASP_KEEP"`
	out, err := ExecRunner{SessionEnv: p.SessionEnv()}.Output(context.Background(), "sh", "-c", show)
	if err != nil || string(out) != ":5|unix:path=/tmp/bus,guid=x|fcitx|1" {
		t.Fatalf("session env = %q %v", out, err)
	}
	out, _ = ExecRunner{Display: ":8", SessionEnv: p.SessionEnv()}.Output(context.Background(), "sh", "-c", `printf %s "$DISPLAY"`)
	if string(out) != ":8" {
		t.Fatalf("explicit Display must win, got %q", out)
	}
	t.Setenv("DISPLAY", ":3")
	out, _ = ExecRunner{SessionEnv: filepath.Join(p.RunDir, "missing")}.Output(context.Background(), "sh", "-c", `printf %s "$DISPLAY"`)
	if string(out) != ":3" {
		t.Fatalf("missing session env keeps process env, got %q", out)
	}
}
