package sandbox

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/ssh"
)

// dataPlaneSSHKeyFileName is the ed25519 private key shared by every sandbox.
// It lives next to secrets.key when that file's directory rule yields a path,
// and in the server working directory otherwise.
const dataPlaneSSHKeyFileName = "sandbox_ssh_ed25519"

// Process-wide data-plane key. The file path must be set before the first use.
// An empty path keeps the historical in-memory key so unit tests do not write
// a key into the server working directory.
var (
	sshKeyOnce   sync.Once
	sshKeyMu     sync.Mutex
	sshKeyPath   string
	sshKeyFrozen bool
	sshSigner    ssh.Signer
	sshAuthKey   string
	sshKeyErr    error
)

// SetDataPlaneKeyFile selects the private key file used by later dials.
// A call after the key has already been loaded is ignored, so a damaged file
// cannot be swapped for a newly generated key in the same process.
func SetDataPlaneKeyFile(path string) {
	sshKeyMu.Lock()
	defer sshKeyMu.Unlock()
	if sshKeyFrozen {
		return
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	sshKeyPath = path
}

// DataPlaneKeyPath is the private key file for a secrets.key path.
// An empty secretsKeyFile means the secrets.key directory rule produced nothing,
// so the key is stored in the server working directory.
func DataPlaneKeyPath(secretsKeyFile string) string {
	secretsKeyFile = strings.TrimSpace(secretsKeyFile)
	if secretsKeyFile != "" {
		return filepath.Join(filepath.Dir(secretsKeyFile), dataPlaneSSHKeyFileName)
	}
	wd, err := os.Getwd()
	if err != nil || strings.TrimSpace(wd) == "" {
		wd = "."
	}
	return filepath.Join(wd, dataPlaneSSHKeyFileName)
}

// sharedSSHKey returns the process-wide signer and authorized_keys line.
// The private key is loaded from disk when a path was configured; otherwise
// one ephemeral key is generated for the process. A file that exists but cannot
// be parsed is left in place and returned as an error.
func sharedSSHKey() (ssh.Signer, string, error) {
	sshKeyOnce.Do(func() {
		sshKeyMu.Lock()
		path := sshKeyPath
		sshKeyFrozen = true
		sshKeyMu.Unlock()
		if path == "" {
			sshSigner, sshAuthKey, sshKeyErr = generateSSHKey()
			if sshKeyErr != nil {
				log.Error().Msg("data-plane ssh key generation failed")
			}
			return
		}
		sshSigner, sshAuthKey, sshKeyErr = loadOrCreateSSHKeyFile(path)
	})
	return sshSigner, sshAuthKey, sshKeyErr
}

// generateSSHKey mints an ed25519 keypair. The signer is the private half;
// the string is the authorized_keys line injected via SSH_KEY.
func generateSSHKey() (ssh.Signer, string, error) {
	signer, pub, _, err := generateSSHKeyPEM()
	return signer, pub, err
}

func generateSSHKeyPEM() (ssh.Signer, string, []byte, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", nil, fmt.Errorf("generate ed25519 key")
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, "", nil, fmt.Errorf("new signer")
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return nil, "", nil, fmt.Errorf("marshal data-plane ssh key")
	}
	authorized := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if authorized == "" {
		return nil, "", nil, fmt.Errorf("empty data-plane public key")
	}
	return signer, authorized, pem.EncodeToMemory(block), nil
}

func parseSSHPrivateKey(b []byte) (ssh.Signer, string, error) {
	signer, err := ssh.ParsePrivateKey(b)
	if err != nil {
		return nil, "", err
	}
	authorized := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if authorized == "" {
		return nil, "", fmt.Errorf("empty data-plane public key")
	}
	return signer, authorized, nil
}

// loadOrCreateSSHKeyFile reads an existing ed25519 private key or creates one
// with mode 0600. An existing file that cannot be parsed is not replaced.
func loadOrCreateSSHKeyFile(path string) (ssh.Signer, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, "", fmt.Errorf("data-plane ssh key path is empty")
	}
	b, err := os.ReadFile(path)
	if err == nil {
		signer, pub, perr := parseSSHPrivateKey(b)
		if perr != nil {
			log.Error().Str("path", path).Msg("data-plane ssh key file is unreadable; not replacing it")
			return nil, "", fmt.Errorf("data-plane ssh key file is invalid")
		}
		if info, statErr := os.Stat(path); statErr == nil && info.Mode().Perm() != 0o600 {
			if chErr := os.Chmod(path, 0o600); chErr != nil {
				log.Warn().Str("path", path).Msg("data-plane ssh key file is not mode 0600")
			}
		}
		log.Info().Str("path", path).Msg("loaded data-plane ssh key")
		return signer, pub, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		log.Error().Str("path", path).Msg("data-plane ssh key file could not be read")
		return nil, "", fmt.Errorf("read data-plane ssh key file")
	}
	signer, pub, pemBytes, gerr := generateSSHKeyPEM()
	if gerr != nil {
		return nil, "", gerr
	}
	if werr := writeNewSSHKeyFile(path, pemBytes); werr != nil {
		if errors.Is(werr, os.ErrExist) {
			return loadOrCreateSSHKeyFile(path)
		}
		log.Error().Str("path", path).Msg("data-plane ssh key file could not be created")
		return nil, "", fmt.Errorf("write data-plane ssh key file")
	}
	log.Info().Str("path", path).Msg("created data-plane ssh key")
	return signer, pub, nil
}

func writeNewSSHKeyFile(path string, pemBytes []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, werr := f.Write(pemBytes); werr != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return werr
	}
	if cerr := f.Close(); cerr != nil {
		_ = os.Remove(path)
		return cerr
	}
	return nil
}
