package auth

import (
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// maxSessionCookies bounds how many cf_session values one request may make
// the server look up.
const maxSessionCookies = 8

// maxClearedCookieSegments bounds the Set-Cookie headers written when copies
// of cf_session must be expired.
const maxClearedCookieSegments = 8

// SetSessionCookie writes the cf_session HttpOnly cookie.
func SetSessionCookie(c *gin.Context, token string) {
	c.SetSameSite(http.SameSiteLaxMode)
	secure := isSecureRequest(c)
	c.SetCookie(CookieName, token, CookieMaxAge, "/", "", secure, true)
}

// ClearSessionCookie expires the cf_session cookie.
func ClearSessionCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	secure := isSecureRequest(c)
	c.SetCookie(CookieName, "", -1, "/", "", secure, true)
}

// SessionToken reads the cf_session cookie from the request.
func SessionToken(c *gin.Context) string {
	token, err := c.Cookie(CookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(token)
}

// SessionTokens returns each distinct cf_session value in request order.
// Cookies ignore ports, so script on an app preview port can add its own
// cf_session with a narrower Path and the browser sends both.
func SessionTokens(c *gin.Context) []string {
	var out []string
	seen := map[string]bool{}
	for _, ck := range c.Request.Cookies() {
		if ck.Name != CookieName {
			continue
		}
		v := strings.TrimSpace(ck.Value)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
		if len(out) == maxSessionCookies {
			break
		}
	}
	return out
}

// clearSessionCookieCopies expires cf_session on every Path and Domain that
// could have matched this request.
func clearSessionCookieCopies(c *gin.Context) {
	secure := isSecureRequest(c)
	host := c.Request.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	for _, path := range cookiePathCandidates(c.Request.URL.EscapedPath()) {
		for _, domain := range []string{"", host} {
			http.SetCookie(c.Writer, &http.Cookie{
				Name: CookieName, Path: path, Domain: domain, MaxAge: -1,
				Secure: secure, HttpOnly: true, SameSite: http.SameSiteLaxMode,
			})
		}
	}
}

// cookiePathCandidates lists the cookie Paths that match requestPath:
// "/", then each segment boundary with and without its trailing slash.
func cookiePathCandidates(requestPath string) []string {
	out := []string{"/"}
	segments := 0
	for i := 1; i < len(requestPath) && segments < maxClearedCookieSegments; i++ {
		if requestPath[i] != '/' {
			continue
		}
		out = append(out, requestPath[:i], requestPath[:i+1])
		segments++
	}
	if requestPath != "/" && !strings.HasSuffix(requestPath, "/") && segments < maxClearedCookieSegments {
		out = append(out, requestPath)
	}
	return out
}

func isSecureRequest(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	proto := strings.ToLower(c.GetHeader("X-Forwarded-Proto"))
	return proto == "https"
}
