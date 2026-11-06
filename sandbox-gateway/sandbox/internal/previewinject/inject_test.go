package previewinject

import (
	"strings"
	"testing"
)

const scriptURL = "http://app.example/preview-pick.js"

func TestInjectHTML_BeforeBody(t *testing.T) {
	t.Parallel()
	in := []byte("<html><body><h1>hi</h1></body></html>")
	got := string(InjectHTML(in, scriptURL))
	if !strings.Contains(got, `<script src="`+scriptURL+`"></script></body>`) {
		t.Fatalf("inject before </body>: %s", got)
	}
	if strings.Contains(got, "<base") {
		t.Fatalf("must not inject <base>: %s", got)
	}
}

func TestInjectHTML_BeforeHTML_NoBody(t *testing.T) {
	t.Parallel()
	in := []byte("<html><p>x</p></html>")
	got := string(InjectHTML(in, scriptURL))
	if !strings.Contains(got, `<script src="`+scriptURL+`"></script></html>`) {
		t.Fatalf("inject before </html>: %s", got)
	}
}

func TestInjectHTML_AppendFragment(t *testing.T) {
	t.Parallel()
	in := []byte("<p>only</p>")
	got := string(InjectHTML(in, scriptURL))
	if !strings.HasSuffix(got, `<script src="`+scriptURL+`"></script>`) {
		t.Fatalf("append: %s", got)
	}
}

func TestInjectHTML_IdempotentSameOrigin(t *testing.T) {
	t.Parallel()
	in := []byte(`<html><body><script src="` + ScriptPath + `"></script></body></html>`)
	got := InjectHTML(in, ScriptPath)
	if string(got) != string(in) {
		t.Fatalf("same-origin already present must be unchanged:\n%s", got)
	}
	if n := strings.Count(string(InjectHTML(InjectHTML([]byte("<body></body>"), ScriptPath), ScriptPath)), ScriptPath); n != 1 {
		t.Fatalf("double inject count=%d", n)
	}
}

func TestInjectHTML_StillInjectsWhenLocalhostTagExists(t *testing.T) {
	t.Parallel()
	in := []byte(`<html><body><script src="http://localhost:8080/preview-pick.js"></script></body></html>`)
	got := string(InjectHTML(in, ""))
	if !strings.Contains(got, `src="`+ScriptPath+`"`) {
		t.Fatalf("must add same-origin script beside unreachable localhost tag: %s", got)
	}
}

func TestInjectHTML_EmptyURLUsesSameOrigin(t *testing.T) {
	t.Parallel()
	got := string(InjectHTML([]byte("<body></body>"), "  "))
	if !strings.Contains(got, `src="`+ScriptPath+`"`) {
		t.Fatalf("empty url should default to ScriptPath: %s", got)
	}
}

func TestInjectHTML_EscapesURL(t *testing.T) {
	t.Parallel()
	got := string(InjectHTML([]byte("<body></body>"), `http://x/"onclick="alert(1)`))
	if strings.Contains(got, `onclick="alert`) {
		t.Fatalf("unescaped: %s", got)
	}
	if !strings.Contains(got, "&#34;") && !strings.Contains(got, "&quot;") {
		t.Fatalf("expected escaped quote: %s", got)
	}
}

func TestRewriteHTMLOnlyChangesCSPMetaElements(t *testing.T) {
	t.Parallel()
	unchanged := []string{
		`<meta charset="utf-8">`,
		`<meta name="description" content="http-equiv='content-security-policy'">`,
		`<!-- <meta http-equiv=content-security-policy content="style-src 'none'"> -->`,
		`<template><meta http-equiv=content-security-policy content="style-src 'none'"></template>`,
		`<script>const re = /<meta http-equiv="Content-Security-Policy">/.test('x');</script>`,
		`<textarea><meta http-equiv=content-security-policy content="style-src 'none'"></textarea>`,
	}
	meta := `<META data-note="a > b" HTTP-EQUIV=content-security&#45;policy content="style-src 'self'; object-src 'none'">`
	in := []byte("<head>" + strings.Join(unchanged, "") + meta + "</head><body></body>")
	csp := newPreviewCSP(ScriptPath, "http://127.0.0.1:3000", "", "test-nonce")
	got := string(scanHTML(in, "").rewrite(in, ScriptPath, csp))
	for _, value := range unchanged {
		if !strings.Contains(got, value) {
			t.Errorf("rewrote unrelated content %s in %s", value, got)
		}
	}
	if !strings.Contains(got, `style-src-elem &#39;self&#39; &#39;nonce-test-nonce&#39;`) ||
		!strings.Contains(got, `object-src &#39;none&#39;`) ||
		!strings.Contains(got, `<script src="/__grasp/preview-pick.js" nonce="test-nonce"></script></body>`) {
		t.Fatalf("expected scoped meta rewrite and injection: %s", got)
	}
}
