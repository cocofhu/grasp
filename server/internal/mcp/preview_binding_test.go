package mcp

import (
	"errors"
	"testing"
)

func TestCanonicalPreviewUsesDurableStaleStateAndNeverReadvertisesDirectLinks(t *testing.T) {
	h := NewHost(nil)
	h.SetPreviewSandboxOps(&fakePreviewOps{direct: true})
	fresh := PreviewPort{RunID: "solo-artifacts", NodeID: "solo", Port: 3000, Kind: PreviewKindPort, SandboxName: "resource-original", Host: "http://127.0.0.1:3000", ProxyURL: "/preview/solo-artifacts/solo/3000/", Healthy: true, WorkspaceBindingID: "retained-binding", WorkspaceBindingFingerprint: "native-reference"}
	h.previewMem = map[string][]PreviewPort{previewKey(fresh.RunID, fresh.NodeID): {fresh}}
	stale := fresh
	stale.Stale = true
	stale.Healthy = false
	store := &memPreviewStore{ports: []PreviewPort{stale}}
	h.SetPreviewStore(store)
	for _, kind := range []string{"stale", "store_error", "missing_durable", "current"} {
		t.Run(kind, func(t *testing.T) {
			store.err = nil
			store.ports = []PreviewPort{stale}
			switch kind {
			case "store_error":
				store.err = errors.New("metadata unavailable")
			case "missing_durable":
				store.ports = nil
			case "current":
				store.ports = []PreviewPort{fresh}
			}
			got := h.ListPreviewPorts(fresh.RunID, fresh.NodeID)
			if len(got) != 1 || got[0].Mode != "" || got[0].DirectURL != "" {
				t.Fatal("untracked direct link advertised", got)
			}
			if kind != "current" && (!got[0].Stale || got[0].Healthy) {
				t.Fatal("memory masked durable uncertainty", got)
			}
			if kind == "current" && (got[0].Stale || !got[0].Healthy) {
				t.Fatal("current registration lost", got)
			}
		})
	}
}
