package acpx

import (
	"context"
	"encoding/json"
	"os"

	"backend/internal/acp"
	"backend/internal/backend/common"
	"backend/internal/provider"
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

// FromBackend adapts an existing common.Backend into an ACP provider, reusing
// its proven argv/AuthEnv/OnEvent logic.
func FromBackend(be common.Backend) provider.Provider {
	return &backendProvider{Provider: Provider{spec: Spec{
		AgentName:  provider.Name(be.Name()),
		Runtime:    be.Runtime(),
		ConfigRoot: be.DefaultConfigRoot(),
		ArgvFn:     be.Argv,
		AuthEnvFn:  be.AuthEnv,
	}}, be: be}
}

// backendProvider wraps Provider so Backend.OnEvent can filter/rewrite frames
// before they reach the bridge.
type backendProvider struct {
	Provider
	be common.Backend
}

func (p *backendProvider) Open(procCtx, handshakeCtx context.Context, opts provider.OpenOptions,
	onEvent func(json.RawMessage), perm provider.PermissionChooser) (provider.Session, error) {
	wrapped := onEvent
	if onEvent != nil {
		be := p.be
		wrapped = func(ev json.RawMessage) {
			out, keep := be.OnEvent(ev)
			if !keep || onEvent == nil {
				return
			}
			onEvent(out)
		}
	}
	return p.Provider.Open(procCtx, handshakeCtx, opts, wrapped, perm)
}

func (p *Provider) Name() provider.Name              { return p.spec.AgentName }
func (p *Provider) Runtime() string                  { return p.spec.Runtime }
func (p *Provider) DefaultConfigRoot() string        { return p.spec.ConfigRoot }
func (p *Provider) Transport() provider.TransportKind { return provider.LongLived }

func (p *Provider) AuthEnv(env []string) []string {
	if p.spec.AuthEnvFn == nil {
		return env
	}
	return p.spec.AuthEnvFn(env)
}

func (p *Provider) ListModels(ctx context.Context) ([]provider.Model, error) {
	if p.spec.ModelsFn == nil {
		return nil, nil
	}
	return p.spec.ModelsFn(ctx)
}

// Open spawns the CLI, performs the ACP handshake + session/new, and wraps the
// panel as a provider.Session.
func (p *Provider) Open(procCtx, handshakeCtx context.Context, opts provider.OpenOptions,
	onEvent func(json.RawMessage), perm provider.PermissionChooser) (provider.Session, error) {
	argv := p.spec.ArgvFn(opts.Model)
	env := provider.ApplyEnvOverlay(p.AuthEnv(os.Environ()), opts.Env)
	fsRoot := opts.FSRoot
	if fsRoot == "" {
		fsRoot = opts.Cwd
	}
	panel, err := acp.NewPanel(procCtx, handshakeCtx, argv, env, opts.Cwd, fsRoot, opts.McpServers,
		onEvent, acp.PermissionChooser(perm))
	if err != nil {
		return nil, err
	}
	return Wrap(panel), nil
}
