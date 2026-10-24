package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestSpecMergesSharedEnvExtendThenAgentOverlay(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentJSON := `{"modelBinding":{"kind":"configuration","configurationId":"test-model"},"env":{"SHARED":"from-agent","AGENT_ONLY":"a1","CURSOR_API_KEY":"agent-cursor"}}`
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(agentJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &acpProvider{
		opts: Options{
			Env: map[string]string{
				"PLATFORM_KEY":   "plat",
				"CURSOR_API_KEY": "should-skip-platform",
				"SHARED":         "from-platform",
			},
			SharedAgent: func() SharedAgentView {
				return SharedAgentView{
					Env: map[string]string{
						"SHARED":            "from-shared",
						"SHARED_ONLY":       "p1",
						"CURSOR_API_KEY":    "shared-cursor",
						"TEMPLATED":         "${vars.region}",
						"ANTHROPIC_API_KEY": "shared-anthropic",
					},
				}
			},
			ModelBinding: testModelBinding,
			ProfilesRoot: root,
		},
		backend: BackendCursor,
	}
	req := NodeReq{
		WorkflowID: "wf-1",
		NodeType:   "agent", Caps: testPlainCaps,
		Token:  "tok",
		Config: map[string]any{"agent_profile": "demo"},
		Vars:   map[string]any{"region": "cn-east"},
	}
	spec, err := c.spec(req)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Env["PLATFORM_KEY"] != "plat" {
		t.Fatalf("platform = %q", spec.Env["PLATFORM_KEY"])
	}
	if spec.Env["SHARED_ONLY"] != "p1" {
		t.Fatalf("shared only = %q", spec.Env["SHARED_ONLY"])
	}
	if spec.Env["SHARED"] != "from-agent" {
		t.Fatalf("shared (agent wins) = %q", spec.Env["SHARED"])
	}
	if spec.Env["AGENT_ONLY"] != "a1" {
		t.Fatalf("agent only = %q", spec.Env["AGENT_ONLY"])
	}
	if spec.Env["TEMPLATED"] != "cn-east" {
		t.Fatalf("templated = %q", spec.Env["TEMPLATED"])
	}
	if v, ok := spec.Env["ANTHROPIC_API_KEY"]; ok {
		t.Fatalf("shared env secret must be dropped, got %q", v)
	}
	if spec.Env["CURSOR_API_KEY"] != "fake" {
		t.Fatalf("auth must come from workspace credentials, got %q", spec.Env["CURSOR_API_KEY"])
	}
}

func TestSpecSetsAgentProviderForEveryBackend(t *testing.T) {
	backends := []struct {
		backend AcpBackend
		authKey string
	}{
		{BackendCursor, "CURSOR_API_KEY"},
		{BackendClaudeCode, "ANTHROPIC_API_KEY"},
		{BackendCodeBuddy, "CODEBUDDY_API_KEY"},
		{BackendTrae, "TRAECLI_PERSONAL_ACCESS_TOKEN"},
		{BackendOpenCode, EnvModelAPIKey},
	}
	for _, tc := range backends {
		t.Run(string(tc.backend), func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "demo")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(`{"modelBinding":{"kind":"configuration","configurationId":"test-model"}}`), 0o644); err != nil {
				t.Fatal(err)
			}
			c := &acpProvider{
				opts:    Options{ProfilesRoot: root, ModelBinding: testModelBinding},
				backend: tc.backend,
			}
			spec, err := c.spec(NodeReq{
				Token:  "tok",
				Config: map[string]any{"agent_profile": "demo"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := spec.Env["AGENT_PROVIDER"]; got != string(tc.backend) {
				t.Fatalf("AGENT_PROVIDER=%q, want %q", got, tc.backend)
			}
			if spec.Env[tc.authKey] != "fake" {
				t.Fatalf("%s=%q, want credential value", tc.authKey, spec.Env[tc.authKey])
			}
			if _, ok := spec.Env["ACP_BACKEND"]; ok {
				t.Fatal("ACP_BACKEND must not be injected")
			}
		})
	}
}

func TestSpecSharedEnvSecretIsNotAuthSource(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(`{"modelBinding":{"kind":"configuration","configurationId":"test-model"},"env":{"AGENT_ONLY":"a1"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &acpProvider{
		opts: Options{
			Env: map[string]string{
				"CURSOR_API_KEY": "platform-must-skip",
			},
			SharedAgent: func() SharedAgentView {
				return SharedAgentView{Env: map[string]string{"CURSOR_API_KEY": "shared-only-key"}}
			},
			ProfilesRoot: root,
		},
		backend: BackendCursor,
	}
	if _, err := c.spec(NodeReq{
		WorkflowID: "wf-1",
		NodeType:   "agent", Caps: testPlainCaps,
		Token:  "tok",
		Config: map[string]any{"agent_profile": "demo"},
	}); err == nil {
		t.Fatal("shared/platform env secrets must not satisfy the auth gate")
	}
}

func TestSpecSkipsSharedEnvWithoutLookup(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(`{"modelBinding":{"kind":"configuration","configurationId":"test-model"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &acpProvider{
		opts: Options{
			Env:          map[string]string{"P": "1"},
			ProfilesRoot: root,
			ModelBinding: testModelBinding,
		},
		backend: BackendCursor,
	}
	spec, err := c.spec(NodeReq{
		WorkflowID: "wf-1",
		NodeType:   "agent", Caps: testPlainCaps,
		Token:  "t",
		Config: map[string]any{"agent_profile": "demo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := spec.Env["SHARED_ONLY"]; ok {
		t.Fatal("unexpected shared env")
	}
}

func TestSpecMergesRunSandboxEnvAfterAgent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentJSON := `{"modelBinding":{"kind":"configuration","configurationId":"test-model"},"env":{"SHARED":"from-agent","AGENT_ONLY":"a1","EMPTY_TARGET":"agent-val"}}`
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(agentJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &acpProvider{
		opts: Options{
			Env: map[string]string{
				"PLATFORM_KEY": "plat",
				"SHARED":       "from-platform",
			},
			SharedAgent: func() SharedAgentView {
				return SharedAgentView{Env: map[string]string{
					"SHARED":      "from-shared",
					"SHARED_ONLY": "p1",
				}}
			},
			RunSandboxEnvForRun: func(runID string) []models.EnvEntry {
				if runID != "run-1" {
					t.Fatalf("runID=%q", runID)
				}
				return []models.EnvEntry{
					{Key: "SHARED", Value: "from-run"},
					{Key: "RUN_ONLY", Value: "r1"},
					{Key: "EMPTY_TARGET", Value: ""},
				}
			},
			ModelBinding: testModelBinding,
			ProfilesRoot: root,
		},
		backend: BackendCursor,
	}
	spec, err := c.spec(NodeReq{
		RunID:      "run-1",
		WorkflowID: "wf-1",
		NodeType:   "agent", Caps: testPlainCaps,
		Token:  "tok",
		Config: map[string]any{"agent_profile": "demo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Env["SHARED"] != "from-run" {
		t.Fatalf("run should win SHARED: %q", spec.Env["SHARED"])
	}
	if spec.Env["RUN_ONLY"] != "r1" {
		t.Fatalf("RUN_ONLY=%q", spec.Env["RUN_ONLY"])
	}
	if spec.Env["EMPTY_TARGET"] != "" {
		t.Fatalf("empty string should override agent: %q", spec.Env["EMPTY_TARGET"])
	}
	if spec.Env["SHARED_ONLY"] != "p1" {
		t.Fatalf("untouched shared=%q", spec.Env["SHARED_ONLY"])
	}
	if spec.Env["AGENT_ONLY"] != "a1" {
		t.Fatalf("untouched agent=%q", spec.Env["AGENT_ONLY"])
	}
	if spec.Env["AGENT_PROVIDER"] != string(BackendCursor) {
		t.Fatalf("AGENT_PROVIDER=%q", spec.Env["AGENT_PROVIDER"])
	}
	if spec.Env["CURSOR_API_KEY"] != "fake" {
		t.Fatalf("auth from workspace credentials must remain: %q", spec.Env["CURSOR_API_KEY"])
	}
	if spec.Env["ACP_BRIDGE_PASSWORD"] != "tok" {
		t.Fatalf("ApplyPasswords must win: %q", spec.Env["ACP_BRIDGE_PASSWORD"])
	}
}

func TestSpecRunSandboxEnvDoesNotOverrideReservedAfterInject(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(`{"modelBinding":{"kind":"configuration","configurationId":"test-model"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &acpProvider{
		opts: Options{
			ProfilesRoot: root,
			ModelBinding: testModelBinding,
			RunSandboxEnvForRun: func(string) []models.EnvEntry {
				return []models.EnvEntry{
					{Key: "AGENT_PROVIDER", Value: "evil"},
					{Key: "GRASP_RUN_ID", Value: "evil-run"},
					{Key: "ACP_BRIDGE_PASSWORD", Value: "evil-pw"},
					{Key: "CONFIG_ROOT", Value: "/evil"},
				}
			},
		},
		backend: BackendCursor,
	}
	spec, err := c.spec(NodeReq{
		RunID:  "run-1",
		Token:  "tok",
		Config: map[string]any{"agent_profile": "demo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Env["AGENT_PROVIDER"] == "evil" {
		t.Fatal("AGENT_PROVIDER must not be overridden by run env")
	}
	if spec.Env["ACP_BRIDGE_PASSWORD"] == "evil-pw" {
		t.Fatal("ACP_BRIDGE_PASSWORD must not be overridden by run env")
	}
}

func TestSpecAgentEnvSecretIsNotAuthSource(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(`{"modelBinding":{"kind":"configuration","configurationId":"test-model"},"env":{"GRASP_CURSOR_API_KEY":"agent-key"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &acpProvider{opts: Options{ProfilesRoot: root}, backend: BackendCursor}
	if _, err := c.spec(NodeReq{RunID: "run-1", Config: map[string]any{"agent_profile": "demo"}}); err == nil {
		t.Fatal("Agent env secret must not satisfy the auth gate")
	}
}

func TestResolvedMCPSpecsSubstitutesSharedEnv(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentJSON := `{"modelBinding":{"kind":"configuration","configurationId":"test-model"},"mcp":[{"name":"server-log","url":"https://logs.example/mcp","headers":{"Authorization":"Bearer ${LOG_CENTER_TOKEN}"}},{"name":"artifact-store","url":"${GRASP_ARTIFACT_URL}","headers":{"Authorization":"Bearer ${GRASP_ARTIFACT_TOKEN}"}}],"env":{"GRASP_ARTIFACT_TOKEN":"evil-agent"}}`
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(agentJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &acpProvider{
		opts: Options{
			SharedAgent: func() SharedAgentView {
				return SharedAgentView{Env: map[string]string{
					"LOG_CENTER_TOKEN": "secret-from-shared",
				}}
			},
			ModelBinding: testModelBinding,
			ProfilesRoot: root,
			MCPEndpoint:  "http://mcp.local",
		},
		backend: BackendCursor,
	}
	req := NodeReq{
		WorkflowID: "wf-1",
		RunID:      "run-1",
		NodeID:     "n1",
		Token:      "tok",
		Config:     map[string]any{"agent_profile": "demo"},
	}
	specs := c.resolvedMCPSpecs(req)
	var logHdr, artHdr string
	for _, sp := range specs {
		switch sp.Name {
		case "server-log":
			logHdr = sp.Headers["Authorization"]
		case "artifact-store":
			artHdr = sp.Headers["Authorization"]
		}
	}
	if logHdr != "Bearer secret-from-shared" {
		t.Fatalf("server-log Authorization = %q", logHdr)
	}
	if artHdr != "Bearer tok" {
		t.Fatalf("artifact-store must keep run token, got %q", artHdr)
	}
	spec, err := c.spec(req)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Env["LOG_CENTER_TOKEN"] != "secret-from-shared" {
		t.Fatalf("OS env LOG_CENTER_TOKEN = %q", spec.Env["LOG_CENTER_TOKEN"])
	}
}

func TestResolvedMCPSpecsAgentEnvOverlaysShared(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentJSON := `{"modelBinding":{"kind":"configuration","configurationId":"test-model"},"mcp":[{"name":"server-log","url":"https://logs.example/mcp","headers":{"Authorization":"Bearer ${LOG_CENTER_TOKEN}"}}],"env":{"LOG_CENTER_TOKEN":"from-agent"}}`
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(agentJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &acpProvider{
		opts: Options{
			SharedAgent: func() SharedAgentView {
				return SharedAgentView{Env: map[string]string{"LOG_CENTER_TOKEN": "secret-from-shared"}}
			},
			ProfilesRoot: root,
		},
		backend: BackendCursor,
	}
	specs := c.resolvedMCPSpecs(NodeReq{
		WorkflowID: "wf-1",
		Token:      "tok",
		Config:     map[string]any{"agent_profile": "demo"},
	})
	if len(specs) != 1 || specs[0].Headers["Authorization"] != "Bearer from-agent" {
		t.Fatalf("agent env should win: %+v", specs)
	}
}

func TestResolvedMCPSpecsWorkspaceCredentialReference(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(`{"modelBinding":{"kind":"configuration","configurationId":"test-model"},"mcp":[{"name":"private","url":"https://logs.example/mcp","headers":{"Authorization":"Bearer ${credential:cred-1}"}}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &acpProvider{opts: Options{
		ProfilesRoot:         root,
		CredentialReferences: func() map[string]string { return map[string]string{"credential:cred-1": "secret-ref"} },
	}, backend: BackendCursor}
	specs := c.resolvedMCPSpecs(NodeReq{WorkflowID: "wf-1", Config: map[string]any{"agent_profile": "demo"}})
	if len(specs) != 1 || specs[0].Headers["Authorization"] != "Bearer secret-ref" {
		t.Fatalf("credential reference not expanded: %+v", specs)
	}
	if spec, err := c.spec(NodeReq{WorkflowID: "wf-1", Config: map[string]any{"agent_profile": "demo"}}); err == nil {
		if _, leaked := spec.Env["credential:cred-1"]; leaked {
			t.Fatal("credential reference must not be a global sandbox env key")
		}
	}
}

func TestMergeEnvIntoTemplateVarsReservedWinAndSubst(t *testing.T) {
	base := map[string]string{
		"GRASP_ARTIFACT_TOKEN": "tok",
		"vars.region":          "cn-east",
	}
	got := MergeEnvIntoTemplateVars(base, map[string]string{
		"LOG_CENTER_TOKEN":     "secret",
		"GRASP_ARTIFACT_TOKEN": "evil",
		"TEMPLATED":            "${vars.region}",
		"":                     "skip",
	})
	if got["LOG_CENTER_TOKEN"] != "secret" {
		t.Fatalf("LOG_CENTER_TOKEN=%q", got["LOG_CENTER_TOKEN"])
	}
	if got["GRASP_ARTIFACT_TOKEN"] != "tok" {
		t.Fatalf("reserved overwritten: %q", got["GRASP_ARTIFACT_TOKEN"])
	}
	if got["TEMPLATED"] != "cn-east" {
		t.Fatalf("TEMPLATED=%q", got["TEMPLATED"])
	}
}

func TestSpecSkipsRunEnvWithoutLookup(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(`{"modelBinding":{"kind":"configuration","configurationId":"test-model"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &acpProvider{
		opts:    Options{ProfilesRoot: root, ModelBinding: testModelBinding},
		backend: BackendCursor,
	}
	spec, err := c.spec(NodeReq{
		RunID: "run-1", NodeType: "agent", Caps: testPlainCaps, Token: "t",
		Config: map[string]any{"agent_profile": "demo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := spec.Env["RUN_ONLY"]; ok {
		t.Fatal("unexpected run env without lookup")
	}
}
