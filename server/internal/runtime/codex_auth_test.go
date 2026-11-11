package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
)

const chatgptLogin = `{"auth_mode":"chatgpt","tokens":{"id_token":"id","access_token":"at","refresh_token":"rt"}}`

func TestValidateCodexLoginFile(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr error
		open    bool
	}{
		{"blank", "", ErrCodexLoginFileEmpty, false},
		{"whitespace", "  \n\t", ErrCodexLoginFileEmpty, false},
		{"raw key", "sk-test", ErrCodexLoginFileAPIKey, true},
		{"auth mode", `{"auth_mode":"apikey","OPENAI_API_KEY":"sk-test"}`, ErrCodexLoginFileAPIKey, true},
		{"api_key mode", `{"auth_mode":"api_key"}`, ErrCodexLoginFileAPIKey, true},
		{"openai only", `{"OPENAI_API_KEY":"sk-live"}`, ErrCodexLoginFileAPIKey, true},
		{"chatgpt", chatgptLogin, nil, false},
		{"other text", "not-json-but-not-a-key", ErrCodexLoginFileInvalid, false},
		{"no refresh token", `{"auth_mode":"chatgpt","tokens":{"access_token":"at","refresh_token":""}}`, ErrCodexLoginFileNoRefreshToken, false},
		{"no tokens", `{"auth_mode":"chatgpt"}`, ErrCodexLoginFileNoRefreshToken, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateCodexLoginFile(tc.in)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err=%v want %v", err, tc.wantErr)
			}
			if tc.open && (err == nil || !strings.Contains(err.Error(), "Responses 模型连接")) {
				t.Fatalf("API key error should point to a Responses model connection: %v", err)
			}
		})
	}
}

