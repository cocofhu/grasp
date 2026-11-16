package services

import (
	"errors"
	"os"
	"path/filepath"
)

// WorkspaceFileMaxBytes is the hard ceiling for a single workspace file
// carried by an agents bundle import (1 MiB).
const WorkspaceFileMaxBytes = 1 << 20 // 1048576

// ErrWorkspaceAgentMissing reports a workspace lookup for an unknown agent.
var ErrWorkspaceAgentMissing = errors.New("agent not found")

// ensureWorkspaceRoot returns the on-disk workspace/ directory for agent,
// creating it when missing. Never touches agent.json.
func (s *AgentService) ensureWorkspaceRoot(agent string) (string, error) {
	name := sanitize(agent)
	if name == "" {
		return "", ErrWorkspaceAgentMissing
	}
	if !s.Exists(name) {
		return "", ErrWorkspaceAgentMissing
	}
	root := filepath.Join(s.root, name, WorkDirName)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	return root, nil
}
