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
	agentJSON := `{"env":{"SHARED":"from-agent","AGENT_ONLY":"a1","CURSOR_API_KEY":"agent-cursor"}}`
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
			ProjectIDForWorkflow: func(workflowID string) string {
				if workflowID != "wf-1" {
					t.Fatalf("workflowID = %q", workflowID)
				}
				return "proj-1"
			},
			SharedAgentForProject: func(projectID string) SharedAgentView {
				if projectID != "proj-1" {
					t.Fatalf("projectID = %q", projectID)
				}
				return SharedAgentView{
					Env: map[string]string{
						"SHARED":            "from-shared",
						"PROJECT_ONLY":      "p1",
						"CURSOR_API_KEY":    "shared-cursor",
						"TEMPLATED":         "${vars.region}",
						"ANTHROPIC_API_KEY": "shared-anthropic",
					},
				}
			},
			ProfilesRoot: root,
		},
		backend: BackendCursor,
	}
	req := NodeReq{
		WorkflowID: "wf-1",
		NodeType:   "agent",
		Token:      "tok",
		Config:     map[string]any{"agent_profile": "demo"},
		Vars:       map[string]any{"region": "cn-east"},
	}
	spec, err := c.spec(req)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Env["PLATFORM_KEY"] != "plat" {
		t.Fatalf("platform = %q", spec.Env["PLATFORM_KEY"])
	}
	if spec.Env["PROJECT_ONLY"] != "p1" {
		t.Fatalf("shared only = %q", spec.Env["PROJECT_ONLY"])
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
	if spec.Env["ANTHROPIC_API_KEY"] != "shared-anthropic" {
		t.Fatalf("shared anthropic = %q", spec.Env["ANTHROPIC_API_KEY"])
	}
	if spec.Env["CURSOR_API_KEY"] != "shared-cursor" {
		t.Fatalf("shared token wins = %q", spec.Env["CURSOR_API_KEY"])
	}
	if spec.Env["CURSOR_API_KEY"] == "should-skip-platform" {
		t.Fatal("platform CURSOR_API_KEY must stay skipped")
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
		{BackendOpenCode, "OPENCODE_API_KEY"},
	}
	for _, tc := range backends {
		t.Run(string(tc.backend), func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "demo")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			agentJSON := `{"env":{"` + tc.authKey + `":"k"}}`
			if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(agentJSON), 0o644); err != nil {
				t.Fatal(err)
			}
			c := &acpProvider{
				opts:    Options{ProfilesRoot: root},
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
			if _, ok := spec.Env["ACP_BACKEND"]; ok {
				t.Fatal("ACP_BACKEND must not be injected")
			}
		})
	}
}

