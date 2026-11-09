package workspacejob

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"syscall"
)

type checkpoint struct {
	Version    int    `json:"version"`
	Resource   string `json:"resource"`
	Lineage    string `json:"lineage"`
	Generation uint64 `json:"generation"`
}
type record struct {
	Spec      Spec    `json:"spec"`
	OwnerHash string  `json:"ownerHash"`
	Receipt   Receipt `json:"receipt"`
}
type envelope struct {
	SHA256 string          `json:"sha256"`
	Body   json.RawMessage `json:"body"`
}

func writeRecord(root *os.Root, name string, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(body)
	data, err := json.Marshal(envelope{SHA256: hex.EncodeToString(hash[:]), Body: body})
	if err != nil {
		return err
	}
	temp := ".pending-" + rand.Text()
	f, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(temp) }()
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err := root.Rename(temp, name); err != nil {
		return err
	}
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

func readRecord(root *os.Root, name string, value any) error {
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 256<<10 {
		return ErrCorrupt
	}
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, (256<<10)+1))
	if err != nil {
		return err
	}
	if len(data) > 256<<10 {
		return ErrCorrupt
	}
	var wrapped envelope
	if json.Unmarshal(data, &wrapped) != nil {
		return ErrCorrupt
	}
	hash := sha256.Sum256(wrapped.Body)
	if wrapped.SHA256 != hex.EncodeToString(hash[:]) || json.Unmarshal(wrapped.Body, value) != nil {
		return ErrCorrupt
	}
	return nil
}

func openJournal(cfg Config) (*os.Root, *os.File, error) {
	if err := os.MkdirAll(cfg.Root, 0700); err != nil {
		return nil, nil, err
	}
	root, err := os.OpenRoot(cfg.Root)
	if err != nil {
		return nil, nil, err
	}
	f, err := root.OpenFile(".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err == nil {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	}
	if err != nil {
		if f != nil {
			_ = f.Close()
		}
		_ = root.Close()
		return nil, nil, ErrInUse
	}
	return root, f, nil
}
