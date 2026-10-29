// Package kimicode runs Kimi Code over ACP with Coding Plan authorization.
// Workspace keys stay in the process environment. Local execution uses saved
// authorization in a private configuration and credential snapshot.
package kimicode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"backend/internal/acp"
	"backend/internal/backend/common"
	"backend/internal/provider"
	"backend/internal/provider/acpx"
	"backend/internal/provider/modelconnection"
)

// Provider launches `kimi acp`.
type Provider struct{}

// New returns the Kimi Code ACP provider.
func New() provider.Provider                        { return &Provider{} }
func (*Provider) Name() provider.Name               { return provider.KimiCode }
func (*Provider) Runtime() string                   { return "kimi-code-acp" }
func (*Provider) DefaultConfigRoot() string         { return ConfigRoot }
func (*Provider) Transport() provider.TransportKind { return provider.LongLived }

func (*Provider) ListModels(context.Context) ([]provider.Model, error) {
	if models, selected, err := modelconnection.Catalog(provider.KimiCode, os.Environ()); selected {
		return models, err
	}
	return []provider.Model{
		{ID: defaultModel, Name: "Kimi K3.1 Pro", IsDefault: true},
		{ID: "k3.1-pro-highspeed", Name: "Kimi K3.1 Pro Highspeed"},
		{ID: "k3", Name: "Kimi K3"},
		{ID: "k3-256k", Name: "Kimi K3 256K"},
		{ID: "kimi-for-coding", Name: "Kimi K2.8 Preview"},
		{ID: "kimi-for-coding-highspeed", Name: "Kimi K2.7 Highspeed"},
		// Saved bindings keep their identity until the user changes them.
		{ID: "kimi-k3.1-pro", Name: "Kimi K3.1 Pro"},
		{ID: "kimi-k3.1-pro-highspeed", Name: "Kimi K3.1 Pro Highspeed"},
		{ID: "kimi-k3", Name: "Kimi K3"},
	}, nil
}

// AuthEnv copies the workspace credential onto APIKeyEnv. Exporting
// MOONSHOT_API_KEY alone does not authenticate this CLI.
func (*Provider) AuthEnv(env []string) []string {
	for _, key := range []string{APIKeyEnv, "GRASP_KIMI_CODE_API_KEY", "ACP_KIMI_CODE_API_KEY"} {
		if value := common.GetEnvValue(env, key); strings.TrimSpace(value) != "" {
			env = common.UpsertEnv(env, APIKeyEnv, value)
			break
		}
	}
	root := strings.TrimSpace(common.GetEnvValue(env, "CONFIG_ROOT"))
	if root == "" {
		root = ConfigRoot
	}
	env = common.UpsertEnv(env, "KIMI_CODE_HOME", root)
	env = common.UpsertEnv(env, "KIMI_CODE_TRUST_WORKSPACE", "1")
	env = common.UpsertEnv(env, "KIMI_DISABLE_TELEMETRY", "1")
	env = common.UpsertEnv(env, "KIMI_CODE_NO_AUTO_UPDATE", "1")
	return env
}