func TestSpecSharedAuthKeyAloneSucceeds(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(`{"env":{"AGENT_ONLY":"a1"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &acpProvider{
		opts: Options{
			Env: map[string]string{
				"CURSOR_API_KEY": "platform-must-skip",
			},
			ProjectIDForWorkflow: func(string) string { return "proj-1" },
			SharedAgentForProject: func(string) SharedAgentView {
				return SharedAgentView{Env: map[string]string{"CURSOR_API_KEY": "shared-only-key"}}
			},
			ProfilesRoot: root,
		},
		backend: BackendCursor,
	}
	spec, err := c.spec(NodeReq{
		WorkflowID: "wf-1",
		NodeType:   "agent",
		Token:      "tok",
		Config:     map[string]any{"agent_profile": "demo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Env["CURSOR_API_KEY"] != "shared-only-key" {
		t.Fatalf("shared-only auth = %q", spec.Env["CURSOR_API_KEY"])
	}
}

func TestSpecSkipsSharedEnvWithoutLookup(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(`{"env":{"GRASP_CURSOR_API_KEY":"k"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &acpProvider{
		opts: Options{
			Env:          map[string]string{"P": "1"},
			ProfilesRoot: root,
		},
		backend: BackendCursor,
	}
	spec, err := c.spec(NodeReq{
		WorkflowID: "wf-1",
		NodeType:   "agent",
		Token:      "t",
		Config:     map[string]any{"agent_profile": "demo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := spec.Env["PROJECT_ONLY"]; ok {
		t.Fatal("unexpected shared env")
	}
}

func TestSpecMergesRunSandboxEnvAfterAgent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentJSON := `{"env":{"SHARED":"from-agent","AGENT_ONLY":"a1","CURSOR_API_KEY":"agent-cursor","EMPTY_TARGET":"agent-val"}}`
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(agentJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &acpProvider{
		opts: Options{
			Env: map[string]string{
				"PLATFORM_KEY": "plat",
				"SHARED":       "from-platform",
			},
			ProjectIDForWorkflow: func(string) string { return "proj-1" },
			SharedAgentForProject: func(string) SharedAgentView {
				return SharedAgentView{Env: map[string]string{
					"SHARED":       "from-shared",
					"PROJECT_ONLY": "p1",
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
			ProfilesRoot: root,
		},
		backend: BackendCursor,
	}
	spec, err := c.spec(NodeReq{
		RunID:      "run-1",
		WorkflowID: "wf-1",
		NodeType:   "agent",
		Token:      "tok",
		Config:     map[string]any{"agent_profile": "demo"},
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
	if spec.Env["PROJECT_ONLY"] != "p1" {
		t.Fatalf("untouched shared=%q", spec.Env["PROJECT_ONLY"])
	}
	if spec.Env["AGENT_ONLY"] != "a1" {
		t.Fatalf("untouched agent=%q", spec.Env["AGENT_ONLY"])
	}
	if spec.Env["AGENT_PROVIDER"] != string(BackendCursor) {
		t.Fatalf("AGENT_PROVIDER=%q", spec.Env["AGENT_PROVIDER"])
	}
	if spec.Env["CURSOR_API_KEY"] != "agent-cursor" {
		t.Fatalf("auth from agent must remain: %q", spec.Env["CURSOR_API_KEY"])
	}
	if spec.Env["PASSWORD"] != "tok" {
		t.Fatalf("ApplyPasswords must win: %q", spec.Env["PASSWORD"])
	}
}

func TestSpecRunSandboxEnvDoesNotOverrideReservedAfterInject(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(`{"env":{"GRASP_CURSOR_API_KEY":"k"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &acpProvider{
		opts: Options{
			ProfilesRoot: root,
			RunSandboxEnvForRun: func(string) []models.EnvEntry {
				return []models.EnvEntry{
					{Key: "AGENT_PROVIDER", Value: "evil"},
					{Key: "GRASP_RUN_ID", Value: "evil-run"},
					{Key: "PASSWORD", Value: "evil-pw"},
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
	if spec.Env["PASSWORD"] == "evil-pw" {
		t.Fatal("PASSWORD must not be overridden by run env")
	}
}

func TestSpecProjectCredentialWinsOverRunEnv(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(`{"env":{"CUSTOM_PROJECT_TOKEN":"agent","GRASP_CURSOR_API_KEY":"agent-key"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &acpProvider{
		opts: Options{
			ProfilesRoot:         root,
			ProjectIDForWorkflow: func(string) string { return "proj-1" },
			ProjectCredentialsForProject: func(string) map[string]string {
				return map[string]string{"CUSTOM_PROJECT_TOKEN": "ui", "GRASP_CURSOR_API_KEY": "ui-key"}
			},
			ProjectCredentialKeysForProject: func(string) map[string]struct{} {
				return map[string]struct{}{"CUSTOM_PROJECT_TOKEN": {}, "GRASP_CURSOR_API_KEY": {}}
			},
			RunSandboxEnvForRun: func(string) []models.EnvEntry {
				return []models.EnvEntry{{Key: "CUSTOM_PROJECT_TOKEN", Value: "run"}, {Key: "GRASP_CURSOR_API_KEY", Value: "run-key"}}
			},
		},
		backend: BackendCursor,
	}
	spec, err := c.spec(NodeReq{RunID: "run-1", WorkflowID: "wf-1", Config: map[string]any{"agent_profile": "demo"}})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Env["CUSTOM_PROJECT_TOKEN"] != "ui" || spec.Env["GRASP_CURSOR_API_KEY"] != "ui-key" {
		t.Fatalf("project credentials were shadowed: %#v", spec.Env)
	}
}

func TestSpecProjectCredentialsDoNotReadProcessEnvOrShadowPlatformKeys(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(`{"env":{"GRASP_CURSOR_API_KEY":"agent-key","PROJECT_ALIAS_SRC":"alias"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_TOKEN", "server-process-token")
	t.Setenv("SERVER_ONLY_SECRET", "server-secret")
	c := &acpProvider{
		opts: Options{
			ProfilesRoot:         root,
			ProjectIDForWorkflow: func(string) string { return "proj-1" },
			ProjectCredentialsForProject: func(string) map[string]string {
				return map[string]string{"GRASP_ARTIFACT_TOKEN": "user-token"}
			},
			ProjectCredentialFallbackEnvForProject: func(string) map[string]string {
				return map[string]string{"LEAK": "SERVER_ONLY_SECRET", "ALIASED": "PROJECT_ALIAS_SRC"}
			},
		},
		backend: BackendCursor,
	}
	spec, err := c.spec(NodeReq{RunID: "run-1", WorkflowID: "wf-1", Token: "platform-token", Config: map[string]any{"agent_profile": "demo"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := spec.Env["GITHUB_TOKEN"]; ok {
		t.Fatalf("server process GITHUB_TOKEN leaked into sandbox: %q", spec.Env["GITHUB_TOKEN"])
	}
	if _, ok := spec.Env["LEAK"]; ok {
		t.Fatal("fallback binding must not read the server process environment")
	}
	if spec.Env["ALIASED"] != "alias" {
		t.Fatalf("fallback from project env not applied: %q", spec.Env["ALIASED"])
	}
	if spec.Env["GRASP_ARTIFACT_TOKEN"] != "platform-token" {
		t.Fatalf("platform token shadowed by credential: %q", spec.Env["GRASP_ARTIFACT_TOKEN"])
	}
}

func TestResolvedMCPSpecsSubstitutesSharedEnv(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentJSON := `{"mcp":[{"name":"server-log","url":"https://logs.example/mcp","headers":{"Authorization":"Bearer ${LOG_CENTER_TOKEN}"}},{"name":"artifact-store","url":"${GRASP_ARTIFACT_URL}","headers":{"Authorization":"Bearer ${GRASP_ARTIFACT_TOKEN}"}}],"env":{"GRASP_ARTIFACT_TOKEN":"evil-agent","GRASP_CURSOR_API_KEY":"k"}}`
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(agentJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &acpProvider{
		opts: Options{
			ProjectIDForWorkflow: func(string) string { return "proj-1" },
			SharedAgentForProject: func(string) SharedAgentView {
				return SharedAgentView{Env: map[string]string{
					"LOG_CENTER_TOKEN": "secret-from-shared",
				}}
			},
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
	agentJSON := `{"mcp":[{"name":"server-log","url":"https://logs.example/mcp","headers":{"Authorization":"Bearer ${LOG_CENTER_TOKEN}"}}],"env":{"LOG_CENTER_TOKEN":"from-agent"}}`
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(agentJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &acpProvider{
		opts: Options{
			ProjectIDForWorkflow: func(string) string { return "proj-1" },
			SharedAgentForProject: func(string) SharedAgentView {
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

func TestResolvedMCPSpecsProjectCredentialReference(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(`{"mcp":[{"name":"private","url":"https://logs.example/mcp","headers":{"Authorization":"Bearer ${credential:cred-1}"}}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &acpProvider{opts: Options{
		ProfilesRoot:                root,
		ProjectIDForWorkflow:        func(string) string { return "proj-1" },
		ProjectCredentialReferences: func(string) map[string]string { return map[string]string{"credential:cred-1": "secret-ref"} },
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
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(`{"env":{"GRASP_CURSOR_API_KEY":"k"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &acpProvider{
		opts:    Options{ProfilesRoot: root},
		backend: BackendCursor,
	}
	spec, err := c.spec(NodeReq{
		RunID: "run-1", NodeType: "agent", Token: "t",
		Config: map[string]any{"agent_profile": "demo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := spec.Env["RUN_ONLY"]; ok {
		t.Fatal("unexpected run env without lookup")
	}
}
