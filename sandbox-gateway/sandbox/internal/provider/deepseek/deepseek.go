// Package deepseek runs DeepSeek Harness over its native ACP transport.
package deepseek

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"backend/internal/acp"
	"backend/internal/backend/common"
	"backend/internal/provider"
	"backend/internal/provider/acpx"
	"backend/internal/provider/modelconnection"
)

const configRoot = "/root/.dsh"
const defaultModel = "deepseek-flash"
const officialProvider = "deepseek-official"

// ACP's shipped initial model can lag the live API catalog. Select a known
// initial route before session/new resolves it, then negotiate the requested
// model against that session's advertised opaque choices. Launch-time auth
// must also win over any interactive credentials previously stored in DSH_HOME.
// This overlay contains no credentials and never alters the user's config.
const startupPatch = `- id: acp
  config:
    provider: deepseek-official
    model: deepseek-flash
- id: credentials
  disabled: true
- id: authorization
  disabled: true
- id: deepseek-account
  disabled: true
- id: llm-deepseek-account
  disabled: true
- id: llm-pi-ai
  disabled: true
- id: settings
  disabled: true
- id: config-editor
  disabled: true
- id: llm-deepseek
  config:
    apiKeyEnv: DEEPSEEK_API_KEY
    baseURL: https://api.deepseek.com/anthropic
- id: web-search-deepseek
  config:
    apiKeyEnv: DEEPSEEK_API_KEY
    baseURL: https://api.deepseek.com/anthropic/v1
- id: web-deepseek-api
  config:
    apiKeyEnv: DEEPSEEK_API_KEY
    baseURL: https://api.deepseek.com/v1/tools
`

type Provider struct{}

func New() provider.Provider                        { return &Provider{} }
func (*Provider) Name() provider.Name               { return provider.DeepSeekHarness }
func (*Provider) Runtime() string                   { return "dsh-acp" }
func (*Provider) DefaultConfigRoot() string         { return configRoot }
func (*Provider) Transport() provider.TransportKind { return provider.LongLived }

func (*Provider) ListModels(context.Context) ([]provider.Model, error) {
	if models, selected, err := modelconnection.Catalog(provider.DeepSeekHarness, os.Environ()); selected {
		return models, err
	}
	return []provider.Model{
		{ID: defaultModel, Name: "DeepSeek Flash", IsDefault: true},
		{ID: "deepseek-v4-pro", Name: "DeepSeek Pro"},
	}, nil
}

func (*Provider) AuthEnv(env []string) []string {
	for _, key := range []string{"DEEPSEEK_API_KEY", "GRASP_DEEPSEEK_API_KEY", "ACP_DEEPSEEK_API_KEY", "ACP_DEEPSEEK_HARNESS_API_KEY"} {
		if value := common.GetEnvValue(env, key); value != "" {
			env = common.UpsertEnv(env, "DEEPSEEK_API_KEY", value)
			break
		}
	}
	root := strings.TrimSpace(common.GetEnvValue(env, "CONFIG_ROOT"))
	if root == "" {
		root = configRoot
	}
	env = common.UpsertEnv(env, "DSH_HOME", root)
	return env
}

