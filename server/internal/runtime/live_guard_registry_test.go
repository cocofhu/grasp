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

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// Exercise the production registry, including a non-default backend. Testing
// the concrete ACP provider alone cannot catch a missing capability forwarder.
func TestProviderRegistryLiveGuardRoutesToParkedBackend(t *testing.T) {
	ctx := context.Background()
	repo := t.TempDir()
	git := func(args ...string) ([]byte, error) {
		return exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.name", "Live registry test"}, {"config", "user.email", "live-registry@example.invalid"}} {
		if out, err := git(args...); err != nil {
			t.Fatalf("git setup: %v: %s", err, out)
		}
	}
	// A real source implementation can contain tracked marker examples.
	if err := os.WriteFile(filepath.Join(repo, "guide.md"), []byte("Example:\n<div data-grasp-live=\"sample01\"><section data-grasp-variant=\"1\" /></div>\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "guide.md"}, {"commit", "-qm", "baseline"}} {
		if out, err := git(args...); err != nil {
			t.Fatalf("git baseline: %v: %s", err, out)
		}
	}
	restore := sandbox.SetExecHook(func(ctx context.Context, _ string, _ int, _ string, stdin io.Reader) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "bash", "-s")
		cmd.Stdin = stdin
		return cmd.CombinedOutput()
	})
	t.Cleanup(restore)
	reg := NewProviderRegistry(mcp.NewHost(newMemStore()), Options{})
	acp := sandbox.NewACPClient("127.0.0.1", 9)
	acp.SeedBridgeForTest(sandbox.BridgeState{}, "", "")
	provider := reg.providers[BackendOpenCode].(*acpProvider)
	provider.sessions["run|node"] = &reactSession{sb: &sandbox.Sandbox{WorkspaceDir: repo}, acp: acp}
	if err := reg.PrepareLiveBaseline(ctx, "run", "node"); err != nil {
		t.Fatal(err)
	}
	if sids, parked, err := reg.LiveMarkerSIDs(ctx, "run", "node"); err != nil || !parked || len(sids) != 0 {
		t.Fatalf("tracked examples were not baselined through registry: %v %v %v", sids, parked, err)
	}
	reg.InstallLiveGuard(ctx, "run", "node")
	hook, err := os.ReadFile(filepath.Join(repo, ".git", "hooks", "pre-commit"))
	if err != nil || !strings.Contains(string(hook), "grasp-live-guard") {
		t.Fatalf("registry did not install the parked backend's guard: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "App.vue"), []byte(`<div data-grasp-live="actual01"><section data-grasp-variant="1" /></div>`), 0o600); err != nil {
		t.Fatal(err)
	}
	if sids, parked, err := reg.LiveMarkerSIDs(ctx, "run", "node"); err != nil || !parked || !reflect.DeepEqual(sids, []string{"actual01"}) {
		t.Fatalf("registry omitted new untracked markers: %v %v %v", sids, parked, err)
	}
	if out, err := git("add", "App.vue"); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}
	if out, err := git("commit", "-qm", "must remain a preview"); err == nil || !strings.Contains(string(out), "grasp:") {
		t.Fatalf("registry-installed hook allowed preview markers: %v: %s", err, out)
	}
	// Advancing HEAD must not silently refresh the fixed pre-Live baseline.
	if out, err := git("-c", "core.hooksPath=/dev/null", "commit", "-qm", "simulate bypassed hook"); err != nil {
		t.Fatalf("advance test HEAD: %v: %s", err, out)
	}
	if err := reg.PrepareLiveBaseline(ctx, "run", "node"); err != nil {
		t.Fatal(err)
	}
	if sids, parked, err := reg.LiveMarkerSIDs(ctx, "run", "node"); err != nil || !parked || !reflect.DeepEqual(sids, []string{"actual01"}) {
		t.Fatalf("registry allowed committed preview markers into baseline: %v %v %v", sids, parked, err)
	}
	if err := os.WriteFile(filepath.Join(repo, "App.vue"), []byte("<main>Adopted design</main>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if sids, parked, err := reg.LiveMarkerSIDs(ctx, "run", "node"); err != nil || !parked || len(sids) != 0 {
		t.Fatalf("cleaned source should pass through registry: %v %v %v", sids, parked, err)
	}
}

func TestProviderRegistryLiveGuardMissingOrUnavailableWorkspace(t *testing.T) {
	ctx := context.Background()
	reg := NewProviderRegistry(mcp.NewHost(newMemStore()), Options{})
	if err := reg.PrepareLiveBaseline(ctx, "run", "missing"); err == nil {
		t.Fatal("baseline preparation must not create a replacement sandbox")
	}
	if sids, parked, err := reg.LiveMarkerSIDs(ctx, "run", "missing"); err != nil || parked || sids != nil {
		t.Fatalf("missing session: %v %v %v", sids, parked, err)
	}
	reg.InstallLiveGuard(ctx, "run", "missing")
	restore := sandbox.SetExecHook(func(ctx context.Context, _ string, _ int, _ string, stdin io.Reader) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "bash", "-s")
		cmd.Stdin = stdin
		return cmd.CombinedOutput()
	})
	t.Cleanup(restore)
	acp := sandbox.NewACPClient("127.0.0.1", 9)
	acp.SeedBridgeForTest(sandbox.BridgeState{}, "", "")
	reg.providers[BackendClaudeCode].(*acpProvider).sessions["run|unavailable"] = &reactSession{
		sb: &sandbox.Sandbox{WorkspaceDir: filepath.Join(t.TempDir(), "gone")}, acp: acp,
	}
	if err := reg.PrepareLiveBaseline(ctx, "run", "unavailable"); err == nil {
		t.Fatal("registry swallowed the parked backend's baseline error")
	}
	if _, parked, err := reg.LiveMarkerSIDs(ctx, "run", "unavailable"); !parked || err == nil {
		t.Fatalf("registry swallowed the parked backend's scan error: %v %v", parked, err)
	}
}
