package auth

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/cocofhu/grasp/internal/models"

	"github.com/gin-gonic/gin"
)

var apiWhitelist = map[string]struct{}{
	"/api/auth/login":  {},
	"/api/auth/logout": {},
}

// APIMiddleware validates cf_session for /api/* except whitelisted paths.
func (s *Service) APIMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if !strings.HasPrefix(path, "/api/") || path == "/api/health" || path == "/api/live" {
			c.Next()
			return
		}
		if rejectCrossOrigin(c) {
			return
		}
		if _, ok := apiWhitelist[path]; ok {
			c.Next()
			return
		}
		token := SessionToken(c)
		sess, err := s.ValidateSession(token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			c.Abort()
			return
		}
		c.Set("auth_session", sess)
		c.Set("auth_username", sess.Username)
		c.Next()
	}
}

// SandboxRedirectMiddleware redirects unauthenticated browser requests to /login.
func (s *Service) SandboxRedirectMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		topLevelNavigation := c.Request.Method == http.MethodGet && c.GetHeader("Sec-Fetch-Dest") == "document"
		if !topLevelNavigation && rejectCrossOrigin(c) {
			return
		}
		token := SessionToken(c)
		if _, err := s.ValidateSession(token); err != nil {
			redirect := ValidateRedirect(c.Request.URL.RequestURI())
			c.Redirect(http.StatusFound, "/login?redirect="+url.QueryEscape(redirect))
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequireSession aborts with 401 when no valid session (for WebSocket upgrade paths).
func (s *Service) RequireSession(c *gin.Context) (*models.Session, bool) {
	if rejectCrossOrigin(c) {
		return nil, false
	}
	token := SessionToken(c)
	sess, err := s.ValidateSession(token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		c.Abort()
		return nil, false
	}
	c.Set("auth_session", sess)
	return sess, true
}

// Cookie authentication is limited to the Grasp origin. Same-site requests
// can come from an app preview on another port and carry the same cookie.
func rejectCrossOrigin(c *gin.Context) bool {
	if sessionOriginAllowed(c) {
		return false
	}
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "cross_origin_session_request"})
	return true
}

func sessionOriginAllowed(c *gin.Context) bool {
	switch c.GetHeader("Sec-Fetch-Site") {
	case "same-origin":
		// A same-origin request can reach a different upstream Host in dev.
		return true
	case "same-site", "cross-site":
		return false
	}
	origin := c.GetHeader("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	scheme := "http"
	if isSecureRequest(c) {
		scheme = "https"
	}
	return err == nil && u.Scheme == scheme && strings.EqualFold(u.Host, c.Request.Host)
}