func (p *Provider) Open(procCtx, handshakeCtx context.Context, opts provider.OpenOptions,
	onEvent func(json.RawMessage), perm provider.PermissionChooser) (provider.Session, error) {
	prepared, err := modelconnection.Prepare(p.Name(), p.AuthEnv(os.Environ()), opts.Model, p.DefaultConfigRoot())
	if err != nil {
		return nil, err
	}
	mcp, err := normalizeMCP(opts.McpServers)
	if err != nil {
		return nil, err
	}
	patch, err := os.CreateTemp("", "grasp-dsh-*.patch.yml")
	if err != nil {
		return nil, fmt.Errorf("dsh startup overlay: %w", err)
	}
	patchPath := patch.Name()
	cleanup := func() { _ = os.Remove(patchPath) }
	patchData := []byte(startupPatch)
	route := officialProvider
	if prepared.Connection != nil {
		patchData = prepared.DSHPatch
		route = prepared.RouteID
	}
	if _, err = patch.Write(patchData); err != nil {
		_ = patch.Close()
		cleanup()
		return nil, fmt.Errorf("dsh startup overlay: %w", err)
	}
	if err = patch.Close(); err != nil {
		cleanup()
		return nil, fmt.Errorf("dsh startup overlay: %w", err)
	}
	panel, err := acp.NewPanel(procCtx, handshakeCtx, []string{"dsh", "--profile", "acp", "--patch", patchPath},
		prepared.Env, opts.Cwd, opts.FSRoot, mcp, onEvent, acp.PermissionChooser(perm))
	if err != nil {
		cleanup()
		return nil, err
	}
	model := prepared.Model
	if prepared.Connection == nil {
		model = strings.TrimSpace(model)
	}
	if prepared.Connection == nil && (model == "" || model == "auto") {
		model = defaultModel
	}
	value, err := modelValueForRoute(panel.ConfigOptions, model, route, prepared.Connection == nil)
	if err == nil && panel.ModelID != value {
		err = panel.SetConfigOption(handshakeCtx, "model", value)
		if err == nil && panel.ModelID != value {
			err = fmt.Errorf("dsh: model selection was not confirmed by the agent")
		}
	}
	if err != nil {
		_ = panel.Close()
		cleanup()
		return nil, err
	}
	if effort := strings.TrimSpace(opts.ReasoningEffort); effort != "" {
		configID, current, effortErr := reasoningOption(panel.ConfigOptions, effort)
		if effortErr == nil && current != effort {
			effortErr = panel.SetConfigOption(handshakeCtx, configID, effort)
			if effortErr == nil {
				_, confirmed, confirmErr := reasoningOption(panel.ConfigOptions, effort)
				if confirmErr != nil || confirmed != effort {
					effortErr = fmt.Errorf("dsh: reasoning effort selection was not confirmed by the agent")
				}
			}
		}
		if effortErr != nil {
			_ = panel.Close()
			cleanup()
			return nil, effortErr
		}
	}
	currentEffort, efforts := reasoningOptions(panel.ConfigOptions)
	home := common.GetEnvValue(prepared.Env, "DSH_HOME")
	if strings.TrimSpace(home) == "" {
		home = p.DefaultConfigRoot()
	}
	usage := newUsageTracker(home, panel.SessionID, model)
	panel.PromptUsage = usage.promptDone
	sess := &session{Session: acpx.Wrap(panel), cleanup: cleanup, selectedRoute: route, currentEffort: currentEffort, efforts: efforts, usage: usage}
	go func() {
		<-sess.Done()
		cleanup()
	}()
	return sess, nil
}

func reasoningOptions(options []acp.ConfigOption) (string, []string) {
	for _, option := range options {
		if option.Category != "thought_level" || option.ID == "" {
			continue
		}
		values := append([]acp.ConfigOptionValue(nil), option.Options...)
		for _, group := range option.Groups {
			values = append(values, group.Options...)
		}
		seen := map[string]bool{}
		out := []string{}
		for _, value := range values {
			if value.Value != "" && !seen[value.Value] {
				out = append(out, value.Value)
				seen[value.Value] = true
			}
		}
		return option.CurrentValue, out
	}
	return "", nil
}

// reasoningOption uses the exact thought-level option and opaque values
// advertised after model negotiation. A model name never implies support.
func reasoningOption(options []acp.ConfigOption, effort string) (id, current string, err error) {
	for _, option := range options {
		if option.Category != "thought_level" {
			continue
		}
		values := append([]acp.ConfigOptionValue(nil), option.Options...)
		for _, group := range option.Groups {
			values = append(values, group.Options...)
		}
		for _, value := range values {
			if value.Value == effort && option.ID != "" {
				return option.ID, option.CurrentValue, nil
			}
		}
	}
	return "", "", fmt.Errorf("dsh: reasoning effort %q is not advertised by the selected model", effort)
}

