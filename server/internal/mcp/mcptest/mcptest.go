// Package mcptest provides an in-memory artifact MCP namespace owner for tests
// that need a token without a real workflow run, Solo session or sandbox.
package mcptest

import (
	"sync"

	"github.com/cocofhu/grasp/internal/mcp"
)

type scopes struct {
	mu     sync.Mutex
	tokens map[string]string
}

var byHost sync.Map // *mcp.Host -> *scopes

func scopesFor(h *mcp.Host) *scopes {
	if v, ok := byHost.Load(h); ok {
		return v.(*scopes)
	}
	v, loaded := byHost.LoadOrStore(h, &scopes{tokens: map[string]string{}})
	s := v.(*scopes)
	if !loaded {
		h.AddScopeSource(s.resolve)
	}
	return s
}

func (s *scopes) resolve(runID string) (string, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tok, ok := s.tokens[runID]
	return tok, ok, ok
}

// Grant makes runID a live namespace on h and returns a fresh token.
func Grant(h *mcp.Host, runID string) string {
	s := scopesFor(h)
	tok := mcp.NewToken()
	s.mu.Lock()
	s.tokens[runID] = tok
	s.mu.Unlock()
	return tok
}

// End ends runID's namespace on h and releases the host's state for it.
func End(h *mcp.Host, runID string) {
	s := scopesFor(h)
	s.mu.Lock()
	delete(s.tokens, runID)
	s.mu.Unlock()
	h.ReleaseRun(runID, "test")
}
