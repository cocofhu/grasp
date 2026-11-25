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
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("ssh gateway: host key dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		// Another replica created it first; serve under that key.
		return readHostKey(path)
	}
	if err != nil {
		return nil, fmt.Errorf("ssh gateway: create host key: %w", err)
	}
	if err := pem.Encode(f, block); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("ssh gateway: write host key: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("ssh gateway: write host key: %w", err)
	}
	return ssh.NewSignerFromKey(priv)
}

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
