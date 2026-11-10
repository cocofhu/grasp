package workspacesnapshot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"backend/internal/workspacejob"
)

type liveRepository struct {
	value        Repository
	work         *os.Root
	git          *os.Root
	common       *os.Root
	stage        *os.Root
	stageDir     *os.File
	stagePath    string
	indexHash    string
	tracked      map[string]bool
	ancestors    map[string]bool
	objectBytes  int64
	gitPath      string
	commonPath   string
	ignoreHashes map[string]string
	materialized bool
	excludeHash  string
}

func validRef(name string) bool {
	if !strings.HasPrefix(name, "refs/") || !utf8.ValidString(name) || len(name) > 1024 || strings.ContainsAny(name, "\\\x00\r\n ~^:?*[") || strings.Contains(name, "..") || strings.Contains(name, "@{") || strings.HasSuffix(name, ".") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
		for _, c := range part {
			if c < 0x20 || c == 0x7f {
				return false
			}
		}
	}
	return true
}

func (r *liveRepository) close() {
	for _, root := range []*os.Root{r.work, r.git, r.common, r.stage} {
		if root != nil {
			_ = root.Close()
		}
	}
	if r.stageDir != nil {
		_ = r.stageDir.Close()
	}
}

func (e *Engine) repositoryPaths(name string, work *os.Root) (gitPath, commonPath string, linked bool, err error) {
	info, err := work.Lstat(".git")
	if err != nil {
		return "", "", false, err
	}
	gitPath = path.Join(name, ".git")
	if !info.IsDir() {
		data, readErr := readSmall(work, ".git", 4096)
		if readErr != nil || !strings.HasPrefix(string(data), "gitdir: ") {
			return "", "", false, ErrGitUnsupported
		}
		value := strings.TrimSpace(strings.TrimPrefix(string(data), "gitdir: "))
		absolute := value
		if !filepath.IsAbs(value) {
			absolute = filepath.Join(e.config.Root, filepath.FromSlash(name), value)
		}
		gitPath, err = nativeRelative(e.config.Root, absolute)
		if err != nil {
			return "", "", false, ErrGitUnsupported
		}
		linked = true
	}
	git, err := e.root.OpenRoot(gitPath)
	if err != nil {
		return "", "", false, ErrGitUnsupported
	}
	defer func() { _ = git.Close() }()
	commonPath = gitPath
	data, err := readSmall(git, "commondir", 4096)
	if err == nil {
		value := strings.TrimSpace(string(data))
		absolute := value
		if !filepath.IsAbs(value) {
			absolute = filepath.Join(e.config.Root, filepath.FromSlash(gitPath), value)
		}
		commonPath, err = nativeRelative(e.config.Root, absolute)
		if err != nil {
			return "", "", false, ErrGitUnsupported
		}
		linked = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", false, err
	}
	return gitPath, commonPath, linked, nil
}

func readRefs(root *os.Root, limit int) ([]GitRef, error) {
	refs := map[string]string{}
	symbolic := map[string]string{}
	packed, err := readSmall(root, "packed-refs", 16<<20)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, line := range strings.Split(string(packed), "\n") {
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "^") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || !oidPattern.MatchString(fields[0]) || !validRef(fields[1]) {
			return nil, ErrGitUnsupported
		}
		refs[fields[1]] = fields[0]
	}
	if _, err := root.Lstat("refs"); err == nil {
		err = fs.WalkDir(root.FS(), "refs", func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return ErrGitUnsupported
			}
			if entry.IsDir() {
				return nil
			}
			if !validRef(name) {
				return ErrGitUnsupported
			}
			if len(refs)+len(symbolic) >= limit {
				return ErrLimit
			}
			data, err := readSmall(root, name, 4096)
			if err != nil {
				return err
			}
			value := strings.TrimSpace(string(data))
			if target, ok := strings.CutPrefix(value, "ref: "); ok {
				if !validRef(target) || target == name {
					return ErrGitUnsupported
				}
				delete(refs, name)
				symbolic[name] = target
				return nil
			}
			if !oidPattern.MatchString(value) {
				return ErrGitUnsupported
			}
			refs[name] = value
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if len(refs)+len(symbolic) > limit {
		return nil, ErrLimit
	}
	values := make([]GitRef, 0, len(refs)+len(symbolic))
	for name, oid := range refs {
		values = append(values, GitRef{Name: name, OID: oid})
	}
	for name, target := range symbolic {
		values = append(values, GitRef{Name: name, Target: target})
	}
	slices.SortFunc(values, func(a, b GitRef) int { return strings.Compare(a.Name, b.Name) })
	return values, nil
}

