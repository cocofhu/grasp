package services

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestWorkspaceBindingPreviewTransportOnlyWithoutMetadataRequiresUnboundRegistration(t *testing.T) {
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("X-Grasp-Upstream", "5173")
	}))
	t.Cleanup(upstream.Close)
	input := mcp.PreviewPort{SandboxName: "legacy-transport", Port: 5173, Host: upstream.URL}
	preview := &PreviewService{}
	if base, healthy := preview.ProbeRegisteredPort(t.Context(), input); base != upstream.URL || !healthy || requests.Load() != 1 {
		t.Fatalf("unbound transport probe failed: base=%s healthy=%t requests=%d", base, healthy, requests.Load())
	}
	for _, field := range []string{"binding-id", "fingerprint", "configured-service"} {
		t.Run(field, func(t *testing.T) {
			preview := &PreviewService{}
			rec := input
			switch field {
			case "binding-id":
				rec.WorkspaceBindingID = "persisted-binding"
			case "fingerprint":
				rec.WorkspaceBindingFingerprint = "persisted-proof"
			case "configured-service":
				preview.leases = &SandboxService{}
				preview.leases.SetWorkspaceBindings(&WorkspaceBindingService{})
			}
			if base, healthy := preview.ProbeRegisteredPort(t.Context(), rec); base != "" || healthy {
				t.Fatalf("missing binding metadata trusted a transport: base=%s healthy=%t", base, healthy)
			}
			closeConsumer, err := preview.BeginPreviewConsumer(t.Context(), rec)
			if !errors.Is(err, ErrWorkspaceJobUnsupported) {
				t.Fatalf("missing binding metadata authorized a consumer: %v", err)
			}
			if err := closeConsumer(); err != nil {
				t.Fatal(err)
			}
		})
	}
	if requests.Load() != 1 {
		t.Fatalf("rejected bindings probed an upstream: %d", requests.Load())
	}
}

type bindingPortTestRemote struct {
	*bindingTestRemote
	reject bool
	seen   int
}

func (r *bindingPortTestRemote) WorkspaceBindingPort(_ context.Context, port int) (sandbox.WorkspaceBindingPort, error) {
	r.seen++
	if r.reject {
		return sandbox.WorkspaceBindingPort{}, ErrWorkspaceJobConflict
	}
	return sandbox.WorkspaceBindingPort{Version: 1, Reference: r.view.Head.Reference, Port: port, Verified: true}, nil
}
func TestWorkspaceBindingPreviewRegistrationMustProveListenerRootAndStaysStaleAfterSwitch(t *testing.T) {
	bindings, remote, solo, _ := bindingResolverFixture(t)
	portRemote := &bindingPortTestRemote{bindingTestRemote: remote}
	bindings.options.Remote = func(context.Context, uint) (WorkspaceBindingRemote, error) { return portRemote, nil }
	bindings.sandbox.SetWorkspaceBindings(bindings)
	preview := NewPreviewService(bindings.db, nil)
	preview.SetSandboxLeases(bindings.sandbox)
	input := mcp.PreviewPort{NodeID: "solo", Kind: mcp.PreviewKindPort, Port: 3000, SandboxName: remote.caps.Resource, Host: "http://fixture-preview:3000", ProxyURL: "/preview/owned/solo/3000/", Healthy: true}
	portRemote.reject = true
	if _, err := preview.BindPreviewPort(t.Context(), input); !errors.Is(err, ErrWorkspaceJobConflict) {
		t.Fatal("unproved listener assigned new binding", err)
	}
	portRemote.reject = false
	bound, err := preview.BindPreviewPort(t.Context(), input)
	if err != nil || bound.WorkspaceBindingID == "" || bound.WorkspaceBindingFingerprint != remote.view.Head.Reference.Fingerprint {
		t.Fatal(bound, err)
	}
	if err := preview.UpsertPreviewPort(bound); err != nil {
		t.Fatal(err)
	}
	got, ok := preview.GetPreviewPort("", "solo", 3000)
	if !ok || got.Stale || !got.Healthy {
		t.Fatal(got, ok)
	}
	if err := bindings.db.Model(&models.WorkspaceBindingHead{}).Where("sandbox_id = ?", solo.SandboxID).Updates(map[string]any{"native_id": "new-native-head", "binding_id": "new-history-binding", "revision": 2, "native_fingerprint": "different-native-proof"}).Error; err != nil {
		t.Fatal(err)
	}
	got, ok = preview.GetPreviewPort("", "solo", 3000)
	if !ok || !got.Stale || got.Healthy || got.DirectURL != "" {
		t.Fatal("old preview presented as new root", got, ok)
	}
	if err := preview.UpsertPreviewPort(bound); err == nil {
		t.Fatal("late old-root preview publication overwrote new registration")
	}
	if _, err := preview.BeginPreviewConsumer(t.Context(), bound); err == nil {
		t.Fatal("old preview opened after switch")
	}
	if portRemote.seen != 2 {
		t.Fatal("unexpected listener inspection", portRemote.seen)
	}
}
