package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/mcp"
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
		{"other text", "not-json-but-not-a-key", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateCodexLoginFile(tc.in)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err=%v want %v", err, tc.wantErr)
			}
			if tc.open && (err == nil || !strings.Contains(err.Error(), "OpenCode")) {
				t.Fatalf("API key error should mention OpenCode: %v", err)
			}
		})
	}
}

func TestPrepareAuthEnvCodexStripsKeyAndRequiresFile(t *testing.T) {
	_, err := PrepareAuthEnv(BackendCodex, map[string]string{"OPENAI_API_KEY": "sk-nope"}, "")
	if !errors.Is(err, ErrCodexLoginFileMissing) || !strings.Contains(err.Error(), "缺少 Codex 登录文件") {
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
		{BackendOpenCode, envauth.EnvOpenCodeAPIKey, "OPENCODE_API_KEY"},
	}
	for _, tc := range cases {
		out, err := MergeAuthEnv(tc.backend, map[string]string{tc.credKey: "cred"})
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

func TestDecideCodexWriteBack(t *testing.T) {
	if _, ok := DecideCodexWriteBack(errors.New("gone"), "x", chatgptLogin); ok {
		t.Fatal("read error must not write")
	}
	if _, ok := DecideCodexWriteBack(nil, chatgptLogin, chatgptLogin); ok {
		t.Fatal("unchanged body must not write")
	}
	next, ok := DecideCodexWriteBack(nil, chatgptLogin+"\n", chatgptLogin)
	if !ok || next != chatgptLogin+"\n" {
		t.Fatalf("changed body: ok=%v next=%q", ok, next)
	}
	if _, ok := ShouldWriteBackCodexLogin(nil, chatgptLogin+"\n", chatgptLogin, true); ok {
		t.Fatal("auth rejection must not write")
	}
	if _, ok := DecideCodexWriteBack(nil, "sk-new", chatgptLogin); ok {
		t.Fatal("API key body must not write")
	}
	if _, ok := DecideCodexWriteBack(nil, "   ", chatgptLogin); ok {
		t.Fatal("blank body must not write")
	}
}

func TestCodexAuthRejectedRewrite(t *testing.T) {
	if !CodexAuthRejected(errors.New("failed to refresh token"), nil) {
		t.Fatal("error text should reject")
	}
	if !CodexAuthRejected(nil, []models.AcpEvent{{Parts: []models.AcpPart{{Output: "invalid_grant"}}}}) {
		t.Fatal("event part should reject")
	}
	if got := RewriteCodexAuthError("could not refresh"); got != CodexLoginRepasteMessage {
		t.Fatalf("rewrite=%q", got)
	}
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
		ProfilesRoot:         root,
		ProjectIDForWorkflow: func(string) string { return "proj-1" },
		ProjectCredentialsForProject: func(string) map[string]string {
			return map[string]string{envauth.EnvCodexAuthFile: chatgptLogin}
		},
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
		p.forgetCodexLogin(codexLoginKey(req))
		removeHome(sp.ConfigHome)
	})
	body, err := os.ReadFile(filepath.Join(sp.ConfigHome, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != chatgptLogin {
		t.Fatalf("auth.json=%q", body)
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
		ProfilesRoot:         root,
		ProjectIDForWorkflow: func(string) string { return "proj-1" },
		ProjectCredentialsForProject: func(string) map[string]string {
			return map[string]string{}
		},
	}, BackendCodex).(*acpProvider)
	if _, err := missing.spec(req); !errors.Is(err, ErrCodexLoginFileMissing) {
		t.Fatalf("missing file with settings.json: %v", err)
	}
}
