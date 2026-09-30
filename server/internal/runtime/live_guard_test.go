package runtime

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestParseLiveMarkerSIDs(t *testing.T) {
	out := "data-grasp-live=\"sid002\"\ndata-grasp-live=\"sid001\"\nnoise\ndata-grasp-live=\"sid002\"\ndata-grasp-live=\"bad sid\""
	if got := parseLiveMarkerSIDs(out); !reflect.DeepEqual(got, []string{"sid001", "sid002"}) {
		t.Fatalf("sids = %v", got)
	}
	if got := parseLiveMarkerSIDs(""); got != nil {
		t.Fatalf("empty = %v", got)
	}
}

func TestLiveGuardScript(t *testing.T) {
	s := liveGuardScript("/root/workspace")
	for _, want := range []string{"grasp-live-guard", "'/root/workspace'/*", ".git/hooks/pre-commit", "chmod +x"} {
		if !strings.Contains(s, want) {
			t.Errorf("script missing %q:\n%s", want, s)
		}
	}
	if !strings.Contains(liveGuardHook, "data-grasp-(live|variant)[[:space:]]*=") {
		t.Error("hook pattern")
	}
}

func TestLiveGuardRejectsStagedMarkers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		block  bool
	}{
		{name: "clean", source: "<main>Hello</main>"},
		{name: "double quotes", source: `<div data-grasp-live="sid001" />`, block: true},
		{name: "single quotes and spaces", source: `<div data-grasp-live = 'sid001' />`, block: true},
		{name: "orphan variant", source: `<section data-grasp-variant = {1} />`, block: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := t.TempDir()
			git := func(args ...string) ([]byte, error) {
				return exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
			}
			for _, args := range [][]string{{"init", "-q"}, {"config", "user.name", "Live test"}, {"config", "user.email", "live-test@example.invalid"}} {
				if out, err := git(args...); err != nil {
					t.Fatalf("git setup: %v: %s", err, out)
				}
			}
			if err := os.WriteFile(filepath.Join(repo, ".git", "hooks", "pre-commit"), []byte(liveGuardHook), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(repo, "App.vue"), []byte(tc.source), 0o600); err != nil {
				t.Fatal(err)
			}
			if out, err := git("add", "App.vue"); err != nil {
				t.Fatalf("git add: %v: %s", err, out)
			}
			out, err := git("commit", "-qm", "test Live guard")
			if (err != nil) != tc.block || (tc.block && !strings.Contains(string(out), "grasp:")) {
				t.Fatalf("commit blocked=%v want=%v: %v: %s", err != nil, tc.block, err, out)
			}
		})
	}
}

func TestLiveMarkerSIDsNotParked(t *testing.T) {
	c := &acpProvider{sessions: map[string]*reactSession{}}
	if sids, ok, err := c.LiveMarkerSIDs(context.Background(), "r", "n"); ok || err != nil || sids != nil {
		t.Fatalf("not parked: %v %v %v", sids, ok, err)
	}
	c.InstallLiveGuard(context.Background(), "r", "n") // no-op without a session
}

// Run the actual SSH stdin script locally so shell error handling and source
// spellings are exercised, rather than supplying a pre-parsed marker list.
func TestLiveMarkerSIDsScansSource(t *testing.T) {
	restore := sandbox.SetExecHook(func(ctx context.Context, _ string, _ int, _ string, stdin io.Reader) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "bash", "-s")
		cmd.Stdin = stdin
		return cmd.CombinedOutput()
	})
	t.Cleanup(restore)
	for _, tc := range []struct {
		name    string
		source  string
		want    []string
		wantErr bool
	}{
		{name: "clean", source: "<main>Hello</main>"},
		{name: "double quotes", source: `<div data-grasp-live="sid001"><section data-grasp-variant="1" /></div>`, want: []string{"sid001"}},
		{name: "single quotes", source: `<div data-grasp-live='sid002'><section data-grasp-variant='1' /></div>`, want: []string{"sid002"}},
		{name: "JSX literals and spacing", source: `<div data-grasp-live = { 'sid003' }><section data-grasp-variant={1} /></div>`, want: []string{"sid003"}},
		{name: "formatter split value", source: "<div data-grasp-live =\n { 'sid004' }><section data-grasp-variant=\"1\" /></div>", want: []string{"sid004"}},
		{name: "multiple sessions", source: `<div data-grasp-live="sid002" /><div data-grasp-live='sid001' /><div data-grasp-live="sid002" />`, want: []string{"sid001", "sid002"}},
		{name: "unresolved wrapper", source: `<div data-grasp-live={sessionId} />`, wantErr: true},
		{name: "orphan variant", source: `<section data-grasp-variant="2" />`, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := t.TempDir()
			if err := os.WriteFile(filepath.Join(ws, "App.vue"), []byte(tc.source), 0o600); err != nil {
				t.Fatal(err)
			}
			p := &acpProvider{sessions: map[string]*reactSession{"r|n": {sb: &sandbox.Sandbox{WorkspaceDir: ws}}}}
			got, parked, err := p.LiveMarkerSIDs(context.Background(), "r", "n")
			if !parked || (err != nil) != tc.wantErr || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("scan = %v, %v, %v; want %v, parked, err=%v", got, parked, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestLiveMarkerSIDsPropagatesFilesystemErrors(t *testing.T) {
	ws := t.TempDir()
	missing := filepath.Join(ws, "missing")
	p := &acpProvider{sessions: map[string]*reactSession{"r|n": {sb: &sandbox.Sandbox{WorkspaceDir: missing}}}}
	restore := sandbox.SetExecHook(func(ctx context.Context, _ string, _ int, _ string, stdin io.Reader) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "bash", "-s")
		cmd.Stdin = stdin
		return cmd.CombinedOutput()
	})
	t.Cleanup(restore)
	if _, parked, err := p.LiveMarkerSIDs(context.Background(), "r", "n"); !parked || err == nil {
		t.Fatalf("missing workspace must fail scan: parked=%v err=%v", parked, err)
	}
	// Simulate grep's read-error exit independently of OS/user permissions.
	// Root can read chmod(000) files, so that is not a portable error fixture.
	bin := filepath.Join(ws, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "grep"), []byte("#!/bin/sh\necho 'read error' >&2\nexit 2\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	p.sessions["r|n"].sb.WorkspaceDir = ws
	if _, parked, err := p.LiveMarkerSIDs(context.Background(), "r", "n"); !parked || err == nil {
		t.Fatalf("grep read error must fail scan: parked=%v err=%v", parked, err)
	}
}

func TestLiveMarkerSIDsExcludesBuildOutput(t *testing.T) {
	ws := t.TempDir()
	for _, name := range []string{"node_modules", "dist", "build", ".git", ".next"} {
		dir := filepath.Join(ws, name)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "generated.js"), []byte(`data-grasp-live="sid001"`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	restore := sandbox.SetExecHook(func(ctx context.Context, _ string, _ int, _ string, stdin io.Reader) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "bash", "-s")
		cmd.Stdin = stdin
		return cmd.CombinedOutput()
	})
	t.Cleanup(restore)
	p := &acpProvider{sessions: map[string]*reactSession{"r|n": {sb: &sandbox.Sandbox{WorkspaceDir: ws}}}}
	if got, parked, err := p.LiveMarkerSIDs(context.Background(), "r", "n"); len(got) != 0 || !parked || err != nil {
		t.Fatalf("excluded generated markers should not block: %v %v %v", got, parked, err)
	}
}
