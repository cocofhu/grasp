package models

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

// ErrGitScope rejects a repository address that cannot scope a credential.
var ErrGitScope = errors.New("invalid Git repository scope")

// NormalizeGitScope accepts host and repository scopes. It rejects credentials
// embedded in URLs, ambiguous URL components, and control/space characters so
// the scope can safely travel in the sandbox's line-based manifest.
func NormalizeGitScope(raw string) (string, error) {
	scope := strings.TrimSpace(raw)
	bad := func() (string, error) {
		return "", fmt.Errorf("%w: scope must be a host or a repository URL", ErrGitScope)
	}
	if scope == "" {
		return "", nil
	}
	if strings.IndexFunc(scope, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 || strings.ContainsAny(scope, "\\?#") {
		return bad()
	}
	if !strings.Contains(scope, "://") && strings.Contains(scope, "@") {
		at, colon := strings.Index(scope, "@"), strings.Index(scope, ":")
		if at <= 0 || colon <= at || !gitScopeUser(scope[:at]) || !gitScopePath(scope[colon+1:]) {
			return bad()
		}
		host, err := normalizeGitScopeHost(scope[at+1 : colon])
		if err != nil {
			return bad()
		}
		return scope[:at] + "@" + host + ":" + strings.TrimRight(scope[colon+1:], "/"), nil
	}
	if !strings.Contains(scope, "://") {
		return normalizeGitScopeHost(scope)
	}
	u, err := url.Parse(scope)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "ssh") || u.Opaque != "" || u.Path != "" && u.Path != "/" && !gitScopePath(strings.TrimPrefix(u.Path, "/")) {
		return bad()
	}
	if u.User != nil {
		if u.Scheme != "ssh" || !gitScopeUser(u.User.Username()) {
			return bad()
		}
		if _, password := u.User.Password(); password {
			return bad()
		}
	}
	u.Host, err = normalizeGitScopeHost(u.Host)
	if err != nil {
		return bad()
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u.String(), nil
}

func gitScopeUser(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r)) {
			return false
		}
	}
	return true
}

func gitScopePath(s string) bool {
	s = strings.TrimRight(s, "/")
	if s == "" || strings.IndexFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 || strings.ContainsAny(s, "\\?#") {
		return false
	}
	for _, part := range strings.Split(s, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func normalizeGitScopeHost(raw string) (string, error) {
	u, err := url.Parse("https://" + raw)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || strings.HasSuffix(raw, ":") {
		return "", fmt.Errorf("%w: invalid scope host", ErrGitScope)
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if net.ParseIP(host) == nil {
		for _, part := range strings.Split(host, ".") {
			if part == "" || strings.HasPrefix(part, "-") || strings.HasSuffix(part, "-") {
				return "", fmt.Errorf("%w: invalid scope host", ErrGitScope)
			}
			for _, r := range part {
				if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
					return "", fmt.Errorf("%w: invalid scope host", ErrGitScope)
				}
			}
		}
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("%w: invalid scope port", ErrGitScope)
		}
		return net.JoinHostPort(host, port), nil
	}
	if strings.Contains(host, ":") {
		return "[" + host + "]", nil
	}
	return host, nil
}
