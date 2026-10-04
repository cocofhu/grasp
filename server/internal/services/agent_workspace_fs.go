package services

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// WorkspaceFileMaxBytes is the hard ceiling for a single workspace file
// read/write via the narrow FS API (1 MiB).
const WorkspaceFileMaxBytes = 1 << 20 // 1048576

// Narrow workspace FS sentinel errors (mapped by pmmcp to clear tool errors).
var (
	ErrWorkspacePathInvalid  = errors.New("workspace path invalid")
	ErrWorkspaceFileTooLarge = errors.New("workspace file exceeds 1MiB limit")
	ErrWorkspaceNotFound     = errors.New("workspace path not found")
	ErrWorkspaceAgentMissing = errors.New("agent not found")
	ErrWorkspaceIsDir        = errors.New("path is a directory")
	ErrWorkspaceNotDir       = errors.New("path is not a directory")
	ErrWorkspaceExists       = errors.New("path already exists")
)

// WorkspaceEntry is one list result under an agent workspace.
type WorkspaceEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size,omitempty"`
}

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

// resolveWorkspacePath maps a workspace-relative path to an absolute path
// that stays under the agent workspace after symlink checks.
// emptyRelOK allows "" to mean the workspace root (for list).
func (s *AgentService) resolveWorkspacePath(agent, rel string, emptyRelOK bool) (absRoot, absPath string, err error) {
	absRoot, err = s.ensureWorkspaceRoot(agent)
	if err != nil {
		return "", "", err
	}
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "." {
		if !emptyRelOK {
			return "", "", fmt.Errorf("%w: empty path", ErrWorkspacePathInvalid)
		}
		resolved, rerr := containResolved(absRoot, absRoot)
		if rerr != nil {
			return "", "", rerr
		}
		return absRoot, resolved, nil
	}
	// Reject ".." segments before path.Clean collapses "../x" → "x"
	// (safeRel alone would accept that as an in-workspace file named x).
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if seg == ".." {
			return "", "", fmt.Errorf("%w: %q", ErrWorkspacePathInvalid, rel)
		}
	}
	safe := safeRel(rel)
	if safe == "" {
		return "", "", fmt.Errorf("%w: %q", ErrWorkspacePathInvalid, rel)
	}
	joined, err := underRoot(absRoot, safe)
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrWorkspacePathInvalid, err)
	}
	resolved, err := containResolved(absRoot, joined)
	if err != nil {
		return "", "", err
	}
	return absRoot, resolved, nil
}

// containResolved evaluates existing path prefixes and asserts the final path
// remains inside absRoot. Symlink nodes are rejected (EscapeRoute hardening).
func containResolved(absRoot, candidate string) (string, error) {
	absRoot, err := filepath.Abs(absRoot)
	if err != nil {
		return "", err
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	sep := string(os.PathSeparator)
	if candidate != absRoot && !strings.HasPrefix(candidate, absRoot+sep) {
		return "", fmt.Errorf("%w: escapes workspace", ErrWorkspacePathInvalid)
	}

	suffix := []string{}
	cur := candidate
	for {
		fi, lerr := os.Lstat(cur)
		if lerr == nil {
			if fi.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("%w: symlink not allowed", ErrWorkspacePathInvalid)
			}
			resolved, rerr := filepath.EvalSymlinks(cur)
			if rerr != nil {
				return "", fmt.Errorf("%w: %v", ErrWorkspacePathInvalid, rerr)
			}
			resolved, rerr = filepath.Abs(resolved)
			if rerr != nil {
				return "", rerr
			}
			if resolved != absRoot && !strings.HasPrefix(resolved, absRoot+sep) {
				return "", fmt.Errorf("%w: symlink escapes workspace", ErrWorkspacePathInvalid)
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			if resolved != absRoot && !strings.HasPrefix(resolved, absRoot+sep) {
				return "", fmt.Errorf("%w: escapes workspace", ErrWorkspacePathInvalid)
			}
			return resolved, nil
		}
		if !os.IsNotExist(lerr) {
			return "", lerr
		}
		if cur == absRoot {
			return absRoot, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("%w: cannot resolve", ErrWorkspacePathInvalid)
		}
		suffix = append(suffix, filepath.Base(cur))
		cur = parent
	}
}

// ListWorkspace lists entries under a workspace-relative directory ("" = root).
func (s *AgentService) ListWorkspace(agent, relDir string) ([]WorkspaceEntry, error) {
	_, abs, err := s.resolveWorkspacePath(agent, relDir, true)
	if err != nil {
		return nil, err
	}
	fi, err := os.Lstat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrWorkspaceNotFound
		}
		return nil, err
	}
	if !fi.IsDir() {
		return nil, ErrWorkspaceNotDir
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	baseRel := safeRel(relDir)
	out := make([]WorkspaceEntry, 0, len(entries))
	for _, e := range entries {
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue // never surface symlinks
		}
		if !e.IsDir() && isWorkspacePlaceholder(e.Name()) {
			continue
		}
		childRel := e.Name()
		if baseRel != "" {
			childRel = baseRel + "/" + e.Name()
		}
		ent := WorkspaceEntry{
			Name:  e.Name(),
			Path:  filepath.ToSlash(childRel),
			IsDir: e.IsDir(),
		}
		if !e.IsDir() {
			ent.Size = info.Size()
		}
		out = append(out, ent)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// ReadWorkspaceFile reads a file under workspace; rejects dirs and >1MiB.
func (s *AgentService) ReadWorkspaceFile(agent, rel string) (string, error) {
	_, abs, err := s.resolveWorkspacePath(agent, rel, false)
	if err != nil {
		return "", err
	}
	fi, err := os.Lstat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrWorkspaceNotFound
		}
		return "", err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%w: symlink not allowed", ErrWorkspacePathInvalid)
	}
	if fi.IsDir() {
		return "", ErrWorkspaceIsDir
	}
	if fi.Size() > WorkspaceFileMaxBytes {
		return "", ErrWorkspaceFileTooLarge
	}
	f, err := os.Open(abs)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	limited := io.LimitReader(f, WorkspaceFileMaxBytes+1)
	b, err := io.ReadAll(limited)
	if err != nil {
		return "", err
	}
	if len(b) > WorkspaceFileMaxBytes {
		return "", ErrWorkspaceFileTooLarge
	}
	return string(b), nil
}

