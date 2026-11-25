package previewexpose

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/publicsuffix"
)

var (
	domainLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	keyPattern  = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

// Base is a validated HTTP(S) DNS origin whose wildcard subdomains route to Grasp.
type Base struct {
	origin url.URL
}

// ParseBase validates and normalizes a preview subdomain base origin.
// It rejects IP literals, credentials, paths, queries, fragments, and trailing dots.
func ParseBase(raw string) (Base, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") {
		return Base{}, fmt.Errorf("preview subdomain base_url must be an http(s) origin without credentials, path, query or fragment")
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if net.ParseIP(host) != nil || len(host) > 220 || strings.HasSuffix(host, ".") {
		return Base{}, fmt.Errorf("preview subdomain base_url requires a DNS hostname (IP literals are not allowed)")
	}
	for _, label := range strings.Split(host, ".") {
		if !domainLabel.MatchString(label) {
			return Base{}, fmt.Errorf("preview subdomain base_url requires a valid DNS hostname")
		}
	}
	port := u.Port()
	if port != "" {
		n, convErr := strconv.Atoi(port)
		if convErr != nil || n < 1 || n > 65535 {
			return Base{}, fmt.Errorf("preview subdomain base_url requires a valid HTTP port")
		}
		port = canonicalPort(scheme, port)
	}
	normalized := url.URL{Scheme: scheme, Host: host}
	if port != "" {
		normalized.Host = net.JoinHostPort(host, port)
	}
	return Base{origin: normalized}, nil
}

// Origin returns the canonical base origin without a trailing slash.
func (b Base) Origin() string {
	if b.origin.Host == "" {
		return ""
	}
	return b.origin.String()
}

// Scheme returns "http" or "https".
func (b Base) Scheme() string { return b.origin.Scheme }

// Hostname returns the lowercase DNS hostname without port.
func (b Base) Hostname() string { return b.origin.Hostname() }

// Port returns the non-default port, or "" for default scheme ports.
func (b Base) Port() string { return b.origin.Port() }

// ContainsHost reports whether authority is the base host itself or any subdomain of it.
func (b Base) ContainsHost(authority string) bool {
	if b.origin.Host == "" {
		return false
	}
	host, _ := authorityParts(authority)
	baseHost := b.Hostname()
	return host == baseHost || strings.HasSuffix(host, "."+baseHost)
}

// Overlaps reports whether two base domains share the same host or one is a subdomain of the other.
func (b Base) Overlaps(other Base) bool {
	if b.origin.Host == "" || other.origin.Host == "" {
		return false
	}
	return b.ContainsHost(other.Hostname()) || other.ContainsHost(b.Hostname())
}

// ValidatePlatformOrigin checks that preview subdomains are isolated from the platform origin.
// In production mode, HTTPS, non-localhost, and a distinct registrable site (eTLD+1) are required.
// In non-production modes, sharing a registrable site returns a warning instead of an error.
func (b Base) ValidatePlatformOrigin(platformRaw string, production bool) (warning string, err error) {
	u, err := url.Parse(strings.TrimSpace(platformRaw))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") {
		return "", fmt.Errorf("subdomain preview requires server.public_advertise to be an http(s) origin")
	}
	platformHost := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	previewHost := b.Hostname()
	if b.ContainsHost(platformHost) || platformHost == previewHost || strings.HasSuffix(previewHost, "."+platformHost) {
		return "", fmt.Errorf("preview subdomain base_url %q must not share or nest with server.public_advertise %q", b.Origin(), platformRaw)
	}
	if production {
		if b.Scheme() != "https" {
			return "", fmt.Errorf("production preview subdomain base_url %q must use https", b.Origin())
		}
		if isLocalhost(platformHost) || isLocalhost(previewHost) {
			return "", fmt.Errorf("localhost preview subdomain origins are restricted to non-production modes")
		}
	}
	platformSite, pErr := cookieSite(platformHost)
	previewSite, bErr := cookieSite(previewHost)
	if production {
		if pErr != nil {
			return "", pErr
		}
		if bErr != nil {
			return "", bErr
		}
		if platformSite == previewSite {
			return "", fmt.Errorf("production preview subdomain base_url %q must use a different registrable site (eTLD+1) from server.public_advertise %q", b.Origin(), platformRaw)
		}
		return "", nil
	}
	if pErr == nil && bErr == nil && platformSite == previewSite {
		return fmt.Sprintf("preview subdomain base_url %q shares registrable site %q with server.public_advertise %q; use a separate root domain in production", b.Origin(), previewSite, platformRaw), nil
	}
	return "", nil
}

