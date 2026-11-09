package workspacesnapshot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strings"

	"backend/internal/workspacejob"
)

type captureWalk struct {
	engine        *Engine
	handle        *workspacejob.Handle
	ctx           context.Context
	stage         *os.Root
	spec          Spec
	files         []Entry
	excluded      []Exclusion
	repos         []*liveRepository
	repoPaths     map[string]*liveRepository
	ignores       map[string]*liveRepository
	bytes         int64
	objects       int64
	scanned       int
	verify        bool
	metadata      int64
	indexes       int
	manifestBytes int64
}

func sameFileState(before, after os.FileInfo) bool {
	return os.SameFile(before, after) && before.Size() == after.Size() && before.Mode() == after.Mode() && before.ModTime().Equal(after.ModTime())
}

func (w *captureWalk) close() {
	for _, repo := range w.repos {
		repo.close()
	}
	for _, repo := range w.ignores {
		repo.close()
	}
}

func (w *captureWalk) scan() error {
	for _, name := range w.spec.Paths {
		info, err := w.engine.root.Lstat(name)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrUnsafe
		}
		root, err := w.engine.root.OpenRoot(name)
		if err != nil {
			return err
		}
		actual, err := root.Stat(".")
		if err == nil && !os.SameFile(info, actual) {
			err = ErrChanged
		}
		if err == nil {
			err = w.directory(root, name, nil)
		}
		_ = root.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (w *captureWalk) prepareIgnore(name string) (_ *liveRepository, retErr error) {
	if existing := w.ignores[name]; existing != nil {
		return existing, nil
	}
	if w.verify {
		return nil, ErrChanged
	}
	r := &liveRepository{value: Repository{Path: name}, tracked: map[string]bool{}, ancestors: map[string]bool{}, ignoreHashes: map[string]string{}}
	defer func() {
		if retErr != nil {
			r.close()
		}
	}()
	stageName := fmt.Sprintf("ignore/%d", len(w.ignores))
	if err := w.handle.Guard(func() error { return w.stage.MkdirAll(stageName, 0700) }); err != nil {
		return nil, err
	}
	var err error
	r.stage, err = w.stage.OpenRoot(stageName)
	if err != nil {
		return nil, err
	}
	r.stageDir, err = r.stage.Open(".")
	if err != nil {
		return nil, err
	}
	r.stagePath = descriptor(r.stageDir)
	if _, err := w.engine.git(w.ctx, w.handle, r.stagePath, nil, nil, nil, "init", "--bare", "--template=", "."); err != nil {
		return nil, err
	}
	if err := w.handle.Guard(func() error { return r.stage.Mkdir("ignore-worktree", 0700) }); err != nil {
		return nil, err
	}
	w.ignores[name] = r
	return r, nil
}

func repoRelative(name string, repo *liveRepository) string {
	if repo.value.Path == "." {
		return name
	}
	if name == repo.value.Path {
		return "."
	}
	return strings.TrimPrefix(name, repo.value.Path+"/")
}

func (w *captureWalk) mirrorIgnore(root *os.Root, name string, repo *liveRepository) error {
	data, err := readSmall(root, ".gitignore", 1<<20)
	if errors.Is(err, os.ErrNotExist) {
		if _, found := repo.ignoreHashes[name]; found {
			return ErrChanged
		}
		return nil
	}
	if err != nil {
		return err
	}
	hash := sha256.Sum256(data)
	value := hex.EncodeToString(hash[:])
	if previous, found := repo.ignoreHashes[name]; found {
		if previous != value {
			return ErrChanged
		}
		return nil
	}
	if w.verify {
		return ErrChanged
	}
	if int64(len(data)) > w.engine.config.Limits.MaxManifestBytes-w.metadata {
		return ErrLimit
	}
	if err := writePrivate(w.handle, repo.stage, path.Join("ignore-worktree", repoRelative(name, repo), ".gitignore"), data); err != nil {
		return err
	}
	repo.ignoreHashes[name] = value
	w.metadata += int64(len(data))
	return nil
}

func (w *captureWalk) ignored(root *os.Root, name string, repo *liveRepository, entries []os.DirEntry) (map[string]bool, error) {
	if err := w.mirrorIgnore(root, name, repo); err != nil {
		return nil, err
	}
	var paths strings.Builder
	for _, entry := range entries {
		value := repoRelative(path.Join(name, entry.Name()), repo)
		if entry.IsDir() {
			value += "/"
		}
		if int64(paths.Len())+int64(len(value))+1 > w.engine.config.Limits.MaxManifestBytes {
			return nil, ErrLimit
		}
		paths.WriteString(value)
		paths.WriteByte(0)
	}
	output, err := w.engine.git(w.ctx, w.handle, repo.stagePath, strings.NewReader(paths.String()), nil, nil, "--git-dir="+repo.stagePath, "--work-tree="+repo.stagePath+"/ignore-worktree", "check-ignore", "--no-index", "-z", "--stdin")
	if err != nil {
		var exit interface{ ExitCode() int }
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return map[string]bool{}, nil
		}
		return nil, err
	}
	ignored := map[string]bool{}
	for _, value := range strings.Split(string(output), "\x00") {
		if value != "" {
			ignored[strings.TrimSuffix(value, "/")] = true
		}
	}
	return ignored, nil
}

