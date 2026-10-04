package qq

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/channels"

	"github.com/rs/zerolog/log"
)

const maxInboundImageBytes = 20 << 20 // 20 MiB (clarified QQ inbound cap)
const maxInboundRedirects = 3
const qqAttachMaxMiB = 20

// errInboundTooLarge is returned when an attachment exceeds maxInboundImageBytes.
var errInboundTooLarge = fmt.Errorf("inbound attachment exceeds %d MiB", qqAttachMaxMiB)

// inboundHTTP fetches attachment bytes with SSRF guards (no private IPs,
// limited redirects, re-validated on every hop).
var inboundHTTP = newSafeHTTPClient(30 * time.Second)

// qqAuthCDNHostSuffixes are QQ multimedia CDNs that require QQBot Authorization.
// Signed qpic / grouptalk URLs typically work without a bot token.
var qqAuthCDNHostSuffixes = []string{
	"multimedia.nt.qq.com.cn",
	"multimedia.nt.qq.com",
}

func hostNeedsQQBotAuth(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return false
	}
	for _, suf := range qqAuthCDNHostSuffixes {
		if h == suf || strings.HasSuffix(h, "."+suf) {
			return true
		}
	}
	return false
}

type imageDownloader func(ctx context.Context, att attachment, authHeader string) (channels.Image, error)

