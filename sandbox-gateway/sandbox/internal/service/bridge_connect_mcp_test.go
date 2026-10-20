package service

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"backend/internal/provider"
)

func TestMcpUpgradeNeeded(t *testing.T) {
	cases := []struct {
		name        string
		current     json.RawMessage
		incoming    json.RawMessage
		wantUpgrade bool
	}{
		{"empty to servers", json.RawMessage(`[]`), json.RawMessage(`[{"name":"artifact-store","type":"http","url":"http://x"}]`), true},
		{"nil to servers", nil, json.RawMessage(`[{"name":"a"}]`), true},
		{"null to servers", json.RawMessage(`null`), json.RawMessage(`[{"name":"a"}]`), true},
		{"servers to servers", json.RawMessage(`[{"name":"a"}]`), json.RawMessage(`[{"name":"b"}]`), false},
		{"empty to empty", json.RawMessage(`[]`), json.RawMessage(`[]`), false},
		{"servers to empty", json.RawMessage(`[{"name":"a"}]`), json.RawMessage(`[]`), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mcpUpgradeNeeded(tc.current, tc.incoming); got != tc.wantUpgrade {
				t.Fatalf("mcpUpgradeNeeded(%s,%s)=%v want %v", tc.current, tc.incoming, got, tc.wantUpgrade)
			}
		})
	}
}

func TestMcpServerSummary(t *testing.T) {
	count, names := mcpServerSummary(json.RawMessage(`[{"name":"artifact-store"},{"name":"other"}]`))
	if count != 2 || len(names) != 2 || names[0] != "artifact-store" || names[1] != "other" {
		t.Fatalf("array summary count=%d names=%v", count, names)
	}
	count, names = mcpServerSummary(json.RawMessage(`{"artifact-store":{},"e2e":{}}`))
	if count != 2 || len(names) != 2 {
		t.Fatalf("object summary count=%d names=%v", count, names)
	}
	count, names = mcpServerSummary(json.RawMessage(`[]`))
	if count != 0 || names != nil {
		t.Fatalf("empty summary count=%d names=%v", count, names)
	}
}

// stubSess is a minimal provider.Session for EnsureAgent rebuild tests.
type stubSess struct {
	id string
}

func (s *stubSess) SessionID() string        { return s.id }
func (s *stubSess) CWD() string              { return "/tmp" }
func (s *stubSess) FSRoot() string           { return "/tmp" }
func (s *stubSess) Info() provider.AgentInfo { return provider.AgentInfo{Name: "stub"} }
func (s *stubSess) Prompt(context.Context, string, []provider.PromptImage) (provider.TurnResult, error) {
	return provider.TurnResult{StopReason: "end_turn"}, nil
}
func (s *stubSess) ReportsUsage() bool                              { return false }
func (s *stubSess) CumulativeUsage() map[string]provider.TokenUsage { return nil }
func (s *stubSess) Cancel() error                                   { return nil }
func (s *stubSess) Close() error                                    { return nil }
func (s *stubSess) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
func (s *stubSess) ExitInfo() (string, error) { return "stub exit", nil }

// TestEnsureAgentRebuildsOnEmptyToNonEmptyMCP (CAPA A4): empty default session
// + connect with non-empty mcpServers must rebuild (not reuse).
func TestEnsureAgentRebuildsOnEmptyToNonEmptyMCP(t *testing.T) {
	b := &Bridge{}
	old := &stubSess{id: "sess-empty-mcp"}
	b.mu.Lock()
	b.sess = old
	b.lastMCP = json.RawMessage(`[]`)
	b.mu.Unlock()

	var rebuilds atomic.Int32
	newSess := &stubSess{id: "sess-with-mcp"}
	b.testConnect = func(cwd, fsRoot string, mcp json.RawMessage, auto *bool) (provider.Session, error) {
		rebuilds.Add(1)
		if mcpEmpty(mcp) {
			t.Fatalf("rebuild Connect called with empty mcp: %s", mcp)
		}
		b.mu.Lock()
		b.sess = newSess
		b.lastMCP = append(json.RawMessage(nil), mcp...)
		b.mu.Unlock()
		return newSess, nil
	}

	incoming := json.RawMessage(`[{"name":"artifact-store","type":"http","url":"http://x"}]`)
	got, err := b.EnsureAgent("/tmp", "/tmp", incoming, nil)
	if err != nil {
		t.Fatalf("EnsureAgent: %v", err)
	}
	if rebuilds.Load() != 1 {
		t.Fatalf("want exactly 1 rebuild Connect, got %d", rebuilds.Load())
	}
	if got.SessionID() != "sess-with-mcp" {
		t.Fatalf("want rebuilt session, got %q", got.SessionID())
	}

	// Second EnsureAgent with same non-empty MCP must reuse (no second rebuild).
	got2, err := b.EnsureAgent("/tmp", "/tmp", incoming, nil)
	if err != nil {
		t.Fatalf("EnsureAgent reuse: %v", err)
	}
	if rebuilds.Load() != 1 {
		t.Fatalf("reuse path must not rebuild again, rebuilds=%d", rebuilds.Load())
	}
	if got2.SessionID() != "sess-with-mcp" {
		t.Fatalf("want reused session, got %q", got2.SessionID())
	}
}
