package execution

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// gitArgPattern is the same anchored allowlist as sandbox.shellArgPattern.
// gitWith rechecks every argv against it before exec.Command.
var gitArgPattern = regexp.MustCompile(`^[A-Za-z0-9_./:@%+=,-]+$`)

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

// ShellDir is the local terminal directory for a launch. A launch with one
// repository checkout starts inside that checkout; otherwise it starts at the
// launch root, which is also the agent workspace.
func ShellDir(launchID string) (string, error) {
	id, err := safeID(launchID)
	if err != nil {
		return "", err
	}
	root := filepath.Join(Root(), id)
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("工作树目录不存在")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			dirs = append(dirs, entry.Name())
		}
	}
	if len(dirs) == 1 {
		return filepath.Join(root, dirs[0]), nil
	}
	return root, nil
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
// Remote clones done here are anonymous; pass credentials with PrepareAuth.
func Prepare(id string, sources []Source) (Launch, error) {
	return PrepareAuth(id, sources, Auth{})
}

// PrepareAuth is Prepare with the launch's already-resolved Git credentials.
// Those credentials are used for clone and fetch and are not written to the
// process-wide git config. A miss does not create a sandbox container.
func PrepareAuth(id string, sources []Source, auth Auth) (Launch, error) {
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
	base, err := filepath.Abs(filepath.Clean(Root()))
	if err != nil {
		return Launch{}, err
	}
	root, err := pathWithin(base, filepath.Join(base, id))
	if err != nil {
		return Launch{}, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Launch{}, err
	}
	prepared, err := prepareSources(root, sources, auth)
	if err != nil {
		detachWorktrees(prepared)
		removeWithin(base, root)
		pruneWorktrees(prepared)
		if mirrors, mErr := mirrorDir(root); mErr == nil {
			removeWithin(filepath.Dir(mirrors), mirrors)
		}
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
// An ID that never created a directory is ignored, so sandbox runs do not
// accumulate here. A later Prepare of an ID that did run fails.
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
		endedIDs[id] = true
	}
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
	removeWithin(filepath.Dir(cur.Root), cur.Root)
	if mirrors, err := mirrorDir(cur.Root); err == nil {
		removeWithin(filepath.Dir(mirrors), mirrors)
	}
	delete(launches, cur.ID)
	endedIDs[cur.ID] = true
}

// mirrorDir is the bare-clone directory for one launch, checked against the
// mirrors parent. Callers must use the returned path for later removal.
func mirrorDir(launchRoot string) (string, error) {
	parent := filepath.Join(filepath.Dir(launchRoot), ".mirrors")
	return pathWithin(parent, filepath.Join(parent, filepath.Base(launchRoot)))
}

func prepareSources(root string, sources []Source, auth Auth) ([]Prepared, error) {
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
			return out, fmt.Errorf("不安全的目录名 %q", name)
		}
		seen[name] = true
		dest, err := pathWithin(root, filepath.Join(root, name))
		if err != nil {
			return out, err
		}
		prep, err := prepareOne(url, strings.TrimSpace(src.Branch), dest, name, root, auth)
		if err != nil {
			return out, err
		}
		out = append(out, prep)
	}
	return out, nil
}

// detachWorktrees unregisters worktrees while their directories still exist.
// pruneWorktrees runs after those directories are deleted, so a remove that
// lost the race still drops the "prunable" registration.
func detachWorktrees(repos []Prepared) {
	for _, repo := range repos {
		if repo.Kind == "worktree" && repo.GitDir != "" {
			_, _ = git(repo.GitDir, "worktree", "remove", "--force", repo.Path)
		}
	}
}

func pruneWorktrees(repos []Prepared) {
	seen := map[string]bool{}
	for _, repo := range repos {
		if repo.Kind != "worktree" || repo.GitDir == "" || seen[repo.GitDir] {
			continue
		}
		seen[repo.GitDir] = true
		_, _ = git(repo.GitDir, "worktree", "prune")
	}
}

func prepareOne(url, branch, dest, name, launchRoot string, auth Auth) (Prepared, error) {
	if err := validateBranch(branch); err != nil {
		return Prepared{}, err
	}
	dest, err := pathWithin(launchRoot, dest)
	if err != nil {
		return Prepared{}, err
	}
	if isRemoteURL(url) {
		remote, err := validateRemote(url)
		if err != nil {
			return Prepared{}, err
		}
		mirrors, err := mirrorDir(launchRoot)
		if err != nil {
			return Prepared{}, err
		}
		cache, err := pathWithin(mirrors, filepath.Join(mirrors, name))
		if err != nil {
			return Prepared{}, err
		}
		if err := ensureMirror(mirrors, cache, remote, branch, auth); err != nil {
			return Prepared{}, err
		}
		return addWorktree(cache, branch, dest, name, launchRoot)
	}
	local, err := localDir(url)
	if err != nil {
		return Prepared{}, err
	}
	if isGitRepo(local) {
		return addWorktree(local, branch, dest, name, launchRoot)
	}
	if err := copyTree(local, dest); err != nil {
		return Prepared{}, err
	}
	return Prepared{Name: name, Path: dest, Kind: "folder"}, nil
}

