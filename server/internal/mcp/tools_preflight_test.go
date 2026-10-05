package mcp

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestToolAllowedFollowsCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name  string
		caps  *models.AgentCapabilities
		allow []string
		deny  []string
	}{
		{"preflight", capsPreflight,
			[]string{"set_preflight", "ask_form", "ask_question", "set_artifact_preview"},
			[]string{"set_clarified_requirement", "set_preview", "update_plan_status"}},
		{"clarify", capsClarify,
			[]string{"set_plan", "set_clarified_requirement", "set_research", "set_proposals", "ask_question", "set_artifact_preview", "set_preview"},
			[]string{"set_root_cause", "set_preflight", "ask_form", "set_test_result", "update_plan_status"}},
		{"clarify+root_cause", capsClarifyRootCause, []string{"set_root_cause"}, nil},
		{"implement", capsImplement,
			[]string{"set_implementation_result", "set_preview", "update_plan_status"},
			[]string{"set_plan", "ask_question", "set_review"}},
		{"nil", nil, nil, []string{"set_plan", "ask_question", "set_preview", "unknown_tool"}},
	} {
		for _, tool := range tc.allow {
			if !toolAllowed(tc.caps, tool) {
				t.Errorf("%s: %s should be allowed", tc.name, tool)
			}
		}
		for _, tool := range tc.deny {
			if toolAllowed(tc.caps, tool) {
				t.Errorf("%s: %s should be denied", tc.name, tool)
			}
		}
	}
}

func TestToolListedReviewAsk(t *testing.T) {
	if !toolListed(capsImplement, "ask_question") {
		t.Fatal("review Agents list ask_question for the review phase")
	}
	if toolListed(capsPlain, "ask_question") || toolListed(capsPlain, "set_plan") {
		t.Fatal("plain Agent must not list ungranted tools")
	}
	if !toolListed(capsPlain, "write_artifact") {
		t.Fatal("non-capability tools are always listed")
	}
}

func TestParseForms(t *testing.T) {
	forms := parseForms(map[string]any{
		"title": "Env",
		"fields": []any{
			map[string]any{"name": "db_url", "label": "数据库", "type": "url", "why": "plan gap"},
			map[string]any{"name": "password", "label": "密码", "type": "text"},
			map[string]any{"name": "bad", "label": "x", "type": "password"}, // rejected
			map[string]any{"name": "nolabel"},                               // rejected
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

	h.SetActiveNode(runID, "n", capsPlain)
	if _, isErr := toolText(t, call(t, h, runID, tok, tc(1, "ask_form", `{"fields":[{"name":"h","label":"主机","type":"text"}]}`))); !isErr {
		t.Fatal("ask_form on agent")
	}
	if _, isErr := toolText(t, call(t, h, runID, tok, tc(2, "set_preflight", `{"summary":"ok","confirmed":true}`))); !isErr {
		t.Fatal("set_preflight on agent")
	}
	if _, isErr := toolText(t, call(t, h, runID, "bad", tc(3, "ask_form", `{"fields":[{"name":"h","label":"主机"}]}`))); !isErr {
		t.Fatal("ask_form bad token")
	}

	h.SetActiveNode(runID, "pf", capsPreflight)
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
