// Package auth implements static-account login with server-side sessions.
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/models"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	CookieName     = "cf_session"
	CookieMaxAge   = 604800 // 7 days in seconds
	ErrInvalidCred = "用户名或密码错误"
)

// Service handles bcrypt login, session CRUD, and cookie helpers.
type Service struct {
	db  *gorm.DB
	cfg func() *config.Config
}

// NewService constructs an auth service. cfg must return the live config snapshot.
func NewService(db *gorm.DB, cfg func() *config.Config) *Service {
	return &Service{db: db, cfg: cfg}
}

// Login validates credentials and creates a session. Returns unified error on failure.
func (s *Service) Login(username, password string) (*models.Session, error) {
	user, ok := s.findUser(username)
	if !ok {
		_ = bcrypt.CompareHashAndPassword([]byte("$2a$10$EY.SdHq0p6drMz6U9JVrz.Kq0jNkg7TWmsVUFLtB1dL1yIelDkITi"), []byte(password))
		return nil, errors.New(ErrInvalidCred)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, errors.New(ErrInvalidCred)
	}
	s.cleanupExpired()
	return s.createSession(user.Username)
}

func (s *Service) createSession(username string) (*models.Session, error) {
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	ttl := s.cfg().Auth.SessionTTLDuration()
	now := time.Now()
	sess := &models.Session{
		ID:        token,
		Username:  username,
		ExpiresAt: now.Add(ttl),
		CreatedAt: now,
	}
	if err := s.db.Create(sess).Error; err != nil {
		return nil, err
	}
	return sess, nil
}

// ValidateSession returns the session when the token is valid and not expired.
func (s *Service) ValidateSession(token string) (*models.Session, error) {
	if token == "" {
		return nil, errors.New("no session")
	}
	var sess models.Session
	if err := s.db.First(&sess, "id = ?", token).Error; err != nil {
		return nil, err
	}
	if time.Now().After(sess.ExpiresAt) {
		_ = s.db.Delete(&sess).Error
		return nil, errors.New("session expired")
	}
	return &sess, nil
}

// DeleteSession removes a session by token (logout).
func (s *Service) DeleteSession(token string) error {
	if token == "" {
		return nil
	}
	return s.db.Delete(&models.Session{}, "id = ?", token).Error
}

func (s *Service) findUser(username string) (config.AuthUser, bool) {
	for _, u := range s.cfg().Auth.Users {
		if u.Username == username && u.PasswordHash != "" {
			return u, true
		}
	}
	return config.AuthUser{}, false
}

// IsAdmin reports whether the user has global platform-rule write access.
func (s *Service) IsAdmin(username string) bool {
	u, ok := s.findUser(username)
	return ok && u.IsAdmin
}

func (s *Service) cleanupExpired() {
	_ = s.db.Where("expires_at < ?", time.Now()).Delete(&models.Session{}).Error
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ValidateRedirect returns a safe in-site relative path or "/".
func ValidateRedirect(redirect string) string {
	if redirect == "" {
		return "/"
	}
	if len(redirect) == 0 || redirect[0] != '/' {
		return "/"
	}
	if len(redirect) >= 2 && redirect[0] == '/' && redirect[1] == '/' {
		return "/"
	}
	return redirect
}
