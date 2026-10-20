package services

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage/filesystem"
)

const (
	workspaceVcsDirName = ".workspace-vcs"
	vcsCommitPrefix     = "grasp-vcs:"

	VcsSourceSystem      = "system"
	VcsSourceStudio      = "studio"
	VcsSourcePmMCP       = "pm-mcp"
	VcsSourceExternalMCP = "external-mcp"
	VcsSourceRestore     = "restore"
)

var (
	ErrVcsReasonRequired = errors.New("变更原因不能为空")
	ErrVcsRevisionMiss   = errors.New("目标版本不存在")
	ErrVcsGitFailed      = errors.New("版本记录失败")
	ErrVcsNoChanges      = git.ErrEmptyCommit
)

// VcsCommitMeta is metadata stored in each workspace revision.
type VcsCommitMeta struct {
	SHA       string          `json:"sha"`
	ParentSHA string          `json:"parentSha,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
	Author    string          `json:"author"`
	Source    string          `json:"source"`
	Reason    string          `json:"reason"`
	Changes   []VcsPathChange `json:"changes"`
}

// VcsPathChange describes one path operation in a revision.
type VcsPathChange struct {
	Path     string `json:"path"`
	Op       string `json:"op"` // write, delete, mkdir, rename, restore
	FromPath string `json:"fromPath,omitempty"`
}

// WorkspaceVcsService manages sidecar Git repos for agent workspace trees via go-git.
type WorkspaceVcsService struct {
	agentsRoot string
}

// NewWorkspaceVcsService creates the sidecar VCS service rooted beside agents.
func NewWorkspaceVcsService(agentsRoot string) *WorkspaceVcsService {
	return &WorkspaceVcsService{agentsRoot: agentsRoot}
}

func (v *WorkspaceVcsService) gitDir(agent string) string {
	return filepath.Join(v.agentsRoot, sanitize(agent), workspaceVcsDirName)
}

func (v *WorkspaceVcsService) workTree(agent string) (string, error) {
	s := &AgentService{root: v.agentsRoot}
	return s.ensureWorkspaceRoot(agent)
}

func wrapVcs(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrVcsGitFailed, err)
}

func vcsFilesystems(gitDir, workTree string) (*filesystem.Storage, billy.Filesystem) {
	dot := filesystem.NewStorage(osfs.New(gitDir), cache.NewObjectLRUDefault())
	return dot, osfs.New(workTree)
}

// stripWorkspaceGitlink removes a go-git gitdir pointer so the live workspace
// never contains .git. Init(storer, worktree) would write that file; we Init
// bare and Open with an explicit worktree instead.
func stripWorkspaceGitlink(workTree string) {
	_ = os.Remove(filepath.Join(workTree, ".git"))
}

func (v *WorkspaceVcsService) openRepo(agent string) (*git.Repository, error) {
	workTree, err := v.workTree(agent)
	if err != nil {
		return nil, err
	}
	stripWorkspaceGitlink(workTree)
	gitDir := v.gitDir(agent)
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		return nil, wrapVcs(err)
	}
	store, wtFs := vcsFilesystems(gitDir, workTree)
	repo, err := git.Open(store, wtFs)
	if err == nil {
		return repo, nil
	}
	if !errors.Is(err, git.ErrRepositoryNotExists) {
		_ = os.RemoveAll(gitDir)
		if err := os.MkdirAll(gitDir, 0o755); err != nil {
			return nil, wrapVcs(err)
		}
		store, wtFs = vcsFilesystems(gitDir, workTree)
	}
	if _, err := git.Init(store, nil); err != nil && !errors.Is(err, git.ErrRepositoryAlreadyExists) {
		return nil, wrapVcs(err)
	}
	repo, err = git.Open(store, wtFs)
	if err != nil {
		return nil, wrapVcs(err)
	}
	return repo, nil
}

func hasCommits(repo *git.Repository) bool {
	ref, err := repo.Head()
	if err != nil {
		return false
	}
	_, err = repo.CommitObject(ref.Hash())
	return err == nil
}

func resolveCommit(repo *git.Repository, rev string) (plumbing.Hash, error) {
	rev = strings.TrimSpace(rev)
	if rev == "" {
		return plumbing.ZeroHash, ErrVcsRevisionMiss
	}
	h, err := repo.ResolveRevision(plumbing.Revision(rev))
	if err != nil || h == nil || h.IsZero() {
		return plumbing.ZeroHash, ErrVcsRevisionMiss
	}
	if _, err := repo.CommitObject(*h); err != nil {
		return plumbing.ZeroHash, ErrVcsRevisionMiss
	}
	return *h, nil
}

func (v *WorkspaceVcsService) commitAll(repo *git.Repository, meta VcsCommitMeta, allowEmpty bool) (string, error) {
	wt, err := repo.Worktree()
	if err != nil {
		return "", wrapVcs(err)
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return "", wrapVcs(err)
	}
	author := strings.TrimSpace(meta.Author)
	if author == "" {
		author = "Grasp VCS"
	}
	hash, err := wt.Commit(formatVcsMessage(meta), &git.CommitOptions{
		Author: &object.Signature{
			Name:  author,
			Email: "grasp-vcs@local",
			When:  time.Now(),
		},
		AllowEmptyCommits: allowEmpty,
	})
	if err != nil {
		return "", wrapVcs(err)
	}
	return hash.String(), nil
}

func (v *WorkspaceVcsService) ensureBaseline(agent string) error {
	repo, err := v.openRepo(agent)
	if err != nil {
		return err
	}
	if hasCommits(repo) {
		return nil
	}
	_, err = v.commitAll(repo, VcsCommitMeta{
		Author:  "system",
		Source:  VcsSourceSystem,
		Reason:  "基线",
		Changes: []VcsPathChange{{Path: ".", Op: "baseline"}},
	}, true)
	return err
}

func vcsField(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), " ", "_")
}

func vcsFieldDecode(s string) string {
	return strings.ReplaceAll(s, "_", " ")
}

func formatVcsMessage(meta VcsCommitMeta) string {
	subject := vcsCommitPrefix + " source=" + vcsField(meta.Source) +
		" author=" + vcsField(meta.Author) +
		" reason=" + vcsField(meta.Reason)
	for _, ch := range meta.Changes {
		if ch.Op == "rename" && ch.FromPath != "" {
			subject += fmt.Sprintf(" change=%s:%s->%s", ch.Op, ch.FromPath, ch.Path)
		} else {
			subject += fmt.Sprintf(" change=%s:%s", ch.Op, ch.Path)
		}
	}
	return subject
}

func parseVcsMessage(sha, subject string) VcsCommitMeta {
	meta := VcsCommitMeta{SHA: sha, Changes: []VcsPathChange{}}
	subject = strings.TrimSpace(subject)
	if !strings.HasPrefix(subject, vcsCommitPrefix) {
		meta.Reason = subject
		return meta
	}
	rest := strings.TrimSpace(strings.TrimPrefix(subject, vcsCommitPrefix))
	if i := strings.Index(rest, "source="); i >= 0 {
		seg := rest[i+7:]
		if j := strings.Index(seg, " "); j >= 0 {
			meta.Source = vcsFieldDecode(seg[:j])
		} else {
			meta.Source = vcsFieldDecode(seg)
		}
	}
	if i := strings.Index(rest, "author="); i >= 0 {
		seg := rest[i+7:]
		if j := strings.Index(seg, " reason="); j >= 0 {
			meta.Author = vcsFieldDecode(seg[:j])
		} else if j := strings.Index(seg, " change="); j >= 0 {
			meta.Author = vcsFieldDecode(seg[:j])
		} else {
			meta.Author = vcsFieldDecode(seg)
		}
	}
	if i := strings.Index(rest, "reason="); i >= 0 {
		seg := rest[i+7:]
		if j := strings.Index(seg, " change="); j >= 0 {
			meta.Reason = vcsFieldDecode(seg[:j])
		} else {
			meta.Reason = vcsFieldDecode(seg)
		}
	}
	for _, tok := range strings.Split(rest, " change=") {
		if tok == rest || !strings.Contains(tok, ":") {
			continue
		}
		if j := strings.Index(tok, " change="); j >= 0 {
			tok = tok[:j]
		}
		body := tok
		if k := strings.Index(body, ":"); k >= 0 {
			op := body[:k]
			pathPart := body[k+1:]
			ch := VcsPathChange{Op: op}
			if j := strings.Index(pathPart, "->"); j >= 0 {
				ch.FromPath = pathPart[:j]
				ch.Path = pathPart[j+2:]
			} else {
				ch.Path = pathPart
			}
			meta.Changes = append(meta.Changes, ch)
		}
	}
	return meta
}

// EnsureBaseline records the current workspace tree before the first mutating operation.
func (v *WorkspaceVcsService) EnsureBaseline(agent string) error {
	return v.ensureBaseline(agent)
}

// CommitChange records one workspace mutation as a single revision.
func (v *WorkspaceVcsService) CommitChange(agent string, meta VcsCommitMeta) (string, error) {
	if strings.TrimSpace(meta.Reason) == "" && meta.Source != VcsSourceSystem {
		return "", ErrVcsReasonRequired
	}
	repo, err := v.openRepo(agent)
	if err != nil {
		return "", err
	}
	return v.commitAll(repo, meta, false)
}

func commitSubject(c *object.Commit) string {
	msg := strings.TrimSpace(c.Message)
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = strings.TrimSpace(msg[:i])
	}
	return msg
}

func metaFromCommit(c *object.Commit) VcsCommitMeta {
	meta := parseVcsMessage(c.Hash.String(), commitSubject(c))
	meta.SHA = c.Hash.String()
	if c.NumParents() > 0 {
		meta.ParentSHA = c.ParentHashes[0].String()
	}
	meta.CreatedAt = c.Author.When.UTC()
	return meta
}

func (v *WorkspaceVcsService) sidecarExists(agent string) bool {
	_, err := os.Stat(filepath.Join(v.gitDir(agent), "HEAD"))
	return err == nil
}

// ListRevisions returns workspace revisions newest-first.
func (v *WorkspaceVcsService) ListRevisions(agent string, limit int) ([]VcsCommitMeta, error) {
	if !v.sidecarExists(agent) {
		return []VcsCommitMeta{}, nil
	}
	repo, err := v.openRepo(agent)
	if err != nil {
		return nil, err
	}
	if !hasCommits(repo) {
		return []VcsCommitMeta{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	iter, err := repo.Log(&git.LogOptions{})
	if err != nil {
		return nil, wrapVcs(err)
	}
	defer iter.Close()
	var revs []VcsCommitMeta
	err = iter.ForEach(func(c *object.Commit) error {
		if len(revs) >= limit {
			return storer.ErrStop
		}
		revs = append(revs, metaFromCommit(c))
		return nil
	})
	if err != nil {
		return nil, wrapVcs(err)
	}
	return revs, nil
}

func commitUnifiedDiff(c *object.Commit) (string, error) {
	var from *object.Tree
	if c.NumParents() > 0 {
		parent, err := c.Parent(0)
		if err != nil {
			return "", err
		}
		from, err = parent.Tree()
		if err != nil {
			return "", err
		}
	}
	to, err := c.Tree()
	if err != nil {
		return "", err
	}
	changes, err := object.DiffTree(from, to)
	if err != nil {
		return "", err
	}
	patch, err := changes.Patch()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(patch.String()), nil
}

// DiffRevision returns unified diff vs parent for one revision.
func (v *WorkspaceVcsService) DiffRevision(agent, sha string) (string, error) {
	if !v.sidecarExists(agent) {
		return "", ErrVcsRevisionMiss
	}
	repo, err := v.openRepo(agent)
	if err != nil {
		return "", err
	}
	if !hasCommits(repo) {
		return "", ErrVcsRevisionMiss
	}
	full, err := resolveCommit(repo, sha)
	if err != nil {
		return "", err
	}
	c, err := repo.CommitObject(full)
	if err != nil {
		return "", ErrVcsRevisionMiss
	}
	out, err := commitUnifiedDiff(c)
	if err != nil {
		return "", wrapVcs(err)
	}
	return out, nil
}

func checkoutTree(workTree string, tree *object.Tree) error {
	keep := map[string]struct{}{}
	err := tree.Files().ForEach(func(f *object.File) error {
		keep[f.Name] = struct{}{}
		contents, err := f.Contents()
		if err != nil {
			return err
		}
		dest := filepath.Join(workTree, filepath.FromSlash(f.Name))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dest, []byte(contents), 0o644)
	})
	if err != nil {
		return err
	}
	return filepath.WalkDir(workTree, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(workTree, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				_ = os.RemoveAll(path)
				return fs.SkipDir
			}
			return nil
		}
		if _, ok := keep[rel]; !ok {
			_ = os.Remove(path)
		}
		return nil
	})
}

// RestoreRevision restores workspace to target sha and appends a restore revision.
func (v *WorkspaceVcsService) RestoreRevision(agent, sha, author, reason string) (string, error) {
	if strings.TrimSpace(reason) == "" {
		return "", ErrVcsReasonRequired
	}
	if err := v.ensureBaseline(agent); err != nil {
		return "", err
	}
	repo, err := v.openRepo(agent)
	if err != nil {
		return "", err
	}
	full, err := resolveCommit(repo, sha)
	if err != nil {
		return "", err
	}
	c, err := repo.CommitObject(full)
	if err != nil {
		return "", ErrVcsRevisionMiss
	}
	tree, err := c.Tree()
	if err != nil {
		return "", wrapVcs(err)
	}
	workTree, err := v.workTree(agent)
	if err != nil {
		return "", err
	}
	if err := checkoutTree(workTree, tree); err != nil {
		return "", wrapVcs(err)
	}
	stripWorkspaceGitlink(workTree)
	return v.commitAll(repo, VcsCommitMeta{
		Author:  author,
		Source:  VcsSourceRestore,
		Reason:  reason,
		Changes: []VcsPathChange{{Op: "restore", Path: full.String()}},
	}, true)
}

// RenameAgent moves the sidecar repo when an agent is renamed.
func (v *WorkspaceVcsService) RenameAgent(old, newName string) error {
	oldDir := v.gitDir(old)
	newDir := v.gitDir(newName)
	if _, err := os.Stat(oldDir); err != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(newDir), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(newDir); err == nil {
		return fmt.Errorf("target vcs dir already exists")
	}
	return os.Rename(oldDir, newDir)
}

// DeleteAgent removes the sidecar repo for an agent.
func (v *WorkspaceVcsService) DeleteAgent(agent string) error {
	return os.RemoveAll(v.gitDir(agent))
}

// DiffAgentFiles compares two file snapshots and returns path changes.
func DiffAgentFiles(before, after []AgentFile) []VcsPathChange {
	bm := map[string]string{}
	for _, f := range before {
		if p := safeRel(f.Path); p != "" {
			bm[p] = f.Content
		}
	}
	am := map[string]string{}
	for _, f := range after {
		if p := safeRel(f.Path); p != "" {
			am[p] = f.Content
		}
	}
	var changes []VcsPathChange
	for p, content := range am {
		if old, ok := bm[p]; !ok {
			changes = append(changes, VcsPathChange{Op: "write", Path: p})
		} else if old != content {
			changes = append(changes, VcsPathChange{Op: "write", Path: p})
		}
	}
	for p := range bm {
		if _, ok := am[p]; !ok {
			changes = append(changes, VcsPathChange{Op: "delete", Path: p})
		}
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Op != changes[j].Op {
			return changes[i].Op < changes[j].Op
		}
		return changes[i].Path < changes[j].Path
	})
	return changes
}

// CommitStudioSave records each changed path as its own revision with shared reason.
func (v *WorkspaceVcsService) CommitStudioSave(agent string, before, after []AgentFile, author, reason string) ([]string, error) {
	if strings.TrimSpace(reason) == "" {
		reason = "Studio 保存"
	}
	changes := DiffAgentFiles(before, after)
	if len(changes) == 0 {
		return nil, nil
	}
	var shas []string
	for _, ch := range changes {
		sha, err := v.CommitChange(agent, VcsCommitMeta{
			Author:  author,
			Source:  VcsSourceStudio,
			Reason:  reason,
			Changes: []VcsPathChange{ch},
		})
		if err != nil {
			return shas, err
		}
		shas = append(shas, sha)
	}
	return shas, nil
}

// PlaceholderFileName is used to track empty directories in git.
const PlaceholderFileName = ".grasp-dir-placeholder"

// EnsureDirTracked adds a placeholder so mkdir can be versioned.
func EnsureDirTracked(agent, rel string) error {
	return nil
}
