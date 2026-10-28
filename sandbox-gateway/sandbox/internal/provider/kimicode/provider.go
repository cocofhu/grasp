// Package kimicode runs Kimi Code over ACP with an open-platform API key.
// The key stays in the process environment. config.toml only names that
// variable and the selected site.
package kimicode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

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
	return []provider.Model{{ID: "kimi-k3.1-pro", Name: "Kimi K3.1 Pro", IsDefault: true}}, nil
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
	// Resolve the model route before copying the API key into the environment.
	// The key is not an input to the route id.
	prepared, err := modelconnection.Prepare(p.Name(), os.Environ(), opts.Model, p.DefaultConfigRoot())
	if err != nil {
		return nil, err
	}
	if prepared.Connection != nil {
		prepared.Cleanup()
		return nil, fmt.Errorf("Kimi Code 只使用开放平台，不接受自定义模型接口")
	}
	env := p.AuthEnv(prepared.Env)
	secret := strings.TrimSpace(common.GetEnvValue(env, APIKeyEnv))
	if secret == "" || strings.ContainsAny(secret, "\r\n\x00") {
		prepared.Cleanup()
		return nil, fmt.Errorf("%s", MsgMissingKey)
	}
	root := strings.TrimSpace(common.GetEnvValue(env, "KIMI_CODE_HOME"))
	if root == "" {
		root = ConfigRoot
	}
	baseURL, err := BaseURL(common.GetEnvValue(env, RegionEnv))
	if err != nil {
		prepared.Cleanup()
		return nil, err
	}
	model := strings.TrimSpace(prepared.Model)
	if model == "auto" {
		model = ""
	}
	if err := WriteConfig(root, baseURL, model); err != nil {
		prepared.Cleanup()
		return nil, fmt.Errorf("kimi config: %w", err)
	}
	wrapped := onEvent
	if onEvent != nil {
		wrapped = func(ev json.RawMessage) { onEvent(RewriteEvent(ev, secret)) }
	}
	panel, err := acp.NewPanel(procCtx, handshakeCtx, LaunchArgv(model), env, opts.Cwd, firstNonEmpty(opts.FSRoot, opts.Cwd), opts.McpServers, wrapped, acp.PermissionChooser(perm))
	if err != nil {
		prepared.Cleanup()
		return nil, fmt.Errorf("%s", Explain(err.Error(), secret))
	}
	return &explainingSession{Session: acpx.Wrap(panel), secret: secret, cleanup: prepared.Cleanup}, nil
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
