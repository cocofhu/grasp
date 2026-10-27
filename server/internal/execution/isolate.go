package execution

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Source is one repository or folder to isolate for a launch.
type Source struct {
	Name   string
	URL    string
	Branch string
}

// Prepared is one directory created for a launch.
type Prepared struct {
	Name   string
	Path   string
	Kind   string // worktree or folder
	GitDir string // main git dir when Kind is worktree; used to detach the worktree
}

// Launch is the directory shared by every node of one run, session, test, or assistant.
type Launch struct {
	ID    string
	Root  string
	Repos []Prepared
}

type launchState struct {
	Launch
	holders int
	ended   bool
	kills   []func()
}

var (
	isolateMu sync.Mutex
	launches  = map[string]*launchState{}
	endedIDs  = map[string]bool{}
)

// ResetForTest clears launch bookkeeping. Production never calls it.
func ResetForTest() {
	isolateMu.Lock()
	defer isolateMu.Unlock()
	launches = map[string]*launchState{}
	endedIDs = map[string]bool{}
}

// Root is the directory that holds per-launch trees. Tests set GRASP_WORKTREE_ROOT.
func Root() string {
	if v := strings.TrimSpace(os.Getenv("GRASP_WORKTREE_ROOT")); v != "" {
		return v
	}
	return "/var/lib/grasp/launches"
}

// Prepare creates the launch directory on the first call and reuses it after that.
// Git sources become a detached worktree at the selected branch HEAD. Anything
// else becomes an independent folder. Parallel launch IDs never share a directory.
func Prepare(id string, sources []Source) (Launch, error) {
	id, err := safeID(id)
	if err != nil {
		return Launch{}, err
	}
	isolateMu.Lock()
	defer isolateMu.Unlock()
	if endedIDs[id] {
		return Launch{}, fmt.Errorf("工作树 %s 已结束", id)
	}
	if cur := launches[id]; cur != nil {
		cur.holders++
		return cur.Launch, nil
	}
	root := filepath.Join(Root(), id)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Launch{}, err
	}
	prepared, err := prepareSources(root, sources)
	if err != nil {
		_ = os.RemoveAll(root)
		return Launch{}, err
	}
	st := &launchState{Launch: Launch{ID: id, Root: root, Repos: prepared}, holders: 1}
	launches[id] = st
	return st.Launch, nil
}

// AddStop registers a process stop invoked when the launch ends.
func AddStop(id string, stop func()) {
	if stop == nil {
		return
	}
	id, err := safeID(id)
	if err != nil {
		return
	}
	isolateMu.Lock()
	defer isolateMu.Unlock()
	if cur := launches[id]; cur != nil {
		cur.kills = append(cur.kills, stop)
	}
}

// Release drops one holder. The directory stays until End when the launch is a
// workflow run that outlives a single node. endNow deletes as soon as no holder
// remains (solo, test, assistant, or a failed start).
func Release(id string, endNow bool) {
	id, err := safeID(id)
	if err != nil {
		return
	}
	isolateMu.Lock()
	defer isolateMu.Unlock()
	cur := launches[id]
	if cur == nil {
		return
	}
	cur.holders--
	if endNow {
		cur.ended = true
	}
	if cur.holders <= 0 && (cur.ended || endNow) {
		removeLocked(cur)
	}
}

// ReleaseFailed drops a holder that never became a running agent. The directory
// is removed when nothing else is using it, and is kept when a sibling node is.
func ReleaseFailed(id string) {
	id, err := safeID(id)
	if err != nil {
		return
	}
	isolateMu.Lock()
	defer isolateMu.Unlock()
	cur := launches[id]
	if cur == nil {
		return
	}
	cur.holders--
	if cur.holders <= 0 {
		removeLocked(cur)
	}
}

// End deletes the launch directory even if a node handle is still winding down.
// Missing IDs are ignored. A later Prepare of the same ID fails.
func End(id string) {
	id, err := safeID(id)
	if err != nil {
		return
	}
	isolateMu.Lock()
	cur := launches[id]
	var kills []func()
	if cur != nil {
		cur.ended = true
		kills = cur.kills
		cur.kills = nil
	}
	endedIDs[id] = true
	isolateMu.Unlock()
	for _, kill := range kills {
		kill()
	}
	isolateMu.Lock()
	defer isolateMu.Unlock()
	if cur = launches[id]; cur != nil {
		removeLocked(cur)
	}
}

