package oneshot

import (
	"context"

	"backend/internal/provider"
)

// CLIConfig is the identity, auth and catalog a one-shot CLI codec is
// configured with.
type CLIConfig struct {
	AgentName  provider.Name
	Bin        string
	Runtime    string
	ConfigRoot string
	AuthEnvFn  func(env []string) []string
	Catalog    []provider.Model
}

// CLI implements the Codec methods every one-shot CLI shares: the identity
// accessors, AuthEnv, Models, usage reporting on and the prompt on argv. A
// codec embeds it and supplies Args and ParseLine, overriding any default.
type CLI struct{ cfg CLIConfig }

// NewCLI returns the shared Codec methods for cfg.
func NewCLI(cfg CLIConfig) CLI { return CLI{cfg: cfg} }

func (c CLI) AgentName() provider.Name { return c.cfg.AgentName }
func (c CLI) Bin() string              { return c.cfg.Bin }
func (c CLI) Runtime() string          { return c.cfg.Runtime }
func (c CLI) ConfigRoot() string       { return c.cfg.ConfigRoot }
func (c CLI) ReportsUsage() bool       { return true }
func (c CLI) PromptViaStdin() bool     { return false }

func (c CLI) AuthEnv(env []string) []string {
	if c.cfg.AuthEnvFn == nil {
		return env
	}
	return c.cfg.AuthEnvFn(env)
}

func (c CLI) Models(context.Context) ([]provider.Model, error) { return c.cfg.Catalog, nil }