// modelValue preserves the exact value advertised by ACP. Bare model IDs are
// resolved only within DeepSeek's API-key provider, keeping account routes and
// similarly named third-party models out of this backend's model selection.
func modelValue(options []acp.ConfigOption, model string) (string, error) {
	return modelValueForRoute(options, model, officialProvider, true)
}

func modelValueForRoute(options []acp.ConfigOption, model, providerRoute string, legacy bool) (string, error) {
	for _, option := range options {
		if option.ID != "model" {
			continue
		}
		values := append([]acp.ConfigOptionValue(nil), option.Options...)
		for _, group := range option.Groups {
			values = append(values, group.Options...)
		}
		for _, value := range values {
			var route []string
			if json.Unmarshal([]byte(value.Value), &route) == nil && len(route) == 2 && route[0] == providerRoute &&
				(model == route[1] || (legacy && (model == officialProvider+"/"+route[1] || model == value.Value))) {
				return value.Value, nil
			}
		}
	}
	return "", fmt.Errorf("dsh: model %q is not advertised by the selected provider", model)
}

// DSH requires absolute stdio executables; HTTP entries remain unchanged.
func normalizeMCP(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return raw, nil
	}
	var servers []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &servers); err != nil {
		return nil, fmt.Errorf("dsh MCP servers: %w", err)
	}
	for _, server := range servers {
		commandJSON, ok := server["command"]
		if !ok {
			continue
		}
		var command string
		if err := json.Unmarshal(commandJSON, &command); err != nil {
			return nil, fmt.Errorf("dsh MCP command: %w", err)
		}
		if !filepath.IsAbs(command) {
			resolved, err := exec.LookPath(command)
			if err != nil {
				return nil, fmt.Errorf("dsh MCP command %q: %w", command, err)
			}
			command, err = filepath.Abs(resolved)
			if err != nil {
				return nil, fmt.Errorf("dsh MCP command path: %w", err)
			}
		}
		encoded, err := json.Marshal(command)
		if err != nil {
			return nil, fmt.Errorf("dsh MCP command: %w", err)
		}
		server["command"] = encoded
	}
	return json.Marshal(servers)
}

type session struct {
	provider.Session
	cleanup       func()
	selectedRoute string
	currentEffort string
	efforts       []string
	usage         *usageTracker
}

// ReasoningOptions reads the immutable post-handshake snapshot; it never races
// with ACP notifications updating Panel.ConfigOptions during prompts.
func (s *session) ReasoningOptions() (string, []string) {
	return s.currentEffort, append([]string(nil), s.efforts...)
}

// Prompt 在每轮开始前记下投影缓存基线，结束后把四桶差分写入 TurnResult。
// prompt_done 帧里的 usage 由 Panel.PromptUsage 在同一轮结束前发出。
func (s *session) Prompt(ctx context.Context, text string, images []provider.PromptImage) (provider.TurnResult, error) {
	s.usage.begin()
	res, err := s.Session.Prompt(ctx, text, images)
	res.Usage = s.usage.turnUsage()
	return res, err
}

// ReportsUsage 表示该会话会按轮上报用量。缓存读取失败时仍按旧行为省略 usage。
func (s *session) ReportsUsage() bool { return s.usage.enabled() }

// CumulativeUsage 不提供整会话累计量：DSH 的缓存分桶不区分模型，
// 跨模型复用会话时无法正确归属。平台只用每轮 prompt_done 的增量。
func (s *session) CumulativeUsage() map[string]provider.TokenUsage { return nil }

func (s *session) Info() provider.AgentInfo {
	info := s.Session.Info()
	var route []string
	selectedRoute := s.selectedRoute
	if selectedRoute == "" {
		selectedRoute = officialProvider
	}
	if json.Unmarshal([]byte(info.ModelID), &route) == nil && len(route) == 2 && route[0] == selectedRoute {
		info.ModelID = route[1]
	}
	return info
}

func (s *session) Close() error {
	err := s.Session.Close()
	s.cleanup()
	return err
}

func (s *session) AgentPIDs() []int {
	if agent, ok := s.Session.(interface{ AgentPIDs() []int }); ok {
		return agent.AgentPIDs()
	}
	return nil
}
