package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backend/internal/provider"
)

// Opt-in only, inside a disposable container with no host workspace write
// mounts. Normal test runs never use a real account or consume model usage.
func TestLiveCodexEditAndResume(t *testing.T) {
	if os.Getenv("GRASP_CODEX_LIVE") != "1" {
		t.Skip("opt-in container smoke test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dir := t.TempDir()
	s, err := New().Open(ctx, ctx, provider.OpenOptions{Cwd: dir, FSRoot: dir, AutoPermission: true}, func(json.RawMessage) {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	for _, step := range []struct{ prompt, want string }{
		{"Create sum.py in the current directory containing exactly: def add(a, b): return a + b . Then run python3 to assert add(2, 3) == 5. Do not read credentials or other directories. Reply DONE.", "return a + b"},
		{"Edit the same sum.py: rename function add to sum_numbers, keep its implementation. Run python3 to assert sum_numbers(2, 3) == 5. Reply DONE.", "def sum_numbers"},
	} {
		result, err := s.Prompt(ctx, step.prompt, nil)
		if err != nil {
			t.Fatal(err)
		}
		if result.StopReason != "end_turn" {
			t.Fatalf("stop=%s", result.StopReason)
		}
		b, err := os.ReadFile(filepath.Join(dir, "sum.py"))
		if err != nil || !strings.Contains(string(b), step.want) {
			t.Fatalf("expected edit %q, err=%v", step.want, err)
		}
		if s.SessionID() == "" {
			t.Fatal("session id missing")
		}
		if len(result.Usage) == 0 {
			t.Fatal("usage missing")
		}
	}
}
