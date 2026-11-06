package previewinject

import (
	"net/url"
	"slices"
	"strings"
)

const cspHeader = "Content-Security-Policy"

var scriptDirectives = []string{"script-src-elem", "script-src", "default-src"}

type previewCSP struct {
	scripts []string
	lookup  string
	frames  string
	nonce   string
}

func newPreviewCSP(scriptURL, origin, framePath, nonce string) previewCSP {
	csp := previewCSP{frames: framePath, nonce: nonce}
	if origin != "" {
		csp.lookup = origin + EmbedOriginPath
	}
	u, err := url.Parse(scriptURL)
	if err != nil {
		return csp
	}
	scriptOrigin := origin
	if u.IsAbs() {
		scriptOrigin = cspOrigin(u.Scheme, u.Host)
	}
	path := u.EscapedPath()
	if scriptOrigin != "" && u.User == nil && u.Opaque == "" && !strings.HasPrefix(scriptURL, "//") &&
		strings.HasPrefix(path, "/") && !strings.HasSuffix(path, "/") && !strings.ContainsAny(path, ",;") {
		csp.scripts = append(csp.scripts, scriptOrigin+path)
		if path == ScriptPath {
			csp.scripts = append(csp.scripts, scriptOrigin+PageControlPath, scriptOrigin+LiveOverlayPath)
		} else if strings.HasSuffix(path, "/preview-pick.js") {
			dir := strings.TrimSuffix(path, "preview-pick.js")
			csp.scripts = append(csp.scripts, scriptOrigin+dir+"page-control.js", scriptOrigin+dir+"live-overlay.js")
		}
	}
	return csp
}

// The ticket's Grasp origin is browser-facing and can differ from the sandbox's
// configured lookup endpoint. Only the current run's embed routes need frames.
func (l EmbedLookup) frameSource() string {
	endpoint, _ := l.endpoint()
	if endpoint == "" {
		return ""
	}
	runID := strings.TrimSpace(l.RunID)
	if runID == "" {
		u, err := url.Parse(endpoint)
		if err != nil {
			return ""
		}
		_, runID, _ = strings.Cut(strings.TrimSuffix(u.EscapedPath(), "/embed-origin"), "/mcp/runs/")
	} else {
		runID = url.PathEscape(runID)
	}
	if runID == "" || runID == "." || runID == ".." || strings.ContainsAny(runID, "/,;") {
		return ""
	}
	return "http://*:*/embed/runs/" + runID + "/nodes/"
}

func cspFields(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f'
	})
}

func cspDirectives(policy string) ([]string, map[string]int) {
	directives := strings.Split(policy, ";")
	indices := make(map[string]int, len(directives))
	for i, directive := range directives {
		if fields := cspFields(directive); len(fields) > 0 {
			name := strings.ToLower(fields[0])
			if _, exists := indices[name]; !exists {
				indices[name] = i
			}
		}
	}
	return directives, indices
}

func cspBlocksInjection(policy string, header bool) bool {
	directives, indices := cspDirectives(policy)
	if _, sandbox := indices["sandbox"]; header && sandbox {
		return true
	}
	for _, name := range scriptDirectives {
		i, ok := indices[name]
		if !ok {
			continue
		}
		for _, source := range cspFields(directives[i])[1:] {
			if !strings.EqualFold(source, "'none'") {
				return false
			}
		}
		return true
	}
	return false
}

func (c previewCSP) rewritePolicies(value string) string {
	policies := strings.Split(value, ",")
	for i, policy := range policies {
		policies[i] = c.rewritePolicy(policy)
	}
	return strings.Join(policies, ",")
}

func (c previewCSP) rewritePolicy(policy string) string {
	directives, indices := cspDirectives(policy)
	// Copy the effective fallback into a more specific directive. Changing a
	// fallback would also change permissions for unrelated resources.
	extend := func(names []string, sources []string, skip func([]string) bool) {
		if len(sources) == 0 {
			return
		}
		for _, name := range names {
			i, ok := indices[name]
			if !ok {
				continue
			}
			fields := cspFields(directives[i])[1:]
			if skip != nil && skip(fields) {
				return
			}
			fields = slices.DeleteFunc(fields, func(s string) bool { return strings.EqualFold(s, "'none'") })
			added := false
			for _, source := range sources {
				if !slices.Contains(fields, source) {
					fields = append(fields, source)
					added = true
				}
			}
			if added {
				directive := names[0] + " " + strings.Join(fields, " ")
				if name == names[0] {
					directives[i] = directive
				} else {
					directives = append(directives, directive)
				}
			}
			return
		}
	}

	extend(scriptDirectives, c.scripts, keepScriptSources)
	if c.nonce != "" {
		extend([]string{"style-src-elem", "style-src", "default-src"}, []string{"'nonce-" + c.nonce + "'"}, allowsInlineStyles)
	}
	if c.lookup != "" {
		extend([]string{"connect-src", "default-src"}, []string{c.lookup}, nil)
	}
	if c.frames != "" {
		extend([]string{"frame-src", "child-src", "default-src"}, []string{c.frames}, nil)
	}
	return strings.Join(directives, ";")
}

func keepScriptSources(sources []string) bool {
	if len(sources) == 0 {
		return true
	}
	for _, source := range sources {
		if source = strings.ToLower(source); source == "'none'" || source == "'strict-dynamic'" ||
			strings.HasPrefix(source, "'nonce-") || strings.HasPrefix(source, "'sha") {
			return true
		}
	}
	return false
}

func allowsInlineStyles(sources []string) bool {
	inline := false
	for _, source := range sources {
		source = strings.ToLower(source)
		if strings.HasPrefix(source, "'nonce-") || strings.HasPrefix(source, "'sha") {
			return false
		}
		inline = inline || source == "'unsafe-inline'"
	}
	return inline
}
