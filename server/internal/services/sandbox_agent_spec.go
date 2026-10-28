package services

import (
	"fmt"
	"os"
	"strings"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/rs/zerolog/log"
)

// preparedAgentContainer is a reusable runtime bundle. Its host config home is
// retained until the live sandbox is replaced or destroyed, so a failed route
// change can put the previous configuration back without touching the workspace.
type preparedAgentContainer struct {
	spec             sandbox.Spec
	specs            []sandbox.MCPServerSpec
	backend          runtime.AcpBackend
	modelFingerprint string
	codexLogin       string
}

func (s *SandboxService) prepareAgentContainer(profile, threadID, runID, sharedToken, name string, platformSpecs []sandbox.MCPServerSpec, agent Agent, extra agentContainerExtra) (*preparedAgentContainer, error) {
	vars := map[string]string{}
	for _, sp := range platformSpecs {
		switch sp.Name {
		case MemoryStoreMCP:
			vars["GRASP_MEMORY_URL"] = sp.URL
			vars["GRASP_MEMORY_TOKEN"] = sharedToken
		case ContextStoreMCP:
			vars["GRASP_CONTEXT_URL"] = sp.URL
			vars["GRASP_CONTEXT_TOKEN"] = sharedToken
		case TaskSchedulerMCP:
			vars["GRASP_SCHEDULER_URL"] = sp.URL
			vars["GRASP_SCHEDULER_TOKEN"] = sharedToken
		case PmProgressMCP, PmWorkflowReadMCP, PmWorkflowWriteMCP:
			vars["GRASP_PM_URL"] = sp.URL
			vars["GRASP_PM_TOKEN"] = sharedToken
		}
	}

	agentEnv := envauth.StripSecretEnvKeys(agent.Env)
	vars = runtime.MergeEnvIntoTemplateVars(vars, agentEnv)
	mcpVars := vars
	if s.credentialReferences != nil {
		// Credential references are scoped to user-authored MCP templates. Keep
		// them out of vars so the ordinary sandbox environment never receives
		// these values as global keys.
		mcpVars = runtime.MergeEnvIntoTemplateVars(s.credentialReferences(), vars)
	}
	specs := filterAgentPlatformMCP(resolveAgentMCP(agent.MCP, mcpVars))
	specs = append(specs, platformSpecs...)
	specs = dedupeMCPByName(specs)

	track := newEnvTracker()
	env := map[string]string{}
	for k, v := range s.env {
		if envauth.IsSecretEnvKey(k) {
			log.Warn().Str("key", k).Msg("dropped secret key from platform sandbox.env; configure it in workspace credentials")
			continue
		}
		env[k] = v
	}
	track.mark(env, EnvSourcePlatform)
	for k, v := range agentEnv {
		if strings.Contains(v, "GRASP_ARTIFACT") {
			continue
		}
		env[k] = v
	}
	track.mark(env, EnvSourceAgent)
	for k, v := range vars {
		env[k] = v
	}
	track.mark(env, EnvSourceSystem)
	creds := map[string]string{}
	backend, err := runtime.ParseBackend(agent.AcpBackend)
	if err != nil {
		return nil, err
	}
	runtime.ApplyCredentialBindings(env, creds, agent.CredentialBindings, s.selectedCredential)
	gitCredentials, err := runtime.ApplyGitBindings(env, creds, agent.GitBindings, s.gitCredential)
	if err != nil {
		return nil, err
	}
	track.mark(env, EnvSourceCredential)
	if err := runtime.ApplyModelBinding(env, string(backend), agent.ModelBinding, s.modelBinding); err != nil {
		return nil, err
	}
	modelFingerprint := modelBindingFingerprint(agent, env)
	workDir := s.skills.WorkDir(profile)
	sharedWorkDir := ""
	if s.shared != nil {
		sharedWorkDir = s.shared.WorkDir()
	}
	codexLogin := runtime.CodexLoginFileFromEnv(backend, env)
	merged, err := runtime.PrepareBoundAuth(backend, env, workDir, sharedWorkDir)
	if err != nil {
		return nil, err
	}
	env = merged
	track.mark(env, EnvSourceCredential)
	home, err := sandbox.BuildConfigHome(sandbox.ConfigHomeSpec{
		BaseWorkDirSrc:       sharedWorkDir,
		WorkDirSrc:           s.skills.WorkDir(profile),
		IncludeArtifactStore: false,
		MCP:                  specs,
		OpenCode:             backend == runtime.BackendOpenCode,
		Codex:                backend == runtime.BackendCodex,
		DeepSeekHarness:      backend == runtime.BackendDeepSeekHarness,
		BrowserMCP:           runtime.EnvEnabled(env["BROWSER_MCP"]),
		Settings:             runtime.CodeBuddySettingsForEnv(backend, env),
	})
	if err != nil {
		return nil, fmt.Errorf("build cursor home: %w", err)
	}
	if err := runtime.InstallBoundCodexLoginFile(backend, home, codexLogin, env); err != nil {
		_ = os.RemoveAll(home)
		return nil, err
	}

	env["AGENT_PROVIDER"] = string(backend)
	configRoot := agent.Layout.ConfigRoot
	if backend == runtime.BackendCodex && strings.TrimSpace(configRoot) == "" {
		configRoot = runtime.CodexConfigRoot
	}
	env["CONFIG_ROOT"] = configRoot
	if backend == runtime.BackendCodex {
		env["CODEX_HOME"] = configRoot
	}
	env["GRASP_THREAD_ID"] = threadID
	env["GRASP_RUN_ID"] = runID
	kind := strings.TrimSpace(extra.LaunchKind)
	if kind == "" {
		kind = "assistant"
	}
	env["GRASP_LAUNCH_KIND"] = kind
	sandbox.ApplyPasswords(env, sharedToken)
	env["GIT_REPOS"] = sandbox.EncodeRepos(extra.Repos)
	if initial := strings.TrimSpace(extra.InitialChatModel); initial != "" {
		env[runtime.EnvInitialChatModel] = initial
	}
	if extra.InitialChatReasoningEffort != nil {
		if effort := strings.TrimSpace(*extra.InitialChatReasoningEffort); effort != "" {
			env[runtime.EnvACPBridgeReasoningEffort] = effort
		} else {
			delete(env, runtime.EnvACPBridgeReasoningEffort)
		}
	}
	track.mark(env, EnvSourceSystem)
	if extra.OnEnv != nil {
		extra.OnEnv(track.sources)
	}

	spec := sandbox.Spec{
		GitBindingsConfigured: true,
		GitCredentials:        gitCredentials,
		Name:                  name,
		Image:                 resolveSandboxImage(string(backend)),
		Env:                   env,
		ConfigHome:            home,
		ConfigRoot:            configRoot,
		WorkspaceDir:          agent.Layout.WorkspaceDir,
	}

	return &preparedAgentContainer{spec: spec, specs: specs, backend: backend, modelFingerprint: modelFingerprint, codexLogin: codexLogin}, nil
}
