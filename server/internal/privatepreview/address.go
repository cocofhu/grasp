// Package privatepreview defines browser origins for private sandbox endpoints.
// Each registration has its own opaque host so sandbox HTML never shares the
// platform's authenticated origin, and apps can keep their normal root paths.
package privatepreview

import (
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

var domainLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// Base is an HTTP origin whose wildcard subdomains route to Grasp's listener.
type Base struct{ origin url.URL }

// Parse requires a DNS hostname, because every preview uses a child hostname.
func Parse(raw string) (Base, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return Base{}, fmt.Errorf("preview_advertise must be an http(s) origin without credentials, path, query or fragment")
	}
	host := strings.ToLower(u.Hostname())
	if net.ParseIP(host) != nil || len(host) > 220 || strings.HasSuffix(host, ".") {
		return Base{}, fmt.Errorf("preview_advertise requires a DNS hostname for wildcard preview hosts")
	}
	for _, label := range strings.Split(host, ".") {
		if !domainLabel.MatchString(label) {
			return Base{}, fmt.Errorf("preview_advertise requires a DNS hostname for wildcard preview hosts")
		}
	}
	port := u.Port()
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return Base{}, fmt.Errorf("preview_advertise requires a valid HTTP port")
		}
	}
	u.Host = host
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	}
	u.Path = ""
	return Base{origin: *u}, nil
}

// Origin returns the configured base origin, without its trailing slash.
func (b Base) Origin() string { return b.origin.String() }

// ValidatePlatformOrigin keeps preview HTML outside the platform's origin and,
// in production, outside its cookie site. Sibling subdomains are different
// origins but still send SameSite cookies to the platform on credentialed
// requests. localhost subdomains are supported for local development only.
func (b Base) ValidatePlatformOrigin(raw string, production bool) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("private previews require server.public_advertise to be an http(s) origin")
	}
	if b.ContainsHost(u.Host) {
		return fmt.Errorf("private preview origin must be separate from server.public_advertise")
	}
	if !production {
		return nil
	}
	platformHost := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	previewHost := b.origin.Hostname()
	if isLocalhost(platformHost) || isLocalhost(previewHost) {
		return fmt.Errorf("localhost private preview origins are restricted to development")
	}
	platformSite, err := cookieSite(platformHost)
	if err != nil {
		return err
	}
	previewSite, err := cookieSite(previewHost)
	if err != nil {
		return err
	}
	if platformSite == previewSite {
		return fmt.Errorf("production private previews require a different registrable site from server.public_advertise")
	}
	return nil
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
		return "", fmt.Errorf("private preview browser origins require a registrable DNS site or platform IP")
	}
	return site, nil
}

// ContainsHost reserves the base host and all its subdomains exclusively for
// previews. An invalid ticket must never fall through to platform API routes.
func (b Base) ContainsHost(authority string) bool {
	host, _ := authorityParts(authority)
	return b.origin.Host != "" && (host == b.origin.Hostname() || strings.HasSuffix(host, "."+b.origin.Hostname()))
}

// Ticket extracts a single opaque registration label from an HTTP Host.
func (b Base) Ticket(authority string) (string, bool) {
	if !b.ContainsHost(authority) {
		return "", false
	}
	host, port := authorityParts(authority)
	ticket := strings.TrimSuffix(host, "."+b.origin.Hostname())
	return ticket, ValidTicket(ticket) && port == b.origin.Port()
}

// ValidTicket checks the 128-bit opaque host label's canonical encoding.
func ValidTicket(ticket string) bool {
	if len(ticket) != 32 || ticket != strings.ToLower(ticket) {
		return false
	}
	_, err := hex.DecodeString(ticket)
	return err == nil
}

// URL returns a registration's isolated origin with its root path.
func (b Base) URL(ticket string) string {
	if !ValidTicket(ticket) || b.origin.Host == "" {
		return ""
	}
	u := b.origin
	u.Host = ticket + "." + u.Host
	u.Path = "/"
	return u.String()
}

// NewURL mints an unguessable browser capability, never a provider credential.
func (b Base) NewURL() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("create preview address: %w", err)
	}
	return b.URL(hex.EncodeToString(buf[:])), nil
}

// OwnsURL checks an exact isolated root URL, including scheme and port.
func (b Base) OwnsURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != b.origin.Scheme || u.User != nil || u.Path != "/" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return false
	}
	_, ok := b.Ticket(u.Host)
	return ok
}

func authorityParts(authority string) (string, string) {
	u := &url.URL{Host: strings.ToLower(strings.TrimSpace(authority))}
	return strings.TrimRight(u.Hostname(), "."), u.Port()
}
