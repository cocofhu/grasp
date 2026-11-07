package runtime

import (
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestProvidersShareConfiguredGateway(t *testing.T) {
	gateway, err := sandbox.NewConfiguredGatewayClient(config.SandboxConfig{GatewayURL: "http://gateway.example.test", GatewayAPIKey: "synthetic-key", GatewayTransport: "gateway"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gateway.Close() })
	registry := NewProviderRegistry(mcp.NewHost(newMemStore()), Options{Gateway: gateway})
	for backend, provider := range registry.providers {
		acp, ok := provider.(*acpProvider)
		if !ok {
			t.Fatalf("%s has unexpected provider type %T", backend, provider)
		}
		manager, ok := acp.mgr.(*sandbox.Manager)
		if !ok || manager.Gateway() != gateway {
			t.Fatalf("%s lost the configured gateway registry", backend)
		}
	}
}
