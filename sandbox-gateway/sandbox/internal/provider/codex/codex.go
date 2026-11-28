// Package codex drives the Codex CLI through its app-server: one long-lived
// `codex app-server` process per session, speaking JSON-RPC over stdio. A
// thread holds the conversation and every prompt is one turn on it; a running
// turn is steered with turn/steer and stopped with turn/interrupt, so neither
// steering nor the watchdog has to kill the process.
package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"slices"
	"strings"

	"backend/internal/acp"
	"backend/internal/provider"
	"backend/internal/provider/modelconnection"
)

const (
	bin        = "codex"
	runtime    = "codex-app-server"
	configRoot = "/root/.codex"
	// envReasoningEffort maps to Codex's model_reasoning_effort config key
	// (minimal / low / medium / high, depending on the model).
	envReasoningEffort = "ACP_BRIDGE_REASONING_EFFORT"
	// envServiceTier is a chat speed tier name, or "default" for an explicit
	// standard reset. Unset leaves the CLI's own configuration in place.
	envServiceTier = "ACP_BRIDGE_SERVICE_TIER"
)

// serviceTierConfig maps Grasp speed tiers to Codex config overrides. A tier
// the CLI does not know falls back to an explicit standard request.
var serviceTierConfig = map[string][]string{
	"fast": {"features.fast_mode=true", "service_tier=\"fast\""},
}

type codexProvider struct{}

// New returns the Codex app-server provider.
func New() provider.Provider { return codexProvider{} }

func (codexProvider) Name() provider.Name               { return provider.Codex }
func (codexProvider) Runtime() string                   { return runtime }
func (codexProvider) DefaultConfigRoot() string         { return configRoot }
func (codexProvider) Transport() provider.TransportKind { return provider.LongLived }

// AuthEnv does not inject an API key. Codex authenticates from
// CODEX_HOME/auth.json, or from GRASP_MODEL_API_KEY for a model connection.
func (codexProvider) AuthEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		switch key {
		case "OPENAI_API_KEY", "CODEX_API_KEY", "ACP_CODEX_API_KEY":
			continue
		default:
			out = append(out, item)
		}
	}
	return out
}

func (p codexProvider) ListModels(context.Context) ([]provider.Model, error) {
	models, _, err := modelconnection.Catalog(p.Name(), os.Environ())
	return models, err
}

// Open starts `codex app-server`, initializes it and starts (or resumes) the
// session's thread.
func (p codexProvider) Open(procCtx, handshakeCtx context.Context, opts provider.OpenOptions,
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
			bin, provider.Codex, err)
	}
	if prepared.Platform || prepared.Connection != nil {
		if err := modelconnection.CheckVersion(procCtx, p.Name(), path, prepared.Env); err != nil {
			prepared.Cleanup()
			return nil, err
		}
	}
	tier, tierSet := requestedServiceTier(opts)
	argv := appServerArgv(path, opts, prepared.Args)
	cleanup := prepared.Cleanup
	if c := prepared.Connection; tierSet && c != nil && slices.Contains(c.ModelServiceTiers[prepared.ChatModelID()], "fast") {
		wireTier := "default"
		if tier == "fast" {
			wireTier = "priority"
		}
		localURL, closeProxy, err := startResponsesTierProxy(c.BaseURL, prepared.ChatModelID(), wireTier)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("start Codex Responses tier proxy: %w", err)
		}
		prefix := "model_providers." + prepared.RouteID + "."
		argv = append(argv, "-c", prefix+"base_url="+quoteTOML(localURL), "-c", prefix+"supports_websockets=false")
		prepared.Env = withLoopbackNoProxy(prepared.Env)
		cleanup = func() {
			closeProxy()
			prepared.Cleanup()
		}
	}
	conn, err := acp.StartAgent(procCtx, argv, prepared.Env)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("start codex app-server: %w", err)
	}
	s := newSession(conn, opts, prepared.Env, onEvent, cleanup)
	if prepared.Connection != nil {
		s.model = prepared.ChatModelID()
	}
	if err := s.handshake(handshakeCtx, opts); err != nil {
		_ = s.Close()
		return nil, errors.New(s.diagnostic(err.Error()))
	}
	log.Printf("codex: 已就绪 thread=%s bin=%s transport=app-server", s.threadID, path)
	return s, nil
}

// appServerArgv is the process command line. Everything the session needs
// for its whole life (model routing, reasoning effort, speed tier) is a -c
// override; per-thread settings travel in thread/start.
func appServerArgv(path string, opts provider.OpenOptions, connectionArgs []string) []string {
	argv := []string{path, "app-server"}
	argv = append(argv, connectionArgs...)
	argv = append(argv, opts.CustomArgs...)
	effort := strings.TrimSpace(opts.ReasoningEffort)
	if !opts.ReasoningEffortSet && effort == "" {
		effort = strings.TrimSpace(os.Getenv(envReasoningEffort))
	}
	if effort != "" && effort != "auto" {
		argv = append(argv, "-c", "model_reasoning_effort="+quoteTOML(effort))
	}
	tier, tierSet := requestedServiceTier(opts)
	if tierSet {
		settings, ok := serviceTierConfig[tier]
		if !ok {
			settings = []string{"service_tier=\"default\""}
		}
		for _, setting := range settings {
			argv = append(argv, "-c", setting)
		}
	}
	return argv
}

func requestedServiceTier(opts provider.OpenOptions) (string, bool) {
	tier := strings.TrimSpace(opts.ServiceTier)
	if opts.ServiceTierSet || tier != "" {
		return tier, true
	}
	tier = strings.TrimSpace(os.Getenv(envServiceTier))
	return tier, tier != ""
}

func quoteTOML(value string) string {
	b, _ := json.Marshal(value)
	return string(b)
}
