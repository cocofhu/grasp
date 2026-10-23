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
`

type Provider struct{}

func New() provider.Provider                        { return &Provider{} }
func (*Provider) Name() provider.Name               { return provider.DeepSeekHarness }
func (*Provider) Runtime() string                   { return "dsh-acp" }
func (*Provider) DefaultConfigRoot() string         { return configRoot }
func (*Provider) Transport() provider.TransportKind { return provider.LongLived }

func (*Provider) ListModels(context.Context) ([]provider.Model, error) {
	return []provider.Model{
		{ID: defaultModel, Name: "DeepSeek Flash", IsDefault: true},
		{ID: "deepseek-v4-pro", Name: "DeepSeek Pro"},
	}, nil
}

func (*Provider) AuthEnv(env []string) []string {
	for _, key := range []string{"DEEPSEEK_API_KEY", "GRASP_DEEPSEEK_API_KEY", "ACP_DEEPSEEK_API_KEY", "ACP_DEEPSEEK_HARNESS_API_KEY"} {
		if value := strings.TrimSpace(common.GetEnvValue(env, key)); value != "" {
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
	if _, err = patch.WriteString(startupPatch); err != nil {
		_ = patch.Close()
		cleanup()
		return nil, fmt.Errorf("dsh startup overlay: %w", err)
	}
	if err = patch.Close(); err != nil {
		cleanup()
		return nil, fmt.Errorf("dsh startup overlay: %w", err)
	}
	panel, err := acp.NewPanel(procCtx, handshakeCtx, []string{"dsh", "--profile", "acp", "--patch", patchPath},
		p.AuthEnv(os.Environ()), opts.Cwd, opts.FSRoot, mcp, onEvent, acp.PermissionChooser(perm))
	if err != nil {
		cleanup()
		return nil, err
	}
	model := strings.TrimSpace(opts.Model)
	if model == "" || model == "auto" {
		model = defaultModel
	}
	value, err := modelValue(panel.ConfigOptions, model)
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
	sess := &session{Session: acpx.Wrap(panel), cleanup: cleanup}
	go func() {
		<-sess.Done()
		cleanup()
	}()
	return sess, nil
}

// modelValue preserves the exact value advertised by ACP. Bare model IDs are
// resolved only within DeepSeek's API-key provider, keeping account routes and
// similarly named third-party models out of this backend's model selection.
func modelValue(options []acp.ConfigOption, model string) (string, error) {
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
			if json.Unmarshal([]byte(value.Value), &route) == nil && len(route) == 2 && route[0] == officialProvider &&
				(model == route[1] || model == officialProvider+"/"+route[1] || model == value.Value) {
				return value.Value, nil
			}
		}
	}
	return "", fmt.Errorf("dsh: model %q is not advertised by the DeepSeek API provider", model)
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
	cleanup func()
}

func (s *session) Info() provider.AgentInfo {
	info := s.Session.Info()
	var route []string
	if json.Unmarshal([]byte(info.ModelID), &route) == nil && len(route) == 2 && route[0] == officialProvider {
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
