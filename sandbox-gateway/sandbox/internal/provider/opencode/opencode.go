// Package opencode drives OpenCode through `opencode serve`: one long-lived
// HTTP server per session, bound to loopback with a random password. Prompts
// go to the native session with prompt_async and the transcript comes from
// the server's event stream. Input posted while a turn runs joins it at the
// next step (steer), abort stops a turn without stopping the server, and the
// session id continues the conversation in a new server (resume).
package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"

	"backend/internal/provider"
	"backend/internal/provider/modelconnection"
)

const (
	bin        = "opencode"
	runtime    = "opencode-serve"
	configRoot = "/root/.config/opencode"
)

// Config carries what the registry decides about OpenCode credentials.
type Config struct {
	AuthEnvFn func(env []string) []string
}

type openCodeProvider struct{ c Config }

// New returns the OpenCode serve provider.
func New(c Config) provider.Provider { return openCodeProvider{c: c} }

func (openCodeProvider) Name() provider.Name               { return provider.OpenCode }
func (openCodeProvider) Runtime() string                   { return runtime }
func (openCodeProvider) DefaultConfigRoot() string         { return configRoot }
func (openCodeProvider) Transport() provider.TransportKind { return provider.LongLived }

func (p openCodeProvider) AuthEnv(env []string) []string {
	if p.c.AuthEnvFn == nil {
		return env
	}
	return p.c.AuthEnvFn(env)
}

func (p openCodeProvider) ListModels(context.Context) ([]provider.Model, error) {
	models, _, err := modelconnection.Catalog(p.Name(), os.Environ())
	return models, err
}

// Open starts `opencode serve` in the session's directory and opens (or
// resumes) its native session.
func (p openCodeProvider) Open(procCtx, handshakeCtx context.Context, opts provider.OpenOptions,
	onEvent func(json.RawMessage), _ provider.PermissionChooser) (provider.Session, error) {
	prepared, err := modelconnection.Prepare(p.Name(), p.AuthEnv(os.Environ()), opts.Model, configRoot)
	if err != nil {
		return nil, err
	}
	if err := prepared.ValidateOptions(p.Name(), opts); err != nil {
		prepared.Cleanup()
		return nil, err
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		prepared.Cleanup()
		return nil, fmt.Errorf("agent %q not found on PATH (sandbox image lacks this CLI; rebuild it with AGENT_PROVIDERS including %q): %w",
			bin, provider.OpenCode, err)
	}
	if prepared.Platform {
		if err := modelconnection.CheckVersion(procCtx, p.Name(), path, prepared.Env); err != nil {
			prepared.Cleanup()
			return nil, err
		}
	}
	// Validate the native directory before custom variants can declare an
	// otherwise unknown official model in the generated configuration.
	if err := modelconnection.CheckOpenCodeCatalog(handshakeCtx, path, prepared, opts.Cwd); err != nil {
		prepared.Cleanup()
		return nil, err
	}
	prepared, err = modelconnection.ApplyOpenCodeVariant(prepared, opts.ReasoningEffort)
	if err != nil {
		prepared.Cleanup()
		return nil, err
	}
	srv, err := startServer(procCtx, path, opts.Cwd, prepared.Env, opts.CustomArgs)
	if err != nil {
		prepared.Cleanup()
		return nil, fmt.Errorf("start opencode serve: %w", err)
	}
	s := newSession(srv, opts, prepared.Env, onEvent, prepared.Cleanup)
	s.model, s.variant = parseModel(prepared.Model), prepared.Variant
	if prepared.Connection != nil {
		s.displayModel = prepared.ChatModelID()
	}
	if err := s.handshake(handshakeCtx, opts.ResumeSessionID); err != nil {
		_ = s.Close()
		return nil, errors.New(provider.RedactDiagnostic(err.Error(), prepared.Env))
	}
	log.Printf("opencode: 已就绪 session=%s version=%s bin=%s transport=serve", s.id, s.version, path)
	return s, nil
}
