// Package execution records which image this process is: worktree or sandbox.
// The value is baked into the image and is not a saved setting.
package execution

import (
	"os"
	"strings"
)

const (
	// EnvMode is set in the image. Operators change it by deploying the other image.
	EnvMode = "GRASP_EXECUTION_MODE"
	// ModeSandbox is the slim Grasp image plus the sandbox image.
	ModeSandbox = "sandbox"
	// ModeWorktree runs agents in this image, with no sandbox container.
	ModeWorktree = "worktree"

	LaunchRun       = "run"
	LaunchSolo      = "solo"
	LaunchTest      = "test"
	LaunchAssistant = "assistant"
)

// Mode returns the image execution mode. Anything other than worktree is sandbox,
// which is what existing deployments already do.
func Mode() string {
	if strings.TrimSpace(os.Getenv(EnvMode)) == ModeWorktree {
		return ModeWorktree
	}
	return ModeSandbox
}

// Worktree reports whether this process must execute locally.
func Worktree() bool { return Mode() == ModeWorktree }
