package workspacearchive

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"slices"
	"strings"
	"unicode/utf8"
)

var ErrIntegrity = errors.New("workspace archive differs from its verified manifest")

func sameManifest(a, b Manifest) bool {
	return a.Version == Version && b.Version == Version && a.Bytes == b.Bytes && slices.Equal(a.Entries, b.Entries)
}

// Restore creates a new private directory beneath an authorized parent. It
// never overwrites an existing target or imports host paths. A failed restore
// removes only the newly created target; the original workspace stays intact.
// Archive and manifest must first be resolved from an authorized snapshot row.
func Restore(ctx context.Context, archive io.ReadSeeker, parent *os.Root, name string, expected Manifest, limits Limits) (retErr error) {
	if parent == nil || name == "" || name == "." || name == ".." || len(name) > 255 || !utf8.ValidString(name) || strings.ContainsAny(name, "/\\\x00") {
		return ErrUnsafe
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return err
	}
	verified, err := Inspect(ctx, archive, limits)
	if err != nil {
		return err
	}
	if !sameManifest(verified, expected) {
		return ErrIntegrity
	}
	if err := parent.Mkdir(name, 0o700); err != nil {
		return err // includes EEXIST: never replace even an empty target
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		return errors.Join(err, parent.RemoveAll(name))
	}
	defer func() {
		retErr = errors.Join(retErr, root.Close())
		if retErr != nil {
			retErr = errors.Join(retErr, parent.RemoveAll(name))
		}
	}()
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return err
	}
	directories := map[string]int64{".": 0o700}
	actual, err := scan(ctx, archive, limits, true, func(entry Entry, content io.Reader) error {
		for dir := path.Dir(entry.Path); ; dir = path.Dir(dir) {
			if _, exists := directories[dir]; !exists {
				directories[dir] = 0o700
			}
			if dir == "." {
				break
			}
		}
		if err := root.MkdirAll(path.Dir(entry.Path), 0o700); err != nil {
			return err
		}
		switch entry.Kind {
		case "directory":
			directories[entry.Path] = entry.Mode
			return root.MkdirAll(entry.Path, 0o700)
		case "file":
			return restoreFile(root, entry, content)
		case "symlink":
			return root.Symlink(entry.Link, entry.Path)
		case "hardlink":
			return root.Link(entry.Link, entry.Path)
		}
		return ErrUnsafe
	})
	if err != nil {
		return err
	}
	if !sameManifest(actual, expected) {
		return ErrIntegrity
	}
	// Sync children before parents and apply restrictive directory modes last.
	// File descriptor Chmod avoids a path-to-symlink race during permission changes.
	names := make([]string, 0, len(directories))
	for name := range directories {
		names = append(names, name)
	}
	slices.SortFunc(names, func(a, b string) int {
		if a == b {
			return 0
		}
		if a == "." {
			return 1
		}
		if b == "." {
			return -1
		}
		if depth := strings.Count(b, "/") - strings.Count(a, "/"); depth != 0 {
			return depth
		}
		return strings.Compare(a, b)
	})
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		mode := os.FileMode(directories[name])
		if err := syncDirectory(root, name, &mode); err != nil {
			return err
		}
	}
	return syncDirectory(parent, ".", nil)
}

func restoreFile(root *os.Root, entry Entry, content io.Reader) error {
	f, err := root.OpenFile(entry.Path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err = io.Copy(f, content); err == nil {
		err = f.Chmod(os.FileMode(entry.Mode))
	}
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}

func syncDirectory(root *os.Root, name string, mode *os.FileMode) error {
	dir, err := root.Open(name)
	if err != nil {
		return err
	}
	if mode != nil {
		err = dir.Chmod(*mode)
	}
	if err == nil {
		err = dir.Sync()
	}
	return errors.Join(err, dir.Close())
}