func resolveHEAD(root *os.Root, refs []GitRef) (head, symbolic string, err error) {
	data, err := readSmall(root, "HEAD", 4096)
	if err != nil {
		return "", "", err
	}
	value := strings.TrimSpace(string(data))
	if strings.HasPrefix(value, "ref: ") {
		symbolic = strings.TrimPrefix(value, "ref: ")
		if !validRef(symbolic) || !strings.HasPrefix(symbolic, "refs/heads/") {
			return "", "", ErrGitUnsupported
		}
		// An unborn branch (or one aliased to a missing ref) has no commit.
		return resolveRef(refs, symbolic), symbolic, nil
	}
	if !oidPattern.MatchString(value) {
		return "", "", ErrGitUnsupported
	}
	return value, "", nil
}

// resolveRef follows symbolic refs the way Git does (bounded depth) and
// returns "" for a dangling or cyclic chain.
func resolveRef(refs []GitRef, name string) string {
	for range 5 {
		i := slices.IndexFunc(refs, func(ref GitRef) bool { return ref.Name == name })
		if i < 0 {
			return ""
		}
		if refs[i].Target == "" {
			return refs[i].OID
		}
		name = refs[i].Target
	}
	return ""
}

func (e *Engine) prepareRepository(ctx context.Context, h *workspacejob.Handle, stage *os.Root, name string, number int, expected *os.Root) (_ *liveRepository, retErr error) {
	r := &liveRepository{tracked: map[string]bool{}, ancestors: map[string]bool{}, ignoreHashes: map[string]string{}, materialized: true}
	defer func() {
		if retErr != nil {
			r.close()
		}
	}()
	var err error
	r.work, err = e.root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	before, err := expected.Stat(".")
	if err != nil {
		return nil, err
	}
	actual, err := r.work.Stat(".")
	if err != nil || !os.SameFile(before, actual) {
		return nil, ErrChanged
	}
	gitPath, commonPath, linked, err := e.repositoryPaths(name, r.work)
	if err != nil {
		return nil, err
	}
	r.gitPath, r.commonPath = gitPath, commonPath
	for _, value := range []string{gitPath, commonPath} {
		if reason := mandatory(value, e.config.PrivatePaths); reason != "" && reason != "git_metadata_rebuilt" {
			return nil, ErrGitUnsupported
		}
	}
	r.git, err = e.root.OpenRoot(gitPath)
	if err != nil {
		return nil, err
	}
	r.common, err = e.root.OpenRoot(commonPath)
	if err != nil {
		return nil, err
	}
	for _, unsafe := range []string{"objects/info/alternates", "objects/info/http-alternates", "info/grafts", "shallow", "reftable", "packed-refs.lock"} {
		if _, err := r.common.Lstat(unsafe); err == nil {
			return nil, ErrGitUnsupported
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	refs, err := readRefs(r.common, e.config.Limits.MaxEntries)
	if err != nil {
		return nil, err
	}
	head, symbolic, err := resolveHEAD(r.git, refs)
	if err != nil {
		return nil, err
	}
	// Query only bounded config bytes on stdin with includes disabled. Native
	// Git never loads the source repository/config or follows its includes.
	// This identifies an unborn SHA-256 repository that has no OIDs/index yet.
	formatConfig, err := readSmall(r.common, "config", 1<<20)
	if err != nil {
		return nil, err
	}
	stageDirectory, err := stage.Open(".")
	if err != nil {
		return nil, err
	}
	configured, configErr := e.git(ctx, h, descriptor(stageDirectory), bytes.NewReader(formatConfig), nil, nil, "config", "--no-includes", "--file", "-", "--get", "extensions.objectformat")
	_ = stageDirectory.Close()
	if configErr != nil {
		var exit interface{ ExitCode() int }
		if !errors.As(configErr, &exit) || exit.ExitCode() != 1 {
			return nil, ErrGitUnsupported
		}
	}
	format := "sha1"
	if value := strings.TrimSpace(string(configured)); value != "" {
		if value != "sha1" && value != "sha256" {
			return nil, ErrGitUnsupported
		}
		format = value
	}
	for _, ref := range append(slices.Clone(refs), GitRef{OID: head}) {
		if len(ref.OID) == 64 {
			format = "sha256"
			break
		}
	}
	index, err := readSmall(r.git, "index", e.config.Limits.MaxManifestBytes)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	decoded, indexFormat, err := decodeIndex(index, e.config.Limits.MaxEntries)
	if err != nil {
		return nil, err
	}
	if indexFormat != "" {
		if (head != "" || len(refs) > 0) && indexFormat != format {
			return nil, ErrGitUnsupported
		}
		format = indexFormat
	}
	if decoded == nil {
		decoded = []IndexEntry{}
	}
	r.value = Repository{Path: name, ObjectFormat: format, HEAD: head, SymbolicHEAD: symbolic, Refs: refs, Linked: linked, Index: decoded}
	r.value.Operation, err = readGitOperation(r.git)
	if err != nil {
		return nil, err
	}
	stageName := fmt.Sprintf("repos/%d", number)
	if err := h.Guard(func() error { return stage.MkdirAll(stageName, 0700) }); err != nil {
		return nil, err
	}
	r.stage, err = stage.OpenRoot(stageName)
	if err != nil {
		return nil, err
	}
	r.stageDir, err = r.stage.Open(".")
	if err != nil {
		return nil, err
	}
	r.stagePath = descriptor(r.stageDir)
	if _, err := e.git(ctx, h, r.stagePath, nil, nil, nil, "init", "--bare", "--template=", "--object-format="+format, "."); err != nil {
		return nil, err
	}
	exclude, err := readSmall(r.common, "info/exclude", 1<<20)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	excludeHash := sha256.Sum256(exclude)
	r.excludeHash = hex.EncodeToString(excludeHash[:])
	if len(exclude) > 0 {
		if err := writePrivate(h, r.stage, "info/exclude", exclude); err != nil {
			return nil, err
		}
	}
	if err := e.copyObjects(h, r, stageName); err != nil {
		return nil, err
	}
	indexHash := sha256.Sum256(index)
	r.indexHash = hex.EncodeToString(indexHash[:])
	r.objectBytes += int64(len(index)) + int64(len(exclude))
	if len(index) > 0 {
		if err := writePrivate(h, r.stage, "source-index", index); err != nil {
			return nil, err
		}
		data, err := e.git(ctx, h, r.stagePath, nil, nil, []string{"GIT_INDEX_FILE=" + r.stagePath + "/source-index"}, "ls-files", "-v", "--stage", "-z")
		if err != nil {
			return nil, ErrGitUnsupported
		}
		native, err := parseIndex(data, e.config.Limits.MaxEntries)
		if err != nil || !slices.EqualFunc(native, decoded, func(a, b IndexEntry) bool {
			a.Intent, b.Intent = false, false
			return a == b
		}) {
			return nil, ErrGitUnsupported
		}
	}
	for _, entry := range r.value.Index {
		r.tracked[entry.Path] = true
		for name := path.Dir(entry.Path); name != "."; name = path.Dir(name) {
			r.ancestors[name] = true
		}
	}
	if err := h.Guard(func() error { return r.stage.Mkdir("ignore-worktree", 0700) }); err != nil {
		return nil, err
	}
	return r, nil
}

func parseIndex(data []byte, limit int) ([]IndexEntry, error) {
	entries := []IndexEntry{}
	seen := map[string]bool{}
	for _, record := range bytes.Split(data, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		fields := bytes.SplitN(record, []byte{'\t'}, 2)
		parts := strings.Fields(string(fields[0]))
		if len(fields) != 2 || len(parts) != 4 || len(parts[0]) != 1 || !safePath(string(fields[1])) || string(fields[1]) == "." || !oidPattern.MatchString(parts[2]) || len(entries) >= limit {
			return nil, ErrGitUnsupported
		}
		for _, part := range strings.Split(string(fields[1]), "/") {
			if strings.EqualFold(part, ".git") {
				return nil, ErrUnsafe
			}
		}
		if parts[1] != "100644" && parts[1] != "100755" && parts[1] != "120000" && parts[1] != "160000" {
			return nil, ErrGitUnsupported
		}
		stage, err := strconv.Atoi(parts[3])
		if err != nil || stage < 0 || stage > 3 {
			return nil, ErrGitUnsupported
		}
		key := strconv.Itoa(stage) + ":" + string(fields[1])
		if seen[key] {
			return nil, ErrGitUnsupported
		}
		seen[key] = true
		tag := parts[0][0]
		entries = append(entries, IndexEntry{Path: string(fields[1]), Mode: parts[1], OID: parts[2], Stage: stage, Assume: tag >= 'a' && tag <= 'z', Skip: tag == 'S' || tag == 's'})
	}
	return entries, nil
}

// Git never reads the original object directory. Copy only confined regular
// object/pack bytes into a private bare repository before invoking native Git;
// a concurrent metadata symlink cannot redirect it to another host directory.
func (e *Engine) copyObjects(h *workspacejob.Handle, r *liveRepository, stageName string) error {
	count := 0
	return fs.WalkDir(r.common.FS(), "objects", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := h.Check(); err != nil {
			return err
		}
		count++
		if count > e.config.Limits.MaxScanEntries || entry.Type()&os.ModeSymlink != 0 {
			return ErrGitUnsupported
		}
		if entry.IsDir() {
			if strings.HasPrefix(name, "objects/info") {
				return fs.SkipDir
			}
			return h.Guard(func() error { return r.stage.MkdirAll(name, 0700) })
		}
		info, err := r.common.Lstat(name)
		if err != nil || !info.Mode().IsRegular() {
			return ErrGitUnsupported
		}
		if info.Size() > e.config.Limits.MaxGitBytes-r.objectBytes {
			return ErrLimit
		}
		if strings.HasPrefix(name, "objects/pack/") {
			base := path.Base(name)
			if strings.HasSuffix(base, ".promisor") {
				return ErrGitUnsupported
			}
			if !strings.HasSuffix(base, ".pack") && !strings.HasSuffix(base, ".idx") {
				return nil
			}
		}
		file, err := r.common.OpenFile(name, os.O_RDONLY|nofollow, 0)
		if err != nil {
			return err
		}
		before, err := file.Stat()
		if err == nil && !os.SameFile(info, before) {
			err = ErrChanged
		}
		if err == nil {
			err = h.WriteFile(stageName+"/"+name, file, max(info.Size(), 1))
		}
		after, statErr := file.Stat()
		err = errors.Join(err, statErr, file.Close())
		if err != nil {
			return err
		}
		if !sameFileState(before, after) {
			return ErrChanged
		}
		r.objectBytes += info.Size()
		return nil
	})
}
