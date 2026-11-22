package acpx

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"backend/internal/acp"
	"backend/internal/provider"
	"backend/internal/provider/modelconnection"
)

// Spec configures a long-lived ACP provider (one process, multi-turn via
// session/prompt). Concrete agents differ only in these fields.
type Spec struct {
	AgentName  provider.Name
	Runtime    string
	ConfigRoot string
	// ArgvFn builds the subprocess argv for the given model.
	ArgvFn func(model string) []string
	// AuthEnvFn normalizes credentials into the CLI's native env (may be nil).
	AuthEnvFn func(env []string) []string
	// ModelsFn optionally returns a model catalog (nil => auto).
	ModelsFn func(ctx context.Context) ([]provider.Model, error)
	// AuthMethodFn picks the ACP authenticate method for the session's
	// environment ("" skips authenticate).
	AuthMethodFn func(env []string) string
	// ResultUsage reads turn usage from session/prompt results. Enable it
	// only for agents verified to report per-turn counters there.
	ResultUsage bool
	// ConfigModel selects the requested model and reasoning effort through
	// the session's advertised "model" and thought_level config options.
	ConfigModel bool
	// FailureText reports agent reply text that means the turn failed even
	// though session/prompt ended normally.
	FailureText func(text string) bool
}

// Provider is a long-lived ACP provider built from a Spec.
type Provider struct{ spec Spec }

// New builds an ACP provider from a Spec.
func New(s Spec) provider.Provider { return &Provider{spec: s} }

func (p *Provider) Name() provider.Name               { return p.spec.AgentName }
func (p *Provider) Runtime() string                   { return p.spec.Runtime }
func (p *Provider) DefaultConfigRoot() string         { return p.spec.ConfigRoot }
func (p *Provider) Transport() provider.TransportKind { return provider.LongLived }

func (p *Provider) AuthEnv(env []string) []string {
	if p.spec.AuthEnvFn == nil {
		return env
	}
	return p.spec.AuthEnvFn(env)
}

func (p *Provider) ListModels(ctx context.Context) ([]provider.Model, error) {
	if models, selected, err := modelconnection.Catalog(p.Name(), os.Environ()); selected {
		return models, err
	}
	if p.spec.ModelsFn == nil {
		return nil, nil
	}
	return p.spec.ModelsFn(ctx)
}

// agentArgv is the vector passed to StartAgent. The program name stays the
// provider literal from ArgvFn. When a connection is active, extra arguments
// come from Prepared (catalog ids only) and are not the raw request model.
func agentArgv(base []string, prepared modelconnection.Prepared, custom []string) []string {
	if prepared.Connection == nil {
		return base
	}
	argv := make([]string, 0, len(base)+len(custom)+len(prepared.Args))
	argv = append(argv, base...)
	argv = append(argv, custom...)
	argv = append(argv, prepared.Args...)
	return argv
}

// Open spawns the CLI, performs the ACP handshake and opens (or reloads) the
// session, and wraps the panel as a provider.Session.
func (p *Provider) Open(procCtx, handshakeCtx context.Context, opts provider.OpenOptions,
	onEvent func(json.RawMessage), perm provider.PermissionChooser) (provider.Session, error) {
	prepared, err := modelconnection.Prepare(p.Name(), p.AuthEnv(os.Environ()), opts.Model, p.DefaultConfigRoot())
	if err != nil {
		return nil, err
	}
	if err := prepared.ValidateOptions(p.Name(), opts); err != nil {
		prepared.Cleanup()
		return nil, err
	}
	if prepared.Connection != nil {
		if err := modelconnection.CheckVersion(handshakeCtx, p.Name(), "claude", prepared.Env); err != nil {
			prepared.Cleanup()
			return nil, err
		}
	}
	argv := agentArgv(p.spec.ArgvFn(prepared.Model), prepared, opts.CustomArgs)
	var failed *turnFailure
	if p.spec.FailureText != nil {
		onEvent, failed = watchFailures(onEvent, p.spec.FailureText)
	}
	spec := acp.PanelSpec{
		Argv: argv, Env: prepared.Env, Cwd: opts.Cwd, FSRoot: opts.FSRoot, MCPServers: opts.McpServers,
		OnEvent: onEvent, Permissions: acp.PermissionChooser(perm), ResumeSessionID: opts.ResumeSessionID,
	}
	if p.spec.AuthMethodFn != nil {
		spec.AuthMethod = p.spec.AuthMethodFn(prepared.Env)
	}
	if p.spec.ResultUsage {
		spec.ResultUsage = acp.PromptResultUsage
	}
	panel, err := acp.NewPanel(procCtx, handshakeCtx, spec)
	if err != nil {
		prepared.Cleanup()
		return nil, err
	}
	if p.spec.ConfigModel && prepared.Connection == nil {
		if err := selectConfig(handshakeCtx, panel, p.Name(), prepared.Model, opts.ReasoningEffort); err != nil {
			_ = panel.Close()
			prepared.Cleanup()
			return nil, err
		}
	}
	s := wrapWithCleanup(panel, prepared.Cleanup).(*session)
	s.failed = failed
	return s, nil
}

// selectConfig applies the requested model and reasoning effort through the
// session's advertised config options, rejecting values the agent does not
// offer.
func selectConfig(ctx context.Context, panel *acp.Panel, agent provider.Name, model, effort string) error {
	model = strings.TrimSpace(model)
	if model != "" && model != "auto" && panel.ModelID != model {
		if !offers(panel.ConfigOptions, "model", "", model) {
			return fmt.Errorf("%s 不提供模型 %q", agent, model)
		}
		if err := panel.SetConfigOption(ctx, "model", model); err != nil {
			return err
		}
	}
	effort = strings.TrimSpace(effort)
	if effort == "" {
		return nil
	}
	for _, option := range panel.ConfigOptions {
		if option.Category != "thought_level" {
			continue
		}
		if option.CurrentValue == effort {
			return nil
		}
		if !offers(panel.ConfigOptions, option.ID, "thought_level", effort) {
			return fmt.Errorf("%s 不支持思考强度 %q", agent, effort)
		}
		return panel.SetConfigOption(ctx, option.ID, effort)
	}
	return fmt.Errorf("%s 不支持设置思考强度", agent)
}

func offers(options []acp.ConfigOption, id, category, value string) bool {
	for _, option := range options {
		if option.ID != id && (category == "" || option.Category != category) {
			continue
		}
		for _, v := range option.Options {
			if v.Value == value {
				return true
			}
		}
		for _, g := range option.Groups {
			for _, v := range g.Options {
				if v.Value == value {
					return true
				}
			}
		}
	}
	return false
}
