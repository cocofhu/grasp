package services

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/sandbox/sandboxtest"
)

// PreviewService must satisfy mcp's optional injector-switch interface, or
// set_preview silently keeps today's no-pick path for non-PREVIEW_PORT apps.
var _ interface {
	SwitchInjectUpstream(context.Context, string, int) (mcp.InjectSwitch, error)
	ProbeInjectUpstream(context.Context, string, int, int) bool
} = (*PreviewService)(nil)

func TestParseInjectSwitch(t *testing.T) {
	cases := []struct {
		in   string
		want mcp.InjectSwitch
		err  bool
	}{
		{"result=ok preview_port=18081 upstream=5173", mcp.InjectSwitch{Result: mcp.InjectSwitched, PreviewPort: 18081}, false},
		{"noise\nresult=busy preview_port=18080\n", mcp.InjectSwitch{Result: mcp.InjectBusy, PreviewPort: 18080}, false},
		{"result=unsupported", mcp.InjectSwitch{Result: mcp.InjectUnsupported}, false},
		{"result=reset preview_port=x", mcp.InjectSwitch{Result: mcp.InjectReset}, false},
		{"", mcp.InjectSwitch{}, true},
		{"bash: preview-inject.sh: not found", mcp.InjectSwitch{}, true},
	}
	for _, tc := range cases {
		got, err := parseInjectSwitch(tc.in)
		if (err != nil) != tc.err || got != tc.want {
			t.Fatalf("parseInjectSwitch(%q)=%+v err=%v want %+v err=%v", tc.in, got, err, tc.want, tc.err)
		}
	}
}

func TestSwitchInjectUpstreamRunsMarkedScript(t *testing.T) {
	db := newTestDB(t)
	fg := sandboxtest.New(t)
	fg.Seed("sb-sw")
	mgr := sandbox.NewManager(fg.Client(), sandbox.ManagerOptions{WorkspaceDir: "/root/workspace"})
	svc := NewPreviewService(db, mgr)

	var saw string
	restore := sandbox.SetExecHook(func(_ context.Context, _ string, _ int, _ string, stdin io.Reader) ([]byte, error) {
		b, _ := io.ReadAll(stdin)
		saw = string(b)
		return []byte("result=ok preview_port=18081 upstream=5173\n"), nil
	})
	t.Cleanup(restore)

	got, err := svc.SwitchInjectUpstream(context.Background(), "sb-sw", 5173)
	if err != nil || got.Result != mcp.InjectSwitched || got.PreviewPort != 18081 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	for _, want := range []string{injectScript, "grep -q '^# grasp:set-upstream'", "echo result=unsupported", `exec "$s" set-upstream 5173`} {
		if !strings.Contains(saw, want) {
			t.Fatalf("script missing %q:\n%s", want, saw)
		}
	}
}

func TestSwitchInjectUpstreamErrors(t *testing.T) {
	if _, err := (&PreviewService{}).SwitchInjectUpstream(context.Background(), "sb", 5173); err == nil {
		t.Fatal("nil manager should error")
	}
	db := newTestDB(t)
	fg := sandboxtest.New(t)
	fg.Seed("sb-sw")
	mgr := sandbox.NewManager(fg.Client(), sandbox.ManagerOptions{WorkspaceDir: "/root/workspace"})
	svc := NewPreviewService(db, mgr)

	restore := sandbox.SetExecHook(func(context.Context, string, int, string, io.Reader) ([]byte, error) {
		return []byte("result=invalid preview_port=18081\n"), errors.New("Process exited with status 2")
	})
	t.Cleanup(restore)
	_, err := svc.SwitchInjectUpstream(context.Background(), "sb-sw", 5173)
	if err == nil || !strings.Contains(err.Error(), "result=invalid") {
		t.Fatalf("err=%v", err)
	}
}

func TestProbeInjectUpstreamChecksHeader(t *testing.T) {
	db := newTestDB(t)
	fg := sandboxtest.New(t)
	fg.Seed("sb-pi")
	mgr := sandbox.NewManager(fg.Client(), sandbox.ManagerOptions{WorkspaceDir: "/root/workspace"})
	svc := NewPreviewService(db, mgr)

	served := "5173"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(injectUpstreamHeader, served)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	fg.SetEndpoints("sb-pi", map[string]string{
		"session": "127.0.0.1:34567",
		"ssh":     "127.0.0.1:2222",
		"18081":   srv.Listener.Addr().String(),
	})

	ctx := context.Background()
	if !svc.ProbeInjectUpstream(ctx, "sb-pi", 18081, 5173) {
		t.Fatal("matching upstream header must be healthy")
	}
	served = "18081"
	if svc.ProbeInjectUpstream(ctx, "sb-pi", 18081, 5173) {
		t.Fatal("a stale app on PREVIEW_PORT must not pass for the switched one")
	}
	if svc.ProbeInjectUpstream(ctx, "sb-pi", 18082, 5173) {
		t.Fatal("unresolvable PREVIEW_PORT must fail")
	}
	if (&PreviewService{}).ProbeInjectUpstream(ctx, "sb-pi", 18081, 5173) {
		t.Fatal("nil manager must fail")
	}
	srv.Close()
	if svc.ProbeInjectUpstream(ctx, "sb-pi", 18081, 5173) {
		t.Fatal("closed server must fail")
	}
}
