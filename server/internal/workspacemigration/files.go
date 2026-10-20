package workspacemigration

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type FileDigest struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
	Mode uint32 `json:"mode"`
}

func scanFiles(root string) ([]FileDigest, error) {
	files := []FileDigest{}
	if root == "" {
		return files, nil
	}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) && p == root {
			return nil
		}
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			// Do not follow links out of a profile into host configuration or
			// silently omit them from a supposedly complete migration backup.
			return fmt.Errorf("symlink requires explicit resolution before migration: %s", rel)
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported profile file: %s", rel)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		h := sha256.Sum256(b)
		files = append(files, FileDigest{Path: filepath.ToSlash(rel), Hash: hex.EncodeToString(h[:]), Mode: uint32(info.Mode().Perm())})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, err
}

func copyTree(src, dst string) error {
	if src == "" {
		return os.MkdirAll(dst, 0o700)
	}
	if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
		return os.MkdirAll(dst, 0o700)
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in migration source: %s", rel)
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			if err := os.MkdirAll(target, info.Mode().Perm()); err != nil {
				return err
			}
			return os.Chmod(target, info.Mode().Perm())
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, b, info.Mode().Perm()); err != nil {
			return err
		}
		// Creation modes are filtered by the process umask. Preserve the actual
		// source permissions so backups retain their fingerprint and executables
		// keep the same access after publish or rollback.
		return os.Chmod(target, info.Mode().Perm())
	})
}

func safeName(s string) bool {
	return s != "" && s != "." && s != ".." && filepath.Base(s) == s && !strings.ContainsAny(s, `/\\`)
}

func containsPath(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// CanonicalPath resolves symlink aliases even when the final directory has not
// been created yet. Comparing only lexical paths can put a backup inside itself.
func CanonicalPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	ancestor := absolute
	var tail []string
	for {
		resolved, err := filepath.EvalSymlinks(ancestor)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", err
		}
		tail = append(tail, filepath.Base(ancestor))
		ancestor = parent
	}
}

func writeJSON(path string, value any) error {
	b, err := jsonBytes(value)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(b)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
