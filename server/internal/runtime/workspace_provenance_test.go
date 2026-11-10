package runtime

import (
	"testing"

	"github.com/cocofhu/grasp/internal/sandbox"
)

type provenanceRegistry struct {
	begun, registered []RunSandboxInfo
}

func (r *provenanceRegistry) BeginRunSandbox(info RunSandboxInfo) {
	r.begun = append(r.begun, info)
}
func (r *provenanceRegistry) RegisterRunSandbox(info RunSandboxInfo) {
	r.registered = append(r.registered, info)
}
func (*provenanceRegistry) UnregisterRunSandbox(string) {}

func TestSandboxRegistrationKeepsExactWorkspaceProvenance(t *testing.T) {
	r := &provenanceRegistry{}
	p := &acpProvider{registry: r}
	req := NodeReq{RunID: "run", NodeID: "node", Iteration: 3, StateRunID: 42, RunAttempt: 2}
	p.beginRunSandbox(req, "pending", "")
	p.registerRunSandbox(req, &sandbox.Sandbox{Name: "physical", CorrelationName: "pending"}, "")
	req.NodeID, req.StateRunID, req.Iteration = "next", 77, 4
	p.registerSharedRunSandbox(req, &sandbox.Sandbox{Name: "shared"}, "")
	if len(r.begun) != 1 || len(r.registered) != 2 {
		t.Fatalf("registrations: %+v", r)
	}
	for _, info := range []RunSandboxInfo{r.begun[0], r.registered[0]} {
		if info.StateRunID != 42 || info.RunAttempt != 2 || info.Iteration != 3 || info.ProviderAttempt != 0 {
			t.Fatalf("per-node attribution changed: %+v", info)
		}
	}
	last := r.registered[1]
	if !last.Shared || last.StateRunID != 77 || last.NodeID != "next" || last.Iteration != 4 || last.RunAttempt != 2 || last.ProviderAttempt != 0 {
		t.Fatalf("shared attribution changed: %+v", last)
	}
}