func addWorktree(repo, branch, dest, name, launchRoot string) (Prepared, error) {
	dest, err := pathWithin(launchRoot, dest)
	if err != nil {
		return Prepared{}, err
	}
	sha, err := branchHEAD(repo, branch)
	if err != nil {
		return Prepared{}, err
	}
	parent, err := pathWithin(launchRoot, filepath.Dir(dest))
	if err != nil {
		return Prepared{}, err
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return Prepared{}, err
	}
	if _, err := git(repo, "worktree", "add", "--detach", dest, sha); err != nil {
		return Prepared{}, err
	}
	got, err := git(dest, "rev-parse", "HEAD")
	if err != nil || got != sha {
		_, _ = git(repo, "worktree", "remove", "--force", dest)
		_, _ = git(repo, "worktree", "prune")
		if err != nil {
			return Prepared{}, err
		}
		return Prepared{}, fmt.Errorf("工作树 %s 停在 %s，所选分支 HEAD 是 %s", name, got, sha)
	}
	return Prepared{Name: name, Path: dest, Kind: "worktree", GitDir: repo}, nil
}

func branchHEAD(repo, branch string) (string, error) {
	if err := validateBranch(branch); err != nil {
		return "", err
	}
	if branch == "" {
		return git(repo, "rev-parse", "--verify", "HEAD")
	}
	if sha, err := git(repo, "rev-parse", "--verify", "--end-of-options", "refs/heads/"+branch); err == nil {
		return sha, nil
	}
	return git(repo, "rev-parse", "--verify", "--end-of-options", branch)
}

func ensureMirror(mirrorRoot, cache, url, branch string, auth Auth) error {
	cache, err := pathWithin(mirrorRoot, cache)
	if err != nil {
		return err
	}
	env, prefix, cleanup, err := auth.gitPrefix(url)
	if err != nil {
		return err
	}
	defer cleanup()
	if _, err := os.Stat(cache); err != nil {
		parent, err := pathWithin(mirrorRoot, filepath.Dir(cache))
		if err != nil {
			return err
		}
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return err
		}
		// URL and cache are data, after options have ended.
		args := append(append([]string{}, prefix...), "clone", "--bare", "--end-of-options", url, cache)
		if _, err := gitWith("", env, args...); err != nil {
			removeWithin(mirrorRoot, cache)
			return err
		}
	}
	if branch != "" {
		args := append(append([]string{}, prefix...), "fetch", "--end-of-options", "origin", branch)
		_, _ = gitWith(cache, env, args...)
	}
	return nil
}

func isRemoteURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	return strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") ||
		strings.HasPrefix(raw, "ssh://") || strings.HasPrefix(raw, "git@")
}

func validateRemote(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "-") || strings.Contains(raw, "..") || !gitArgPattern.MatchString(raw) {
		return "", fmt.Errorf("非法仓库地址")
	}
	switch {
	case strings.HasPrefix(raw, "http://"), strings.HasPrefix(raw, "https://"), strings.HasPrefix(raw, "ssh://"):
		rest := raw
		switch {
		case strings.HasPrefix(raw, "https://"):
			rest = strings.TrimPrefix(raw, "https://")
		case strings.HasPrefix(raw, "http://"):
			rest = strings.TrimPrefix(raw, "http://")
		default:
			rest = strings.TrimPrefix(raw, "ssh://")
		}
		if rest == "" || strings.HasPrefix(rest, "/") {
			return "", fmt.Errorf("非法仓库地址")
		}
		host := rest
		if i := strings.IndexByte(host, '/'); i >= 0 {
			host = host[:i]
		}
		if host == "" {
			return "", fmt.Errorf("非法仓库地址")
		}
		return raw, nil
	case strings.HasPrefix(raw, "git@"):
		colon := strings.Index(raw, ":")
		if colon <= len("git@") || strings.Contains(raw, "://") {
			return "", fmt.Errorf("不支持的仓库地址")
		}
		host := raw[len("git@"):colon]
		path := raw[colon+1:]
		if host == "" || path == "" {
			return "", fmt.Errorf("不支持的仓库地址")
		}
		return raw, nil
	default:
		return "", fmt.Errorf("不支持的仓库地址")
	}
}

