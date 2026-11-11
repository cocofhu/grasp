// Package previewinject rewrites inbound HTML so Grasp can load
// preview-pick.js on IP-direct preview without changing the app.
package previewinject

import (
	"bytes"
	stdhtml "html"
	"io"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
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

// ResolveScriptURL returns scriptURL, or ScriptPath when empty.
func ResolveScriptURL(scriptURL string) string {
	if s := strings.TrimSpace(scriptURL); s != "" {
		return s
	}
	return ScriptPath
}

type htmlTag struct {
	start, end int
	token      html.Token
}

type previewHTML struct {
	insertAt int
	script   *htmlTag
	policies []htmlTag
}

func scanHTML(doc []byte, origin string) previewHTML {
	page := previewHTML{insertAt: len(doc)}
	tokens := html.NewTokenizer(bytes.NewReader(doc))
	offset, templates := 0, 0
	for kind := tokens.Next(); kind != html.ErrorToken; kind = tokens.Next() {
		start := offset
		offset += len(tokens.Raw())
		switch kind {
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokens.Token()
			if token.DataAtom == atom.Template {
				templates++
			}
			if templates > 0 {
				continue
			}
			switch token.DataAtom {
			case atom.Meta:
				equiv := attribute(token, "http-equiv")
				if equiv >= 0 && strings.EqualFold(token.Attr[equiv].Val, cspHeader) && attribute(token, "content") >= 0 {
					page.policies = append(page.policies, htmlTag{start, offset, token})
				}
			case atom.Script:
				src := attribute(token, "src")
				if page.script == nil && src >= 0 {
					path := strings.TrimSpace(token.Attr[src].Val)
					path, _, _ = strings.Cut(path, "?")
					path, _, _ = strings.Cut(path, "#")
					if path == ScriptPath || path == origin+ScriptPath {
						page.script = &htmlTag{start, offset, token}
					}
				}
			}
		case html.EndTagToken:
			switch tokens.Token().DataAtom {
			case atom.Template:
				if templates > 0 {
					templates--
				}
			case atom.Body, atom.Html:
				if templates == 0 && page.insertAt == len(doc) {
					page.insertAt = start
				}
			}
		}
	}
	// An incomplete tag is still in the source. Keep the insertion ahead of
	// those bytes so they cannot absorb the injected script.
	if tokens.Err() == io.EOF && page.insertAt == len(doc) {
		page.insertAt = offset
	}
	return page
}

func attribute(token html.Token, key string) int {
	for i, attr := range token.Attr {
		if attr.Key == key {
			return i
		}
	}
	return -1
}

func (p previewHTML) rewrite(doc []byte, scriptURL string, csp previewCSP) []byte {
	var out bytes.Buffer
	at := 0
	write := func(start, end int, text string) {
		out.Write(doc[at:start])
		out.WriteString(text)
		at = end
	}
	writeScript := func() {
		if p.script == nil {
			tag := `<script src="` + stdhtml.EscapeString(scriptURL) + `"`
			if csp.nonce != "" {
				tag += ` nonce="` + csp.nonce + `"`
			}
			write(p.insertAt, p.insertAt, tag+`></script>`)
		} else if csp.nonce != "" && attribute(p.script.token, "nonce") < 0 {
			token := p.script.token
			token.Attr = append(token.Attr, html.Attribute{Key: "nonce", Val: csp.nonce})
			write(p.script.start, p.script.end, token.String())
		}
	}
	scriptAt := p.insertAt
	if p.script != nil {
		scriptAt = p.script.start
	}
	scriptWritten := false
	for _, meta := range p.policies {
		if !scriptWritten && scriptAt < meta.start {
			writeScript()
			scriptWritten = true
		}
		token := meta.token
		content := attribute(token, "content")
		old := token.Attr[content].Val
		if policy := csp.rewritePolicy(old); policy != old {
			token.Attr[content].Val = policy
			write(meta.start, meta.end, token.String())
		}
	}
	if !scriptWritten {
		writeScript()
	}
	out.Write(doc[at:])
	return out.Bytes()
}