func TestPrepareAuthEnvCodexStripsKeyAndRequiresFile(t *testing.T) {
	_, err := PrepareAuthEnv(BackendCodex, map[string]string{"OPENAI_API_KEY": "sk-nope"}, "")
	if !errors.Is(err, ErrCodexLoginFileMissing) || !strings.Contains(err.Error(), "缺少 Codex 登录") {
		t.Fatalf("missing file: %v", err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"env":{"OPENAI_API_KEY":"sk-from-settings"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareAuthEnv(BackendCodex, nil, dir); !errors.Is(err, ErrCodexLoginFileMissing) {
		t.Fatalf("settings.json must not skip the Codex gate: %v", err)
	}

	out, err := PrepareAuthEnv(BackendCodex, map[string]string{
		envauth.EnvCodexAuthFile: chatgptLogin,
		"OPENAI_API_KEY":         "sk-nope",
		"CODEX_API_KEY":          "sk-2",
		"ACP_CODEX_API_KEY":      "sk-3",
	}, dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{envauth.EnvCodexAuthFile, "OPENAI_API_KEY", "CODEX_API_KEY", "ACP_CODEX_API_KEY"} {
		if _, ok := out[k]; ok {
			t.Fatalf("env still has %s: %#v", k, out)
		}
	}
}

func TestMergeAuthEnvExistingBackendsUnchanged(t *testing.T) {
	cases := []struct {
		backend AcpBackend
		credKey string
		cliKey  string
	}{
		{BackendCursor, envauth.EnvCursorAPIKey, "CURSOR_API_KEY"},
		{BackendClaudeCode, envauth.EnvClaudeAPIKey, "ANTHROPIC_API_KEY"},
		{BackendCodeBuddy, envauth.EnvCodeBuddyAPIKey, "CODEBUDDY_API_KEY"},
		{BackendTrae, envauth.EnvTraeAPIKey, EnvTraeCLIToken},
	}
	for _, tc := range cases {
		out, err := mergeAuthEnv(tc.backend, map[string]string{tc.credKey: "cred"}, true)
		if err != nil {
			t.Fatal(err)
		}
		if out[tc.cliKey] != "cred" {
			t.Fatalf("%s mapped to %q", tc.backend, out[tc.cliKey])
		}
		if _, ok := out["OPENAI_API_KEY"]; ok && tc.backend != BackendOpenCode {
			t.Fatalf("%s unexpectedly set OPENAI_API_KEY", tc.backend)
		}
	}
}

func TestCodexAuthRejectionIgnoresRefreshMention(t *testing.T) {
	text := "Rotated the refresh token and kept the session"
	if IsCodexAuthRejectionText(text) || IsCodexAuthRejectionText("run codex login locally") {
		t.Fatal("success text must not be a login rejection")
	}
	events := []models.AcpEvent{{
		Kind: "message",
		Text: text,
	}, {
		Kind: models.AcpKindTimeline,
		Parts: []models.AcpPart{{
			Kind:   "tool",
			Text:   "codex login",
			Output: "Rotated the refresh token and kept the session",
		}},
	}}
	if CodexAuthRejected(nil, events) {
		t.Fatal("narration and tool output must not count as a login refusal")
	}
	fail := []models.AcpEvent{{
		Kind:   models.AcpKindTurnEnd,
		Status: "failed",
		Text:   "unexpected status 401 Unauthorized: Missing bearer, url: https://api.openai.com/v1/responses",
	}}
	if !CodexAuthRejected(nil, fail) {
		t.Fatal("turn failure 401 must reject")
	}
	if !CodexTurnAuthRejected(nil, raw401(), true) {
		t.Fatal("live turn error text must reject")
	}
	if CodexTurnAuthRejected(nil, text, false) {
		t.Fatal("successful live turn must not reject")
	}
}

func TestCodexRotationFailuresAreLoginRejections(t *testing.T) {
	for _, text := range []string{
		"Your access token could not be refreshed because your refresh token was already used. Please log out and sign in again.",
		"refresh_token_reused",
		"error: refresh_token_expired",
		"refresh_token_invalidated: revoked",
		"Your access token could not be refreshed. Please sign in again.",
		"Token data is not available.",
		"ChatGPT auth is missing a refresh token",
		"No more recovery steps available.",
		"Your access token could not be refreshed because you have since logged out or signed in to another account. Please sign in again.",
		// Observed from codex-cli 0.164 running a sandbox copy against a 401.
		"ERROR: Failed to refresh token: 400 Bad Request",
		"unexpected status 401 Unauthorized: {\"detail\":\"Unauthorized\"}, url: https://chatgpt.com/backend-api/codex/responses",
	} {
		if !CodexTurnAuthRejected(nil, text, true) {
			t.Errorf("%q must be a login refusal", text)
		}
		if CodexTurnAuthRejected(nil, "", false) {
			t.Fatal("empty successful turn")
		}
	}
	if CodexTurnAuthRejected(nil, "", false) || IsCodexAuthRejectionText("") {
		t.Fatal("empty text is not a refusal")
	}
	if IsCodexAuthRejectionText("unexpected status 401 Unauthorized, url: https://api.github.com/user") {
		t.Fatal("a 401 from an unrelated host is not a Codex login refusal")
	}
}

func raw401() string {
	return "unexpected status 401 Unauthorized: Missing bearer or basic authentication in header, url: https://api.openai.com/v1/responses"
}

func TestCodexSpecInstallsLoginFile(t *testing.T) {
	root := writeAgent(t, "dev", `{"acpBackend":"codex"}`)
	ws := filepath.Join(root, "dev", "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "settings.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	host := mcp.NewHost(newMemStore())
	p := newBaseACPProvider(host, Options{
		ProfilesRoot: root,
		ModelBinding: testModelBinding,
	}, BackendCodex).(*acpProvider)
	req := NodeReq{
		RunID: "run-codex-spec", NodeID: "n", WorkflowID: "wf", Token: "tkn",
		NodeType: "agent", Caps: testPlainCaps,
		Config: map[string]any{"agent_profile": "dev"},
	}
	sp, err := p.spec(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		p.unregisterCodexLogin(req)
		removeHome(sp.ConfigHome)
	})
	body, err := os.ReadFile(filepath.Join(sp.ConfigHome, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"rt"`) || !strings.Contains(string(body), `"access_token": "at"`) || !strings.Contains(string(body), `"refresh_token": ""`) {
		t.Fatalf("auth.json must be the sandbox copy without refresh token: %q", body)
	}
	if sp.ConfigRoot != CodexConfigRoot || sp.Env["CODEX_HOME"] != CodexConfigRoot {
		t.Fatalf("root=%q CODEX_HOME=%q", sp.ConfigRoot, sp.Env["CODEX_HOME"])
	}
	for _, k := range []string{"OPENAI_API_KEY", envauth.EnvCodexAuthFile, "CODEX_API_KEY", "ACP_CODEX_API_KEY"} {
		if _, ok := sp.Env[k]; ok {
			t.Fatalf("sandbox env has %s", k)
		}
	}
	info, err := os.Stat(filepath.Join(sp.ConfigHome, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}

	missing := newBaseACPProvider(host, Options{
		ProfilesRoot: root,
		ModelBinding: func(string, *modelrouting.Binding) (modelrouting.Resolved, error) {
			return modelrouting.Resolved{}, errors.New("selected model credential is unavailable")
		},
	}, BackendCodex).(*acpProvider)
	if _, err := missing.spec(req); err == nil || !strings.Contains(err.Error(), "selected model credential") {
		t.Fatalf("missing file with settings.json: %v", err)
	}
}