func validateBranch(branch string) error {
	if branch == "" {
		return nil
	}
	if strings.HasPrefix(branch, "-") || strings.Contains(branch, "..") || strings.ContainsAny(branch, " \t\n\r") || !gitArgPattern.MatchString(branch) {
		return fmt.Errorf("非法分支名 %q", branch)
	}
	return nil
}

func localDir(url string) (string, error) {
	u := strings.TrimSpace(url)
	if strings.HasPrefix(u, "file://") {
		u = strings.TrimSpace(strings.TrimPrefix(u, "file://"))
	}
	// Reject ".." before Clean collapses it into another directory.
	if strings.Contains(u, "..") {
		return "", fmt.Errorf("本机目录不能包含 ..")
	}
	cleaned := filepath.Clean(u)
	if strings.Contains(cleaned, "..") {
		return "", fmt.Errorf("本机目录不能包含 ..")
	}
	if !filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("不支持的仓库地址")
	}
	info, err := os.Stat(cleaned)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("本机目录不可用")
	}
	return cleaned, nil
}

func isGitRepo(path string) bool {
	out, err := git(path, "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

func copyTree(src, dst string) error {
	srcRoot := filepath.Clean(src)
	dstRoot := filepath.Clean(dst)
	return filepath.Walk(srcRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		target, err := copyTarget(dstRoot, rel)
		if err != nil {
			return err
		}
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
		return copyFile(srcRoot, path, dstRoot, target, info.Mode().Perm())
	})
}

// copyTarget joins a walk-relative path onto dstRoot and keeps the result inside it.
func copyTarget(dstRoot, rel string) (string, error) {
	if rel != "." && !filepath.IsLocal(rel) {
		return "", fmt.Errorf("路径越界")
	}
	return pathWithin(dstRoot, filepath.Join(dstRoot, rel))
}

func copyFile(srcRoot, src, dstRoot, dst string, mode os.FileMode) error {
	srcSafe, err := pathWithin(srcRoot, src)
	if err != nil {
		return err
	}
	dstSafe, err := pathWithin(dstRoot, dst)
	if err != nil {
		return err
	}
	parent, err := pathWithin(dstRoot, filepath.Dir(dstSafe))
	if err != nil {
		return err
	}
	in, err := os.Open(srcSafe)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dstSafe, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	_, err = io.Copy(out, in)
	return err
}

func git(dir string, args ...string) (string, error) {
	return gitWith(dir, nil, args...)
}

func gitWith(dir string, extra []string, args ...string) (string, error) {
	safe := make([]string, 0, len(args))
	for _, arg := range args {
		if !gitArgPattern.MatchString(arg) {
			return "", fmt.Errorf("git 参数不被接受")
		}
		safe = append(safe, arg)
	}
	cmd := exec.Command("git", safe...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = mergeEnv(os.Environ(), append([]string{"GIT_TERMINAL_PROMPT=0"}, extra...))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(safe, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// pathWithin returns candidate only after it is confined to root.
// Relative candidates must be local; absolute ones must equal root or sit under root+separator.
// The returned string is the value that passed the check.
func pathWithin(root, candidate string) (string, error) {
	root = filepath.Clean(root)
	sep := string(os.PathSeparator)
	if !filepath.IsAbs(root) || root == sep {
		return "", fmt.Errorf("路径越界")
	}
	candidate = filepath.Clean(candidate)
	if candidate == "" {
		return "", fmt.Errorf("路径越界")
	}
	if !filepath.IsAbs(candidate) {
		if !filepath.IsLocal(candidate) {
			return "", fmt.Errorf("路径越界")
		}
		candidate = filepath.Clean(filepath.Join(root, candidate))
	}
	if strings.HasPrefix(candidate, root+sep) {
		return candidate, nil
	}
	if candidate == root && strings.HasPrefix(candidate, root) {
		return candidate, nil
	}
	return "", fmt.Errorf("路径越界")
}

// removeWithin deletes candidate only when it is still inside root.
func removeWithin(root, candidate string) {
	safe, err := pathWithin(root, candidate)
	if err != nil {
		return
	}
	_ = os.RemoveAll(safe)
}

func mergeEnv(base, extra []string) []string {
	overlay := map[string]string{}
	order := make([]string, 0, len(extra))
	for _, item := range extra {
		key, val, ok := strings.Cut(item, "=")
		if !ok || key == "" {
			continue
		}
		if _, seen := overlay[key]; !seen {
			order = append(order, key)
		}
		overlay[key] = val
	}
	out := make([]string, 0, len(base)+len(order))
	for _, item := range base {
		key, _, _ := strings.Cut(item, "=")
		if _, skip := overlay[key]; skip {
			continue
		}
		out = append(out, item)
	}
	for _, key := range order {
		out = append(out, key+"="+overlay[key])
	}
	return out
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