// downloadImage fetches an inbound attachment into a normalized channels.Image.
// Any content type is accepted (PDF/zip/etc.); size is capped at 20 MiB.
// authHeader is Authorization: QQBot {token} and is only sent to auth CDN hosts.
func downloadImage(ctx context.Context, att attachment, authHeader string) (channels.Image, error) {
	rawURL := att.URL
	if rawURL == "" {
		return channels.Image{}, fmt.Errorf("empty attachment url")
	}
	// QQ attachment URLs sometimes omit the scheme.
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}
	if err := validatePublicHTTPURL(rawURL); err != nil {
		return channels.Image{}, err
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return channels.Image{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return channels.Image{}, err
	}
	if authHeader != "" && hostNeedsQQBotAuth(parsed.Hostname()) {
		req.Header.Set("Authorization", authHeader)
	}
	resp, err := inboundHTTP.Do(req)
	if err != nil {
		return channels.Image{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return channels.Image{}, fmt.Errorf("download image http %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxInboundImageBytes+1))
	if err != nil {
		return channels.Image{}, err
	}
	if len(data) > maxInboundImageBytes {
		return channels.Image{}, errInboundTooLarge
	}
	mime := normalizeInboundImageMIME(att.ContentType, resp.Header.Get("Content-Type"), data)
	name := strings.TrimSpace(att.Filename)
	if name == "" {
		name = fallbackAttachmentName(att, mime)
	}
	return channels.Image{Data: data, MimeType: mime, URL: att.URL, Filename: name}, nil
}

// normalizeInboundImageMIME always prefers magic-bytes for JPEG/PNG/WEBP/GIF
// (does not copy sandbox qqbot "only sniff when Content-Type is empty") and
// strips charset parameters so nosniff clients can decode.
func normalizeInboundImageMIME(claimed, responseCT string, data []byte) string {
	claimed = blob.StripContentTypeParams(claimed)
	responseCT = blob.StripContentTypeParams(responseCT)
	detected := blob.StripContentTypeParams(http.DetectContentType(data))
	if sniffed := blob.SniffSupportedImageMIME(data); sniffed != "" {
		return sniffed
	}
	claimedLower := strings.ToLower(claimed)
	// Claimed image/* / empty / «file» must not keep a lie; use sniffed type.
	if claimedLower == "" || claimedLower == "file" || strings.HasPrefix(claimedLower, "image/") {
		if detected != "" {
			return strings.ToLower(detected)
		}
		if responseCT != "" {
			return strings.ToLower(responseCT)
		}
		return "application/octet-stream"
	}
	if claimed != "" {
		return claimed
	}
	if responseCT != "" {
		return responseCT
	}
	if detected != "" {
		return strings.ToLower(detected)
	}
	return "application/octet-stream"
}

func isSniffedImageMIME(mime string) bool {
	switch strings.ToLower(blob.StripContentTypeParams(mime)) {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

// looksLikeInboundImage reports whether an attachment should count toward the
// QQ image download-failure hint (N/M). Unknown QQ types (empty/file + MD5
// name) are treated as potential images; obvious non-image extensions are not.
func looksLikeInboundImage(att attachment) bool {
	if isImageAttachment(att) {
		return true
	}
	ct := strings.ToLower(blob.StripContentTypeParams(att.ContentType))
	if ct != "" && ct != "file" && ct != "application/octet-stream" {
		return false
	}
	name := strings.ToLower(att.Filename + " " + att.URL)
	for _, ext := range []string{".pdf", ".zip", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".txt", ".mp3", ".mp4", ".mov", ".wav", ".rar", ".7z"} {
		if strings.Contains(name, ext) {
			return false
		}
	}
	return true
}

// formatQQImageDownloadHint builds the PM-only channel hint (Demo copy).
// received is N (image attachments including over-cap); failed is M.
func formatQQImageDownloadHint(received, failed int) string {
	if failed <= 0 || received <= 0 {
		return ""
	}
	if failed >= received {
		if received == 1 {
			return "收到 1 张图片，但下载失败，未能展示。"
		}
		return fmt.Sprintf("收到 %d 张图片，但下载失败，未能展示。", received)
	}
	return fmt.Sprintf("收到 %d 张图片，其中 %d 张下载失败，未能展示。", received, failed)
}

// collectInboundAttachments downloads up to maxInboundImages successes, counts
// image download failures (including over-cap skips) for a PM channel hint,
// and keeps oversized names on the existing QQ reply path.
func collectInboundAttachments(ctx context.Context, atts []attachment, authHeader string, dl imageDownloader) (images []channels.Image, hint string, oversized []string) {
	if dl == nil {
		dl = downloadImage
	}
	var received, failed int
	for _, att := range atts {
		asImage := looksLikeInboundImage(att)
		if asImage {
			received++
		}
		if len(images) >= maxInboundImages {
			if asImage {
				failed++
			}
			continue
		}
		img, err := dl(ctx, att, authHeader)
		if err != nil {
			if errors.Is(err, errInboundTooLarge) {
				name := strings.TrimSpace(att.Filename)
				if name == "" {
					name = "附件"
				}
				oversized = append(oversized, name)
				if asImage {
					received--
				}
				continue
			}
			log.Warn().Err(err).Msg("qq: inbound attachment download failed; skipping")
			if asImage {
				failed++
			}
			continue
		}
		if asImage && !isSniffedImageMIME(img.MimeType) {
			log.Warn().Str("mime", img.MimeType).Msg("qq: inbound claimed image is not jpeg/png/gif/webp; skipping")
			failed++
			continue
		}
		images = append(images, img)
	}
	return images, formatQQImageDownloadHint(received, failed), oversized
}

func fallbackAttachmentName(att attachment, mime string) string {
	base := "attachment"
	switch {
	case strings.HasPrefix(mime, "image/png"):
		return base + ".png"
	case strings.HasPrefix(mime, "image/jpeg"), strings.HasPrefix(mime, "image/jpg"):
		return base + ".jpg"
	case strings.HasPrefix(mime, "image/webp"):
		return base + ".webp"
	case strings.HasPrefix(mime, "image/gif"):
		return base + ".gif"
	case strings.HasPrefix(mime, "application/pdf"):
		return base + ".pdf"
	case strings.Contains(mime, "zip"):
		return base + ".zip"
	default:
		if u := att.URL; u != "" {
			if i := strings.LastIndexByte(u, '/'); i >= 0 && i+1 < len(u) {
				tail := u[i+1:]
				if q := strings.IndexByte(tail, '?'); q >= 0 {
					tail = tail[:q]
				}
				if tail != "" {
					return tail
				}
			}
		}
		return base
	}
}

// newSafeHTTPClient builds an HTTP client that refuses private/link-local/
// loopback destinations and re-checks every redirect target.
func newSafeHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			var last error
			for _, ip := range ips {
				if isBlockedIP(ip.IP) {
					last = fmt.Errorf("blocked destination IP %s", ip.IP)
					continue
				}
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
				if err == nil {
					return conn, nil
				}
				last = err
			}
			if last == nil {
				last = fmt.Errorf("no usable address for %s", host)
			}
			return nil, last
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxInboundRedirects {
				return fmt.Errorf("too many redirects")
			}
			return validatePublicHTTPURL(req.URL.String())
		},
	}
}

// validatePublicHTTPURL rejects non-http(s) schemes and hosts that resolve
// only to private / link-local / loopback / unspecified addresses.
func validatePublicHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("bad url: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported url scheme %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("empty url host")
	}
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return fmt.Errorf("blocked destination IP %s", ip)
		}
		return nil
	}
	// Hostname: resolve and require at least one public address. Dial still
	// re-checks; this is a fast fail before the request leaves.
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", host, err)
	}
	public := 0
	for _, ip := range ips {
		if !isBlockedIP(ip) {
			public++
		}
	}
	if public == 0 {
		return fmt.Errorf("host %s has no public addresses", host)
	}
	return nil
}

func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	// Cloud metadata / CGNAT commonly abused for SSRF.
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 169 && ip4[1] == 254 { // link-local already covered; keep explicit
			return true
		}
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 { // RFC 6598 CGNAT
			return true
		}
	}
	return false
}

// isImageAttachment reports whether an attachment looks like an image.
func isImageAttachment(att attachment) bool {
	ct := strings.ToLower(att.ContentType)
	if strings.HasPrefix(ct, "image/") {
		return true
	}
	name := strings.ToLower(att.Filename + att.URL)
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp"} {
		if strings.Contains(name, ext) {
			return true
		}
	}
	return false
}
