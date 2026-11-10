package workspacesnapshot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"backend/internal/workspacejob"
)

func (e *Engine) restoreGit(ctx context.Context, h *workspacejob.Handle, target, stage *os.Root, repo Repository) error {
	root, err := target.OpenRoot(repo.Path)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	if _, err := e.git(ctx, h, descriptor(dir), nil, nil, nil, "init", "--template=", "--object-format="+repo.ObjectFormat, "."); err != nil {
		return err
	}
	if repo.Pack != "" {
		pack, err := stage.OpenFile("restore-inputs/"+repo.Pack, os.O_RDONLY|nofollow, 0)
		if err != nil {
			return err
		}
		_, err = e.git(ctx, h, descriptor(dir), &fencedReader{handle: h, reader: pack}, nil, nil, "index-pack", "--strict", "--stdin")
		err = errors.Join(err, pack.Close())
		if err != nil {
			return err
		}
	}
	git, err := root.OpenRoot(".git")
	if err != nil {
		return err
	}
	defer func() { _ = git.Close() }()
	var refs strings.Builder
	for _, ref := range repo.Refs {
		if ref.Target == "" {
			fmt.Fprintf(&refs, "create %s %s\n", ref.Name, ref.OID)
		}
	}
	if refs.Len() > 0 {
		if _, err := e.git(ctx, h, descriptor(dir), strings.NewReader(refs.String()), nil, nil, "update-ref", "--stdin"); err != nil {
			return err
		}
	}
	for _, ref := range repo.Refs {
		if ref.Target != "" {
			if _, err := e.git(ctx, h, descriptor(dir), nil, nil, nil, "symbolic-ref", "--", ref.Name, ref.Target); err != nil {
				return err
			}
		}
	}
	head := repo.HEAD + "\n"
	if repo.SymbolicHEAD != "" {
		head = "ref: " + repo.SymbolicHEAD + "\n"
	}
	// This HEAD belongs to the fresh repository just initialized above, not
	// an original or previously returned workspace. Replayed jobs never get
	// past the exclusive destination directory creation.
	if err := h.Guard(func() error {
		file, err := git.OpenFile("HEAD", os.O_WRONLY|os.O_TRUNC|nofollow, 0)
		if err != nil {
			return err
		}
		_, err = file.WriteString(head)
		if err == nil {
			err = file.Sync()
		}
		return errors.Join(err, file.Close())
	}); err != nil {
		return err
	}
	if len(repo.Index) > 0 {
		index, err := encodeIndex(repo.Index, repo.ObjectFormat, e.config.Limits.MaxManifestBytes)
		if err != nil {
			return err
		}
		if err := writePrivate(h, git, "index", index); err != nil {
			return err
		}
	}
	for _, value := range repo.Operation {
		if err := writePrivate(h, git, value.Name, value.Data); err != nil {
			return err
		}
	}
	if _, err := e.git(ctx, h, descriptor(dir), nil, nil, nil, "fsck", "--connectivity-only", "--strict", "--no-reflogs"); err != nil {
		return err
	}
	if repo.HEAD != "" {
		value, err := e.git(ctx, h, descriptor(dir), nil, nil, nil, "rev-parse", "--verify", "HEAD")
		if err != nil || strings.TrimSpace(string(value)) != repo.HEAD {
			return ErrIntegrity
		}
	}
	return syncGitTree(h, git, e.config.Limits.MaxEntries*2+64)
}

func syncGitTree(h *workspacejob.Handle, root *os.Root, limit int) error {
	count := 0
	var sync func(*os.Root) error
	sync = func(dir *os.Root) error {
		file, err := dir.Open(".")
		if err != nil {
			return err
		}
		entries, err := file.ReadDir(limit + 1)
		_ = file.Close()
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		count += len(entries)
		if count > limit {
			return ErrLimit
		}
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 {
				return ErrUnsafe
			}
			if entry.IsDir() {
				child, err := dir.OpenRoot(entry.Name())
				if err != nil {
					return err
				}
				err = sync(child)
				_ = child.Close()
				if err != nil {
					return err
				}
			} else if err := h.Guard(func() error {
				file, err := dir.OpenFile(entry.Name(), os.O_RDONLY|nofollow, 0)
				if err != nil {
					return err
				}
				return errors.Join(file.Sync(), file.Close())
			}); err != nil {
				return err
			}
		}
		return syncDirectory(h, dir, ".", 0700)
	}
	return sync(root)
}
