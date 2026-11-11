package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/auth"

	"github.com/gin-gonic/gin"
)

const upstreamLoginTimeout = 5 * time.Second

// DefaultCodeServerWorkspaceFolder matches the sandbox image default workspace;
// used to fill ?folder= on proxied root paths.
const DefaultCodeServerWorkspaceFolder = "/root/workspace"

func upstreamLoginClient() *http.Client {
	return &http.Client{
		Timeout: upstreamLoginTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func sandboxMountPrefix(id uint) string {
	return "/sandbox/" + strconv.FormatUint(uint64(id), 10)
}

// codeServerSessionCookie is the session cookie name code-server sets on /login.
const codeServerSessionCookie = "key"

func requestHasUpstreamCookie(req *http.Request) bool {
	// Only the code-server session cookie counts. Any unrelated site cookie
	// (locale, analytics, …) previously skipped auto-login and left users on
	// the "Password was set from $PASSWORD" wall.
	for _, c := range req.Cookies() {
		if c.Name == codeServerSessionCookie && c.Value != "" {
			return true
		}
	}
	return false
}

func shouldAutoLoginCodeServer(req *http.Request, sandboxID uint) bool {
	if !requestHasUpstreamCookie(req) {
		return true
	}
	loginPath := sandboxMountPrefix(sandboxID) + "/login"
	return req.URL.Path == loginPath || strings.HasPrefix(req.URL.Path, loginPath+"/")
}

func mountedCookie(c *http.Cookie, mountPrefix string) *http.Cookie {
	out := *c
	out.Path = strings.TrimRight(mountPrefix, "/") + "/"
	out.Domain = ""
	// Always Secure (CodeQL #12). Local HTTP debugging requires HTTPS or a
	// trusted local TLS terminator; browsers will not store the cookie on plain HTTP.
	out.Secure = true
	return &out
}

func addCookiesToRequest(req *http.Request, cookies []*http.Cookie) {
	if len(cookies) == 0 {
		return
	}
	remove := make(map[string]struct{}, len(cookies))
	for _, c := range cookies {
		if c != nil && c.Name != "" {
			remove[c.Name] = struct{}{}
		}
	}
	var kept []string
	for _, part := range strings.Split(req.Header.Get("Cookie"), ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, _, ok := strings.Cut(part, "=")
		if ok {
			if _, drop := remove[strings.TrimSpace(name)]; drop {
				continue
			}
		}
		kept = append(kept, part)
	}
	for _, c := range cookies {
		if c != nil && c.Name != "" {
			kept = append(kept, c.Name+"="+c.Value)
		}
	}
	if len(kept) == 0 {
		req.Header.Del("Cookie")
		return
	}
	req.Header.Set("Cookie", strings.Join(kept, "; "))
}

func applyUpstreamLoginCookies(c *gin.Context, mountPrefix string, cookies []*http.Cookie) {
	if len(cookies) == 0 {
		return
	}
	mounted := make([]*http.Cookie, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie == nil || cookie.Name == "" {
			continue
		}
		fixed := mountedCookie(cookie, mountPrefix)
		http.SetCookie(c.Writer, fixed)
		mounted = append(mounted, fixed)
	}
	addCookiesToRequest(c.Request, mounted)
}

// forceUpstreamLogin POSTs to code-server /login for a fresh session cookie.
func forceUpstreamLogin(parent context.Context, upstreamHost, password string) ([]*http.Cookie, error) {
	form := url.Values{}
	form.Set("password", password)
	ctx, cancel := context.WithTimeout(parent, upstreamLoginTimeout)
	defer cancel()
	loginURL := (&url.URL{Scheme: "http", Host: upstreamHost, Path: "/login"}).String()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := upstreamLoginClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return nil, fmt.Errorf("login returned %s", resp.Status)
	}
	return resp.Cookies(), nil
}

// autoLoginCodeServer obtains a code-server session on behalf of the already
// authenticated platform user so the IDE iframe never shows the password form.
// Mirrors remote-dev's SandboxProxy auto-login.
func autoLoginCodeServer(c *gin.Context, sandboxID uint, upstreamHost, password string) error {
	if strings.TrimSpace(password) == "" {
		return nil
	}
	if !shouldAutoLoginCodeServer(c.Request, sandboxID) {
		return nil
	}
	cookies, err := forceUpstreamLogin(c.Request.Context(), upstreamHost, password)
	if err != nil {
		return err
	}
	applyUpstreamLoginCookies(c, sandboxMountPrefix(sandboxID), cookies)
	return nil
}

func shouldAutoLoginVibe(req *http.Request, mountPrefix string) bool {
	if !requestHasUpstreamCookie(req) {
		return true
	}
	loginPath := strings.TrimRight(mountPrefix, "/") + "/login"
	return req.URL.Path == loginPath || strings.HasPrefix(req.URL.Path, loginPath+"/")
}

// forceVibeLogin POSTs JSON to acp-bridge /api/login (remote-dev VibeCodingProxy).
func forceVibeLogin(parent context.Context, upstreamHost, password string) ([]*http.Cookie, error) {
	body, err := json.Marshal(map[string]string{"password": password})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, upstreamLoginTimeout)
	defer cancel()
	loginURL := (&url.URL{Scheme: "http", Host: upstreamHost, Path: "/api/login"}).String()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := upstreamLoginClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("vibe login returned %s", resp.Status)
	}
	return resp.Cookies(), nil
}

