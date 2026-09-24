package handlers

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// previewHostLabel is the single host label that makes the preview document a
// different origin from the approval page while staying same-site for a normal
// registrable domain (pv.app.example.com under app.example.com). It is not a
// cookie name.
const previewHostLabel = "pv"

// previewCookiePrefix is applied to every upstream Set-Cookie name, and only
// cookies carrying this prefix are forwarded upstream (with the prefix
// removed). Platform cookies such as cf_session never gain the prefix, so they
// are not sent to the sandboxed app. The rule does not branch on a cookie name.
const previewCookiePrefix = "pv."

// previewSessionLimits is the browser behavior this proxy does not special-case.
// One rewrite still applies to every Set-Cookie.
const previewSessionLimits = "" +
	"__Host- cookies must use Path=/ and must not set Domain. The uniform Path " +
	"scope under the preview prefix makes the browser drop a cookie whose name " +
	"still starts with __Host-; this proxy does not add a separate carrier for " +
	"that reserved prefix. Root-absolute script requests such as fetch('/login') " +
	"are not rewritten, so they never enter the preview channel and do not carry " +
	"the preview session."

func requestPublicHost(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	if fh := strings.TrimSpace(c.GetHeader("X-Forwarded-Host")); fh != "" {
		// A proxy may pass a comma-separated chain; the first is the browser host.
		if i := strings.Index(fh, ","); i >= 0 {
			fh = strings.TrimSpace(fh[:i])
		}
		return fh
	}
	return c.Request.Host
}

func requestScheme(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return "http"
	}
	if proto := strings.ToLower(strings.TrimSpace(c.GetHeader("X-Forwarded-Proto"))); proto == "https" || proto == "http" {
		return proto
	}
	if c.Request.TLS != nil {
		return "https"
	}
	return "http"
}

func splitHostPortLoose(hostport string) (name, port string) {
	hostport = strings.TrimSpace(hostport)
	if hostport == "" {
		return "", ""
	}
	if h, p, err := net.SplitHostPort(hostport); err == nil {
		return strings.Trim(h, "[]"), p
	}
	return strings.Trim(hostport, "[]"), ""
}

func joinHostPort(name, port string) string {
	if port == "" {
		return name
	}
	return net.JoinHostPort(name, port)
}

func isPreviewDocumentHost(hostport string) bool {
	name, _ := splitHostPortLoose(hostport)
	return strings.HasPrefix(strings.ToLower(name), previewHostLabel+".")
}

// toPreviewDocumentHost maps the approval host onto the preview document host.
// DNS names become pv.<host>. Raw IPs become pv.<ip>.sslip.io so the name
// still resolves to that address without a cookie-name branch.
func toPreviewDocumentHost(hostport string) string {
	name, port := splitHostPortLoose(hostport)
	if name == "" {
		return ""
	}
	if isPreviewDocumentHost(name) {
		return joinHostPort(name, port)
	}
	if ip := net.ParseIP(name); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return joinHostPort(previewHostLabel+"."+v4.String()+".sslip.io", port)
		}
		dashed := strings.ReplaceAll(ip.String(), ":", "-")
		return joinHostPort(previewHostLabel+"."+dashed+".sslip.io", port)
	}
	return joinHostPort(previewHostLabel+"."+name, port)
}

// approvalHostFromPreview is the approval host a preview document may be framed by.
func approvalHostFromPreview(previewHostport string) string {
	name, port := splitHostPortLoose(previewHostport)
	lower := strings.ToLower(name)
	if !strings.HasPrefix(lower, previewHostLabel+".") {
		return joinHostPort(name, port)
	}
	rest := name[len(previewHostLabel)+1:]
	if strings.HasSuffix(strings.ToLower(rest), ".sslip.io") {
		core := rest[:len(rest)-len(".sslip.io")]
		if ip := net.ParseIP(core); ip != nil {
			return joinHostPort(ip.String(), port)
		}
	}
	return joinHostPort(rest, port)
}

func approvalFrameAncestors(publicHost string) string {
	parent := approvalHostFromPreview(publicHost)
	if !isPreviewDocumentHost(publicHost) {
		parent = publicHost
	}
	parent = strings.TrimSpace(parent)
	if parent == "" || !safeCSPHost(parent) {
		return ""
	}
	return "http://" + parent + " https://" + parent
}

func safeCSPHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '-', r == ':', r == '_':
		default:
			return false
		}
	}
	return true
}

// PublicGateCSP is the public approval page policy. frame-src allows the
// preview document host so the iframe is cross-origin, not sandboxed
// same-origin.
func PublicGateCSP(host string) string {
	frame := "frame-src 'self'"
	if ph := toPreviewDocumentHost(host); ph != "" && safeCSPHost(ph) {
		frame += " http://" + ph + " https://" + ph
	}
	return "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; " +
		frame + " blob:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"
}

// redirectOffApprovalOrigin sends the browser to the preview document host so
// the app is never rendered on the approval origin. Returns true when it wrote
// the redirect.
func redirectOffApprovalOrigin(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	public := requestPublicHost(c)
	if public == "" || isPreviewDocumentHost(public) {
		return false
	}
	destHost := toPreviewDocumentHost(public)
	if destHost == "" || strings.EqualFold(destHost, public) {
		return false
	}
	u := &url.URL{
		Scheme:   requestScheme(c),
		Host:     destHost,
		Path:     c.Request.URL.Path,
		RawQuery: c.Request.URL.RawQuery,
	}
	c.Redirect(http.StatusTemporaryRedirect, u.String())
	return true
}

// forwardPreviewCookies keeps only cookies written by this preview. Each name
// uses the same prefix; the prefix is removed before the upstream app sees it.
// Cookies without the prefix, including the platform session, are dropped.
func forwardPreviewCookies(req *http.Request) {
	if req == nil {
		return
	}
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
		name, value, ok := strings.Cut(part, "=")
		name = strings.TrimSpace(name)
		if !ok || !strings.HasPrefix(name, previewCookiePrefix) {
			continue
		}
		bare := strings.TrimPrefix(name, previewCookiePrefix)
		if bare == "" {
			continue
		}
		kept = append(kept, bare+"="+value)
	}
	if len(kept) == 0 {
		req.Header.Del("Cookie")
		return
	}
	req.Header.Set("Cookie", strings.Join(kept, "; "))
}

func shouldPartitionPreviewCookies(r *http.Request, publicHost string) bool {
	if r == nil || !strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")), "cross-site") {
		return false
	}
	if r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
		return true
	}
	name, _ := splitHostPortLoose(publicHost)
	h := strings.ToLower(name)
	return h == "localhost" || strings.HasSuffix(h, ".localhost") || h == "127.0.0.1"
}
