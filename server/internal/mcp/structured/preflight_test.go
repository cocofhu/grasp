package structured

import "testing"

func TestParsePreflight(t *testing.T) {
	doc, err := ParsePreflight(map[string]any{
		"summary":   "env ok",
		"confirmed": true,
		"fields": []any{
			map[string]any{"name": "db_host", "label": "DB", "value": "localhost", "verification": "sandbox_probe", "source": "form"},
			map[string]any{"name": "db_host", "value": "127.0.0.1"}, // last wins
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Fields) != 1 || doc.Fields[0].Value != "127.0.0.1" {
		t.Fatalf("dedupe: %+v", doc.Fields)
	}

	if _, err := ParsePreflight(map[string]any{"summary": "s", "confirmed": false}); err == nil {
		t.Fatal("confirmed false should fail")
	}
	if _, err := ParsePreflight(map[string]any{
		"summary": "s", "confirmed": true, "unresolved": []any{"need db"},
	}); err == nil {
		t.Fatal("unresolved nonempty should fail")
	}
	if _, err := ParsePreflight(map[string]any{
		"summary": "s", "confirmed": true, "fields": []any{},
	}); err != nil {
		t.Fatalf("empty fields OK: %v", err)
	}
	if _, err := ParsePreflight(map[string]any{
		"summary": "s", "confirmed": true,
		"fields": []any{map[string]any{"name": "x"}},
	}); err == nil {
		t.Fatal("missing value should fail")
	}
	if _, err := ParsePreflight(map[string]any{
		"summary": "s", "confirmed": true,
		"fields": []any{map[string]any{"value": "v"}},
	}); err == nil {
		t.Fatal("missing name should fail")
	}
	if _, err := ParsePreflight(map[string]any{
		"summary": "s", "confirmed": true,
		"fields": []any{map[string]any{"name": "a", "value": "b", "verification": "nope"}},
	}); err == nil {
		t.Fatal("bad verification should fail")
	}
	if _, err := ParsePreflight(map[string]any{"summary": "", "confirmed": true}); err == nil {
		t.Fatal("empty summary should fail")
	}
}

func TestPreflightIncompleteAndRender(t *testing.T) {
	if got := PreflightIncomplete(`{bad`); got == "" {
		t.Fatal("unparseable should be incomplete")
	}
	if got := PreflightIncomplete(`{"summary":"s","confirmed":false}`); got == "" {
		t.Fatal("unconfirmed incomplete")
	}
	if got := PreflightIncomplete(`{"summary":"s","confirmed":true,"unresolved":["x"]}`); got == "" {
		t.Fatal("unresolved incomplete")
	}
	ok := `{"summary":"ready","confirmed":true,"fields":[{"name":"k","value":"v"}]}`
	if PreflightIncomplete(ok) != "" {
		t.Fatal("valid should be complete")
	}
	md := RenderPreflightMarkdown(ok)
	if !containsAll(md, "环境确认", "ready", "k", "v") {
		t.Fatalf("render: %s", md)
	}
	if RenderPreflightMarkdown(`{bad`) != `{bad` {
		t.Fatal("raw on parse error")
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !stringContains(s, p) {
			return false
		}
	}
	return true
}

func stringContains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		})())
}
