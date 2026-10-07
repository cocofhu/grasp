package auth

import (
	"net/http"

	"github.com/cocofhu/grasp/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// LoginRequest is the JSON body for POST /api/auth/login.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Redirect string `json:"redirect"`
}

// MeResponse is returned by GET /api/auth/me.
type MeResponse struct {
	Username  string `json:"username"`
	ExpiresAt string `json:"expires_at"`
	IsAdmin   bool   `json:"is_admin"`
}

// Login handles POST /api/auth/login.
func (s *Service) LoginHandler(c *gin.Context) {
	var body LoginRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	sess, err := s.Login(body.Username, body.Password)
	if err != nil {
		// Audit failed credentials (username only, never the password) so
		// brute-force attempts are visible server-side.
		log.Warn().Str("client_ip", c.ClientIP()).Str("username", body.Username).Msg("login failed: invalid credentials")
		c.JSON(http.StatusUnauthorized, gin.H{"error": ErrInvalidCred})
		return
	}

	SetSessionCookie(c, sess.ID)
	c.JSON(http.StatusOK, gin.H{
		"username":   sess.Username,
		"expires_at": sess.ExpiresAt.UTC().Format(timeRFC3339),
		"redirect":   ValidateRedirect(body.Redirect),
	})
}

// LogoutHandler handles POST /api/auth/logout (idempotent).
func (s *Service) LogoutHandler(c *gin.Context) {
	token := SessionToken(c)
	_ = s.DeleteSession(token)
	ClearSessionCookie(c)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// MeHandler handles GET /api/auth/me (requires valid session via middleware).
func (s *Service) MeHandler(c *gin.Context) {
	sess, ok := GetSession(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	c.JSON(http.StatusOK, MeResponse{
		Username:  sess.Username,
		ExpiresAt: sess.ExpiresAt.UTC().Format(timeRFC3339),
		IsAdmin:   s.IsAdmin(sess.Username),
	})
}

const timeRFC3339 = "2006-01-02T15:04:05Z07:00"

// GetSession returns the validated session from gin context.
func GetSession(c *gin.Context) (*models.Session, bool) {
	v, ok := c.Get("auth_session")
	if !ok {
		return nil, false
	}
	sess, ok := v.(*models.Session)
	return sess, ok && sess != nil
}
