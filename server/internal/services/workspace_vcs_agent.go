package services

import (
	"fmt"
	"strings"
)

// WorkspaceWriteOpts carries VCS metadata for a workspace mutation.
type WorkspaceWriteOpts struct {
	Author string
	Source string
	Reason string
}

func requireWorkspaceReason(reason string) error {
	if strings.TrimSpace(reason) == "" {
		return ErrVcsReasonRequired
	}
	return nil
}

func vcsPersistError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("文件已写入但版本记录失败: %w", err)
}

// WriteWorkspaceFileVcs writes a file and records one revision.
func (s *AgentService) WriteWorkspaceFileVcs(agent, rel, content string, opts WorkspaceWriteOpts) (string, error) {
	if err := requireWorkspaceReason(opts.Reason); err != nil {
		return "", err
	}
	if s.Vcs != nil {
		if err := s.Vcs.EnsureBaseline(agent); err != nil {
			return "", err
		}
	}
	if err := s.WriteWorkspaceFile(agent, rel, content); err != nil {
		return "", err
	}
	path := safeRel(rel)
	if path == "" {
		return "", fmt.Errorf("%w: %q", ErrWorkspacePathInvalid, rel)
	}
	sha, err := s.Vcs.CommitChange(agent, VcsCommitMeta{
		Author:  opts.Author,
		Source:  opts.Source,
		Reason:  strings.TrimSpace(opts.Reason),
		Changes: []VcsPathChange{{Op: "write", Path: path}},
	})
	if err != nil {
		return "", vcsPersistError(err)
	}
	return sha, nil
}

// DeleteWorkspacePathVcs deletes a path and records one revision.
func (s *AgentService) DeleteWorkspacePathVcs(agent, rel string, opts WorkspaceWriteOpts) (string, error) {
	if err := requireWorkspaceReason(opts.Reason); err != nil {
		return "", err
	}
	if s.Vcs != nil {
		if err := s.Vcs.EnsureBaseline(agent); err != nil {
			return "", err
		}
	}
	path := safeRel(rel)
	if path == "" {
		return "", fmt.Errorf("%w: %q", ErrWorkspacePathInvalid, rel)
	}
	if err := s.DeleteWorkspacePath(agent, rel); err != nil {
		return "", err
	}
	sha, err := s.Vcs.CommitChange(agent, VcsCommitMeta{
		Author:  opts.Author,
		Source:  opts.Source,
		Reason:  strings.TrimSpace(opts.Reason),
		Changes: []VcsPathChange{{Op: "delete", Path: path}},
	})
	return sha, vcsPersistError(err)
}

// MkdirWorkspaceVcs creates a directory (with placeholder) and records one revision.
func (s *AgentService) MkdirWorkspaceVcs(agent, rel string, opts WorkspaceWriteOpts) (string, error) {
	if err := requireWorkspaceReason(opts.Reason); err != nil {
		return "", err
	}
	if s.Vcs != nil {
		if err := s.Vcs.EnsureBaseline(agent); err != nil {
			return "", err
		}
	}
	path := safeRel(rel)
	if path == "" {
		return "", fmt.Errorf("%w: %q", ErrWorkspacePathInvalid, rel)
	}
	if err := s.MkdirWorkspace(agent, rel); err != nil {
		return "", err
	}
	sha, err := s.Vcs.CommitChange(agent, VcsCommitMeta{
		Author:  opts.Author,
		Source:  opts.Source,
		Reason:  strings.TrimSpace(opts.Reason),
		Changes: []VcsPathChange{{Op: "mkdir", Path: path}},
	})
	return sha, vcsPersistError(err)
}

// RenameWorkspaceVcs renames within workspace and records one revision.
func (s *AgentService) RenameWorkspaceVcs(agent, fromRel, toRel string, opts WorkspaceWriteOpts) (string, error) {
	if err := requireWorkspaceReason(opts.Reason); err != nil {
		return "", err
	}
	if s.Vcs != nil {
		if err := s.Vcs.EnsureBaseline(agent); err != nil {
			return "", err
		}
	}
	from := safeRel(fromRel)
	to := safeRel(toRel)
	if from == "" || to == "" {
		return "", fmt.Errorf("%w", ErrWorkspacePathInvalid)
	}
	if err := s.RenameWorkspace(agent, fromRel, toRel); err != nil {
		return "", err
	}
	sha, err := s.Vcs.CommitChange(agent, VcsCommitMeta{
		Author:  opts.Author,
		Source:  opts.Source,
		Reason:  strings.TrimSpace(opts.Reason),
		Changes: []VcsPathChange{{Op: "rename", Path: to, FromPath: from}},
	})
	return sha, vcsPersistError(err)
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
