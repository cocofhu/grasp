package execution

import "testing"

func TestModeDefaultsToSandbox(t *testing.T) {
	t.Setenv(EnvMode, "")
	if Mode() != ModeSandbox || Worktree() {
		t.Fatalf("mode=%s", Mode())
	}
	t.Setenv(EnvMode, "sandbox")
	if Worktree() {
		t.Fatal("sandbox image must not take the worktree path")
	}
	t.Setenv(EnvMode, ModeWorktree)
	if Mode() != ModeWorktree || !Worktree() {
		t.Fatal("worktree image was not recognized")
	}
}
