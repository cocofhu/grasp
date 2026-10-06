package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/envauth"
)

func TestNormalizeBackend(t *testing.T) {
	cases := []struct {
		in   string
		want AcpBackend
	}{
		{"cursor", BackendCursor},
		{"claude_code", BackendClaudeCode},
		{"codebuddy", BackendCodeBuddy},
		{"trae", BackendTrae},
		{"opencode", BackendOpenCode},
		{" opencode ", BackendOpenCode},
		{"", ""},
		{" unknown ", ""},
		{"CURSOR", ""}, // case-sensitive
	}
	for _, tc := range cases {
		if got := NormalizeBackend(tc.in); got != tc.want {
			t.Fatalf("NormalizeBackend(%q)=%q want %q", tc.in, got, tc.want)
		}
		got, err := ParseBackend(tc.in)
		if got != tc.want || (err == nil) != (tc.want != "") {
			t.Fatalf("ParseBackend(%q)=%q,%v want %q", tc.in, got, err, tc.want)
		}
	}
}

func TestDefaultConfigRootAndResolve(t *testing.T) {
	if got := DefaultConfigRoot(BackendCodeBuddy); got != "/root/.codebuddy" {
		t.Fatalf("codebuddy root=%q", got)
	}
	if got := DefaultConfigRoot(BackendTrae); got != "/root/.trae" {
		t.Fatalf("trae root=%q", got)
	}
	if got := DefaultConfigRoot(BackendClaudeCode); got != "/root/.claude" {
		t.Fatalf("claude root=%q", got)
	}
	if got := DefaultConfigRoot(BackendOpenCode); got != "/root/.config/opencode" {
		t.Fatalf("opencode root=%q", got)
	}
	if got := ResolveConfigRoot(BackendTrae, "  /custom  "); got != "/custom" {
		t.Fatalf("explicit root=%q", got)
	}
	if got := ResolveConfigRoot(BackendTrae, ""); got != "/root/.trae" {
		t.Fatalf("default root=%q", got)
	}
}

func TestAgentRuntimeLabel(t *testing.T) {
	cases := map[AcpBackend]string{
		BackendCursor:     "cursor-agent",
		BackendClaudeCode: "claude-code-acp",
		BackendCodeBuddy:  "codebuddy-acp",
		BackendTrae:       "trae-acp",
		BackendOpenCode:   "opencode-json",
	}
	for b, want := range cases {
		if got := AgentRuntimeLabel(b); got != want {
			t.Fatalf("label(%s)=%q want %q", b, got, want)
		}
	}
}

func TestMergeAuthEnv_CredentialKeyOnly(t *testing.T) {
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
		t.Run(string(tc.backend), func(t *testing.T) {
			out, err := MergeAuthEnv(tc.backend, map[string]string{tc.credKey: "cred"})
			if err != nil {
				t.Fatal(err)
			}
			if out[tc.cliKey] != "cred" {
				t.Fatalf("%s=%q want cred (env=%#v)", tc.cliKey, out[tc.cliKey], out)
			}
			if _, err := MergeAuthEnv(tc.backend, map[string]string{tc.cliKey: "cli"}); err == nil {
				t.Fatalf("%s alone must not satisfy auth", tc.cliKey)
			}
		})
	}
	if _, err := MergeAuthEnv(BackendTrae, map[string]string{"TRAE_API_KEY": "legacy"}); err == nil {
		t.Fatal("TRAE_API_KEY must not satisfy auth")
	}
}

