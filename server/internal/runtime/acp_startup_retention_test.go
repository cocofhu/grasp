package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/sandbox"
)

type uncertainStartupManager struct {
	known   bool
	creates int
	home    string
}

func (m *uncertainStartupManager) Create(_ context.Context, spec sandbox.Spec) (*sandbox.Sandbox, error) {
	m.creates++
	m.home = spec.ConfigHome
	if err := os.WriteFile(filepath.Join(spec.ConfigHome, "original-startup-receipt"), []byte(spec.Name), 0o600); err != nil {
		return nil, err
	}
	var sb *sandbox.Sandbox
	if m.known {
		sb = &sandbox.Sandbox{Name: "retained-provider-resource", ID: "retained-provider-resource", CorrelationName: spec.Name}
	}
	return sb, fmt.Errorf("%w: connection reset after create", sandbox.ErrCreateOutcomeUnknown)
}

type startupReceiptRegistry struct {
	countingRegistry
	begin, final RunSandboxInfo
}

func (r *startupReceiptRegistry) BeginRunSandbox(info RunSandboxInfo) { r.begin = info }
func (r *startupReceiptRegistry) RegisterRunSandbox(info RunSandboxInfo) {
	r.countingRegistry.RegisterRunSandbox(info)
	r.final = info
}

func TestRunAgentUnknownStartupRetainsReceiptWithoutReplacement(t *testing.T) {
	for _, known := range []bool{false, true} {
		t.Run(fmt.Sprint("known-provider-", known), func(t *testing.T) {
			host := mcp.NewHost(newMemStore())
			token := host.RegisterRun(t.Name())
			defer host.UnregisterRun(t.Name())
			manager := &uncertainStartupManager{known: known}
			provider, _ := newTestProvider(t, host, testOpts(), manager)
			provider.opts.SandboxMaxAttempts = 4
			registry := &startupReceiptRegistry{}
			provider.registry = registry
			req := reqWithProfile(NodeReq{RunID: t.Name(), NodeID: "work", NodeType: "agent", Token: token, Config: map[string]any{"prompt": "must not replay"}})
			_, err := provider.RunAgent(t.Context(), req)
			if !errors.Is(err, errSandboxStartupUnconfirmed) || isRetryableSandboxErr(err) {
				t.Fatalf("unknown accepted creation was retryable: %v", err)
			}
			defer removeHome(manager.home)
			if manager.creates != 1 || registry.unregistered != 0 || registry.registered != 1 {
				t.Fatalf("original receipt replaced/forgotten: creates=%d register=%d unregister=%d", manager.creates, registry.registered, registry.unregistered)
			}
			if registry.final.StartupError == "" || registry.begin.Name == "" || registry.final.CorrelationName != registry.begin.Name {
				t.Fatalf("failed startup identity lost: before=%+v after=%+v", registry.begin, registry.final)
			}
			if body, readErr := os.ReadFile(filepath.Join(manager.home, "original-startup-receipt")); readErr != nil || string(body) != registry.begin.Name {
				t.Fatalf("unknown startup removed its original home: %q %v", body, readErr)
			}
		})
	}
}
