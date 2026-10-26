package models

import (
	"errors"
	"net/url"
	"strings"
)

var ErrGitCredentialHost = errors.New("Git credential host must be an HTTP(S) origin without a path, credentials, query or fragment")

// NormalizeGitCredentialHost keeps the credential's authority separate from an
// Agent's narrower repository scope. A bare hostname uses HTTPS.
func NormalizeGitCredentialHost(provider, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		switch strings.ToLower(strings.TrimSpace(provider)) {
		case "github", "gh":
			raw = "https://github.com"
		case "gitlab", "glab":
			raw = "https://gitlab.com"
		default:
			return "", ErrGitCredentialHost
		}
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" && u.Scheme != "http" || u.Host == "" || u.User != nil || u.Opaque != "" || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || strings.ContainsAny(raw, "?#") {
		return "", ErrGitCredentialHost
	}
	host, err := normalizeGitScopeHost(u.Host)
	if err != nil {
		return "", ErrGitCredentialHost
	}
	return u.Scheme + "://" + host, nil
}
