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
	if toolAllowed("approve", "ask_form") || toolAllowed("grasp", "ask_form") {
		t.Fatal("ask_form must not be allowed on grasp/approve")
	}
}

func TestToolAllowedGraspAlias(t *testing.T) {
	for _, typ := range []string{"grasp", "approve"} {
		if !toolAllowed(typ, "set_plan") || !toolAllowed(typ, "set_clarified_requirement") {
			t.Fatalf("set_plan/set_clarified_requirement must allow %s", typ)
		}
		if !toolAllowed(typ, "set_research") || !toolAllowed(typ, "set_proposals") {
			t.Fatalf("optional set_* must allow %s", typ)
		}
		if !toolAllowed(typ, "set_root_cause") {
			t.Fatalf("set_root_cause must allow %s", typ)
		}
		if !toolAllowed(typ, "ask_question") || !toolAllowed(typ, "set_artifact_preview") {
			t.Fatalf("ask_question/set_artifact_preview must allow %s", typ)
		}
	}
	if toolAllowed("human_gate", "set_plan") || toolAllowed("react", "set_plan") {
		t.Fatal("set_plan must stay blocked on human_gate/react")
	}
}

func TestParseForms(t *testing.T) {
	forms := parseForms(map[string]any{
		"title": "Env",
		"fields": []any{
			map[string]any{"name": "db_url", "label": "数据库", "type": "url", "why": "plan gap"},
			map[string]any{"name": "password", "label": "密码", "type": "text"},
			map[string]any{"name": "bad", "label": "x", "type": "password"}, // rejected
			map[string]any{"name": "nolabel"},                              // rejected
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
	if parseForms(nil) != nil {
		t.Fatal("nil args")
	}

	multi := parseForms(map[string]any{
		"forms": []any{
			map[string]any{
				"title": "A",
				"fields": []any{
					map[string]any{"name": "host", "label": "主机", "type": "", "placeholder": "h", "value": "localhost", "required": true, "reason": "from plan"},
					"skip-me",
				},
			},
			map[string]any{"title": "empty"},
			"not-object",
		},
	})
	if len(multi) != 1 || len(multi[0].Fields) != 1 {
		t.Fatalf("forms[]: %+v", multi)
	}
	f := multi[0].Fields[0]
	if f.Type != "text" || f.Placeholder != "h" || f.Value != "localhost" || !f.Required || f.Why != "from plan" {
		t.Fatalf("field extras: %+v", f)
	}
	if asBool("true") != true || asBool("1") != true || asBool("no") || asBool(1) {
		t.Fatal("asBool branches")
	}
}

func TestPreflightToolsDispatch(t *testing.T) {
	store := &memStore{}
	h := NewHost(store)
	runID := "r"
	tok := h.RegisterRun(runID)
	tc := func(id int, name, argsJSON string) string {
		return `{"jsonrpc":"2.0","id":` + itoa(id) + `,"method":"tools/call","params":{"name":"` + name + `","arguments":` + argsJSON + `}}`
	}

	h.SetActiveNode(runID, "n", "agent")
	if _, isErr := toolText(t, call(t, h, runID, tok, tc(1, "ask_form", `{"fields":[{"name":"h","label":"主机","type":"text"}]}`))); !isErr {
		t.Fatal("ask_form on agent")
	}
	if _, isErr := toolText(t, call(t, h, runID, tok, tc(2, "set_preflight", `{"summary":"ok","confirmed":true}`))); !isErr {
		t.Fatal("set_preflight on agent")
	}
	if _, isErr := toolText(t, call(t, h, runID, "bad", tc(3, "ask_form", `{"fields":[{"name":"h","label":"主机"}]}`))); !isErr {
		t.Fatal("ask_form bad token")
	}

	h.SetActiveNode(runID, "pf", "preflight")
	if _, isErr := toolText(t, call(t, h, runID, tok, tc(4, "ask_form", `{"fields":[]}`))); !isErr {
		t.Fatal("ask_form empty fields")
	}
	txt, isErr := toolText(t, call(t, h, runID, tok, tc(5, "ask_form", `{"title":"Env","fields":[{"name":"db_url","label":"库地址","type":"url","why":"plan"}]}`)))
	if isErr || !containsStr(txt, "已记录表单") {
		t.Fatalf("ask_form ok: %q err=%v", txt, isErr)
	}
	forms := h.TakePendingForms(runID, "pf")
	if len(forms) != 1 || len(forms[0].Fields) != 1 || forms[0].Fields[0].Name != "db_url" {
		t.Fatalf("pending forms: %+v", forms)
	}
	if h.TakePendingForms(runID, "pf") != nil && len(h.TakePendingForms(runID, "missing")) != 0 {
		t.Fatal("take should clear")
	}
	if h.TakePendingForms("no-run", "pf") != nil {
		t.Fatal("unknown run")
	}

	if _, isErr := toolText(t, call(t, h, runID, tok, tc(6, "ask_question", `{"questions":[{"prompt":"用哪套?","options":["A","B"]}]}`))); isErr {
		t.Fatal("ask_question on preflight")
	}

	if _, isErr := toolText(t, call(t, h, runID, tok, tc(7, "write_artifact", `{"name":"preflight.json","content":"{}"}`))); !isErr {
		t.Fatal("write_artifact must not forge preflight.json")
	}

	txt, isErr = toolText(t, call(t, h, runID, tok, tc(8, "set_preflight", `{"summary":"ready","confirmed":true,"fields":[{"name":"db_host","label":"DB","value":"localhost","verification":"user_attested","source":"form","notes":"ok","verified":true}]}`)))
	if isErr || !containsStr(txt, "已写入环境确认") {
		t.Fatalf("set_preflight: %q err=%v", txt, isErr)
	}
	if _, ok := store.Get(runID, PreflightArtifactName); !ok {
		t.Fatal("preflight.json not written")
	}
	got, isErr := toolText(t, call(t, h, runID, tok, tc(9, "get_preflight", `{}`)))
	if isErr || !containsStr(got, "localhost") {
		t.Fatalf("get_preflight: %q err=%v", got, isErr)
	}
	if _, isErr := toolText(t, call(t, h, runID, tok, tc(10, "set_preflight", `{"summary":"","confirmed":true}`))); !isErr {
		t.Fatal("set_preflight empty summary")
	}

	if toolDeniedMsg("set_preflight") == "" || toolDeniedMsg("ask_form") == "" || toolDeniedMsg("other") == "" {
		t.Fatal("toolDeniedMsg")
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(sub) > 0 && indexOf(s, sub) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
