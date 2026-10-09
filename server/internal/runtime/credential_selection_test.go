package runtime

import (
	"testing"

	"github.com/cocofhu/grasp/internal/envauth"
)

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

func TestMergeCodingCredentialIDsPriority(t *testing.T) {
	kind := func(id string) (string, bool) {
		switch id {
		case "shared-cursor":
			return envauth.EnvCursorAPIKey, true
		case "shared-oc":
			return envauth.EnvOpenCodeAPIKey, true
		case "gone":
			return "", false
		default:
			return "", false
		}
	}
	ai, oc := MergeCodingCredentialIDs("cursor", "agent-cursor", "", "shared-cursor", "shared-oc", kind)
	if ai != "agent-cursor" || oc != "" {
		t.Fatalf("agent should win ai=%q oc=%q", ai, oc)
	}
	ai, oc = MergeCodingCredentialIDs("cursor", "", "", "shared-cursor", "shared-oc", kind)
	if ai != "shared-cursor" || oc != "" {
		t.Fatalf("shared cursor fallback ai=%q oc=%q", ai, oc)
	}
	ai, oc = MergeCodingCredentialIDs("claude_code", "", "", "shared-cursor", "", kind)
	if ai != "" || oc != "" {
		t.Fatalf("kind mismatch ai=%q oc=%q", ai, oc)
	}
	ai, oc = MergeCodingCredentialIDs("cursor", "", "", "", "", kind)
	if ai != "" || oc != "" {
		t.Fatalf("both empty ai=%q oc=%q", ai, oc)
	}
	ai, oc = MergeCodingCredentialIDs("cursor", "", "", "gone", "", kind)
	if ai != "" {
		t.Fatalf("stale shared id used: %q", ai)
	}
	ai, oc = MergeCodingCredentialIDs("opencode", "agent-ai", "", "shared-cursor", "shared-oc", kind)
	if oc != "shared-oc" || ai != "agent-ai" {
		t.Fatalf("opencode slot ai=%q oc=%q", ai, oc)
	}
	ai, oc = MergeCodingCredentialIDs("opencode", "", "agent-oc", "shared-cursor", "shared-oc", kind)
	if oc != "agent-oc" {
		t.Fatalf("agent opencode lost: %q", oc)
	}
	ai, _ = MergeCodingCredentialIDs("cursor", "agent-cursor", "", "shared-cursor", "", nil)
	if ai != "agent-cursor" {
		t.Fatalf("nil kind dropped agent id: %q", ai)
	}
	ai, _ = MergeCodingCredentialIDs("cursor", "", "", "shared-cursor", "", nil)
	if ai != "" {
		t.Fatalf("nil kind copied shared id: %q", ai)
	}
}

func TestOverlayAgentFileUsesSameCredentialPriority(t *testing.T) {
	kind := func(id string) (string, bool) {
		if id == "shared-cursor" {
			return envauth.EnvCursorAPIKey, true
		}
		return "", false
	}
	got := overlayAgentFile(SharedAgentView{
		AcpBackend:     "cursor",
		AiCredentialID: "shared-cursor",
	}, agentFile{AiCredentialID: "agent-cursor"}, kind)
	if got.AiCredentialID != "agent-cursor" {
		t.Fatalf("overlay dropped agent id: %q", got.AiCredentialID)
	}
	got = overlayAgentFile(SharedAgentView{
		AcpBackend:     "cursor",
		AiCredentialID: "shared-cursor",
	}, agentFile{}, kind)
	if got.AiCredentialID != "shared-cursor" || got.AcpBackend != "cursor" {
		t.Fatalf("overlay = %+v", got)
	}
	wantAI, wantOC := MergeCodingCredentialIDs(got.AcpBackend, "", "", "shared-cursor", "", kind)
	if got.AiCredentialID != wantAI || got.OpenCodeCredentialID != wantOC {
		t.Fatalf("overlay %q %q != merge %q %q", got.AiCredentialID, got.OpenCodeCredentialID, wantAI, wantOC)
	}
}

func TestMismatchedOrEmptySharedIDKeepsEmptySelection(t *testing.T) {
	kind := func(id string) (string, bool) {
		if id == "shared-cursor" {
			return envauth.EnvCursorAPIKey, true
		}
		return "", false
	}
	ai, _ := MergeCodingCredentialIDs("claude_code", "", "", "shared-cursor", "", kind)
	env := map[string]string{envauth.EnvClaudeAPIKey: "only-claude"}
	creds := map[string]string{envauth.EnvClaudeAPIKey: "only-claude"}
	ApplyAgentCredentialChoice(env, creds, "p", AgentCredentialChoice{
		Backend:        "claude_code",
		AiCredentialID: ai,
	}, func(_, id string) (string, string, bool) {
		if id == "shared-cursor" {
			return envauth.EnvCursorAPIKey, "cursor-secret", true
		}
		return "", "", false
	})
	if env[envauth.EnvClaudeAPIKey] != "only-claude" {
		t.Fatalf("mismatched shared id cleared the only claude key: %v", env)
	}
	if _, ok := env[envauth.EnvCursorAPIKey]; ok {
		t.Fatal("cursor secret was injected into claude")
	}

	ai, _ = MergeCodingCredentialIDs("cursor", "", "", "gone", "", kind)
	stale := map[string]string{envauth.EnvCursorAPIKey: "only-cursor"}
	staleCreds := map[string]string{envauth.EnvCursorAPIKey: "only-cursor"}
	ApplyAgentCredentialChoice(stale, staleCreds, "p", AgentCredentialChoice{
		Backend:        "cursor",
		AiCredentialID: ai,
	}, func(_, _ string) (string, string, bool) { return "", "", false })
	if stale[envauth.EnvCursorAPIKey] != "only-cursor" {
		t.Fatalf("stale shared id cleared the single slot: %v", stale)
	}

	ai, _ = MergeCodingCredentialIDs("cursor", "", "", "", "", kind)
	many := map[string]string{envauth.EnvCursorAPIKey: "other-alias"}
	ApplyAgentCredentialChoice(many, map[string]string{}, "p", AgentCredentialChoice{
		Backend:        "cursor",
		AiCredentialID: ai,
	}, func(_, id string) (string, string, bool) {
		if id == "other" {
			return envauth.EnvCursorAPIKey, "other-alias", true
		}
		return "", "", false
	})
	if _, ok := many[envauth.EnvCursorAPIKey]; ok {
		t.Fatal("both-empty selection switched to another alias")
	}
}
