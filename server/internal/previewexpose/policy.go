package previewexpose

import (
	"fmt"
	"strings"
)

// ConnSpec describes one gateway connection's preview exposure inputs.
type ConnSpec struct {
	ConnID    string // "" for legacy/primary gateway, otherwise sandbox.gateways[].id
	Transport string // "direct" or "gateway"
	Strategy  string // Connection-level preview_expose.strategy ("" inherits global)
	BaseURL   string // Connection-level preview_expose.subdomain.base_url ("" inherits global)
}

// PolicyOptions configures a Policy across all gateway connections.
type PolicyOptions struct {
	GlobalStrategy   string // "" | "auto" | "subdomain" | "none" ("direct" forbidden globally)
	GlobalBaseURL    string
	PrivateEndpoints bool // User-explicit sandbox.private_endpoints (affects auto only)
	PublicAdvertise  string
	Production       bool
	Connections      []ConnSpec
}

// ConnSummary records the resolved exposure strategy for startup logging.
type ConnSummary struct {
	ConnID   string
	Strategy string
	BaseURL  string
}

// Policy maps each gateway connection ID to its resolved Exposer.
type Policy struct {
	byConn    map[string]Exposer
	inbounds  []Inbound
	summaries []ConnSummary
}

// NewPolicy validates and resolves effective preview exposure strategies per connection.
func NewPolicy(opts PolicyOptions) (*Policy, []string, error) {
	globalStrategy := strings.ToLower(strings.TrimSpace(opts.GlobalStrategy))
	if globalStrategy == "" {
		globalStrategy = StrategyAuto
	}
	switch globalStrategy {
	case StrategyAuto, StrategySubdomain, StrategyNone:
	case StrategyDirect:
		return nil, nil, fmt.Errorf("preview.expose.strategy cannot be %q globally; configure it per direct gateway connection instead", StrategyDirect)
	default:
		return nil, nil, fmt.Errorf("invalid preview.expose.strategy %q (must be auto, subdomain, or none)", opts.GlobalStrategy)
	}
	globalBase := strings.TrimSpace(opts.GlobalBaseURL)

	p := &Policy{
		byConn:    make(map[string]Exposer, len(opts.Connections)),
		summaries: make([]ConnSummary, 0, len(opts.Connections)),
	}
	byOrigin := make(map[string]*SubdomainExposer)
	var bases []Base
	var warnings []string
	warnedOrigins := make(map[string]bool)

	for _, conn := range opts.Connections {
		label := conn.ConnID
		if label == "" {
			label = "legacy"
		}
		transport := strings.ToLower(strings.TrimSpace(conn.Transport))
		if transport == "" {
			transport = "direct"
		}
		rawStrategy := strings.ToLower(strings.TrimSpace(conn.Strategy))
		if rawStrategy == "" {
			rawStrategy = globalStrategy
		}
		switch rawStrategy {
		case StrategyAuto, StrategyDirect, StrategySubdomain, StrategyNone:
		default:
			return nil, nil, fmt.Errorf("gateway %q: invalid preview_expose.strategy %q", label, conn.Strategy)
		}

		baseRaw := strings.TrimSpace(conn.BaseURL)
		if baseRaw == "" {
			baseRaw = globalBase
		}
		private := transport == "gateway" || opts.PrivateEndpoints

		effective := rawStrategy
		if effective == StrategyAuto {
			switch {
			case private && baseRaw != "":
				effective = StrategySubdomain
			case private:
				effective = StrategyNone
			default:
				effective = StrategyDirect
			}
		}

		switch effective {
		case StrategyDirect:
			if transport == "gateway" {
				return nil, nil, fmt.Errorf("gateway %q: preview_expose.strategy %q is not allowed on gateway transport", label, StrategyDirect)
			}
			p.byConn[conn.ConnID] = Direct()
			p.summaries = append(p.summaries, ConnSummary{ConnID: conn.ConnID, Strategy: StrategyDirect})
		case StrategyNone:
			p.byConn[conn.ConnID] = None()
			p.summaries = append(p.summaries, ConnSummary{ConnID: conn.ConnID, Strategy: StrategyNone})
		case StrategySubdomain:
			if baseRaw == "" {
				return nil, nil, fmt.Errorf("gateway %q: preview_expose.strategy %q requires subdomain.base_url", label, StrategySubdomain)
			}
			base, err := ParseBase(baseRaw)
			if err != nil {
				return nil, nil, fmt.Errorf("gateway %q: %w", label, err)
			}
			warn, err := base.ValidatePlatformOrigin(opts.PublicAdvertise, opts.Production)
			if err != nil {
				return nil, nil, fmt.Errorf("gateway %q: %w", label, err)
			}
			origin := base.Origin()
			if warn != "" && !warnedOrigins[origin] {
				warnedOrigins[origin] = true
				warnings = append(warnings, warn)
			}
			exp, exists := byOrigin[origin]
			if !exists {
				for _, prev := range bases {
					if prev.Overlaps(base) {
						return nil, nil, fmt.Errorf("gateway %q: preview subdomain base_url %q overlaps with %q", label, origin, prev.Origin())
					}
				}
				exp = NewSubdomain(base)
				byOrigin[origin] = exp
				bases = append(bases, base)
				p.inbounds = append(p.inbounds, exp)
			}
			p.byConn[conn.ConnID] = exp
			p.summaries = append(p.summaries, ConnSummary{ConnID: conn.ConnID, Strategy: StrategySubdomain, BaseURL: origin})
		}
	}
	return p, warnings, nil
}

// For returns the Exposer bound to connID. Unknown connections fail closed to None().
func (p *Policy) For(connID string, known bool) Exposer {
	if p == nil || !known {
		return None()
	}
	if exp, ok := p.byConn[connID]; ok && exp != nil {
		return exp
	}
	return None()
}

// Inbounds returns the deduplicated list of Inbound handlers across all connections.
func (p *Policy) Inbounds() []Inbound {
	if p == nil || len(p.inbounds) == 0 {
		return nil
	}
	out := make([]Inbound, len(p.inbounds))
	copy(out, p.inbounds)
	return out
}

// Summaries returns the resolved strategy summary for each configured connection.
func (p *Policy) Summaries() []ConnSummary {
	if p == nil || len(p.summaries) == 0 {
		return nil
	}
	out := make([]ConnSummary, len(p.summaries))
	copy(out, p.summaries)
	return out
}
