package mcp

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type staleDisplacementOps struct {
	*switchPreviewOps
	failedPort  int
	probedPorts []int
	afterSwitch func()
}

func (s *staleDisplacementOps) ProbeHTTPPort(ctx context.Context, name string, port int) bool {
	s.probedPorts = append(s.probedPorts, port)
	return port != s.failedPort && s.fakePreviewOps.ProbeHTTPPort(ctx, name, port)
}

func (s *staleDisplacementOps) SwitchInjectUpstream(ctx context.Context, name string, port int) (InjectSwitch, error) {
	result, err := s.switchPreviewOps.SwitchInjectUpstream(ctx, name, port)
	if s.afterSwitch != nil {
		s.afterSwitch()
	}
	return result, err
}

func staleWorkspacePreview() PreviewPort {
	return PreviewPort{RunID: "r", NodeID: "n", Kind: PreviewKindPort, Port: 3000,
		SandboxName: "sb", ProxyURL: "/preview/r/n/3000/", Host: "http://10.0.0.8:18081", InjectorPort: 18081,
		WorkspaceBindingID: "retired-workspace", WorkspaceBindingFingerprint: "retired-proof", Stale: true}
}

func TestSetPreviewSwitchIgnoresUnavailableRetiredWorkspaceWithoutRepublishingIt(t *testing.T) {
	fastPreviewProbe(t)
	previous := staleWorkspacePreview()
	store := &memPreviewStore{ports: []PreviewPort{previous}}
	h := NewHost(&memStore{})
	h.SetPreviewStore(store)
	ops := &staleDisplacementOps{switchPreviewOps: newSwitchOps(InjectSwitch{Result: InjectSwitched, PreviewPort: 18081}), failedPort: previous.Port}
	h.SetPreviewSandboxOps(ops)
	reg, err := h.setPreviewPort("r", "n", 5173, "current workspace")
	if err != nil || reg.ProxyURL == "" || len(ops.switches) != 1 || ops.switches[0] != 5173 {
		t.Fatalf("retired workspace blocked current preview: reg=%+v err=%v switches=%v", reg, err, ops.switches)
	}
	for _, port := range append(ops.published, ops.probedPorts...) {
		if port == previous.Port {
			t.Fatal("retired preview was probed or republished", port)
		}
	}
	old, ok := store.GetPreviewPort("r", "n", previous.Port)
	if !ok || !reflect.DeepEqual(*old, previous) {
		t.Fatal("retired registration was rewritten for the current root", old)
	}
	if current, ok := store.GetPreviewPort("r", "n", 5173); !ok || !current.Healthy || current.Stale {
		t.Fatal("new preview was not registered", current)
	}
}

func TestSetPreviewSwitchStillProtectsUnreachableCurrentWorkspaceRegistration(t *testing.T) {
	fastPreviewProbe(t)
	previous := staleWorkspacePreview()
	previous.Stale, previous.Healthy = false, true
	previous.WorkspaceBindingID, previous.WorkspaceBindingFingerprint = "current-workspace", "current-proof"
	store := &memPreviewStore{ports: []PreviewPort{previous}}
	h := NewHost(&memStore{})
	h.SetPreviewStore(store)
	ops := &staleDisplacementOps{switchPreviewOps: newSwitchOps(InjectSwitch{Result: InjectSwitched, PreviewPort: 18081}), failedPort: previous.Port}
	h.SetPreviewSandboxOps(ops)
	if _, err := h.setPreviewPort("r", "n", 5173, "new app"); err == nil || !strings.Contains(err.Error(), "旧端口 3000 发布后不可达") {
		t.Fatal("network failure retired a current registration", err)
	}
	if len(ops.switches) != 0 || len(store.ports) != 1 || !reflect.DeepEqual(store.ports[0], previous) {
		t.Fatal("current preview was switched or modified after a failed prerequisite", ops.switches, store.ports)
	}
}

type staleDisplacementBindingStore struct {
	*memPreviewStore
	bindingChanged bool
	bindCalls      int
}

func (s *staleDisplacementBindingStore) BindPreviewPort(_ context.Context, rec PreviewPort) (PreviewPort, error) {
	s.bindCalls++
	if s.bindingChanged {
		return rec, errors.New("workspace binding changed during injector switch")
	}
	rec.WorkspaceBindingID, rec.WorkspaceBindingFingerprint = "current-workspace", "current-proof"
	return rec, nil
}

func TestSetPreviewSwitchRetiredBypassDoesNotBypassFinalBindingProof(t *testing.T) {
	fastPreviewProbe(t)
	previous := staleWorkspacePreview()
	store := &staleDisplacementBindingStore{memPreviewStore: &memPreviewStore{ports: []PreviewPort{previous}}}
	h := NewHost(&memStore{})
	h.SetPreviewStore(store)
	ops := &staleDisplacementOps{switchPreviewOps: newSwitchOps(InjectSwitch{Result: InjectSwitched, PreviewPort: 18081}), failedPort: previous.Port,
		afterSwitch: func() { store.bindingChanged = true }}
	h.SetPreviewSandboxOps(ops)
	if _, err := h.setPreviewPort("r", "n", 5173, "new app"); err == nil || !strings.Contains(err.Error(), "binding changed") {
		t.Fatal("late binding change authorized a preview", err)
	}
	if store.bindCalls != 1 || len(ops.switches) != 1 || len(store.ports) != 1 || !reflect.DeepEqual(store.ports[0], previous) {
		t.Fatal("late binding rejection mutated registrations", store.bindCalls, ops.switches, store.ports)
	}
	if ports := h.ListPreviewPorts("r", "n"); len(ports) != 1 || !ports[0].Stale || ports[0].Healthy {
		t.Fatal("failed current registration escaped through memory", ports)
	}
}
