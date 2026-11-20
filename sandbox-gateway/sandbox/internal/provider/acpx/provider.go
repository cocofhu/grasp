package acpx

import (
	"context"
	"encoding/json"
	"os"

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
	panel, err := acp.NewPanel(procCtx, handshakeCtx, acp.PanelSpec{
		Argv: argv, Env: prepared.Env, Cwd: opts.Cwd, FSRoot: opts.FSRoot, MCPServers: opts.McpServers,
		OnEvent: onEvent, Permissions: acp.PermissionChooser(perm), ResumeSessionID: opts.ResumeSessionID,
	})
	if err != nil {
		prepared.Cleanup()
		return nil, err
	}
	return wrapWithCleanup(panel, prepared.Cleanup), nil
}
