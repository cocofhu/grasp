package runtime

import "testing"

func TestApplyAgentCredentialChoiceUsesOnlyTheSelectedRow(t *testing.T) {
	resolve := func(_, id string) (string, string, bool) {
		switch id {
		case "work":
			return "GRASP_CURSOR_API_KEY", "work-key", true
		case "gh":
			return "GITHUB_TOKEN", "gh-token", true
		default:
			return "", "", false
		}
	}
	env := map[string]string{"GRASP_CURSOR_API_KEY": "other", "GITHUB_TOKEN": "other"}
	creds := map[string]string{"GRASP_CURSOR_API_KEY": "other", "GITHUB_TOKEN": "other"}
	ApplyAgentCredentialChoice(env, creds, "p", AgentCredentialChoice{
		Backend:        "cursor",
		AiCredentialID: "work",
	}, resolve)
	if env["GRASP_CURSOR_API_KEY"] != "work-key" || creds["GRASP_CURSOR_API_KEY"] != "work-key" {
		t.Fatalf("selected cursor not applied env=%v creds=%v", env, creds)
	}
	if env["GITHUB_TOKEN"] != "other" {
		t.Fatal("unselected git key changed")
	}

	ApplyAgentCredentialChoice(env, creds, "p", AgentCredentialChoice{
		Backend:        "cursor",
		AiCredentialID: "missing",
	}, resolve)
	if _, ok := env["GRASP_CURSOR_API_KEY"]; ok {
		t.Fatal("missing selection fell back to another cursor key")
	}

	ApplyAgentCredentialChoice(env, creds, "p", AgentCredentialChoice{
		Backend:           "cursor",
		GitCredentialType: "github_https",
		GitCredentialID:   "gh",
	}, resolve)
	if env["GITHUB_TOKEN"] != "gh-token" {
		t.Fatalf("github=%q", env["GITHUB_TOKEN"])
	}
	if _, ok := env["GRASP_CURSOR_API_KEY"]; ok {
		t.Fatal("empty ai selection injected a cursor key")
	}
}

func TestEmptySelectionDropsKeyWhenProjectCredsOmitIt(t *testing.T) {
	resolve := func(_, _ string) (string, string, bool) { return "", "", false }
	env := map[string]string{"GRASP_CURSOR_API_KEY": "builtin-key"}
	ApplyAgentCredentialChoice(env, map[string]string{}, "p", AgentCredentialChoice{
		Backend: "cursor",
	}, resolve)
	if _, ok := env["GRASP_CURSOR_API_KEY"]; ok {
		t.Fatal("empty selection kept a cursor key that project credentials no longer provide")
	}

	env = map[string]string{"GRASP_CURSOR_API_KEY": "only-slot"}
	creds := map[string]string{"GRASP_CURSOR_API_KEY": "only-slot"}
	ApplyAgentCredentialChoice(env, creds, "p", AgentCredentialChoice{Backend: "cursor"}, resolve)
	if env["GRASP_CURSOR_API_KEY"] != "only-slot" {
		t.Fatalf("single slot was cleared: %v", env)
	}
}
