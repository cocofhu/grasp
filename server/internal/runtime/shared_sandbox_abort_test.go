package runtime

import (
	"context"
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// Two backend providers share one coordinator (as NewProviderRegistry wires
// them). The node serving a preview lives in one provider; whichever provider
// the registry happens to abort first, the shared container must be retired
// for the preview, not destroyed. Registry order is map order, so each case
// is repeated to cover both orders.
func TestRegistryAbortRunKeepsSharedPreviewAcrossProviders(t *testing.T) {
	for _, previewIn := range []AcpBackend{BackendCursor, BackendCodex} {
		t.Run(string(previewIn), func(t *testing.T) {
			for round := 0; round < 16; round++ {
				abortSharedPreviewRun(t, previewIn)
			}
		})
	}
}

func abortSharedPreviewRun(t *testing.T, previewIn AcpBackend) {
	t.Helper()
	host := mcp.NewHost(newMemStore())
	host.PutPreviewPortForTest("runX", "nodeP", 8080, "web")
	coord := newSharedSandboxCoordinator()
	reg := &retiringRegistry{}
	dm := &destroyingManager{}
	providers := map[AcpBackend]ExecProvider{}
	for _, b := range []AcpBackend{BackendCursor, BackendCodex} {
		p := newACPProvider(host, Options{}).(*acpProvider)
		p.registry, p.coordinator, p.mgr = reg, coord, dm
		providers[b] = p
	}
	sb := &sandbox.Sandbox{Name: "shared-x"}
	if _, err := coord.acquire(context.Background(), "runX", "", func() (sharedCreateResult, error) {
		return sharedCreateResult{sb: sb, home: t.TempDir()}, nil
	}); err != nil {
		t.Fatal(err)
	}
	holder := providers[previewIn].(*acpProvider)
	holder.mu.Lock()
	holder.inflightACP["runX|nodeP"] = nil
	holder.live["runX|nodeP"] = sb
	holder.mu.Unlock()

	(&ProviderRegistry{providers: providers, coordinator: coord}).AbortRun("runX")

	reg.mu.Lock()
	retired, unregistered := reg.retired, reg.unregistered
	reg.mu.Unlock()
	if retired != 1 || unregistered != 0 || len(dm.destroyedIDs()) != 0 {
		t.Fatalf("shared container serving a preview was torn down: retired=%d unregistered=%d destroyed=%v",
			retired, unregistered, dm.destroyedIDs())
	}
}
