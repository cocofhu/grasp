package codex

import (
	"slices"
	"strings"
	"testing"

	"backend/internal/provider"
)

func TestProviderIdentity(t *testing.T) {
	p := New()
	if p.Name() != provider.Codex || p.Runtime() != runtime || p.DefaultConfigRoot() != configRoot || p.Transport() != provider.LongLived {
		t.Fatalf("identity: %s %s %s %v", p.Name(), p.Runtime(), p.DefaultConfigRoot(), p.Transport())
	}
}

func TestAuthEnvDropsPlatformKeys(t *testing.T) {
	got := New().AuthEnv([]string{"OPENAI_API_KEY=a", "CODEX_API_KEY=b", "ACP_CODEX_API_KEY=c", "HOME=/root", "GRASP_MODEL_API_KEY=d"})
	if !slices.Equal(got, []string{"HOME=/root", "GRASP_MODEL_API_KEY=d"}) {
		t.Fatalf("AuthEnv = %v", got)
	}
}

func TestAppServerArgv(t *testing.T) {
	for _, tc := range []struct {
		name      string
		opts      provider.OpenOptions
		env       map[string]string
		want, not []string
	}{
		{name: "plain", not: []string{"model_reasoning_effort", "service_tier"}},
		{name: "auto effort", opts: provider.OpenOptions{ReasoningEffort: "auto"}, not: []string{"model_reasoning_effort"}},
		{name: "env fallback", env: map[string]string{envReasoningEffort: "medium", envServiceTier: "fast"},
			want: []string{`model_reasoning_effort="medium"`, "features.fast_mode=true"}},
		{name: "explicit clear beats env", opts: provider.OpenOptions{ReasoningEffortSet: true, ServiceTierSet: true},
			env:  map[string]string{envReasoningEffort: "medium", envServiceTier: "fast"},
			want: []string{`service_tier="default"`}, not: []string{"model_reasoning_effort", "fast_mode"}},
		{name: "unknown tier is standard", opts: provider.OpenOptions{ServiceTier: "turbo"}, want: []string{`service_tier="default"`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(envReasoningEffort, tc.env[envReasoningEffort])
			t.Setenv(envServiceTier, tc.env[envServiceTier])
			argv := strings.Join(appServerArgv("/bin/codex", tc.opts, []string{"-c", "model=\"m\""}), " ")
			if !strings.HasPrefix(argv, `/bin/codex app-server -c model="m"`) {
				t.Fatalf("argv = %q", argv)
			}
			for _, w := range tc.want {
				if !strings.Contains(argv, w) {
					t.Fatalf("argv %q lacks %q", argv, w)
				}
			}
			for _, n := range tc.not {
				if strings.Contains(argv, n) {
					t.Fatalf("argv %q must not contain %q", argv, n)
				}
			}
		})
	}
}

func TestSandboxMode(t *testing.T) {
	if sandboxMode(true) != "danger-full-access" || sandboxMode(false) != "workspace-write" {
		t.Fatal("sandbox mode mapping")
	}
}

func TestCodexAuthRejected(t *testing.T) {
	for _, text := range []string{
		"Not logged in", "refresh_token_reused", "401 Unauthorized at https://chatgpt.com/backend-api/codex",
		codexLoginRepaste, "请重新粘贴登录文件",
	} {
		if !codexAuthRejected(text) {
			t.Fatalf("%q should be an auth rejection", text)
		}
	}
	for _, text := range []string{"", "401 Unauthorized at https://example.com", "rate limited"} {
		if codexAuthRejected(text) {
			t.Fatalf("%q is not an auth rejection", text)
		}
	}
	if rewriteCodexAuthError("boom") != "boom" {
		t.Fatal("non-auth errors pass through")
	}
}

func TestToolMappingHelpers(t *testing.T) {
	if toolTitle(threadItem{Type: "dynamicToolCall", Tool: "lookup"}) != "lookup" || toolTitle(threadItem{Type: "imageGeneration"}) != "imageGeneration" {
		t.Fatal("tool title fallbacks")
	}
	if toolStatus("declined") != "failed" || toolStatus("completed") != "completed" {
		t.Fatal("tool status mapping")
	}
	if toolOutput(threadItem{Error: &turnError{Message: "nope"}}) != "nope" {
		t.Fatal("tool error output")
	}
	if planStatus("pending") != "pending" {
		t.Fatal("plan status passthrough")
	}
	if truncate("abcdef", 3) != "abc…" || truncate("ab", 3) != "ab" {
		t.Fatal("truncate")
	}
	if firstNonEmpty(" ", "", "x") != "x" || firstNonEmpty() != "" {
		t.Fatal("firstNonEmpty")
	}
}

func TestConfigFilesFollowCodexHome(t *testing.T) {
	var _ provider.ConfigReader = (*session)(nil)
	s := &session{env: []string{"PATH=/bin", "CODEX_HOME=/tmp/conn-home"}}
	if got := s.ConfigFiles(); len(got) != 2 || got[0] != "/tmp/conn-home/auth.json" || got[1] != "/tmp/conn-home/config.toml" {
		t.Fatalf("ConfigFiles = %v", got)
	}
	if got := (&session{}).ConfigFiles(); got[0] != configRoot+"/auth.json" {
		t.Fatalf("default ConfigFiles = %v", got)
	}
}
