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
	// ModeLocal runs agents on the host machine using worktree isolation and host CLI/Git authorization.
	ModeLocal = "local"

	LaunchRun       = "run"
	LaunchSolo      = "solo"
	LaunchTest      = "test"
	LaunchAssistant = "assistant"
)

// Mode returns the execution mode ("local", "worktree", or "sandbox").
func Mode() string {
	switch strings.TrimSpace(os.Getenv(EnvMode)) {
	case ModeWorktree:
		return ModeWorktree
	case ModeLocal:
		return ModeLocal
	default:
		return ModeSandbox
	}
}

// Worktree reports whether this process must execute locally without sandbox containers.
func Worktree() bool {
	m := Mode()
	return m == ModeWorktree || m == ModeLocal
}

// Local reports whether this process runs in host-local mode with native CLI/Git authorization.
func Local() bool { return Mode() == ModeLocal }