func (w *captureWalk) directory(root *os.Root, name string, repo *liveRepository) error {
	if err := w.handle.Check(); err != nil {
		return err
	}
	if info, err := root.Lstat(".git"); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return ErrGitUnsupported
		}
		if previous := w.repoPaths[name]; previous != nil {
			repo = previous
		} else if w.verify {
			return ErrChanged
		} else {
			if len(w.repos) >= w.engine.config.Limits.MaxRepositories {
				return ErrLimit
			}
			created, err := w.engine.prepareRepository(w.ctx, w.handle, w.stage, name, len(w.repos), root)
			if err != nil {
				return err
			}
			w.repos, w.repoPaths[name], repo = append(w.repos, created), created, created
			w.objects += created.objectBytes
			w.indexes += len(created.value.Index) + len(created.value.Refs)
			metadata, err := json.Marshal(created.value)
			if err != nil {
				return err
			}
			w.manifestBytes += int64(len(metadata))
			if w.objects > w.engine.config.Limits.MaxGitBytes || w.indexes > w.engine.config.Limits.MaxEntries || w.manifestBytes > w.engine.config.Limits.MaxManifestBytes {
				return ErrLimit
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else if w.verify && w.repoPaths[name] != nil {
		return ErrChanged
	}
	if repo == nil {
		var err error
		repo, err = w.prepareIgnore(name)
		if err != nil {
			return err
		}
	}
	info, err := root.Stat(".")
	if err != nil {
		return err
	}
	if name != "." {
		if err := w.append(Entry{Path: name, Kind: "directory", Mode: int64(info.Mode().Perm())}); err != nil {
			return err
		}
	}
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(w.engine.config.Limits.MaxScanEntries + 1)
	_ = dir.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(entries) > w.engine.config.Limits.MaxScanEntries-w.scanned {
		return ErrLimit
	}
	w.scanned += len(entries)
	slices.SortFunc(entries, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	ignored, err := w.ignored(root, name, repo, entries)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := w.handle.Check(); err != nil {
			return err
		}
		full := path.Join(name, entry.Name())
		if !safePath(full) {
			return ErrUnsafe
		}
		if reason := mandatory(full, w.engine.config.PrivatePaths); reason != "" {
			if err := w.exclude(Exclusion{Path: full, Reason: reason}); err != nil {
				return err
			}
			continue
		}
		rel := repoRelative(full, repo)
		if ignored[rel] && !repo.tracked[rel] && !repo.ancestors[rel] && !ignoredAncestor(full, w.spec.IncludeIgnored) {
			if err := w.exclude(Exclusion{Path: full, Reason: "ignored"}); err != nil {
				return err
			}
			continue
		}
		fileInfo, err := root.Lstat(entry.Name())
		if err != nil {
			return err
		}
		if fileInfo.IsDir() {
			child, err := root.OpenRoot(entry.Name())
			if err != nil {
				return err
			}
			actual, err := child.Stat(".")
			if err == nil && !os.SameFile(fileInfo, actual) {
				err = ErrChanged
			}
			if err == nil {
				err = w.directory(child, full, repo)
			}
			_ = child.Close()
			if err != nil {
				return err
			}
			continue
		}
		value := Entry{Path: full, Mode: int64(fileInfo.Mode().Perm())}
		switch {
		case fileInfo.Mode()&os.ModeSymlink != 0:
			value.Kind, value.Mode = "symlink", 0777
			value.Link, err = root.Readlink(entry.Name())
			if err != nil || !safeLink(full, value.Link) {
				return ErrUnsafe
			}
		case fileInfo.Mode().IsRegular():
			value.Kind, value.Size = "file", fileInfo.Size()
			if value.Size < 0 || value.Size > w.engine.config.Limits.MaxFileBytes || value.Size > w.engine.config.Limits.MaxBytes-w.bytes {
				return ErrLimit
			}
			file, err := root.OpenFile(entry.Name(), os.O_RDONLY|nofollow, 0)
			if err != nil {
				return err
			}
			before, err := file.Stat()
			if err == nil && !os.SameFile(fileInfo, before) {
				err = ErrChanged
			}
			hash := sha256.New()
			var size int64
			if err == nil {
				size, err = io.Copy(hash, io.LimitReader(&fencedReader{handle: w.handle, reader: file}, fileInfo.Size()+1))
			}
			after, statErr := file.Stat()
			err = errors.Join(err, statErr, file.Close())
			if err != nil {
				return err
			}
			if size != fileInfo.Size() || !sameFileState(before, after) {
				return ErrChanged
			}
			value.SHA256 = hex.EncodeToString(hash.Sum(nil))
			w.bytes += size
		default:
			return ErrUnsafe
		}
		if err := w.append(value); err != nil {
			return err
		}
	}
	return nil
}

func (w *captureWalk) append(entry Entry) error {
	if len(w.files) >= w.engine.config.Limits.MaxEntries {
		return ErrLimit
	}
	metadata, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	w.manifestBytes += int64(len(metadata))
	if w.manifestBytes > w.engine.config.Limits.MaxManifestBytes {
		return ErrLimit
	}
	w.files = append(w.files, entry)
	return nil
}

func (w *captureWalk) exclude(entry Exclusion) error {
	metadata, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	w.manifestBytes += int64(len(metadata))
	if len(w.excluded) >= w.engine.config.Limits.MaxEntries || w.manifestBytes > w.engine.config.Limits.MaxManifestBytes {
		return ErrLimit
	}
	w.excluded = append(w.excluded, entry)
	return nil
}

type fencedReader struct {
	handle *workspacejob.Handle
	reader io.Reader
}

func (r *fencedReader) Read(p []byte) (int, error) {
	if err := r.handle.Check(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if fence := r.handle.Check(); fence != nil {
		return 0, fence
	}
	return n, err
}