// URL builds the browser-facing subdomain URL for key (with trailing slash).
func (b Base) URL(key string) string {
	if b.origin.Host == "" || !ValidKey(key) {
		return ""
	}
	host := key + "." + b.Hostname()
	if p := b.Port(); p != "" {
		host = net.JoinHostPort(host, p)
	}
	return (&url.URL{Scheme: b.Scheme(), Host: host, Path: "/"}).String()
}

// ValidKey reports whether key is a canonical 128-bit lowercase hex DNS label.
func ValidKey(key string) bool {
	return keyPattern.MatchString(key)
}

// NewKey generates a cryptographically random 128-bit lowercase hex key.
func NewKey() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate preview expose key: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

// SubdomainExposer serves previews on per-registration random subdomains under Base.
type SubdomainExposer struct {
	base Base
}

var (
	_ Exposer = (*SubdomainExposer)(nil)
	_ Inbound = (*SubdomainExposer)(nil)
)

// NewSubdomain builds a SubdomainExposer for a validated Base.
func NewSubdomain(base Base) *SubdomainExposer {
	return &SubdomainExposer{base: base}
}

func (s *SubdomainExposer) Name() string { return StrategySubdomain }

func (s *SubdomainExposer) Bind(ctx context.Context, _ Target) (Binding, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return Binding{}, err
		}
	}
	key, err := NewKey()
	if err != nil {
		return Binding{}, err
	}
	return Binding{Strategy: StrategySubdomain, Key: key}, nil
}

func (s *SubdomainExposer) URL(_ Target, b Binding) string {
	if b.Strategy != StrategySubdomain || !ValidKey(b.Key) {
		return ""
	}
	return s.base.URL(b.Key)
}

// MatchHost claims requests targeting the base host or any of its subdomains.
// key is non-empty only when authority has a single valid 128-bit hex child label
// and its port matches the configured base port.
func (s *SubdomainExposer) MatchHost(authority string) (key string, claimed bool) {
	if !s.base.ContainsHost(authority) {
		return "", false
	}
	host, port := authorityParts(authority)
	suffix := "." + s.base.Hostname()
	if !strings.HasSuffix(host, suffix) {
		return "", true
	}
	label := strings.TrimSuffix(host, suffix)
	if !ValidKey(label) || canonicalPort(s.base.Scheme(), port) != s.base.Port() {
		return "", true
	}
	return label, true
}

// TicketHost reports whether authority's first DNS label is a canonical expose
// key, the shape of every subdomain preview origin this package issues. Such a
// host must never reach platform routes, even after its base_url is retired.
func TicketHost(authority string) bool {
	host, _ := authorityParts(authority)
	label, rest, ok := strings.Cut(host, ".")
	return ok && rest != "" && ValidKey(label)
}

func authorityParts(authority string) (host, port string) {
	raw := strings.TrimSpace(authority)
	if raw == "" {
		return "", ""
	}
	if h, p, err := net.SplitHostPort(raw); err == nil {
		return strings.ToLower(strings.TrimSuffix(h, ".")), p
	}
	return strings.ToLower(strings.TrimSuffix(strings.Trim(raw, "[]"), ".")), ""
}

func canonicalPort(scheme, port string) string {
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		return ""
	}
	return port
}

func isLocalhost(host string) bool {
	return host == "localhost" || strings.HasSuffix(host, ".localhost")
}

func cookieSite(host string) (string, error) {
	if ip := net.ParseIP(host); ip != nil {
		return ip.String(), nil
	}
	site, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return "", fmt.Errorf("preview subdomain origin requires a registrable DNS site or platform IP: %w", err)
	}
	return site, nil
}
