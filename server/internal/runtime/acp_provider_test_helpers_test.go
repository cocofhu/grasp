package runtime

import (
	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/mcp"
)

// testAuthCredentials holds a fake workspace credential for every ACP backend.
var testAuthCredentials = map[string]string{
	envauth.EnvCursorAPIKey:    "fake",
	envauth.EnvClaudeAPIKey:    "fake",
	envauth.EnvCodeBuddyAPIKey: "fake",
	envauth.EnvTraeAPIKey:      "fake",
	envauth.EnvOpenCodeAPIKey:  "fake",
	envauth.EnvDeepSeekAPIKey:  "fake",
}

// withTestCredentials supplies testAuthCredentials as workspace credentials
// unless the test configured its own.
func withTestCredentials(opts Options) Options {
	if opts.Credentials == nil {
		opts.Credentials = testCredentials
	}
	return opts
}

func testCredentials() map[string]string {
	out := make(map[string]string, len(testAuthCredentials))
	for k, v := range testAuthCredentials {
		out[k] = v
	}
	return out
}

// newACPProvider builds a Cursor-backend ACP provider for tests.
// Production wiring uses NewProviderRegistry → newBaseACPProvider.
func newACPProvider(host *mcp.Host, opts Options) ExecProvider {
	return newBaseACPProvider(host, withTestCredentials(opts), BackendCursor)
}
