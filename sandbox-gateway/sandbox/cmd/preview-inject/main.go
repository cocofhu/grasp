// preview-inject is the in-sandbox HTML injector for IP-direct preview.
// It listens on :17980 (not PREVIEW_PORT) and reverse-proxies to the app.
package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"backend/internal/previewinject"
)

type options struct {
	Listen    string
	Upstream  string
	ScriptURL string
}

func main() {
	opt, err := parseOptions(os.Args[1:], os.Getenv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "preview-inject: %v\n", err)
		os.Exit(2)
	}
	upstream, err := url.Parse(opt.Upstream)
	if err != nil || upstream.Host == "" {
		fmt.Fprintf(os.Stderr, "preview-inject: bad upstream %q\n", opt.Upstream)
		os.Exit(2)
	}
	fmt.Fprintf(os.Stderr, "preview-inject: listen %s → %s script %s\n", opt.Listen, opt.Upstream, opt.ScriptURL)
	h := previewinject.NewHandlerWithEmbed(upstream, opt.ScriptURL, previewinject.EmbedLookup{
		RunURL: os.Getenv("GRASP_ARTIFACT_URL"),
		Token:  os.Getenv("GRASP_ARTIFACT_TOKEN"),
	})
	if err := http.ListenAndServe(opt.Listen, h); err != nil {
		fmt.Fprintf(os.Stderr, "preview-inject: %v\n", err)
		os.Exit(1)
	}
}

func parseOptions(args []string, getenv func(string) string) (options, error) {
	fs := flag.NewFlagSet("preview-inject", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	listen := fs.String("listen", "", "listen address (default :17980)")
	upstream := fs.String("upstream", "", "upstream URL or host:port (default http://127.0.0.1:$PREVIEW_PORT)")
	script := fs.String("script-url", "", "script src to inject (default same-origin /__grasp/preview-pick.js)")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}

	opt := options{
		Listen:   firstNonEmpty(*listen, getenv("PREVIEW_INJECT_LISTEN"), fmt.Sprintf(":%d", previewinject.ListenPort)),
		Upstream: firstNonEmpty(*upstream, getenv("PREVIEW_INJECT_UPSTREAM")),
		// Always default to the same-origin path. PREVIEW_PICK_SCRIPT_URL is
		// for the Agent HTML fallback; Grasp often sets it to
		// http://localhost:8080/preview-pick.js, which the reviewer's
		// browser cannot load from http://IP:PREVIEW_PORT/.
		ScriptURL: firstNonEmpty(*script, previewinject.ScriptPath),
	}
	if opt.Upstream == "" {
		if p := strings.TrimSpace(getenv("PREVIEW_PORT")); p != "" {
			opt.Upstream = "http://127.0.0.1:" + p
		}
	}
	opt.Upstream = normalizeUpstream(opt.Upstream)
	if opt.Upstream == "" {
		return opt, fmt.Errorf("upstream required (--upstream, PREVIEW_INJECT_UPSTREAM, or PREVIEW_PORT)")
	}
	lp := listenPort(opt.Listen)
	up, err := strconv.Atoi(upstreamPort(opt.Upstream))
	if err == nil && lp > 0 && lp == up {
		return opt, fmt.Errorf("listen port %d must not equal upstream/PREVIEW_PORT (KeepalivePort would attach to the injector)", lp)
	}
	return opt, nil
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func normalizeUpstream(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		return "http://" + raw
	}
	return raw
}

func listenPort(addr string) int {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		// ":17980" is valid; SplitHostPort handles it.
		return 0
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return 0
	}
	return n
}

func upstreamPort(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	_, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		return ""
	}
	return port
}
