package sshgateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

// loadOrCreateHostKey reads the gateway host key from path, creating an
// ed25519 key there on first start. Every sandbox is served under this one
// key, so it must live on persistent storage shared by all replicas.
func loadOrCreateHostKey(path string) (ssh.Signer, error) {
	if path == "" {
		return nil, errors.New("ssh gateway: host_key_path is required")
	}
	if signer, err := readHostKey(path); err == nil || !errors.Is(err, fs.ErrNotExist) {
		return signer, err
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	block, err := ssh.MarshalPrivateKey(priv, "grasp-ssh-gateway")
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("ssh gateway: host key dir: %w", err)
	}
	tmp, err := writeTempKey(dir, pem.EncodeToMemory(block))
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(tmp) }()
	// Link publishes the complete file atomically and fails if another
	// replica published first, in which case that key wins.
	if err := linkFile(tmp, path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return readHostKey(path)
		}
		// Some shared volumes have no hard links. Rename still publishes a
		// complete file; only the first-writer-wins guarantee is weaker.
		if _, statErr := os.Stat(path); statErr == nil {
			return readHostKey(path)
		}
		if rerr := os.Rename(tmp, path); rerr != nil {
			return nil, fmt.Errorf("ssh gateway: publish host key: %w", errors.Join(err, rerr))
		}
		return readHostKey(path)
	}
	return ssh.NewSignerFromKey(priv)
}

// writeTempKey writes data to a synced 0600 file in dir and returns its path.
func writeTempKey(dir string, data []byte) (string, error) {
	f, err := os.CreateTemp(dir, ".host_key-*")
	if err != nil {
		return "", fmt.Errorf("ssh gateway: create host key: %w", err)
	}
	name := f.Name()
	if err = f.Chmod(0o600); err == nil {
		if _, err = f.Write(data); err == nil {
			err = f.Sync()
		}
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(name)
		return "", fmt.Errorf("ssh gateway: write host key: %w", err)
	}
	return name, nil
}

// linkFile is os.Link; tests replace it to simulate volumes without hard links.
var linkFile = os.Link

func readHostKey(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("ssh gateway: parse host key %s: %w", path, err)
	}
	return signer, nil
}
