// Package workspacegit transfers selected immutable Git commits into a new,
// isolated workspace directory. It never edits, commits or pushes a source.
package workspacegit

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"unicode/utf8"

	"backend/internal/workspacejob"
)

const (
	Version               = 1
	MaxRepositories       = 8
	MaxBundleBytes  int64 = 64 << 20
	MaxTotalBytes   int64 = 128 << 20
	MaxFileBytes    int64 = 32 << 20
	MaxEntries            = 50000
	ImportDirectory       = ".grasp-code-imports"
)

var (
	ErrUnsafe      = errors.New("unsafe workspace code transfer")
	ErrUnsupported = errors.New("workspace format is not supported for Git commit import")
	ErrLimit       = errors.New("workspace code transfer exceeds its limit")
	ErrConflict    = errors.New("import directory already exists without a matching complete receipt")
	commitPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	idPattern      = regexp.MustCompile(`^[a-zA-Z0-9-]{1,64}$`)
)

type Selection struct {
	Path   string `json:"path"`
	Commit string `json:"commit"`
}
type Repository struct {
	Path      string `json:"path"`
	Commit    string `json:"commit"`
	Head      string `json:"head"`
	Dirty     bool   `json:"dirty"`
	Untracked bool   `json:"untracked"`
	Files     int    `json:"files"`
	Bytes     int64  `json:"bytes"`
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
}
type Inventory struct {
	Version        int          `json:"version"`
	RenewableLease bool         `json:"renewableLease"`
	Repositories   []Repository `json:"repositories"`
	WorkingChanges bool         `json:"workingChanges"`
	NonGit         bool         `json:"nonGit"`
	IgnoredFiles   bool         `json:"ignoredFiles"`
	MaxBundleBytes int64        `json:"maxBundleBytes"`
	MaxTotalBytes  int64        `json:"maxTotalBytes"`
	MaxFileBytes   int64        `json:"maxFileBytes"`
}
type Manifest struct {
	Version      int          `json:"version"`
	Repositories []Repository `json:"repositories"`
}
type Receipt struct {
	ID            string       `json:"id"`
	ArchiveSHA256 string       `json:"archiveSha256"`
	Directory     string       `json:"directory"`
	Repositories  []Repository `json:"repositories"`
}
type Service struct {
	Root string
	mu   sync.Mutex
}

func (s *Service) Inventory(ctx context.Context) (Inventory, error) {
	result := Inventory{Version: Version, RenewableLease: true, Repositories: []Repository{}, MaxBundleBytes: MaxBundleBytes, MaxTotalBytes: MaxTotalBytes, MaxFileBytes: MaxFileBytes}
	root, err := os.OpenRoot(s.Root)
	if err != nil {
		return result, err
	}
	defer func() { _ = root.Close() }()
	if _, err := root.Lstat(".git"); err == nil {
		repo, err := s.inspect(ctx, ".", "")
		if err != nil {
			result.Repositories = append(result.Repositories, Repository{Path: ".", Reason: repositoryReason(err)})
		} else {
			result.Repositories = append(result.Repositories, repo)
		}
		return result, nil
	}
	dir, err := root.Open(".")
	if err != nil {
		return result, err
	}
	defer func() { _ = dir.Close() }()
	entries, err := dir.ReadDir(129)
	if err != nil && !errors.Is(err, io.EOF) {
		return result, err
	}
	if len(entries) > 128 {
		return result, ErrLimit
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if _, err := root.Lstat(path.Join(entry.Name(), ".git")); err != nil {
			continue
		}
		if len(result.Repositories) >= MaxRepositories {
			return result, ErrLimit
		}
		repo, err := s.inspect(ctx, entry.Name(), "")
		if err != nil {
			result.Repositories = append(result.Repositories, Repository{Path: entry.Name(), Reason: repositoryReason(err)})
		} else {
			result.Repositories = append(result.Repositories, repo)
		}
	}
	return result, nil
}

func safeRepoPath(value string) bool {
	return value == "." || (value != "" && value != ".." && !strings.HasPrefix(value, ".") && !strings.ContainsAny(value, "/\\\x00\r\n") && utf8.ValidString(value) && len(value) <= 255)
}

