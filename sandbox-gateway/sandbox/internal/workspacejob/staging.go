package workspacejob

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Identity exposes operation metadata to trusted in-process executors. It is
// not a browser DTO or an ownership capability.
type Identity struct {
	ID         string
	Resource   string
	Lineage    string
	Generation uint64
}

// SeparateSource rejects capturing the control journal/staging tree through
// an ancestor workspace. Startup supplies both roots; no HTTP path is used.
func (h *Handle) SeparateSource(source string) error {
	control, err := filepath.EvalSymlinks(h.service.config.Root)
	if err != nil {
		return err
	}
	control, err = filepath.Abs(control)
	if err != nil {
		return err
	}
	source, err = filepath.EvalSymlinks(source)
	if err != nil {
		return err
	}
	for _, pair := range [][2]string{{source, control}, {control, source}} {
		rel, err := filepath.Rel(pair[0], pair[1])
		if err != nil || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return ErrInvalid
		}
	}
	return h.Check()
}

func (h *Handle) Identity() Identity {
	return Identity{ID: h.id, Resource: h.service.config.Resource, Lineage: h.service.config.Lineage, Generation: h.generation}
}

// Guard confines an individual trusted filesystem operation to this live
// generation. A successor waits for the executor to join; a slow syscall can
// never be acknowledged as successful after the independent expiry watchdog.
func (h *Handle) Guard(operation func() error) error {
	h.service.mu.Lock()
	defer h.service.mu.Unlock()
	if err := h.Check(); err != nil {
		return err
	}
	if err := operation(); err != nil {
		return err
	}
	return h.Check()
}

// OpenStage returns only this job's retained private stage. The trusted
// collector must fence its writes; HTTP can never choose a staging root.
func (h *Handle) OpenStage() (root *os.Root, err error) {
	err = h.Guard(func() error {
		name := "data/" + h.id
		if err := h.service.root.Mkdir(name, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		root, err = h.service.root.OpenRoot(name)
		if err != nil {
			return err
		}
		parent, err := h.service.root.Open("data")
		if err != nil {
			return err
		}
		return errors.Join(parent.Sync(), parent.Close())
	})
	if err != nil && root != nil {
		_ = root.Close()
		root = nil
	}
	return root, err
}
