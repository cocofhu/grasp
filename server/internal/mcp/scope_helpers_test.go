package mcp

import "sync"

// testScopes is an in-memory namespace owner for this package's tests.
type testScopes struct {
	mu     sync.Mutex
	tokens map[string]string
}

var testScopesByHost sync.Map // *Host -> *testScopes

func scopesFor(h *Host) *testScopes {
	if v, ok := testScopesByHost.Load(h); ok {
		return v.(*testScopes)
	}
	v, loaded := testScopesByHost.LoadOrStore(h, &testScopes{tokens: map[string]string{}})
	s := v.(*testScopes)
	if !loaded {
		h.AddScopeSource(s.resolve)
	}
	return s
}

func (s *testScopes) resolve(runID string) (string, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tok, ok := s.tokens[runID]
	return tok, ok, ok
}

// grantScope makes runID a live namespace on h and returns a fresh token.
func grantScope(h *Host, runID string) string {
	s := scopesFor(h)
	tok := NewToken()
	s.mu.Lock()
	s.tokens[runID] = tok
	s.mu.Unlock()
	return tok
}

// endScope ends runID's namespace and releases the host's state for it.
func endScope(h *Host, runID string) {
	s := scopesFor(h)
	s.mu.Lock()
	delete(s.tokens, runID)
	s.mu.Unlock()
	h.ReleaseRun(runID, "test")
}