// repo returns a stable directory handle. Git runs through this handle rather
// than a re-resolvable workspace path, so renaming/symlinking a source cannot
// redirect it outside the selected workspace.
func (s *Service) repo(value string) (*os.File, error) {
	if !safeRepoPath(value) {
		return nil, ErrUnsafe
	}
	root, err := os.OpenRoot(s.Root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	child, err := root.OpenRoot(value)
	if err != nil {
		return nil, err
	}
	defer func() { _ = child.Close() }()
	info, err := child.Lstat(".git")
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, unsupportedRepository("repository_external_worktree")
	}
	for _, name := range []string{".git/commondir", ".git/objects/info/alternates", ".git/objects/info/http-alternates", ".git/info/grafts", ".git/shallow"} {
		if _, err := child.Lstat(name); err == nil {
			reason := "repository_external_objects"
			switch name {
			case ".git/commondir":
				reason = "repository_external_worktree"
			case ".git/info/grafts":
				reason = "repository_rewritten_history"
			case ".git/shallow":
				reason = "repository_shallow"
			}
			return nil, unsupportedRepository(reason)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	// Git itself is not an os.Root-aware reader. Refuse metadata symlinks so
	// its object/index/config reads cannot follow a crafted external directory.
	entries := 0
	if err := fs.WalkDir(child.FS(), ".git", func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		entries++
		if entries > MaxEntries {
			return ErrLimit
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return unsupportedRepository("repository_unsafe_metadata")
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return child.Open(".")
}
func handlePath(dir *os.File) string { return fmt.Sprintf("/proc/%d/fd/%d", os.Getpid(), dir.Fd()) }

func command(ctx context.Context, dir string, stdout io.Writer, args ...string) ([]byte, error) {
	return commandInput(ctx, dir, stdout, nil, args...)
}

// Git ownership exceptions are private to an already validated, open source
// directory. Git requires its canonical pathname in safe.directory; /proc FD
// spellings alone are not accepted. The working directory remains the pinned
// FD, so a subsequent pathname replacement cannot redirect this command.
// A rename racing the pathname check may fail closed, never trust its neighbor.
func sourceCommand(ctx context.Context, dir *os.File, stdout io.Writer, stdin io.Reader, args ...string) ([]byte, error) {
	root := handlePath(dir)
	name, err := os.Readlink(root)
	if err != nil || !filepath.IsAbs(name) || strings.Contains(name, "*") {
		return nil, unsupportedRepository("repository_unsafe_metadata")
	}
	opened, err := dir.Stat()
	if err != nil {
		return nil, err
	}
	current, err := os.Stat(name)
	if err != nil || !os.SameFile(opened, current) {
		return nil, unsupportedRepository("repository_unsafe_metadata")
	}
	return commandInput(ctx, root, stdout, stdin, append([]string{"-c", "safe.directory=" + name}, args...)...)
}

func commandInput(ctx context.Context, dir string, stdout io.Writer, stdin io.Reader, args ...string) ([]byte, error) {
	base := []string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-c", "core.attributesFile=/dev/null", "-c", "core.excludesFile=/dev/null", "-c", "submodule.recurse=false", "-c", "gc.auto=0", "-c", "maintenance.auto=false", "-c", "protocol.allow=never", "-c", "protocol.file.allow=always", "-c", "pack.threads=1", "-c", "uploadpack.packObjectsHook="}
	cmd := workspacejob.NewSupervisedCommand(ctx, append([]string{"git"}, append(base, args...)...)...)
	cmd.Dir = dir
	cmd.Stdin = stdin
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "GIT_") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_NO_REPLACE_OBJECTS=1", "GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	var output bytes.Buffer
	if stdout == nil {
		cmd.Stdout = &boundedWriter{writer: &output, remaining: 16 << 20}
	} else {
		cmd.Stdout = stdout
	}
	var stderr bytes.Buffer
	cmd.Stderr = &boundedWriter{writer: &stderr, remaining: 16 << 10}
	err := cmd.Run()
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		workspacejob.WaitProcessGroupExit(cmd.Process.Pid)
	}
	if ctx.Err() != nil {
		return nil, context.Cause(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("git transfer operation failed: %w", err)
	}
	return output.Bytes(), nil
}

type boundedWriter struct {
	writer    io.Writer
	remaining int64
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, ErrLimit
	}
	n, err := w.writer.Write(p)
	w.remaining -= int64(n)
	return n, err
}

func (s *Service) inspect(ctx context.Context, repoPath, selected string) (Repository, error) {
	dir, err := s.repo(repoPath)
	if err != nil {
		return Repository{}, err
	}
	defer func() { _ = dir.Close() }()
	// Status may invoke a repository's custom clean/process filter while
	// checking files. Such repositories require a snapshot path; a read-only
	// preview must not execute arbitrary filter programs or refresh their index.
	filters, filterErr := sourceCommand(ctx, dir, nil, nil, "config", "--no-includes", "--local", "--null", "--get-regexp", `^(filter\..*\.(clean|smudge|process)|core\.worktree|extensions\.worktreeconfig|include\..*|includeif\..*)$`)
	if filterErr != nil {
		var exit *exec.ExitError
		if !errors.As(filterErr, &exit) || exit.ExitCode() != 1 {
			return Repository{}, filterErr
		}
	}
	if len(filters) != 0 {
		reason := "repository_custom_configuration"
		for _, setting := range bytes.Split(filters, []byte{0}) {
			key := string(bytes.SplitN(setting, []byte{'\n'}, 2)[0])
			if key == "core.worktree" || key == "extensions.worktreeconfig" {
				reason = "repository_external_worktree"
				break
			}
		}
		return Repository{}, unsupportedRepository(reason)
	}
	head, err := sourceCommand(ctx, dir, nil, nil, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return Repository{}, err
	}
	if selected == "" {
		selected = strings.TrimSpace(string(head))
	}
	if !commitPattern.MatchString(selected) {
		return Repository{}, ErrUnsafe
	}
	commit, err := sourceCommand(ctx, dir, nil, nil, "rev-parse", "--verify", selected+"^{commit}")
	if err != nil || strings.TrimSpace(string(commit)) != selected {
		return Repository{}, ErrUnsupported
	}
	files, total, err := validateSourceTree(ctx, dir, selected)
	if err != nil {
		return Repository{}, err
	}
	status, err := sourceCommand(ctx, dir, nil, nil, "status", "--porcelain=v1", "-z", "--untracked-files=normal")
	if err != nil {
		return Repository{}, err
	}
	dirty, untracked := false, false
	for _, entry := range bytes.Split(status, []byte{0}) {
		if len(entry) < 3 {
			continue
		}
		if bytes.HasPrefix(entry, []byte("?? ")) {
			untracked = true
		} else {
			dirty = true
		}
	}
	return Repository{Path: repoPath, Commit: selected, Head: strings.TrimSpace(string(head)), Dirty: dirty, Untracked: untracked, Files: files, Bytes: total, Supported: true}, nil
}
func validateTree(ctx context.Context, root, commit string) (int, int64, error) {
	return validateTreeWith(ctx, commit, func(stdout io.Writer, stdin io.Reader, args ...string) ([]byte, error) {
		return commandInput(ctx, root, stdout, stdin, args...)
	})
}

func validateSourceTree(ctx context.Context, dir *os.File, commit string) (int, int64, error) {
	return validateTreeWith(ctx, commit, func(stdout io.Writer, stdin io.Reader, args ...string) ([]byte, error) {
		return sourceCommand(ctx, dir, stdout, stdin, args...)
	})
}

func validateTreeWith(ctx context.Context, commit string, run func(io.Writer, io.Reader, ...string) ([]byte, error)) (int, int64, error) {
	data, err := run(nil, nil, "ls-tree", "-rlz", "--full-tree", commit)
	if err != nil {
		return 0, 0, err
	}
	count := 0
	var total int64
	type inspectedBlob struct {
		name, object string
		symlink      bool
		size         int64
	}
	var inspect []inspectedBlob
	var query strings.Builder
	for _, entry := range bytes.Split(data, []byte{0}) {
		if len(entry) == 0 {
			continue
		}
		count++
		if count > MaxEntries {
			return 0, 0, ErrLimit
		}
		parts := bytes.SplitN(entry, []byte{'\t'}, 2)
		if len(parts) != 2 {
			return 0, 0, ErrUnsafe
		}
		name := string(parts[1])
		if !utf8.ValidString(name) || path.IsAbs(name) || strings.ContainsAny(name, "\\\x00") {
			return 0, 0, ErrUnsafe
		}
		for _, part := range strings.Split(name, "/") {
			if part == ".." || part == "." || strings.EqualFold(part, ".git") {
				return 0, 0, ErrUnsafe
			}
		}
		var mode, kind, object string
		var size int64
		if bytes.HasPrefix(parts[0], []byte("160000 ")) {
			return 0, 0, unsupportedRepository("repository_submodules")
		}
		if _, err := fmt.Sscanf(string(parts[0]), "%s %s %s %d", &mode, &kind, &object, &size); err != nil {
			return 0, 0, ErrUnsupported
		}
		if kind != "blob" || (mode != "100644" && mode != "100755" && mode != "120000") {
			return 0, 0, ErrUnsupported
		}
		if size > MaxFileBytes || size < 0 || size > MaxTotalBytes-total {
			return 0, 0, ErrLimit
		}
		total += size
		if mode == "120000" || size < 1024 {
			if !commitPattern.MatchString(object) {
				return 0, 0, ErrUnsafe
			}
			inspect = append(inspect, inspectedBlob{name: name, object: object, symlink: mode == "120000", size: size})
			query.WriteString(object)
			query.WriteByte('\n')
		}
	}
	if len(inspect) != 0 {
		var output bytes.Buffer
		// One bounded batch handles the complete tree. Starting one process per
		// small blob made valid repositories approach the lease deadline.
		limit := MaxTotalBytes + int64(MaxEntries)*128
		_, err := run(&boundedWriter{writer: &output, remaining: limit}, strings.NewReader(query.String()), "cat-file", "--batch")
		if err != nil {
			return 0, 0, err
		}
		reader := bufio.NewReader(&output)
		for _, entry := range inspect {
			if err := ctx.Err(); err != nil {
				return 0, 0, err
			}
			header, err := reader.ReadString('\n')
			if err != nil || header != fmt.Sprintf("%s blob %d\n", entry.object, entry.size) {
				return 0, 0, ErrUnsafe
			}
			blob := make([]byte, int(entry.size))
			if _, err := io.ReadFull(reader, blob); err != nil {
				return 0, 0, err
			}
			if end, err := reader.ReadByte(); err != nil || end != '\n' {
				return 0, 0, ErrUnsafe
			}
			if bytes.HasPrefix(blob, []byte("version https://git-lfs.github.com/spec/v1")) {
				return 0, 0, unsupportedRepository("repository_lfs")
			}
			if entry.symlink {
				target := string(blob)
				resolved := path.Clean(path.Join(path.Dir(entry.name), target))
				if path.IsAbs(target) || strings.ContainsAny(target, "\\\x00") || resolved == ".." || strings.HasPrefix(resolved, "../") {
					return 0, 0, ErrUnsafe
				}
			}
		}
		if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
			return 0, 0, ErrUnsafe
		}
	}
	return count, total, nil
}

func (s *Service) Export(ctx context.Context, selections []Selection, destination io.Writer) (Manifest, error) {
	if len(selections) < 1 || len(selections) > MaxRepositories {
		return Manifest{}, ErrLimit
	}
	manifest := Manifest{Version: Version}
	seen := map[string]bool{}
	temp, err := os.MkdirTemp("", "grasp-git-export-*")
	if err != nil {
		return manifest, err
	}
	defer func() { _ = os.RemoveAll(temp) }()
	var bundleBytes int64
	var fileBytes int64
	var fileCount int
	for i, selection := range selections {
		if seen[selection.Path] {
			return manifest, ErrUnsafe
		}
		seen[selection.Path] = true
		repo, err := s.inspect(ctx, selection.Path, selection.Commit)
		if err != nil {
			return manifest, err
		}
		manifest.Repositories = append(manifest.Repositories, repo)
		fileBytes += repo.Bytes
		fileCount += repo.Files
		if fileBytes > MaxTotalBytes || fileCount > MaxEntries {
			return manifest, ErrLimit
		}
		source, err := s.repo(selection.Path)
		if err != nil {
			return manifest, err
		}
		bare := filepath.Join(temp, fmt.Sprintf("transfer-%d.git", i))
		_, err = command(ctx, temp, nil, "init", "--bare", "--template=", bare)
		if err == nil {
			err = os.WriteFile(filepath.Join(bare, "objects/info/alternates"), []byte(handlePath(source)+"/.git/objects\n"), 0600)
		}
		if err == nil {
			_, err = command(ctx, bare, nil, "update-ref", "refs/heads/import", repo.Commit)
		}
		file, e := os.OpenFile(filepath.Join(temp, fmt.Sprintf("%d.bundle", i)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			_ = source.Close()
			return manifest, e
		}
		if err == nil {
			_, err = command(ctx, bare, &boundedWriter{writer: file, remaining: MaxBundleBytes}, "bundle", "create", "-", "refs/heads/import")
		}
		_ = source.Close()
		info, e := file.Stat()
		err = errors.Join(err, e, file.Close())
		if err != nil {
			return manifest, err
		}
		bundleBytes += info.Size()
		if bundleBytes > MaxTotalBytes {
			return manifest, ErrLimit
		}
	}
	z := gzip.NewWriter(destination)
	tw := tar.NewWriter(z)
	metadata, err := json.Marshal(manifest)
	if err != nil {
		return manifest, err
	}
	if err = tw.WriteHeader(&tar.Header{Name: "manifest.json", Typeflag: tar.TypeReg, Mode: 0600, Size: int64(len(metadata))}); err == nil {
		_, err = tw.Write(metadata)
	}
	if err != nil {
		return manifest, err
	}
	for i := range selections {
		f, err := os.Open(filepath.Join(temp, fmt.Sprintf("%d.bundle", i)))
		if err != nil {
			return manifest, err
		}
		info, err := f.Stat()
		if err == nil {
			err = tw.WriteHeader(&tar.Header{Name: fmt.Sprintf("repos/%d.bundle", i), Typeflag: tar.TypeReg, Mode: 0600, Size: info.Size()})
		}
		if err == nil {
			_, err = io.Copy(tw, f)
		}
		err = errors.Join(err, f.Close())
		if err != nil {
			return manifest, err
		}
	}
	return manifest, errors.Join(tw.Close(), z.Close())
}

func (s *Service) Receipt(id string) (Receipt, error) {
	if !idPattern.MatchString(id) {
		return Receipt{}, ErrUnsafe
	}
	root, err := os.OpenRoot(s.Root)
	if err != nil {
		return Receipt{}, err
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open(path.Join(ImportDirectory, id, ".grasp-import.json"))
	if err != nil {
		return Receipt{}, err
	}
	defer func() { _ = f.Close() }()
	var result Receipt
	err = json.NewDecoder(io.LimitReader(f, 64<<10)).Decode(&result)
	if err != nil {
		return result, err
	}
	if result.ID != id || result.Directory != path.Join(ImportDirectory, id) {
		return Receipt{}, ErrUnsafe
	}
	return result, nil
}

func (s *Service) Import(ctx context.Context, id, digest string, source io.Reader) (Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	if !idPattern.MatchString(id) || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(digest) {
		return Receipt{}, ErrUnsafe
	}
	if existing, err := s.Receipt(id); err == nil {
		if existing.ArchiveSHA256 != digest {
			return Receipt{}, ErrConflict
		}
		return existing, nil
	}
	temp, err := os.MkdirTemp("", "grasp-git-import-*")
	if err != nil {
		return Receipt{}, err
	}
	defer func() { _ = os.RemoveAll(temp) }()
	hash := sha256.New()
	compressed := &io.LimitedReader{R: io.TeeReader(contextReader{ctx: ctx, source: source}, hash), N: MaxTotalBytes + (1 << 20)}
	z, err := gzip.NewReader(compressed)
	if err != nil {
		return Receipt{}, err
	}
	defer func() { _ = z.Close() }()
	tr := tar.NewReader(z)
	var manifest Manifest
	seen := map[string]bool{}
	var total int64
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Receipt{}, err
		}
		if h.Typeflag != tar.TypeReg || h.Size < 0 || seen[h.Name] || len(seen) > MaxRepositories {
			return Receipt{}, ErrUnsafe
		}
		seen[h.Name] = true
		if h.Name == "manifest.json" {
			if h.Size > 64<<10 {
				return Receipt{}, ErrLimit
			}
			if err = json.NewDecoder(io.LimitReader(tr, h.Size)).Decode(&manifest); err != nil {
				return Receipt{}, err
			}
			continue
		}
		var index int
		count, parseErr := fmt.Sscanf(h.Name, "repos/%d.bundle", &index)
		if parseErr != nil || count != 1 || index < 0 || index >= MaxRepositories || h.Name != fmt.Sprintf("repos/%d.bundle", index) {
			return Receipt{}, ErrUnsafe
		}
		if h.Size > MaxBundleBytes || h.Size > MaxTotalBytes-total {
			return Receipt{}, ErrLimit
		}
		total += h.Size
		f, err := os.OpenFile(filepath.Join(temp, fmt.Sprintf("%d.bundle", index)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return Receipt{}, err
		}
		_, err = io.Copy(f, tr)
		err = errors.Join(err, f.Close())
		if err != nil {
			return Receipt{}, err
		}
	}
	// Consume and validate gzip's footer; only bounded zero tar padding may follow.
	padding, err := io.ReadAll(io.LimitReader(z, (1<<20)+1))
	if err != nil {
		return Receipt{}, err
	}
	if len(padding) > 1<<20 || bytes.Count(padding, []byte{0}) != len(padding) {
		return Receipt{}, ErrUnsafe
	}
	if compressed.N <= 0 || hex.EncodeToString(hash.Sum(nil)) != digest {
		return Receipt{}, ErrUnsafe
	}
	if manifest.Version != Version || len(manifest.Repositories) < 1 || len(manifest.Repositories) > MaxRepositories || len(seen) != len(manifest.Repositories)+1 {
		return Receipt{}, ErrUnsafe
	}
	names := map[string]bool{}
	for i, repo := range manifest.Repositories {
		name := repo.Path
		if name == "." {
			name = fmt.Sprintf("repo-%d", i+1)
		}
		if !safeRepoPath(repo.Path) || !commitPattern.MatchString(repo.Commit) || !seen[fmt.Sprintf("repos/%d.bundle", i)] || names[name] {
			return Receipt{}, ErrUnsafe
		}
		names[name] = true
	}
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	root, err := os.OpenRoot(s.Root)
	if err != nil {
		return Receipt{}, err
	}
	defer func() { _ = root.Close() }()
	if err = root.Mkdir(ImportDirectory, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return Receipt{}, err
	}
	imports, err := root.OpenRoot(ImportDirectory)
	if err != nil {
		return Receipt{}, err
	}
	defer func() { _ = imports.Close() }()
	if err = imports.Mkdir(id, 0700); err != nil {
		return Receipt{}, ErrConflict
	}
	// The exclusive directory is retained on failure: it can never be reused to
	// overwrite a partial import or user edits after an uncertain response.
	destination, err := imports.OpenRoot(id)
	if err != nil {
		return Receipt{}, err
	}
	defer func() { _ = destination.Close() }()
	dir, err := destination.Open(".")
	if err != nil {
		return Receipt{}, err
	}
	defer func() { _ = dir.Close() }()
	var fileBytes int64
	var fileCount int
	for i, repo := range manifest.Repositories {
		if err := ctx.Err(); err != nil {
			return Receipt{}, err
		}
		name := fmt.Sprintf("repo-%d", i+1)
		if repo.Path != "." {
			name = repo.Path
		}
		if err = destination.Mkdir(name, 0700); err != nil {
			return Receipt{}, err
		}
		repoPath := path.Join(handlePath(dir), name)
		if _, err = command(ctx, repoPath, nil, "init", "--template=", "."); err != nil {
			return Receipt{}, err
		}
		bundle := filepath.Join(temp, fmt.Sprintf("%d.bundle", i))
		if _, err = command(ctx, repoPath, nil, "bundle", "verify", bundle); err != nil {
			return Receipt{}, err
		}
		if _, err = command(ctx, repoPath, nil, "fetch", "--no-tags", bundle, "refs/heads/import"); err != nil {
			return Receipt{}, err
		}
		files, size, treeErr := validateTree(ctx, repoPath, repo.Commit)
		if treeErr != nil {
			return Receipt{}, treeErr
		}
		fileBytes += size
		fileCount += files
		if fileBytes > MaxTotalBytes || fileCount > MaxEntries {
			return Receipt{}, ErrLimit
		}
		if files != repo.Files || size != repo.Bytes {
			return Receipt{}, ErrUnsafe
		}
		if _, err = command(ctx, repoPath, nil, "checkout", "-b", "import/"+repo.Commit[:12], repo.Commit); err != nil {
			return Receipt{}, err
		}
	}
	result := Receipt{ID: id, ArchiveSHA256: digest, Directory: path.Join(ImportDirectory, id), Repositories: manifest.Repositories}
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return Receipt{}, err
	}
	f, err := destination.OpenFile(".grasp-import.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return Receipt{}, err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err == nil {
		err = dir.Sync()
	}
	if err != nil {
		return Receipt{}, err
	}
	return result, nil
}

type contextReader struct {
	ctx    context.Context
	source io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.source.Read(p)
}
