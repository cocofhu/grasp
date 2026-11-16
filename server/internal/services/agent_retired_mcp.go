package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// StripRetiredSchedulerMCP removes the retired task-scheduler server from
// every stored agent.json; its endpoint and template vars no longer exist.
// Files without such an entry are not rewritten, so a second call is a no-op.
func (s *AgentService) StripRetiredSchedulerMCP() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(s.root, e.Name(), "agent.json")
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		cfg := agentConfig{InheritModel: true}
		if json.Unmarshal(b, &cfg) != nil {
			continue
		}
		kept := make([]MCPServer, 0, len(cfg.MCP))
		for _, m := range cfg.MCP {
			if !isRetiredSchedulerMCP(m) {
				kept = append(kept, m)
			}
		}
		if len(kept) == len(cfg.MCP) {
			continue
		}
		cfg.MCP = kept
		out, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, out, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func isRetiredSchedulerMCP(m MCPServer) bool {
	return strings.EqualFold(strings.TrimSpace(m.Name), "task-scheduler") ||
		strings.Contains(m.URL, "/mcp/task-scheduler/") ||
		strings.Contains(m.URL, "${GRASP_SCHEDULER_URL}")
}
