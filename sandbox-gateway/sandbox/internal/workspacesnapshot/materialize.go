package workspacesnapshot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"slices"
	"strings"

	"backend/internal/workspacejob"
)

type RestoreReceipt struct {
	Version        int    `json:"version"`
	Directory      string `json:"directory"`
	ArchiveSHA256  string `json:"archiveSha256"`
	ManifestSHA256 string `json:"manifestSha256"`
	Files          int    `json:"files"`
	Bytes          int64  `json:"bytes"`
	Repositories   int    `json:"repositories"`
}

// Materialize verifies the complete immutable artifact before creating an
// exclusive new target. It never overlays a current workspace or removes a
// partial target. Callers grant the parent root and choose a fresh job name;
// neither is an arbitrary browser host path.
func (e *Engine) Materialize(ctx context.Context, h *workspacejob.Handle, parent *os.Root, name string, archive io.ReadSeeker, data []byte, receipt Receipt) (RestoreReceipt, error) {
	if parent == nil || !safePath(name) || name == "." || strings.Contains(name, "/") || len(name) > 128 {
		return RestoreReceipt{}, ErrUnsafe
	}
	manifest, err := e.validateManifest(data, receipt)
	if err != nil {
		return RestoreReceipt{}, err
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return RestoreReceipt{}, err
	}
	if err := inspectArchive(h, archive, receipt, manifest.Archive, nil); err != nil {
		return RestoreReceipt{}, err
	}
	if err := h.Guard(func() error { return parent.Mkdir(name, 0700) }); err != nil {
		return RestoreReceipt{}, err
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		return RestoreReceipt{}, err
	}
	defer func() { _ = root.Close() }()
	stage, err := h.OpenStage()
	if err != nil {
		return RestoreReceipt{}, err
	}
	defer func() { _ = stage.Close() }()
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return RestoreReceipt{}, err
	}
	directories := map[string]int64{".": 0700}
	err = inspectArchive(h, archive, receipt, manifest.Archive, func(entry Entry, content io.Reader) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Path == "workspace" || entry.Path == "git" {
			return nil
		}
		if strings.HasPrefix(entry.Path, "git/") {
			return h.WriteFile("restore-inputs/"+entry.Path, content, max(entry.Size, 1))
		}
		if !strings.HasPrefix(entry.Path, "workspace/") {
			return ErrUnsafe
		}
		entry.Path = strings.TrimPrefix(entry.Path, "workspace/")
		if err := h.Guard(func() error { return root.MkdirAll(path.Dir(entry.Path), 0700) }); err != nil {
			return err
		}
		for parent := path.Dir(entry.Path); parent != "."; parent = path.Dir(parent) {
			if _, ok := directories[parent]; !ok {
				directories[parent] = 0700
			}
		}
		switch entry.Kind {
		case "directory":
			directories[entry.Path] = entry.Mode
			return h.Guard(func() error { return root.MkdirAll(entry.Path, 0700) })
		case "symlink":
			return h.Guard(func() error { return root.Symlink(entry.Link, entry.Path) })
		case "file":
			return restoreFile(h, root, entry, content)
		}
		return ErrUnsafe
	})
	if err != nil {
		return RestoreReceipt{}, err
	}
	for _, repo := range manifest.Repositories {
		if err := e.restoreGit(ctx, h, root, stage, repo); err != nil {
			return RestoreReceipt{}, err
		}
	}
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
		if err := syncDirectory(h, root, name, os.FileMode(directories[name])); err != nil {
			return RestoreReceipt{}, err
		}
	}
	if err := h.Guard(func() error {
		dir, err := parent.Open(".")
		if err != nil {
			return err
		}
		return errors.Join(dir.Sync(), dir.Close())
	}); err != nil {
		return RestoreReceipt{}, err
	}
	result := RestoreReceipt{Version: Version, Directory: name, ArchiveSHA256: receipt.Archive.SHA256, ManifestSHA256: receipt.Manifest.SHA256, Files: receipt.Files, Bytes: receipt.Bytes, Repositories: len(manifest.Repositories)}
	metadata, err := json.Marshal(result)
	if err != nil {
		return RestoreReceipt{}, err
	}
	if err := h.WriteFile("materialized.json", &byteReader{data: metadata}, int64(len(metadata))); err != nil {
		return RestoreReceipt{}, err
	}
	if err := syncDirectory(h, stage, ".", 0700); err != nil {
		return RestoreReceipt{}, err
	}
	return result, nil
}

func restoreFile(h *workspacejob.Handle, root *os.Root, entry Entry, content io.Reader) error {
	var file *os.File
	err := h.Guard(func() error {
		var err error
		file, err = root.OpenFile(entry.Path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|nofollow, 0600)
		return err
	})
	if err != nil {
		if file != nil {
			_ = file.Close()
		}
		return err
	}
	defer func() { _ = file.Close() }()
	buffer := make([]byte, 32<<10)
	for {
		if err := h.Check(); err != nil {
			return err
		}
		n, readErr := content.Read(buffer)
		if n > 0 {
			if err := h.Guard(func() error { _, err := file.Write(buffer[:n]); return err }); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	return h.Guard(func() error {
		if err := file.Chmod(os.FileMode(entry.Mode)); err != nil {
			return err
		}
		return file.Sync()
	})
}

func syncDirectory(h *workspacejob.Handle, root *os.Root, name string, mode os.FileMode) error {
	return h.Guard(func() error {
		file, err := root.OpenFile(name, os.O_RDONLY|nofollow, 0)
		if err != nil {
			return err
		}
		if err := file.Chmod(mode); err != nil {
			_ = file.Close()
			return err
		}
		return errors.Join(file.Sync(), file.Close())
	})
}