func removeLocked(cur *launchState) {
	for _, repo := range cur.Repos {
		if repo.Kind == "worktree" && repo.GitDir != "" {
			_, _ = git(repo.GitDir, "worktree", "remove", "--force", repo.Path)
			_, _ = git(repo.GitDir, "worktree", "prune")
		}
	}
	_ = os.RemoveAll(cur.Root)
	delete(launches, cur.ID)
	endedIDs[cur.ID] = true
}

func prepareSources(root string, sources []Source) ([]Prepared, error) {
	if len(sources) == 0 {
		return nil, nil
	}
	out := make([]Prepared, 0, len(sources))
	seen := map[string]bool{}
	for _, src := range sources {
		name := strings.TrimSpace(src.Name)
		url := strings.TrimSpace(src.URL)
		if name == "" || url == "" || seen[name] {
			continue
		}
		if name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
			return nil, fmt.Errorf("不安全的目录名 %q", name)
		}
		seen[name] = true
		dest := filepath.Join(root, name)
		prep, err := prepareOne(url, strings.TrimSpace(src.Branch), dest, name)
		if err != nil {
			return nil, err
		}
		out = append(out, prep)
	}
	return out, nil
}

func prepareOne(url, branch, dest, name string) (Prepared, error) {
	if local, ok := localDir(url); ok {
		if isGitRepo(local) {
			return addWorktree(local, branch, dest, name)
		}
		if err := copyTree(local, dest); err != nil {
			return Prepared{}, err
		}
		return Prepared{Name: name, Path: dest, Kind: "folder"}, nil
	}
	cache := filepath.Join(Root(), "_mirrors", mirrorKey(url))
	if err := ensureMirror(cache, url, branch); err != nil {
		return Prepared{}, err
	}
	return addWorktree(cache, branch, dest, name)
}

func addWorktree(repo, branch, dest, name string) (Prepared, error) {
	sha, err := branchHEAD(repo, branch)
	if err != nil {
		return Prepared{}, err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return Prepared{}, err
	}
	if _, err := git(repo, "worktree", "add", "--detach", dest, sha); err != nil {
		return Prepared{}, err
	}
	got, err := git(dest, "rev-parse", "HEAD")
	if err != nil {
		return Prepared{}, err
	}
	if got != sha {
		return Prepared{}, fmt.Errorf("工作树 %s 停在 %s，所选分支 HEAD 是 %s", name, got, sha)
	}
	return Prepared{Name: name, Path: dest, Kind: "worktree", GitDir: repo}, nil
}

func branchHEAD(repo, branch string) (string, error) {
	if branch == "" {
		return git(repo, "rev-parse", "--verify", "HEAD")
	}
	if strings.ContainsAny(branch, " \t\n\r") || strings.HasPrefix(branch, "-") || strings.Contains(branch, "..") {
		return "", fmt.Errorf("非法分支名 %q", branch)
	}
	if sha, err := git(repo, "rev-parse", "--verify", "--end-of-options", "refs/heads/"+branch); err == nil {
		return sha, nil
	}
	return git(repo, "rev-parse", "--verify", "--end-of-options", branch)
}

func ensureMirror(cache, url, branch string) error {
	if _, err := os.Stat(cache); err != nil {
		if err := os.MkdirAll(filepath.Dir(cache), 0o755); err != nil {
			return err
		}
		if _, err := git("", "clone", "--bare", url, cache); err != nil {
			_ = os.RemoveAll(cache)
			return err
		}
	}
	if branch != "" && !strings.ContainsAny(branch, " \t\n\r") && !strings.HasPrefix(branch, "-") {
		_, _ = git(cache, "fetch", "origin", branch)
	}
	return nil
}

func localDir(url string) (string, bool) {
	u := strings.TrimSpace(url)
	switch {
	case strings.HasPrefix(u, "file://"):
		u = strings.TrimPrefix(u, "file://")
	case strings.HasPrefix(u, "http://"), strings.HasPrefix(u, "https://"), strings.HasPrefix(u, "ssh://"), strings.HasPrefix(u, "git@"):
		return "", false
	}
	if !filepath.IsAbs(u) {
		return "", false
	}
	info, err := os.Stat(u)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return u, true
}

func isGitRepo(path string) bool {
	out, err := git(path, "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func mirrorKey(url string) string {
	sum := sha256.Sum256([]byte(url))
	return hex.EncodeToString(sum[:8])
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func safeID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" || strings.Contains(id, "..") {
		return "", fmt.Errorf("非法的隔离目录名")
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return "", fmt.Errorf("非法的隔离目录名")
		}
	}
	return id, nil
}
