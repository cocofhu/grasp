package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"backend/internal/provider"
)

// persistedBridge is a backend process whose default chat persists its
// session at path.
func persistedBridge(t *testing.T, agent *lifeAgent, path string) *Bridge {
	t.Helper()
	b := NewBridge()
	b.agentProvider = agent
	b.turnIdle, b.turnMax = 0, 0
	b.PersistSession(path)
	t.Cleanup(func() {
		_, sessions := agent.opened()
		for _, s := range sessions {
			s.die()
		}
	})
	return b
}

// awaitPointer waits until the pointer at path records the dialogue.
func awaitPointer(t *testing.T, path string) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); ; {
		var p sessionPointer
		if raw, err := os.ReadFile(path); err == nil && json.Unmarshal(raw, &p) == nil && p.History {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the pointer never recorded the dialogue")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRestartedBackendContinuesThePersistedSession(t *testing.T) {
	agent := &lifeAgent{transport: provider.LongLived}
	path, cwd := filepath.Join(t.TempDir(), "agent-session.json"), t.TempDir()
	prev := persistedBridge(t, agent, path)
	first, err := prev.EnsureAgent(cwd, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	takeTurn(t, prev, first.(*lifeSess))
	awaitPointer(t, path)

	next := persistedBridge(t, agent, path)
	sess, err := next.EnsureAgent(cwd, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if opens, _ := agent.opened(); opens[1].ResumeSessionID != "native-1" || sess.SessionID() != "native-1" {
		t.Fatalf("restart opened %+v as %q, want native-1 resumed", opens[1], sess.SessionID())
	} else if opens[1].StateDir != filepath.Dir(path) {
		t.Fatalf("state dir = %q, want the pointer's directory", opens[1].StateDir)
	}
	if next.ConnectedPayload(sess)["resumed"] != true {
		t.Fatal("connected must report that the native session was resumed")
	}
}

func TestAFailedFirstOpenStillLeavesThePersistedSessionToResume(t *testing.T) {
	agent := &lifeAgent{transport: provider.LongLived}
	path, cwd := filepath.Join(t.TempDir(), "agent-session.json"), t.TempDir()
	prev := persistedBridge(t, agent, path)
	first, err := prev.EnsureAgent(cwd, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	takeTurn(t, prev, first.(*lifeSess))
	awaitPointer(t, path)

	next := persistedBridge(t, agent, path)
	agent.mu.Lock()
	agent.failOpens = 1
	agent.mu.Unlock()
	if _, err := next.EnsureAgent(cwd, "", nil, nil); err == nil {
		t.Fatal("the first open was meant to fail")
	}
	sess, err := next.EnsureAgent(cwd, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if opens, _ := agent.opened(); opens[1].ResumeSessionID != "native-1" || sess.SessionID() != "native-1" {
		t.Fatalf("retry opened %+v as %q, want native-1 resumed", opens[1], sess.SessionID())
	}
}

func TestRestartedBackendDoesNotReportAnUnusedSessionAsResumed(t *testing.T) {
	agent := &lifeAgent{transport: provider.LongLived}
	path, cwd := filepath.Join(t.TempDir(), "agent-session.json"), t.TempDir()
	if _, err := persistedBridge(t, agent, path).EnsureAgent(cwd, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	next := persistedBridge(t, agent, path)
	sess, err := next.EnsureAgent(cwd, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if opens, _ := agent.opened(); opens[1].ResumeSessionID != "native-1" {
		t.Fatalf("restart opened %+v, want native-1 resumed", opens[1])
	}
	if next.ConnectedPayload(sess)["resumed"] != false {
		t.Fatal("a session that never took a turn holds no dialogue to keep")
	}
}

func TestPersistedSessionIsOnlyResumedWhereItBelongs(t *testing.T) {
	agent := &lifeAgent{transport: provider.LongLived}
	path := filepath.Join(t.TempDir(), "agent-session.json")
	if _, err := persistedBridge(t, agent, path).EnsureAgent(t.TempDir(), "", nil, nil); err != nil {
		t.Fatal(err)
	}
	other := persistedBridge(t, agent, path)
	sess, err := other.EnsureAgent(t.TempDir(), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if opens, _ := agent.opened(); opens[1].ResumeSessionID != "" {
		t.Fatalf("another directory resumed %q", opens[1].ResumeSessionID)
	}
	if other.ConnectedPayload(sess)["resumed"] != false {
		t.Fatal("a new conversation must not be reported as resumed")
	}
}

func TestNewConversationReplacesThePersistedSession(t *testing.T) {
	agent := &lifeAgent{transport: provider.LongLived}
	path := filepath.Join(t.TempDir(), "agent-session.json")
	b := persistedBridge(t, agent, path)
	if _, err := b.EnsureAgent(t.TempDir(), "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := b.RestartAgent(); err != nil {
		t.Fatal(err)
	}
	var p sessionPointer
	raw, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(raw, &p) != nil || p.Session != "native-2" {
		t.Fatalf("pointer = %s (%v), want native-2", raw, err)
	}
}

func TestSessionThatCannotResumeLeavesNoPointer(t *testing.T) {
	agent := &lifeAgent{transport: provider.LongLived}
	path := filepath.Join(t.TempDir(), "agent-session.json")
	b := persistedBridge(t, agent, path)
	if _, err := b.EnsureAgent(t.TempDir(), "", nil, nil); err != nil {
		t.Fatal(err)
	}
	agent.noResume = true
	if _, err := b.RestartAgent(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("pointer still present: %v", err)
	}
}

func TestMCPUpgradeKeepsTheConversation(t *testing.T) {
	agent := &lifeAgent{transport: provider.LongLived}
	b := NewBridge()
	b.agentProvider = agent
	cwd := t.TempDir()
	first, err := b.EnsureAgent(cwd, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	takeTurn(t, b, first.(*lifeSess))
	visible := len(b.evLog.snapshot())
	sess, err := b.EnsureAgent(cwd, "", json.RawMessage(`[{"name":"grasp-tool"}]`), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { first.(*lifeSess).die(); sess.(*lifeSess).die() })
	if sess.SessionID() != first.SessionID() || b.ConnectedPayload(sess)["resumed"] != true {
		t.Fatalf("upgrade opened %q, want %q resumed", sess.SessionID(), first.SessionID())
	}
	if visible == 0 || len(b.evLog.snapshot()) != visible {
		t.Fatal("adding MCP servers must keep the visible conversation")
	}
}

// A sandbox starts its agent before the platform connects with MCP servers;
// continuing that unused session must not read as keeping the dialogue, or a
// cold restore would skip replaying it.
func TestMCPUpgradeOfAnUnusedSessionIsNotResumed(t *testing.T) {
	agent := &lifeAgent{transport: provider.LongLived}
	b := NewBridge()
	b.agentProvider = agent
	cwd := t.TempDir()
	first, err := b.EnsureAgent(cwd, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := b.EnsureAgent(cwd, "", json.RawMessage(`[{"name":"grasp-tool"}]`), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { first.(*lifeSess).die(); sess.(*lifeSess).die() })
	if sess.SessionID() != first.SessionID() {
		t.Fatalf("upgrade opened %q, want %q", sess.SessionID(), first.SessionID())
	}
	if b.ConnectedPayload(sess)["resumed"] != false {
		t.Fatal("an unused session must not be reported as resumed")
	}
}