// autoLoginVibeCoding logs into acp-bridge for the platform-proxied ACP iframe.
// Best-effort: bridges without /api/login (404) leave cookies empty.
func autoLoginVibeCoding(c *gin.Context, _ uint, mountPrefix, upstreamHost, password string) error {
	if strings.TrimSpace(password) == "" {
		return nil
	}
	if !shouldAutoLoginVibe(c.Request, mountPrefix) {
		return nil
	}
	cookies, err := forceVibeLogin(c.Request.Context(), upstreamHost, password)
	if err != nil {
		return err
	}
	applyUpstreamLoginCookies(c, mountPrefix, cookies)
	return nil
}

// forwardCookiesToUpstream drops the platform session cookie so it is not
// forwarded to code-server; other cookies (including the just-minted session)
// pass through.
func forwardCookiesToUpstream(req *http.Request) {
	raw := req.Header.Get("Cookie")
	if raw == "" {
		return
	}
	var kept []string
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, _, ok := strings.Cut(part, "=")
		if ok && strings.TrimSpace(name) == auth.CookieName {
			continue
		}
		kept = append(kept, part)
	}
	if len(kept) == 0 {
		req.Header.Del("Cookie")
	} else {
		req.Header.Set("Cookie", strings.Join(kept, "; "))
	}
}

func rewriteRedirectLocation(resp *http.Response, base string) {
	loc := resp.Header.Get("Location")
	if loc == "" {
		return
	}
	u, err := url.Parse(loc)
	if err != nil {
		return
	}
	var out string
	switch {
	case u.IsAbs() || u.Host != "":
		p := u.EscapedPath()
		if p == "" {
			p = "/"
		}
		if p == "/" {
			out = base + "/"
		} else {
			out = base + p
		}
		if u.RawQuery != "" {
			out += "?" + u.RawQuery
		}
	default:
		p := u.Path
		if p == "" {
			return
		}
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		if strings.HasPrefix(p, base) {
			return
		}
		out = base + p
		if u.RawQuery != "" {
			out += "?" + u.RawQuery
		}
	}
	if out != "" {
		resp.Header.Set("Location", out)
	}
}

// setCookiePathRE matches an existing Path= attribute in Set-Cookie.
var setCookiePathRE = regexp.MustCompile(`(?i)(;\s*path=)(/[^;]*)`)

// rewriteUpstreamSetCookiePaths scopes upstream Set-Cookie Path under mountPrefix
// so browsers only send them for that sandbox (avoids cross-sandbox clobber).
func rewriteUpstreamSetCookiePaths(resp *http.Response, mountPrefix string) {
	vals := resp.Header.Values("Set-Cookie")
	if len(vals) == 0 {
		return
	}
	resp.Header.Del("Set-Cookie")
	for _, v := range vals {
		if setCookiePathRE.MatchString(v) {
			fixed := setCookiePathRE.ReplaceAllStringFunc(v, func(m string) string {
				parts := setCookiePathRE.FindStringSubmatch(m)
				origPath := parts[2]
				if origPath == "/" {
					return parts[1] + mountPrefix + "/"
				}
				return parts[1] + mountPrefix + origPath
			})
			resp.Header.Add("Set-Cookie", fixed)
		} else {
			resp.Header.Add("Set-Cookie", v+"; Path="+mountPrefix+"/")
		}
	}
}

func mergeUpstreamQuery(req *http.Request, clientQuery url.Values) {
	outQ := req.URL.Query()
	for k, vv := range clientQuery {
		outQ.Del(k)
		for _, v := range vv {
			outQ.Add(k, v)
		}
	}
	p := req.URL.Path
	if p == "/" || p == "" {
		if strings.TrimSpace(outQ.Get("folder")) == "" {
			outQ.Set("folder", DefaultCodeServerWorkspaceFolder)
		}
	}
	req.URL.RawQuery = outQ.Encode()
	req.Form = nil
	req.PostForm = nil
}

func isWebSocketUpgrade(r *http.Request) bool {
	for _, val := range r.Header.Values("Connection") {
		for _, part := range strings.Split(val, ",") {
			if strings.EqualFold(strings.TrimSpace(part), "upgrade") {
				return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
			}
		}
	}
	return false
}
