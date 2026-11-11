package previewinject

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"crypto/rand"
	_ "embed"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

//go:embed preview-pick.js
var pickScript []byte

//go:embed page-control.js
var pageControlScript []byte

//go:embed live-overlay.js
var liveOverlayScript []byte

var errUnsupportedEncoding = errors.New("unsupported content-encoding")

var cspAuthority = regexp.MustCompile(`^[A-Za-z0-9.-]+(:[0-9]{1,5})?$`)

type previewOriginKey struct{}

const flushInterval = 100 * time.Millisecond

// graspSessionCookie must match auth.CookieName in the Grasp server.
const graspSessionCookie = "cf_session"

// NewSwitchableHandler proxies to upstream, serving EmbedOriginPath for the
// chat drawer. Its app port can be switched at runtime: upstreamFile holds a port number (written by
// `preview-inject.sh set-upstream`). Without a usable file the fixed upstream
// serves. Every response names the port it came from in UpstreamHeader.
func NewSwitchableHandler(upstream *url.URL, upstreamFile, scriptURL string, embed EmbedLookup) http.Handler {
	if upstream == nil {
		panic("previewinject: nil upstream")
	}
	scriptURL = ResolveScriptURL(scriptURL)
	sel := newUpstreamSelector(upstream, upstreamFile)
	inject := modifyResponse(scriptURL, embed.frameSource())

	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			inHost := r.In.Host
			r.SetURL(sel.pick())
			r.Out.Host = inHost
			r.Out.Header.Set("Accept-Encoding", "identity")
			if r.In.Method == http.MethodGet && strings.Contains(r.In.Header.Get("Accept"), "text/html") {
				// Validators from an earlier injector version cannot revalidate
				// an HTML representation with a different CSP or nonce.
				r.Out.Header.Del("If-None-Match")
				r.Out.Header.Del("If-Modified-Since")
			}
			r.Out = r.Out.WithContext(context.WithValue(r.Out.Context(), previewOriginKey{}, browserOrigin(r.In)))
			stripGraspSessionCookie(r.Out.Header, "Cookie", ";")
		},
		ModifyResponse: func(resp *http.Response) error {
			stripGraspSessionCookie(resp.Header, "Set-Cookie", "")
			if resp.Request != nil {
				resp.Header.Set(UpstreamHeader, resp.Request.URL.Port())
			}
			return inject(resp)
		},
		FlushInterval: flushInterval,
		ErrorHandler:  closeOnUpstreamError,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == ScriptPath {
			serveScript(w, pickScript)
			return
		}
		if r.URL.Path == PageControlPath {
			serveScript(w, pageControlScript)
			return
		}
		if r.URL.Path == LiveOverlayPath {
			serveScript(w, liveOverlayScript)
			return
		}
		if r.URL.Path == EmbedOriginPath {
			embed.serve(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}

// Strip the Grasp cookie in both directions while leaving app cookies intact.
// Cookies are host scoped, so the browser can send it to a different port.
func stripGraspSessionCookie(header http.Header, field, separator string) {
	values := header.Values(field)
	header.Del(field)
	for _, value := range values {
		parts := []string{value}
		if separator != "" {
			parts = strings.Split(value, separator)
		}
		kept := parts[:0]
		for _, part := range parts {
			name, _, _ := strings.Cut(part, "=")
			if strings.TrimSpace(name) != graspSessionCookie {
				kept = append(kept, part)
			}
		}
		if len(kept) > 0 {
			header.Add(field, strings.TrimSpace(strings.Join(kept, separator)))
		}
	}
}

func serveScript(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func closeOnUpstreamError(w http.ResponseWriter, _ *http.Request, _ error) {
	if hj, ok := w.(http.Hijacker); ok {
		conn, _, err := hj.Hijack()
		if err == nil {
			_ = conn.Close()
			return
		}
	}
	w.WriteHeader(http.StatusBadGateway)
}

func browserOrigin(r *http.Request) string {
	// The platform's private preview proxy sends the browser authority
	// separately to keep the upstream Host compatible with app dev servers.
	if origin := cspOrigin(r.Header.Get("X-Forwarded-Proto"), r.Header.Get("X-Forwarded-Host")); origin != "" {
		return origin
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return cspOrigin(scheme, r.Host)
}

func cspOrigin(scheme, host string) string {
	if (scheme == "http" || scheme == "https") && cspAuthority.MatchString(host) {
		return scheme + "://" + host
	}
	return ""
}

func modifyResponse(scriptURL, frameSource string) func(*http.Response) error {
	return func(resp *http.Response) error {
		if resp.StatusCode < http.StatusOK || resp.StatusCode == http.StatusNoContent ||
			resp.StatusCode == http.StatusPartialContent || resp.StatusCode == http.StatusNotModified ||
			resp.Request != nil && resp.Request.Method == http.MethodHead {
			return nil
		}
		if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
			return nil
		}

		enc := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding")))
		if unsupportedEncoding(enc) {
			return nil
		}
		policies := resp.Header.Values(cspHeader)
		for _, value := range policies {
			for _, policy := range strings.Split(value, ",") {
				if cspBlocksInjection(policy, true) {
					return nil
				}
			}
		}

		body, err := readDecodableBody(resp)
		if err != nil {
			if errors.Is(err, errUnsupportedEncoding) {
				return nil
			}
			return err
		}

		var origin, nonce string
		if resp.Request != nil {
			origin, _ = resp.Request.Context().Value(previewOriginKey{}).(string)
		}
		page := scanHTML(body, origin)
		for _, meta := range page.policies {
			policy := meta.token.Attr[attribute(meta.token, "content")].Val
			if cspBlocksInjection(policy, false) {
				setDecodedBody(resp, body)
				return nil
			}
		}
		if (len(policies) > 0 || len(page.policies) > 0) &&
			(page.script == nil || attribute(page.script.token, "nonce") < 0) {
			nonce = rand.Text()
		}
		csp := newPreviewCSP(scriptURL, origin, frameSource, nonce)
		// Header.Values returns the original slice, so each policy stays in
		// its own header field and keeps its order.
		for i, policy := range policies {
			policies[i] = csp.rewritePolicies(policy)
		}
		injectURL := scriptURL
		if scriptURL == ScriptPath && origin != "" && nonce != "" {
			injectURL = origin + ScriptPath
		}
		rewritten := page.rewrite(body, injectURL, csp)

		setDecodedBody(resp, rewritten)
		if !bytes.Equal(body, rewritten) {
			resp.Header.Del("ETag")
			resp.Header.Del("Last-Modified")
		}
		if nonce != "" {
			resp.Header.Set("Cache-Control", "no-store")
		}
		return nil
	}
}

func setDecodedBody(resp *http.Response, body []byte) {
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
	resp.Header.Del("Content-Encoding")
}

func unsupportedEncoding(enc string) bool {
	switch enc {
	case "", "identity", "gzip", "x-gzip", "deflate":
		return false
	default:
		return true
	}
}

func readDecodableBody(resp *http.Response) ([]byte, error) {
	defer func() { _ = resp.Body.Close() }()
	enc := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding")))
	var r io.Reader = resp.Body
	switch enc {
	case "", "identity":
	case "gzip", "x-gzip":
		gr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer func() { _ = gr.Close() }()
		r = gr
	case "deflate":
		fr := flate.NewReader(resp.Body)
		defer func() { _ = fr.Close() }()
		r = fr
	default:
		return nil, errUnsupportedEncoding
	}
	return io.ReadAll(r)
}
