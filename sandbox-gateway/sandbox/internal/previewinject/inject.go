// Package previewinject rewrites inbound HTML so Grasp can load
// preview-pick.js on IP-direct preview without changing the app.
package previewinject

import (
	"html"
	"regexp"
	"strings"
)

// ListenPort is the in-sandbox forwarder port. It sits below the preview
// pool (18080–18999) so KeepalivePort / set_preview never mistake this
// process for the app (the app still binds PREVIEW_PORT).
const ListenPort = 17980

// ScriptPath is the same-origin URL the forwarder serves and injects.
// Reviewers load the app at http://IP:PREVIEW_PORT/; a localhost Grasp
// URL is unreachable from their browser.
const ScriptPath = "/__grasp/preview-pick.js"

// PageControlPath serves the agent page-control executor; preview-pick.js
// loads it next to itself on demand.
const PageControlPath = "/__grasp/page-control.js"

// LiveOverlayPath serves the Live variants overlay; preview-pick.js loads it
// from its own directory once the drawer reports Live is on.
const LiveOverlayPath = "/__grasp/live-overlay.js"

var (
	reBodyClose      = regexp.MustCompile(`(?i)</body>`)
	reHTMLClose      = regexp.MustCompile(`(?i)</html>`)
	reSameOriginPick = regexp.MustCompile(`(?i)__grasp/preview-pick\.js`)
	reMetaCSP        = regexp.MustCompile(`(?is)<meta\b[^>]*\bhttp-equiv\s*=\s*["']?content-security-policy\b[^>]*>`)
)

// ResolveScriptURL returns scriptURL, or ScriptPath when empty.
func ResolveScriptURL(scriptURL string) string {
	if s := strings.TrimSpace(scriptURL); s != "" {
		return s
	}
	return ScriptPath
}

// AlreadyHasPickScript reports whether html already has the same-origin
// inject path. A leftover Agent tag like http://localhost:8080/preview-pick.js
// does not count — that URL is unreachable from the reviewer's browser.
func AlreadyHasPickScript(html []byte) bool {
	return reSameOriginPick.Match(html)
}

// StripMetaCSP removes <meta http-equiv="Content-Security-Policy"> tags.
// preview-pick.js needs inline styles, a same-origin fetch and frames on the
// Grasp origin, which is only known at runtime, so no allowlist can be added.
func StripMetaCSP(doc []byte) []byte {
	return reMetaCSP.ReplaceAll(doc, nil)
}

// InjectHTML inserts <script src="scriptURL"> before </body> (else </html>,
// else append). Empty scriptURL becomes ScriptPath. It is a no-op only when
// the document already has the same-origin ScriptPath tag. A leftover
// localhost Grasp URL does not count. Paths and <base> are left untouched.
func InjectHTML(doc []byte, scriptURL string) []byte {
	scriptURL = ResolveScriptURL(scriptURL)
	if AlreadyHasPickScript(doc) {
		return doc
	}
	tag := []byte(`<script src="` + html.EscapeString(scriptURL) + `"></script>`)
	if loc := reBodyClose.FindIndex(doc); loc != nil {
		return concat(doc[:loc[0]], tag, doc[loc[0]:])
	}
	if loc := reHTMLClose.FindIndex(doc); loc != nil {
		return concat(doc[:loc[0]], tag, doc[loc[0]:])
	}
	out := make([]byte, 0, len(doc)+len(tag))
	return append(append(out, doc...), tag...)
}

func concat(parts ...[]byte) []byte {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
