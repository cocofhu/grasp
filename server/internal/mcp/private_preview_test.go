package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/privatepreview"
)

func TestPrivatePreviewRegistrationAndRecovery(t *testing.T) {
	fastPreviewProbe(t)
	base, err := privatepreview.Parse("https://preview.example.net")
	if err != nil {
		t.Fatal(err)
	}
	h := NewHost(&memStore{})
	h.SetPrivatePreviewBase(base)
	store := &memPreviewStore{}
	h.SetPreviewStore(store)
	ops := newSwitchOps(InjectSwitch{Result: InjectSwitched, PreviewPort: 18081})
	h.SetPreviewSandboxOps(ops)
	reg, err := h.setPreviewPort("run", "node", 5174, "web")
	if err != nil {
		t.Fatal(err)
	}
	if !reg.Picked || !base.OwnsURL(reg.PublicURL) || reg.PublicURL != reg.ProxyURL || strings.Contains(reg.PublicURL, "10.0.0.8") {
		t.Fatalf("private registration: %+v", reg)
	}
	if len(store.ports) != 1 || store.ports[0].Host != "http://10.0.0.8:18081" || store.ports[0].InjectorPort != 18081 {
		t.Fatalf("injector binding not persisted: %+v", store.ports)
	}
	// A new server process uses the persisted capability without reissuing a
	// provider token, changing the browser origin, or losing the injector port.
	fresh := NewHost(&memStore{})
	fresh.SetPrivatePreviewBase(base)
	fresh.SetPreviewStore(store)
	fresh.SetPreviewSandboxOps(ops)
	ports := fresh.ListPreviewPorts("run", "node")
	if len(ports) != 1 || ports[0].Mode != "direct" || ports[0].DirectURL != reg.PublicURL || ports[0].InjectorPort != 18081 {
		t.Fatalf("recovered ports: %+v", ports)
	}
	body, err := json.Marshal(ports)
	if err != nil || strings.Contains(string(body), "10.0.0.8") || strings.Contains(string(body), "injectorPort") {
		t.Fatalf("private endpoint escaped into browser DTO: %s, %v", body, err)
	}
	// A repeated native set_preview (for example after a Live patch) retains
	// the origin and session storage of the already open browser tab.
	fresh.SetPreviewSandboxOps(newSwitchOps(InjectSwitch{Result: InjectSwitched, PreviewPort: 18081}))
	repeated, err := fresh.setPreviewPort("run", "node", 5174, "updated")
	if err != nil || repeated.PublicURL != reg.PublicURL {
		t.Fatalf("re-registration rotated origin: %+v, %v", repeated, err)
	}
	// A new sandbox generation must get a different capability.
	newOps := newSwitchOps(InjectSwitch{Result: InjectSwitched, PreviewPort: 18081})
	newOps.name = "next-sandbox"
	fresh.SetPreviewSandboxOps(newOps)
	replacement, err := fresh.setPreviewPort("run", "node", 5174, "replacement")
	if err != nil || replacement.PublicURL == reg.PublicURL || !base.OwnsURL(replacement.PublicURL) {
		t.Fatalf("new generation retained old capability: %+v, %v", replacement, err)
	}
	// Registrations created before opting in are not exposed as raw endpoints.
	store.ports[0].ProxyURL = "/preview/run/node/5174/"
	legacy := NewHost(&memStore{})
	legacy.SetPrivatePreviewBase(base)
	legacy.SetPreviewStore(store)
	legacy.SetPreviewSandboxOps(ops)
	if got := legacy.ListPreviewPorts("run", "node"); got[0].DirectURL != "" {
		t.Fatalf("legacy raw address leaked: %+v", got)
	}
}

func TestPrivatePreviewDisplacementTracksGuestPortAfterRepublish(t *testing.T) {
	fastPreviewProbe(t)
	base, _ := privatepreview.Parse("https://preview.example.net")
	previousURL := base.URL(strings.Repeat("a", 32))
	store := &memPreviewStore{ports: []PreviewPort{{
		RunID: "run", NodeID: "node", Kind: PreviewKindPort, Port: 3001,
		ProxyURL: previousURL, Host: "http://127.0.0.1:old", InjectorPort: 18081,
		SandboxName: "sb", Healthy: true,
	}}}
	h := NewHost(&memStore{})
	h.SetPrivatePreviewBase(base)
	h.SetPreviewStore(store)
	h.SetPreviewSandboxOps(newSwitchOps(InjectSwitch{Result: InjectSwitched, PreviewPort: 18081}))
	if _, err := h.setPreviewPort("run", "node", 5174, "new app"); err != nil {
		t.Fatal(err)
	}
	previous, ok := store.GetPreviewPort("run", "node", 3001)
	if !ok || previous.ProxyURL != previousURL || previous.InjectorPort != 0 || previous.Host != "http://10.0.0.8:49153" {
		t.Fatalf("displaced preview still points at injector: %+v", previous)
	}
}
