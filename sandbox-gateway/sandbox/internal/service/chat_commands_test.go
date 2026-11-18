package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backend/internal/provider"
)

type commandSession struct {
	stubSess
	prompts chan string
}

func (s *commandSession) Prompt(_ context.Context, prompt string, _ []provider.PromptImage) (provider.TurnResult, error) {
	s.prompts <- prompt
	return provider.TurnResult{StopReason: "end_turn"}, nil
}

func TestConsoleCommandsReuseWorkspaceAndKeepQueue(t *testing.T) {
	t.Setenv("GRASP_CONFIGURED_MODELS", "")
	home := t.TempDir()
	skill := filepath.Join(home, "skills", "review")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("# Review"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_ROOT", home)
	initial := &commandSession{stubSess: stubSess{id: "initial"}, prompts: make(chan string, 8)}
	b := newTestBridge(initial)
	restarts := 0
	b.testConnect = func(cwd, fsRoot string, _ json.RawMessage, _ *bool) (provider.Session, error) {
		if cwd != initial.CWD() || fsRoot != initial.FSRoot() {
			t.Errorf("workspace changed during reset: %q %q", cwd, fsRoot)
		}
		restarts++
		next := &commandSession{stubSess: stubSess{id: "fresh"}, prompts: initial.prompts}
		b.mu.Lock()
		b.sess = next
		b.mu.Unlock()
		return next, nil
	}
	for i, input := range []string{"old requirement", "/compact", "continue", "/new chat", "/skill review check draft", "plain"} {
		if err := b.ChatWithLimits(input, string(rune('a'+i)), UserChatCommand, nil, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	waitIdle(t, b)
	if restarts != 2 || len(initial.prompts) != 4 {
		t.Fatalf("resets=%d, provider prompts=%d", restarts, len(initial.prompts))
	}
	<-initial.prompts
	if prompt := <-initial.prompts; !strings.Contains(prompt, "old requirement") || !strings.HasSuffix(prompt, "continue") {
		t.Fatalf("compacted prompt = %q", prompt)
	}
	if prompt := <-initial.prompts; !strings.Contains(prompt, filepath.Join(skill, "SKILL.md")) ||
		strings.Contains(prompt, "old requirement") || !strings.HasSuffix(prompt, "check draft") {
		t.Fatalf("skill prompt = %q", prompt)
	}
	if prompt := <-initial.prompts; prompt != "plain" {
		t.Fatalf("ordinary prompt = %q", prompt)
	}
	if _, total, _ := b.EventLogRecentTurns(10); total != 6 {
		t.Fatalf("archived turns = %d, want 6", total)
	}
	if _, err := b.RestartChatContext(); err != nil {
		t.Fatal(err)
	}
	if history := b.compactTranscript(""); history != "" {
		t.Fatalf("platform reset left old history in the console: %q", history)
	}
}
