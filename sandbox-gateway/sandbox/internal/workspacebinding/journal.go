package workspacebinding

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const maxJournalBytes = 16 << 20

type retained struct {
	Head   Head   `json:"head"`
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}
type checkpoint struct {
	Version      int                  `json:"version"`
	Resource     string               `json:"resource"`
	Lineage      string               `json:"lineage"`
	OriginalRoot string               `json:"originalRoot"`
	RestoreRoot  string               `json:"restoreRoot"`
	HeadID       string               `json:"headId"`
	PendingID    string               `json:"pendingId,omitempty"`
	Bindings     map[string]retained  `json:"bindings"`
	Operations   map[string]Operation `json:"operations"`
}
type envelope struct {
	SHA256 string          `json:"sha256"`
	Body   json.RawMessage `json:"body"`
}

func writeCheckpoint(root *os.Root, value checkpoint) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(body) > maxJournalBytes {
		return workspaceLimit()
	}
	hash := sha256.Sum256(body)
	data, err := json.Marshal(envelope{hex.EncodeToString(hash[:]), body})
	if err != nil {
		return err
	}
	name := ".pending-" + rand.Text()
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(name) }()
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	if err = root.Rename(name, "head.json"); err != nil {
		return err
	}
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

func readCheckpoint(root *os.Root) (checkpoint, error) {
	var value checkpoint
	info, err := root.Lstat("head.json")
	if err != nil {
		return value, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxJournalBytes*2 {
		return value, ErrCorrupt
	}
	f, err := root.OpenFile("head.json", os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return value, err
	}
	defer func() { _ = f.Close() }()
	actual, err := f.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return value, ErrCorrupt
	}
	data, err := io.ReadAll(io.LimitReader(f, maxJournalBytes*2+1))
	if err != nil {
		return value, err
	}
	var wrapped envelope
	if len(data) > maxJournalBytes*2 || json.Unmarshal(data, &wrapped) != nil {
		return value, ErrCorrupt
	}
	hash := sha256.Sum256(wrapped.Body)
	if wrapped.SHA256 != hex.EncodeToString(hash[:]) || json.Unmarshal(wrapped.Body, &value) != nil {
		return value, ErrCorrupt
	}
	return value, nil
}

func realDirectory(name string, private bool) (string, uint64, uint64, error) {
	if !filepath.IsAbs(name) || strings.ContainsRune(name, 0) {
		return "", 0, 0, ErrInvalid
	}
	clean := filepath.Clean(name)
	actual, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", 0, 0, err
	}
	if actual != clean {
		return "", 0, 0, ErrInvalid
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return "", 0, 0, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || (private && info.Mode().Perm()&0077 != 0) {
		return "", 0, 0, ErrInvalid
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", 0, 0, ErrInvalid
	}
	return clean, uint64(st.Dev), st.Ino, nil
}

func overlap(a, b string) bool {
	for _, pair := range [][2]string{{a, b}, {b, a}} {
		rel, err := filepath.Rel(pair[0], pair[1])
		if err != nil || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return true
		}
	}
	return false
}

func cloneCheckpoint(value checkpoint) checkpoint {
	copy := value
	copy.Bindings = make(map[string]retained, len(value.Bindings))
	for id, row := range value.Bindings {
		copy.Bindings[id] = row
	}
	copy.Operations = make(map[string]Operation, len(value.Operations))
	for id, row := range value.Operations {
		copy.Operations[id] = copyOperation(row)
	}
	return copy
}

func copyOperation(op Operation) Operation {
	if op.Result != nil {
		result := *op.Result
		result.Consumers = append([]string(nil), result.Consumers...)
		op.Result = &result
	}
	return op
}

// StartupOriginalRoot recovers startup-owned configuration before collectors
// or Agents are opened. Reprime may have persisted the confirmed active root
// as WORKSPACE_DIR; it cannot redefine the retained original root. New takes
// the exclusive lock and validates the full checkpoint again before use.
func StartupOriginalRoot(control, restore, configured, resource, lineage string) (string, error) {
	if !filepath.IsAbs(control) || !filepath.IsAbs(restore) || !filepath.IsAbs(configured) || !idPattern.MatchString(resource) || !idPattern.MatchString(lineage) {
		return "", ErrInvalid
	}
	_, _, _, err := realDirectory(control, true)
	if errors.Is(err, os.ErrNotExist) {
		return configured, nil
	}
	if err != nil {
		return "", err
	}
	root, err := os.OpenRoot(control)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	state, err := readCheckpoint(root)
	if errors.Is(err, os.ErrNotExist) {
		return configured, nil
	}
	if err != nil {
		return "", err
	}
	row, ok := state.Bindings[state.HeadID]
	if !ok || state.Version != Version || state.Resource != resource || state.Lineage != lineage || state.RestoreRoot != filepath.Clean(restore) || row.Head.Reference.Fingerprint != fingerprint(row.Head) {
		return "", ErrCorrupt
	}
	active := state.OriginalRoot
	if row.Head.Target.Kind == "restore" && idPattern.MatchString(row.Head.Target.ID) {
		active = filepath.Join(restore, row.Head.Target.ID)
	} else if row.Head.Target.Kind != "original" || row.Head.Target.ID != "" {
		return "", ErrCorrupt
	}
	if filepath.Clean(configured) != state.OriginalRoot && filepath.Clean(configured) != active {
		return "", ErrConflict
	}
	if _, _, _, err := realDirectory(state.OriginalRoot, false); err != nil {
		return "", err
	}
	return state.OriginalRoot, nil
}