func (p *Provider) Open(procCtx, handshakeCtx context.Context, opts provider.OpenOptions,
	onEvent func(json.RawMessage), perm provider.PermissionChooser) (provider.Session, error) {
	// Configuration paths belong to the process, independently of the model
	// route. Never derive filesystem roots from a model-prepared env slice.
	root := strings.TrimSpace(os.Getenv("CONFIG_ROOT"))
	if root == "" {
		root = ConfigRoot
	}
	// Resolve the model route before copying the API key into the environment.
	// The key is not an input to the route id.
	prepared, err := modelconnection.Prepare(p.Name(), os.Environ(), opts.Model, p.DefaultConfigRoot())
	if err != nil {
		return nil, err
	}
	if prepared.Connection != nil {
		prepared.Cleanup()
		return nil, fmt.Errorf("Kimi Code 只使用 Coding Plan，不接受自定义模型接口")
	}
	env := p.AuthEnv(prepared.Env)
	secret := strings.TrimSpace(common.GetEnvValue(env, APIKeyEnv))
	localAuth := strings.TrimSpace(common.GetEnvValue(env, "GRASP_LOCAL_AUTH")) == "1"
	if !localAuth && (secret == "" || strings.ContainsAny(secret, "\r\n\x00")) {
		prepared.Cleanup()
		return nil, fmt.Errorf("%s", MsgMissingKey)
	}
	baseURL, err := BaseURL(common.GetEnvValue(env, RegionEnv))
	if err != nil {
		prepared.Cleanup()
		return nil, err
	}
	model := strings.TrimSpace(prepared.Model)
	if model == "" || model == "auto" {
		model = defaultModel
	}
	if _, supported := canonicalModel(model); !supported {
		prepared.Cleanup()
		return nil, fmt.Errorf("%s", MsgModelUnavailable)
	}
	effort := strings.TrimSpace(opts.ReasoningEffort)
	if err := validateReasoningEffort(model, effort); err != nil {
		prepared.Cleanup()
		return nil, err
	}
	home, err := isolatedHome(root, baseURL, model, effort)
	if err != nil {
		prepared.Cleanup()
		return nil, fmt.Errorf("kimi config: %w", err)
	}
	defer func() { _ = home.Close() }()
	var cleanupOnce sync.Once
	cleanup := func() { cleanupOnce.Do(prepared.Cleanup) }
	env = common.UpsertEnv(env, "KIMI_CODE_HOME", home.Name())
	// Native model environment overrides have higher precedence than config
	// and ACP state. The selected Coding Plan route owns model, credential
	// and strength, including an empty reset to the native default.
	cleanEnv := make([]string, 0, len(env))
	for _, item := range env {
		if key, _, _ := strings.Cut(item, "="); !strings.HasPrefix(key, "KIMI_MODEL_") {
			cleanEnv = append(cleanEnv, item)
		}
	}
	writeConfig := func() error { return writeConfigAt(home, baseURL, model, effort) }
	if localAuth {
		writeConfig = func() error { return writeLocalConfigAt(root, home, baseURL, model, effort) }
	}
	if err := writeConfig(); err != nil {
		cleanup()
		return nil, fmt.Errorf("kimi config: %w", err)
	}
	wrapped := onEvent
	if onEvent != nil {
		wrapped = func(ev json.RawMessage) { onEvent(RewriteEvent(ev, secret)) }
	}
	panel, err := acp.NewPanel(procCtx, handshakeCtx, LaunchArgv(model), cleanEnv, opts.Cwd, firstNonEmpty(opts.FSRoot, opts.Cwd), opts.McpServers, wrapped, acp.PermissionChooser(perm))
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("%s", Explain(err.Error(), secret))
	}
	if err := selectReasoningEffort(handshakeCtx, panel, effort); err != nil {
		_ = panel.Close()
		cleanup()
		return nil, fmt.Errorf("%s", Explain(err.Error(), secret))
	}
	session := &explainingSession{Session: acpx.Wrap(panel), secret: secret, cleanup: cleanup}
	go func() { <-session.Done(); cleanup() }()
	return session, nil
}

// Confirm the native ACP thought-level selection before any model request.
// A missing or boolean-only selector must never silently accept a strength.
func selectReasoningEffort(ctx context.Context, panel *acp.Panel, effort string) error {
	if effort == "" {
		return nil
	}
	option, ok := reasoningOption(panel.ConfigOptions, effort)
	if !ok {
		return fmt.Errorf("Kimi Code: reasoning effort was not advertised by the agent")
	}
	if option.CurrentValue != effort {
		if err := panel.SetConfigOption(ctx, option.ID, effort); err != nil {
			return err
		}
	}
	option, ok = reasoningOption(panel.ConfigOptions, effort)
	if !ok || option.CurrentValue != effort {
		return fmt.Errorf("Kimi Code: reasoning effort selection was not confirmed by the agent")
	}
	return nil
}

func reasoningOption(options []acp.ConfigOption, effort string) (acp.ConfigOption, bool) {
	for _, option := range options {
		if option.Category != "thought_level" || option.ID == "" {
			continue
		}
		for _, value := range option.Options {
			if value.Value == effort {
				return option, true
			}
		}
		for _, group := range option.Groups {
			for _, value := range group.Options {
				if value.Value == effort {
					return option, true
				}
			}
		}
	}
	return acp.ConfigOption{}, false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type explainingSession struct {
	provider.Session
	secret  string
	cleanup func()
}

func (s *explainingSession) Prompt(ctx context.Context, text string, images []provider.PromptImage) (provider.TurnResult, error) {
	result, err := s.Session.Prompt(ctx, text, images)
	if err != nil {
		return result, fmt.Errorf("%s", Explain(err.Error(), s.secret))
	}
	return result, nil
}

func (s *explainingSession) ExitInfo() (string, error) {
	msg, err := s.Session.ExitInfo()
	if err != nil {
		return Explain(msg, s.secret), fmt.Errorf("%s", Explain(err.Error(), s.secret))
	}
	return Explain(msg, s.secret), nil
}

func (s *explainingSession) Close() error {
	err := s.Session.Close()
	if s.cleanup != nil {
		s.cleanup()
	}
	if err != nil {
		return fmt.Errorf("%s", Explain(err.Error(), s.secret))
	}
	return nil
}
