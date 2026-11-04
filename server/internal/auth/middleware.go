package auth

import (
	"errors"
	"net"
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
		sess, err := s.SessionFromRequest(c)
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
		if _, err := s.SessionFromRequest(c); err != nil {
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
	sess, err := s.SessionFromRequest(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		c.Abort()
		return nil, false
	}
	c.Set("auth_session", sess)
	return sess, true
}

var errAmbiguousSession = errors.New("ambiguous session")

// SessionFromRequest returns the one valid session among the request's
// cf_session cookies. Two valid sessions mean another page planted one, so
// neither is trusted and every copy is expired.
func (s *Service) SessionFromRequest(c *gin.Context) (*models.Session, error) {
	var found *models.Session
	for _, token := range SessionTokens(c) {
		sess, err := s.ValidateSession(token)
		if err != nil {
			continue
		}
		if found != nil && found.ID != sess.ID {
			clearSessionCookieCopies(c)
			return nil, errAmbiguousSession
		}
		found = sess
	}
	if found == nil {
		return nil, errors.New("no session")
	}
	return found, nil
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
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	// The scheme is not compared: a TLS-terminating proxy may omit
	// X-Forwarded-Proto, and one host:port cannot serve both schemes.
	return strings.EqualFold(originHostPort(u), requestHostPort(c.Request.Host, u.Scheme))
}

func originHostPort(u *url.URL) string {
	if port := u.Port(); port == "" || port == defaultPort(u.Scheme) {
		return u.Hostname()
	}
	return u.Host
}

func requestHostPort(host, scheme string) string {
	h, port, err := net.SplitHostPort(host)
	if err != nil {
		return strings.Trim(host, "[]")
	}
	if port == defaultPort(scheme) {
		return h
	}
	return host
}

func defaultPort(scheme string) string {
	if scheme == "https" {
		return "443"
	}
	return "80"
}
