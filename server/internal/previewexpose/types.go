// Package previewexpose abstracts how registered sandbox app previews are
// exposed to browsers (direct host/IP:port, wildcard subdomain reverse proxy,
// or none / VNC-only) independently per gateway connection.
package previewexpose

import (
	"context"
	"strings"
)

const (
	StrategyAuto      = "auto"
	StrategyDirect    = "direct"
	StrategySubdomain = "subdomain"
	StrategyNone      = "none"
)

// Target describes a registered preview's upstream sandbox endpoint.
type Target struct {
	RunID        string
	NodeID       string
	SandboxName  string
	Port         int
	InjectorPort int
	Upstream     string // Persisted RunPreviewPort.Host
}

// Binding is the persisted exposure state stored on a preview registration.
type Binding struct {
	Strategy string // Resolved strategy name (never "auto")
	Key      string // Opaque route key; empty for stateless strategies
}

// Exposer defines a browser-facing preview exposure strategy.
type Exposer interface {
	Name() string
	// Bind is called once when set_preview succeeds; it may perform I/O.
	Bind(ctx context.Context, t Target) (Binding, error)
	// URL computes the browser-facing DirectURL on read; must be cheap and side-effect free.
	// Returns "" when no standalone origin is exposed.
	URL(t Target, b Binding) string
}

// Releaser is optionally implemented by exposers that allocate external resources (e.g. tunnels).
type Releaser interface {
	Release(ctx context.Context, b Binding) error
}

// Inbound is optionally implemented by exposers that claim inbound HTTP traffic by Host.
// claimed == true means the host belongs to this strategy's trust boundary and must never
// fall through to platform routes even when key is empty or unknown.
type Inbound interface {
	MatchHost(authority string) (key string, claimed bool)
}

type directExposer struct{}

var directSingleton Exposer = directExposer{}

// Direct returns the stateless direct-URL exposer (Upstream + "/").
func Direct() Exposer { return directSingleton }

func (directExposer) Name() string { return StrategyDirect }

func (directExposer) Bind(_ context.Context, _ Target) (Binding, error) {
	return Binding{Strategy: StrategyDirect}, nil
}

func (directExposer) URL(t Target, _ Binding) string {
	u := strings.TrimRight(strings.TrimSpace(t.Upstream), "/")
	if u == "" {
		return ""
	}
	return u + "/"
}

type noneExposer struct{}

var noneSingleton Exposer = noneExposer{}

// None returns the no-exposure strategy (VNC-only; no DirectURL).
func None() Exposer { return noneSingleton }

func (noneExposer) Name() string { return StrategyNone }

func (noneExposer) Bind(_ context.Context, _ Target) (Binding, error) {
	return Binding{Strategy: StrategyNone}, nil
}

func (noneExposer) URL(_ Target, _ Binding) string { return "" }
