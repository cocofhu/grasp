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
	if _, err := ParsePreflight(map[string]any{
		"summary": "s", "confirmed": true,
		"fields": []any{map[string]any{"name": "a", "value": "b", "source": "nope"}},
	}); err == nil {
		t.Fatal("bad source should fail")
	}
	if _, err := ParsePreflight(map[string]any{"summary": "", "confirmed": true}); err == nil {
		t.Fatal("empty summary should fail")
	}
	doc2, err := ParsePreflight(map[string]any{
		"summary": "mixed", "confirmed": true, "unresolved": []any{"  "},
		"fields": []any{map[string]any{
			"name": "pw", "value": "s3cret!", "verification": "MIXED", "source": "CHAT", "notes": "attested",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if doc2.Fields[0].Verification != "mixed" || doc2.Fields[0].Source != "chat" || doc2.Unresolved != nil {
		t.Fatalf("norm: %+v", doc2)
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
	if PreflightIncomplete(`{"summary":"","confirmed":true}`) == "" {
		t.Fatal("empty summary incomplete")
	}
	if PreflightIncomplete(`{"summary":"s","confirmed":true,"fields":[{"name":"","value":"v"}]}`) == "" {
		t.Fatal("missing field name incomplete")
	}
	unconf := `{"summary":"wait","confirmed":false,"fields":[{"name":"k","value":"v","notes":"n","verification":"sandbox_probe","source":"choice"}],"unresolved":["need db"]}`
	md2 := RenderPreflightMarkdown(unconf)
	if !containsAll(md2, "未确认", "wait", "k", "v", "sandbox_probe", "choice", "n", "未决项") {
		t.Fatalf("unconfirmed render: %s", md2)
	}
	plain := RenderPreflightMarkdown(`{"summary":"s","confirmed":true,"fields":[{"name":"only","value":"x"}]}`)
	if !containsAll(plain, "only", "x", "已确认") {
		t.Fatalf("plain render: %s", plain)
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
