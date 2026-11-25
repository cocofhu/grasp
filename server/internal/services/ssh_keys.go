package services

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sshgateway"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
)

// SSHKeyError is a rejected key submission; its message is shown to the user.
type SSHKeyError struct{ Msg string }

func (e *SSHKeyError) Error() string { return e.Msg }

var (
	ErrSSHKeyInvalid  = &SSHKeyError{"无法识别的 SSH 公钥，请粘贴 ~/.ssh/*.pub 中的一整行"}
	ErrSSHKeyLimit    = &SSHKeyError{"每个账号最多登记 20 个公钥"}
	ErrSSHKeyExists   = errors.New("该公钥已经登记过")
	ErrSSHKeyNotFound = errors.New("公钥不存在")
)

const (
	maxSSHKeysPerUser  = 20
	maxSSHKeyNameRunes = 100
	sshKeyTouchEvery   = time.Minute
)

// allowedSSHKeyTypes excludes DSA, which OpenSSH itself has dropped.
var allowedSSHKeyTypes = map[string]bool{
	ssh.KeyAlgoED25519:    true,
	ssh.KeyAlgoSKED25519:  true,
	ssh.KeyAlgoECDSA256:   true,
	ssh.KeyAlgoECDSA384:   true,
	ssh.KeyAlgoECDSA521:   true,
	ssh.KeyAlgoSKECDSA256: true,
	ssh.KeyAlgoRSA:        true,
}

// SSHKeyService stores the public keys users register for the SSH gateway
// and authorizes gateway logins with them.
type SSHKeyService struct {
	db     *gorm.DB
	cfg    func() *config.Config
	now    func() time.Time
	mu     sync.Mutex
	revoke func(fingerprint string)
}

func NewSSHKeyService(db *gorm.DB, cfg func() *config.Config) *SSHKeyService {
	return &SSHKeyService{db: db, cfg: cfg, now: time.Now}
}

// SetRevokeHook is called with a key's fingerprint after it is deleted so
// open gateway connections using it can be closed.
func (s *SSHKeyService) SetRevokeHook(fn func(fingerprint string)) {
	s.mu.Lock()
	s.revoke = fn
	s.mu.Unlock()
}

func (s *SSHKeyService) List(username string) ([]models.UserSSHKey, error) {
	keys := []models.UserSSHKey{}
	err := s.db.Where("username = ?", username).Order("created_at asc, id asc").Find(&keys).Error
	return keys, err
}

func (s *SSHKeyService) HasKey(username string) bool {
	var n int64
	return s.db.Model(&models.UserSSHKey{}).Where("username = ?", username).Count(&n).Error == nil && n > 0
}

// ParseSSHPublicKey validates one authorized_keys line and returns the key
// and its comment.
func ParseSSHPublicKey(line string) (ssh.PublicKey, string, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.ContainsAny(line, "\r\n") {
		return nil, "", ErrSSHKeyInvalid
	}
	key, comment, options, rest, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil || len(options) > 0 || len(rest) > 0 {
		return nil, "", ErrSSHKeyInvalid
	}
	if !allowedSSHKeyTypes[key.Type()] {
		return nil, "", &SSHKeyError{fmt.Sprintf("不支持 %s 类型的公钥，请使用 ed25519、ECDSA 或 RSA", key.Type())}
	}
	if crypto, ok := key.(ssh.CryptoPublicKey); ok {
		if rsaKey, ok := crypto.CryptoPublicKey().(*rsa.PublicKey); ok && rsaKey.N.BitLen() < 2048 {
			return nil, "", &SSHKeyError{"RSA 公钥至少需要 2048 位"}
		}
	}
	return key, strings.TrimSpace(comment), nil
}

// Add registers publicKey for username. An empty name uses the key comment.
func (s *SSHKeyService) Add(username, name, publicKey string) (*models.UserSSHKey, error) {
	key, comment, err := ParseSSHPublicKey(publicKey)
	if err != nil {
		return nil, err
	}
	fingerprint := ssh.FingerprintSHA256(key)
	name = strings.TrimSpace(name)
	if name == "" {
		name = comment
	}
	if name == "" {
		name = key.Type()
	}
	if utf8.RuneCountInString(name) > maxSSHKeyNameRunes {
		name = string([]rune(name)[:maxSSHKeyNameRunes])
	}
	row := &models.UserSSHKey{
		Username: username, Name: name, Fingerprint: fingerprint,
		PublicKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))), CreatedAt: s.now(),
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&models.UserSSHKey{}).Where("fingerprint = ?", fingerprint).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return ErrSSHKeyExists
		}
		if err := tx.Model(&models.UserSSHKey{}).Where("username = ?", username).Count(&n).Error; err != nil {
			return err
		}
		if n >= maxSSHKeysPerUser {
			return ErrSSHKeyLimit
		}
		return tx.Create(row).Error
	})
	if err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, ErrSSHKeyExists
		}
		return nil, err
	}
	return row, nil
}

// Delete removes one of username's keys and disconnects sessions using it.
func (s *SSHKeyService) Delete(username string, id uint) error {
	var row models.UserSSHKey
	if err := s.db.Where("id = ? AND username = ?", id, username).First(&row).Error; err != nil {
		return ErrSSHKeyNotFound
	}
	if err := s.db.Delete(&models.UserSSHKey{}, row.ID).Error; err != nil {
		return err
	}
	s.mu.Lock()
	revoke := s.revoke
	s.mu.Unlock()
	if revoke != nil {
		revoke(row.Fingerprint)
	}
	return nil
}

// AuthorizeKey implements sshgateway.Authorizer. The owning account must
// still exist in auth.users (and be an admin when admins_only is set).
func (s *SSHKeyService) AuthorizeKey(key ssh.PublicKey) (sshgateway.Identity, error) {
	fingerprint := ssh.FingerprintSHA256(key)
	var row models.UserSSHKey
	if err := s.db.Where("fingerprint = ?", fingerprint).First(&row).Error; err != nil {
		return sshgateway.Identity{}, errors.New("key not registered")
	}
	if row.PublicKey != strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) {
		return sshgateway.Identity{}, errors.New("key not registered")
	}
	user, ok := s.account(row.Username)
	if !ok {
		return sshgateway.Identity{}, errors.New("account no longer exists")
	}
	if s.cfg != nil && s.cfg().SSHGateway.AdminsOnly && !user.IsAdmin {
		return sshgateway.Identity{}, errors.New("the SSH gateway is limited to admins")
	}
	now := s.now()
	if row.LastUsedAt == nil || now.Sub(*row.LastUsedAt) >= sshKeyTouchEvery {
		_ = s.db.Model(&models.UserSSHKey{}).Where("id = ?", row.ID).Update("last_used_at", now).Error
	}
	return sshgateway.Identity{Username: row.Username, Fingerprint: fingerprint}, nil
}

func (s *SSHKeyService) account(username string) (config.AuthUser, bool) {
	if s.cfg == nil {
		return config.AuthUser{}, false
	}
	for _, u := range s.cfg().Auth.Users {
		if u.Username == username && u.PasswordHash != "" {
			return u, true
		}
	}
	return config.AuthUser{}, false
}