func TestMergeAuthEnv_MissingKey(t *testing.T) {
	for _, b := range []AcpBackend{BackendCursor, BackendClaudeCode, BackendCodeBuddy, BackendTrae, BackendOpenCode} {
		t.Run(string(b), func(t *testing.T) {
			_, err := MergeAuthEnv(b, map[string]string{})
			if err == nil {
				t.Fatal("expected error when key missing")
			}
			if !strings.Contains(err.Error(), "鉴权未配置") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestMergeAuthEnv_CursorNoRegionMutation(t *testing.T) {
	out, err := MergeAuthEnv(BackendCursor, map[string]string{
		"GRASP_CURSOR_API_KEY": "ck",
		EnvCodeBuddyRegion:     "staging",
		EnvTraeRegion:          "intl",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out[EnvCodeBuddyInternet] != "" {
		t.Fatalf("cursor must not set codebuddy internet: %q", out[EnvCodeBuddyInternet])
	}
	if out[EnvTraeCLIHost] != "" {
		t.Fatalf("cursor must not set trae host: %q", out[EnvTraeCLIHost])
	}
	if CodeBuddySettingsForEnv(BackendCursor, out) != nil {
		t.Fatal("cursor must not produce codebuddy settings")
	}
}

func TestMergeRegionEnv_CodeBuddy(t *testing.T) {
	t.Run("default public", func(t *testing.T) {
		out, err := MergeAuthEnv(BackendCodeBuddy, map[string]string{
			"GRASP_CODEBUDDY_API_KEY": "ck_x",
		})
		if err != nil {
			t.Fatal(err)
		}
		if out[EnvCodeBuddyInternet] != "public" {
			t.Fatalf("internet=%q want public", out[EnvCodeBuddyInternet])
		}
		if out[EnvCodeBuddyBaseURL] != "" {
			t.Fatalf("unexpected base url %q", out[EnvCodeBuddyBaseURL])
		}
	})
	t.Run("internal via region alias", func(t *testing.T) {
		for _, alias := range []string{"cn", "china", "internal"} {
			out, err := MergeAuthEnv(BackendCodeBuddy, map[string]string{
				"GRASP_CODEBUDDY_API_KEY": "ck_x",
				EnvCodeBuddyRegion:        alias,
			})
			if err != nil {
				t.Fatal(err)
			}
			if out[EnvCodeBuddyInternet] != "internal" {
				t.Fatalf("alias %q → internet=%q want internal", alias, out[EnvCodeBuddyInternet])
			}
		}
	})
	t.Run("intl aliases map to public", func(t *testing.T) {
		for _, alias := range []string{"public", "intl", "international"} {
			out, err := MergeAuthEnv(BackendCodeBuddy, map[string]string{
				"GRASP_CODEBUDDY_API_KEY": "ck_x",
				EnvCodeBuddyRegion:        alias,
			})
			if err != nil {
				t.Fatal(err)
			}
			if out[EnvCodeBuddyInternet] != "public" {
				t.Fatalf("alias %q → internet=%q want public", alias, out[EnvCodeBuddyInternet])
			}
		}
	})
	t.Run("ioa via region", func(t *testing.T) {
		out, err := MergeAuthEnv(BackendCodeBuddy, map[string]string{
			"GRASP_CODEBUDDY_API_KEY": "ck_x",
			EnvCodeBuddyRegion:        "ioa",
		})
		if err != nil {
			t.Fatal(err)
		}
		if out[EnvCodeBuddyInternet] != "ioa" {
			t.Fatalf("internet=%q want ioa", out[EnvCodeBuddyInternet])
		}
	})
	t.Run("internet-only without region", func(t *testing.T) {
		out, err := MergeAuthEnv(BackendCodeBuddy, map[string]string{
			"GRASP_CODEBUDDY_API_KEY": "ck_x",
			EnvCodeBuddyInternet:      "internal",
		})
		if err != nil {
			t.Fatal(err)
		}
		if out[EnvCodeBuddyInternet] != "internal" {
			t.Fatalf("internet=%q want internal", out[EnvCodeBuddyInternet])
		}
		if CodeBuddySettingsForEnv(BackendCodeBuddy, out) != nil {
			t.Fatal("non-staging must not write settings")
		}
	})
	t.Run("unknown region passthrough", func(t *testing.T) {
		out, err := MergeAuthEnv(BackendCodeBuddy, map[string]string{
			"GRASP_CODEBUDDY_API_KEY": "ck_x",
			EnvCodeBuddyRegion:        "custom-site",
		})
		if err != nil {
			t.Fatal(err)
		}
		if out[EnvCodeBuddyInternet] != "custom-site" {
			t.Fatalf("internet=%q want custom-site", out[EnvCodeBuddyInternet])
		}
	})
	t.Run("staging sets region for settings.json", func(t *testing.T) {
		out, err := MergeAuthEnv(BackendCodeBuddy, map[string]string{
			"GRASP_CODEBUDDY_API_KEY": "ck_x",
			EnvCodeBuddyRegion:        "staging",
		})
		if err != nil {
			t.Fatal(err)
		}
		if out[EnvCodeBuddyInternet] != "public" {
			t.Fatalf("internet=%q want public", out[EnvCodeBuddyInternet])
		}
		if out[EnvCodeBuddyRegion] != "staging" {
			t.Fatalf("region=%q want staging", out[EnvCodeBuddyRegion])
		}
		if out[EnvCodeBuddyBaseURL] != "" {
			t.Fatalf("staging must not invent BASE_URL, got %q", out[EnvCodeBuddyBaseURL])
		}
		settings := CodeBuddySettingsForEnv(BackendCodeBuddy, out)
		if settings == nil {
			t.Fatal("expected settings for staging")
		}
		if settings["endpoint"] != CodeBuddyStagingEndpoint {
			t.Fatalf("endpoint=%v", settings["endpoint"])
		}
		if settings["envRouteMode"] != "staging" {
			t.Fatalf("envRouteMode=%v", settings["envRouteMode"])
		}
		envNested, _ := settings["env"].(map[string]string)
		if envNested[EnvCodeBuddyInternet] != "public" {
			t.Fatalf("settings.env=%v", settings["env"])
		}
	})
	t.Run("staging keeps custom BASE_URL for settings endpoint", func(t *testing.T) {
		custom := "https://staging.example.com"
		out, err := MergeAuthEnv(BackendCodeBuddy, map[string]string{
			"GRASP_CODEBUDDY_API_KEY": "ck_x",
			EnvCodeBuddyRegion:        "staging",
			EnvCodeBuddyBaseURL:       custom,
		})
		if err != nil {
			t.Fatal(err)
		}
		if out[EnvCodeBuddyBaseURL] != custom {
			t.Fatalf("base url mutated: %q", out[EnvCodeBuddyBaseURL])
		}
		settings := CodeBuddySettingsForEnv(BackendCodeBuddy, out)
		if settings == nil || settings["endpoint"] != custom {
			t.Fatalf("settings=%v want endpoint %q", settings, custom)
		}
	})
	t.Run("explicit official vars win", func(t *testing.T) {
		out, err := MergeAuthEnv(BackendCodeBuddy, map[string]string{
			"GRASP_CODEBUDDY_API_KEY": "ck_x",
			EnvCodeBuddyRegion:        "cn",
			EnvCodeBuddyInternet:      "ioa",
		})
		if err != nil {
			t.Fatal(err)
		}
		if out[EnvCodeBuddyInternet] != "ioa" {
			t.Fatalf("internet=%q want ioa (explicit)", out[EnvCodeBuddyInternet])
		}
		if CodeBuddySettingsForEnv(BackendCodeBuddy, out) != nil {
			t.Fatal("cn should not write staging settings")
		}
	})
}

func TestCodeBuddySettingsForEnv(t *testing.T) {
	staging := map[string]string{EnvCodeBuddyRegion: "staging"}
	if CodeBuddySettingsForEnv(BackendCodeBuddy, nil) != nil {
		t.Fatal("nil env")
	}
	if CodeBuddySettingsForEnv(BackendCodeBuddy, map[string]string{EnvCodeBuddyRegion: "public"}) != nil {
		t.Fatal("public must not write settings")
	}
	if CodeBuddySettingsForEnv(BackendCodeBuddy, map[string]string{EnvCodeBuddyRegion: "STAGING"}) == nil {
		t.Fatal("region compare is case-insensitive")
	}
	// Stray CodeBuddy REGION on other backends must not materialize settings.json.
	for _, b := range []AcpBackend{BackendCursor, BackendClaudeCode, BackendTrae, BackendOpenCode} {
		if CodeBuddySettingsForEnv(b, staging) != nil {
			t.Fatalf("backend %s must ignore codebuddy staging region", b)
		}
	}
}

func TestMergeRegionEnv_Trae(t *testing.T) {
	t.Run("cn default no host", func(t *testing.T) {
		out, err := MergeAuthEnv(BackendTrae, map[string]string{
			"GRASP_TRAE_API_KEY": "trae-lt-x",
		})
		if err != nil {
			t.Fatal(err)
		}
		if out[EnvTraeCLIHost] != "" {
			t.Fatalf("unexpected host %q", out[EnvTraeCLIHost])
		}
	})
	t.Run("cn aliases leave host unset", func(t *testing.T) {
		for _, alias := range []string{"cn", "china", "internal"} {
			out, err := MergeAuthEnv(BackendTrae, map[string]string{
				"GRASP_TRAE_API_KEY": "trae-lt-x",
				EnvTraeRegion:        alias,
			})
			if err != nil {
				t.Fatal(err)
			}
			if out[EnvTraeCLIHost] != "" {
				t.Fatalf("alias %q set host %q", alias, out[EnvTraeCLIHost])
			}
		}
	})
	t.Run("intl sets host", func(t *testing.T) {
		for _, alias := range []string{"intl", "international", "public", "ai"} {
			out, err := MergeAuthEnv(BackendTrae, map[string]string{
				"GRASP_TRAE_API_KEY": "trae-lt-x",
				EnvTraeRegion:        alias,
			})
			if err != nil {
				t.Fatal(err)
			}
			if out[EnvTraeCLIHost] != TraeIntlHost {
				t.Fatalf("alias %q host=%q want %q", alias, out[EnvTraeCLIHost], TraeIntlHost)
			}
			if out[EnvTraeCLIToken] != "trae-lt-x" {
				t.Fatalf("token not mapped: %q", out[EnvTraeCLIToken])
			}
		}
	})
	t.Run("unknown region no host mutation", func(t *testing.T) {
		out, err := MergeAuthEnv(BackendTrae, map[string]string{
			"GRASP_TRAE_API_KEY": "trae-lt-x",
			EnvTraeRegion:        "corp-custom",
		})
		if err != nil {
			t.Fatal(err)
		}
		if out[EnvTraeCLIHost] != "" {
			t.Fatalf("unknown region must not invent host, got %q", out[EnvTraeCLIHost])
		}
	})
	t.Run("explicit host wins", func(t *testing.T) {
		out, err := MergeAuthEnv(BackendTrae, map[string]string{
			"GRASP_TRAE_API_KEY": "trae-lt-x",
			EnvTraeRegion:        "intl",
			EnvTraeCLIHost:       "https://corp.example",
		})
		if err != nil {
			t.Fatal(err)
		}
		if out[EnvTraeCLIHost] != "https://corp.example" {
			t.Fatalf("host=%q want corp", out[EnvTraeCLIHost])
		}
	})
}

func TestIsPlatformAuthEnvKey(t *testing.T) {
	for _, k := range []string{"CURSOR_API_KEY", "ANTHROPIC_API_KEY", "CODEBUDDY_API_KEY", EnvTraeCLIToken} {
		if !envauth.IsPlatformAuthEnvKey(k) {
			t.Fatalf("%s should be platform auth key", k)
		}
	}
	for _, k := range []string{"GITLAB_TOKEN", "GRASP_CURSOR_API_KEY", "GRASP_TRAE_API_KEY", EnvCodeBuddyRegion, EnvTraeRegion} {
		if envauth.IsPlatformAuthEnvKey(k) {
			t.Fatalf("%s must not be filtered as platform auth", k)
		}
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("  ", "", "x", "y"); got != "x" {
		t.Fatalf("got %q", got)
	}
	if got := firstNonEmpty("", "  "); got != "" {
		t.Fatalf("got %q", got)
	}
}

func writeSettingsJSON(dir string, content string) {
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(content), 0o644); err != nil {
		panic(err)
	}
}

func TestPrepareAuthEnv_SettingsOnly(t *testing.T) {
	workDir := t.TempDir()
	writeSettingsJSON(workDir, `{"env":{"ANTHROPIC_API_KEY":"sk-ant-from-settings"}}`)

	out, err := PrepareAuthEnv(BackendClaudeCode, map[string]string{}, workDir)
	if err != nil {
		t.Fatalf("PrepareAuthEnv: %v", err)
	}
	if out["ANTHROPIC_API_KEY"] != "sk-ant-from-settings" {
		t.Fatalf("ANTHROPIC_API_KEY=%q", out["ANTHROPIC_API_KEY"])
	}
}

func TestPrepareAuthEnv_EnvOnly(t *testing.T) {
	workDir := t.TempDir()
	out, err := PrepareAuthEnv(BackendCursor, map[string]string{
		"GRASP_CURSOR_API_KEY": "crsr-env",
	}, workDir)
	if err != nil {
		t.Fatalf("PrepareAuthEnv: %v", err)
	}
	if out["CURSOR_API_KEY"] != "crsr-env" {
		t.Fatalf("CURSOR_API_KEY=%q", out["CURSOR_API_KEY"])
	}
}

func TestPrepareAuthEnv_NeitherConfigured(t *testing.T) {
	workDir := t.TempDir()
	_, err := PrepareAuthEnv(BackendClaudeCode, map[string]string{}, workDir)
	if err == nil {
		t.Fatal("expected error when no auth configured")
	}
	if !strings.Contains(err.Error(), "settings.json") {
		t.Fatalf("error should mention settings.json: %v", err)
	}
	if !strings.Contains(err.Error(), "项目共享") {
		t.Fatalf("error should mention 项目共享: %v", err)
	}
	if !strings.Contains(err.Error(), "GRASP_CLAUDE_API_KEY") {
		t.Fatalf("error should mention env keys: %v", err)
	}
}

func TestPrepareAuthEnv_InvalidSettingsJSON(t *testing.T) {
	workDir := t.TempDir()
	writeSettingsJSON(workDir, `{bad json`)

	out, err := PrepareAuthEnv(BackendClaudeCode, map[string]string{}, workDir)
	if err != nil {
		t.Fatalf("invalid settings.json must pass gate when file exists: %v", err)
	}
	if out == nil {
		t.Fatal("expected env map")
	}
}

func TestPrepareAuthEnv_EnvOverridesSettings(t *testing.T) {
	workDir := t.TempDir()
	writeSettingsJSON(workDir, `{"env":{"ANTHROPIC_API_KEY":"from-settings"}}`)

	out, err := PrepareAuthEnv(BackendClaudeCode, map[string]string{
		"GRASP_CLAUDE_API_KEY": "from-env",
	}, workDir)
	if err != nil {
		t.Fatalf("PrepareAuthEnv: %v", err)
	}
	if out["ANTHROPIC_API_KEY"] != "from-env" {
		t.Fatalf("env should win: got %q", out["ANTHROPIC_API_KEY"])
	}
}

func TestPrepareAuthEnv_EmptyEnvOverridesSettings(t *testing.T) {
	workDir := t.TempDir()
	writeSettingsJSON(workDir, `{"env":{"CURSOR_API_KEY":"from-settings"}}`)

	out, err := PrepareAuthEnv(BackendCursor, map[string]string{
		"CURSOR_API_KEY": "",
	}, workDir)
	if err != nil {
		t.Fatalf("settings.json present must skip env gate: %v", err)
	}
	if out["CURSOR_API_KEY"] != "" {
		t.Fatalf("empty explicit env should remain: got %q", out["CURSOR_API_KEY"])
	}
}

func TestPrepareAuthEnv_AllBackendsFromSettings(t *testing.T) {
	cases := []struct {
		backend AcpBackend
		content string
		cliKey  string
	}{
		{BackendClaudeCode, `{"env":{"ANTHROPIC_API_KEY":"claude-key"}}`, "ANTHROPIC_API_KEY"},
		{BackendCursor, `{"env":{"CURSOR_API_KEY":"cursor-key"}}`, "CURSOR_API_KEY"},
		{BackendCodeBuddy, `{"env":{"CODEBUDDY_API_KEY":"cb-key"}}`, "CODEBUDDY_API_KEY"},
		{BackendTrae, `{"env":{"TRAECLI_PERSONAL_ACCESS_TOKEN":"trae-token"}}`, EnvTraeCLIToken},
	}
	for _, tc := range cases {
		t.Run(string(tc.backend), func(t *testing.T) {
			workDir := t.TempDir()
			writeSettingsJSON(workDir, tc.content)
			out, err := PrepareAuthEnv(tc.backend, map[string]string{}, workDir)
			if err != nil {
				t.Fatalf("PrepareAuthEnv: %v", err)
			}
			if out[tc.cliKey] == "" {
				t.Fatalf("%s unset", tc.cliKey)
			}
		})
	}
}

func TestReadSettingsAuthEnv_EmptyKeyIgnored(t *testing.T) {
	workDir := t.TempDir()
	writeSettingsJSON(workDir, `{"env":{"ANTHROPIC_API_KEY":""}}`)

	got := ReadSettingsAuthEnv(workDir, BackendClaudeCode)
	if got != nil {
		t.Fatalf("empty key should be ignored: %v", got)
	}
}

func TestSettingsFileExists(t *testing.T) {
	workDir := t.TempDir()
	if SettingsFileExists(workDir) {
		t.Fatal("missing file")
	}
	if SettingsFileExists("") {
		t.Fatal("empty dir")
	}
	writeSettingsJSON(workDir, `{}`)
	if !SettingsFileExists(workDir) {
		t.Fatal("file should exist")
	}
}

func TestPrepareAuthEnv_SettingsFileExistsNoEnvNoKeys(t *testing.T) {
	workDir := t.TempDir()
	writeSettingsJSON(workDir, `{"model":"claude-sonnet"}`)

	before, err := os.ReadFile(filepath.Join(workDir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}

	out, err := PrepareAuthEnv(BackendClaudeCode, map[string]string{}, workDir)
	if err != nil {
		t.Fatalf("file exists gate must pass: %v", err)
	}
	if out == nil {
		t.Fatal("expected env map")
	}

	after, err := os.ReadFile(filepath.Join(workDir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("settings.json must not be modified:\nbefore=%s\nafter=%s", before, after)
	}
}

func TestPrepareAuthEnv_NoSettingsEnvConfigured(t *testing.T) {
	workDir := t.TempDir()
	out, err := PrepareAuthEnv(BackendCursor, map[string]string{
		"GRASP_CURSOR_API_KEY": "crsr-env",
	}, workDir)
	if err != nil {
		t.Fatalf("PrepareAuthEnv: %v", err)
	}
	if out["CURSOR_API_KEY"] != "crsr-env" {
		t.Fatalf("CURSOR_API_KEY=%q", out["CURSOR_API_KEY"])
	}
}

func TestResolveSettingsWorkDir(t *testing.T) {
	agentDir := t.TempDir()
	sharedDir := t.TempDir()

	if got := ResolveSettingsWorkDir(agentDir, ""); got != agentDir {
		t.Fatalf("empty shared: got %q want %q", got, agentDir)
	}
	if got := ResolveSettingsWorkDir(agentDir, sharedDir); got != agentDir {
		t.Fatalf("neither file: got %q want agent %q", got, agentDir)
	}

	writeSettingsJSON(sharedDir, `{}`)
	if got := ResolveSettingsWorkDir(agentDir, sharedDir); got != sharedDir {
		t.Fatalf("shared only: got %q want %q", got, sharedDir)
	}

	writeSettingsJSON(agentDir, `{}`)
	if got := ResolveSettingsWorkDir(agentDir, sharedDir); got != agentDir {
		t.Fatalf("agent overlay wins: got %q want %q", got, agentDir)
	}

	if got := ResolveSettingsWorkDir("", sharedDir); got != sharedDir {
		t.Fatalf("empty agent with shared file: got %q want %q", got, sharedDir)
	}
}

func TestPrepareAuthEnv_SharedSettingsOnly(t *testing.T) {
	agentDir := t.TempDir()
	sharedDir := t.TempDir()
	writeSettingsJSON(sharedDir, `{"env":{"ANTHROPIC_API_KEY":"sk-from-shared"}}`)

	out, err := PrepareAuthEnv(BackendClaudeCode, map[string]string{}, agentDir, sharedDir)
	if err != nil {
		t.Fatalf("shared settings.json must pass gate: %v", err)
	}
	if out["ANTHROPIC_API_KEY"] != "sk-from-shared" {
		t.Fatalf("ANTHROPIC_API_KEY=%q", out["ANTHROPIC_API_KEY"])
	}
}

func TestPrepareAuthEnv_AgentOverlayBeatsShared(t *testing.T) {
	agentDir := t.TempDir()
	sharedDir := t.TempDir()
	writeSettingsJSON(sharedDir, `{"env":{"ANTHROPIC_API_KEY":"from-shared"}}`)
	writeSettingsJSON(agentDir, `{"env":{"ANTHROPIC_API_KEY":"from-agent"}}`)

	out, err := PrepareAuthEnv(BackendClaudeCode, map[string]string{}, agentDir, sharedDir)
	if err != nil {
		t.Fatalf("PrepareAuthEnv: %v", err)
	}
	if out["ANTHROPIC_API_KEY"] != "from-agent" {
		t.Fatalf("agent overlay should win: got %q", out["ANTHROPIC_API_KEY"])
	}
}

func TestPrepareAuthEnv_AgentFileExistsIgnoresSharedKeys(t *testing.T) {
	agentDir := t.TempDir()
	sharedDir := t.TempDir()
	// Agent file present but no auth keys; shared has keys — must NOT read shared keys
	// (file-exists gate still passes via Agent file).
	writeSettingsJSON(agentDir, `{"model":"claude-sonnet"}`)
	writeSettingsJSON(sharedDir, `{"env":{"ANTHROPIC_API_KEY":"from-shared"}}`)

	out, err := PrepareAuthEnv(BackendClaudeCode, map[string]string{}, agentDir, sharedDir)
	if err != nil {
		t.Fatalf("agent settings.json presence must pass: %v", err)
	}
	if out["ANTHROPIC_API_KEY"] != "" {
		t.Fatalf("must not read shared env keys when agent file exists: got %q", out["ANTHROPIC_API_KEY"])
	}
}

func TestPrepareAuthEnv_NeitherLayerNorEnv(t *testing.T) {
	agentDir := t.TempDir()
	sharedDir := t.TempDir()
	_, err := PrepareAuthEnv(BackendClaudeCode, map[string]string{}, agentDir, sharedDir)
	if err == nil {
		t.Fatal("expected auth error when neither layer has settings.json")
	}
	if !strings.Contains(err.Error(), "项目共享") {
		t.Fatalf("error should mention 项目共享: %v", err)
	}
	if !strings.Contains(err.Error(), "settings.json") {
		t.Fatalf("error should mention settings.json: %v", err)
	}
}

func TestPrepareAuthEnv_EmptySharedFallsBackToAgent(t *testing.T) {
	agentDir := t.TempDir()
	writeSettingsJSON(agentDir, `{"env":{"ANTHROPIC_API_KEY":"from-agent"}}`)

	out, err := PrepareAuthEnv(BackendClaudeCode, map[string]string{}, agentDir, "")
	if err != nil {
		t.Fatalf("empty sharedWorkDir must fall back to agent: %v", err)
	}
	if out["ANTHROPIC_API_KEY"] != "from-agent" {
		t.Fatalf("ANTHROPIC_API_KEY=%q", out["ANTHROPIC_API_KEY"])
	}

	emptyAgent := t.TempDir()
	_, err = PrepareAuthEnv(BackendClaudeCode, map[string]string{}, emptyAgent, "")
	if err == nil {
		t.Fatal("expected error with empty shared and no agent settings")
	}
}
