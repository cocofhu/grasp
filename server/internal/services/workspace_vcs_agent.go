package services

import "strings"

func requireWorkspaceReason(reason string) error {
	if strings.TrimSpace(reason) == "" {
		return ErrVcsReasonRequired
	}
	return nil
}

// RestoreWorkspaceVcs restores workspace to a revision.
func (s *AgentService) RestoreWorkspaceVcs(agent, sha, author, reason string) (string, error) {
	if err := requireWorkspaceReason(reason); err != nil {
		return "", err
	}
	return s.Vcs.RestoreRevision(agent, sha, author, strings.TrimSpace(reason))
}

// SaveAgentWithVcs saves agent files and records studio revisions per changed path.
func (s *AgentService) SaveAgentWithVcs(a Agent, author, reason string, recordVcs bool) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Validate before EnsureBaseline, which commits a baseline revision: a
	// rejected save must not leave anything behind.
	if _, err := s.prepareSave(a); err != nil {
		return nil, err
	}
	var before []AgentFile
	if recordVcs && s.Vcs != nil {
		if err := s.Vcs.EnsureBaseline(a.Name); err != nil {
			return nil, err
		}
		before = s.readFiles(a.Name)
	}
	if err := s.saveUnlocked(a); err != nil {
		return nil, err
	}
	if !recordVcs || s.Vcs == nil {
		return nil, nil
	}
	return s.Vcs.CommitStudioSave(a.Name, before, a.Files, author, reason)
}

// isWorkspacePlaceholder reports internal git tracking files hidden from listings.
func isWorkspacePlaceholder(name string) bool {
	return name == PlaceholderFileName
}