// WriteWorkspaceFile creates/overwrites a file; MkdirAll parents; rejects >1MiB.
func (s *AgentService) WriteWorkspaceFile(agent, rel, content string) error {
	if len(content) > WorkspaceFileMaxBytes {
		return ErrWorkspaceFileTooLarge
	}
	_, abs, err := s.resolveWorkspacePath(agent, rel, false)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	if fi, lerr := os.Lstat(abs); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: symlink not allowed", ErrWorkspacePathInvalid)
	}
	tmp := abs + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, abs); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// DeleteWorkspacePath removes a file or recursively deletes a directory.
func (s *AgentService) DeleteWorkspacePath(agent, rel string) error {
	absRoot, abs, err := s.resolveWorkspacePath(agent, rel, false)
	if err != nil {
		return err
	}
	if abs == absRoot {
		return fmt.Errorf("%w: cannot delete workspace root", ErrWorkspacePathInvalid)
	}
	if _, err := os.Lstat(abs); err != nil {
		if os.IsNotExist(err) {
			return ErrWorkspaceNotFound
		}
		return err
	}
	return os.RemoveAll(abs)
}

// MkdirWorkspace creates a directory (and parents) under workspace.
func (s *AgentService) MkdirWorkspace(agent, rel string) error {
	_, abs, err := s.resolveWorkspacePath(agent, rel, false)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return err
	}
	placeholder := filepath.Join(abs, PlaceholderFileName)
	if _, err := os.Stat(placeholder); os.IsNotExist(err) {
		return os.WriteFile(placeholder, []byte(""), 0o644)
	}
	return nil
}

// RenameWorkspace moves/renames within the same agent workspace.
func (s *AgentService) RenameWorkspace(agent, fromRel, toRel string) error {
	_, fromAbs, err := s.resolveWorkspacePath(agent, fromRel, false)
	if err != nil {
		return err
	}
	_, toAbs, err := s.resolveWorkspacePath(agent, toRel, false)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(fromAbs); err != nil {
		if os.IsNotExist(err) {
			return ErrWorkspaceNotFound
		}
		return err
	}
	if _, err := os.Lstat(toAbs); err == nil {
		return ErrWorkspaceExists
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(toAbs), 0o755); err != nil {
		return err
	}
	return os.Rename(fromAbs, toAbs)
}

// WorkspaceRootAbs returns the absolute workspace path (creating if needed).
func (s *AgentService) WorkspaceRootAbs(agent string) (string, error) {
	return s.ensureWorkspaceRoot(agent)
}
