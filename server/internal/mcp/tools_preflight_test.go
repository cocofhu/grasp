package mcp

import "testing"

func TestToolAllowedPreflight(t *testing.T) {
	if !toolAllowed("preflight", "set_preflight") {
		t.Fatal("set_preflight on preflight")
	}
	if !toolAllowed("preflight", "ask_form") {
		t.Fatal("ask_form on preflight")
	}
	if !toolAllowed("preflight", "ask_question") {
		t.Fatal("ask_question on preflight")
	}
	if !toolAllowed("preflight", "set_artifact_preview") {
		t.Fatal("set_artifact_preview on preflight")
	}
	if toolAllowed("react", "set_preflight") || toolAllowed("react", "ask_form") {
		t.Fatal("preflight tools must not be allowed on react")
	}
	if toolAllowed("preflight", "set_clarified_requirement") {
		t.Fatal("set_clarified_requirement must not be allowed on preflight")
	}
	if toolAllowed("approve", "ask_form") {
		t.Fatal("ask_form must not be allowed on approve")
	}
}

func TestParseForms(t *testing.T) {
	forms := parseForms(map[string]any{
		"title": "Env",
		"fields": []any{
			map[string]any{"name": "db_url", "label": "数据库", "type": "url", "why": "plan gap"},
			map[string]any{"name": "password", "label": "密码", "type": "text"},
			map[string]any{"name": "bad", "label": "x", "type": "password"}, // rejected
			map[string]any{"name": "nolabel"}, // rejected
		},
	})
	if len(forms) != 1 || len(forms[0].Fields) != 2 {
		t.Fatalf("got %+v", forms)
	}
	if forms[0].Fields[0].Why != "plan gap" || forms[0].Fields[1].Type != "text" {
		t.Fatalf("fields: %+v", forms[0].Fields)
	}
	if parseForms(map[string]any{"fields": []any{}}) != nil {
		t.Fatal("empty fields")
	}
}
