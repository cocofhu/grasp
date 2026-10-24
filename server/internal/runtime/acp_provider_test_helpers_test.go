package runtime

import (
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/modelrouting"
)

// withTestModelBinding supplies a named-model resolver for fake CLI tests.
func withTestModelBinding(opts Options) Options {
	if opts.ModelBinding == nil {
		opts.ModelBinding = testModelBinding
	}
	return opts
}

// newACPProvider builds a Cursor-backend ACP provider for tests.
// Production wiring uses NewProviderRegistry → newBaseACPProvider.
func newACPProvider(host *mcp.Host, opts Options) ExecProvider {
	return newBaseACPProvider(host, withTestModelBinding(opts), BackendCursor)
}

// testModelBinding supplies an explicit saved model fixture for each test CLI.
func testModelBinding(backend string, binding *modelrouting.Binding) (modelrouting.Resolved, error) {
	if err := modelrouting.ValidateConfigurationReference(binding); err != nil {
		return modelrouting.Resolved{}, err
	}
	resolved := modelrouting.Resolved{Backend: backend, CredentialID: "test-model-key", Secret: "fake", Binding: modelrouting.Binding{Kind: "platform", CredentialID: "test-model-key", ModelID: "auto"}}
	switch backend {
	case "codex":
		resolved.Secret = chatgptLogin
	case "opencode", "deepseek_harness":
		resolved.Binding = modelrouting.Binding{Kind: "connection", ConnectionID: "test-connection", ModelID: "test/model"}
		resolved.Connection = &modelrouting.Connection{ID: "test-connection", Protocol: modelrouting.ProtocolChat, ProviderID: "test", BaseURL: "https://test.example/v1", Auth: modelrouting.Auth{Scheme: "bearer"}, Models: []modelrouting.Model{{ID: "test/model"}}}
	}
	return resolved, nil
}
