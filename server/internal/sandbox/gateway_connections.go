package sandbox

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/config"
)

const gatewayIDPrefix = "gw1_"
const gatewayBindingLabel = "grasp.gateway.id"

type GatewayInfo struct {
	Version   int            `json:"version"`
	Driver    string         `json:"driver"`
	Transport string         `json:"transport"`
	Ports     map[string]int `json:"ports"`
	Profiles  map[string]struct {
		Image string `json:"image"`
	} `json:"profiles"`
	Capabilities struct {
		StopMode string `json:"stopMode"`
		DiskMode string `json:"diskMode"`
	} `json:"capabilities"`
}

// InfoFor observes the gateway bound to this sandbox; it never starts a guest.
func (g *GatewayClient) InfoFor(ctx context.Context, id string) (*GatewayInfo, error) {
	client, _, err := g.resolveID(id)
	if err != nil {
		return nil, err
	}
	if client.bridge != nil {
		return client.bridge.info(ctx)
	}
	var info GatewayInfo
	if err := client.doDirect(ctx, http.MethodGet, "/api/v1/info", nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// NewConfiguredGatewayClient preserves the legacy URL for unqualified IDs.
// Named IDs encode a stable connection key, so changing the default cannot
// redirect a running sandbox's credentials or lifecycle calls to another host.
func NewConfiguredGatewayClient(cfg config.SandboxConfig) (*GatewayClient, error) {
	if err := cfg.ValidateGateways(); err != nil {
		return nil, err
	}
	g := NewGatewayClient(cfg.GatewayURL, cfg.GatewayAPIKey)
	g.transport, g.defaultGateway = cfg.GatewayTransport, cfg.DefaultGateway
	g.gateways = map[string]*GatewayClient{}
	for _, entry := range cfg.Gateways {
		key := entry.APIKey
		if entry.APIKeyEnv != "" {
			key = os.Getenv(entry.APIKeyEnv)
			if strings.TrimSpace(key) == "" {
				return nil, fmt.Errorf("sandbox gateway %q API key environment variable is empty", entry.ID)
			}
		}
		if strings.ContainsAny(key, "\r\n") {
			return nil, fmt.Errorf("sandbox gateway %q API key is not a valid header value", entry.ID)
		}
		child := NewGatewayClient(entry.URL, key)
		child.id, child.transport, child.profile = entry.ID, entry.Transport, entry.Profile
		if child.transport == "" {
			child.transport = "gateway"
		}
		g.gateways[entry.ID] = child
	}
	for _, client := range g.connections() {
		if client.transport == "gateway" || client.profile != "" {
			client.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		}
		if client.transport == "gateway" {
			client.bridge = newGatewayBridge(client)
		}
	}
	return g, nil
}

func (g *GatewayClient) connections() []*GatewayClient {
	clients := []*GatewayClient{g}
	ids := make([]string, 0, len(g.gateways))
	for id := range g.gateways {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		clients = append(clients, g.gateways[id])
	}
	return clients
}

func (g *GatewayClient) Close() error {
	for _, client := range g.connections() {
		if client.bridge != nil {
			client.bridge.close()
		}
		client.http.CloseIdleConnections()
	}
	return nil
}

func qualifiedGatewayID(gatewayID, nativeID string) string {
	if gatewayID == "" {
		return nativeID
	}
	return gatewayIDPrefix + gatewayID + "_" + base64.RawURLEncoding.EncodeToString([]byte(nativeID))
}

func (g *GatewayClient) resolveID(id string) (*GatewayClient, string, error) {
	if !strings.HasPrefix(id, gatewayIDPrefix) {
		return g, id, nil
	}
	name, encoded, ok := strings.Cut(strings.TrimPrefix(id, gatewayIDPrefix), "_")
	native, err := base64.RawURLEncoding.DecodeString(encoded)
	if !ok || err != nil || len(native) == 0 || strings.ContainsAny(string(native), "/\\?#") || string(native) == "." || string(native) == ".." {
		return nil, "", errors.New("invalid sandbox gateway binding")
	}
	client, ok := g.gateways[name]
	if !ok {
		return nil, "", fmt.Errorf("sandbox gateway %q is not configured; retain its connection until its sandboxes are retired", name)
	}
	return client, string(native), nil
}

func (g *GatewayClient) routePath(path string) (*GatewayClient, string, error) {
	const prefix = "/api/v1/sandboxes/"
	if !strings.HasPrefix(path, prefix) {
		return g, path, nil
	}
	segment, suffix, _ := strings.Cut(strings.TrimPrefix(path, prefix), "/")
	if strings.Contains(segment, "?") {
		segment, suffix, _ = strings.Cut(segment, "?")
		suffix = "?" + suffix
	} else if suffix != "" {
		suffix = "/" + suffix
	}
	id, err := url.PathUnescape(segment)
	if err != nil {
		return nil, "", err
	}
	client, native, err := g.resolveID(id)
	if err != nil {
		return nil, "", err
	}
	return client, prefix + url.PathEscape(native) + suffix, nil
}

func (g *GatewayClient) prepareCreate(ctx context.Context, req *GWCreateRequest) error {
	if g.bridge != nil {
		info, err := g.bridge.info(ctx)
		if err != nil {
			return err
		}
		if g.profile != "" && info.Profiles[g.profile].Image == "" {
			return fmt.Errorf("gateway does not declare environment profile %q", g.profile)
		}
	} else if g.profile != "" {
		var info GatewayInfo
		if err := g.doDirect(ctx, http.MethodGet, "/api/v2/info", nil, &info); err != nil {
			return err
		}
		if info.Version != 2 || info.Profiles[g.profile].Image == "" {
			return fmt.Errorf("gateway does not declare environment profile %q", g.profile)
		}
	}
	if g.profile != "" {
		req.Profile = g.profile
		// The profile resolves its provider image in the gateway. Agent backend
		// selection remains in env and is independent of the sandbox provider.
		req.Image = ""
	}
	if g.id != "" {
		labels := make(map[string]string, len(req.Labels)+1)
		for k, v := range req.Labels {
			labels[k] = v
		}
		labels[gatewayBindingLabel] = g.id
		req.Labels = labels
	}
	return nil
}

func (g *GatewayClient) adaptSandbox(ctx context.Context, sb *GWSandbox) (*GWSandbox, error) {
	return g.adaptSandboxID(ctx, sb, qualifiedGatewayID(g.id, sb.ID))
}

func (g *GatewayClient) adaptSandboxID(ctx context.Context, sb *GWSandbox, id string) (*GWSandbox, error) {
	nativeID := sb.ID
	sb.ID = id
	if g.bridge == nil || len(sb.Endpoints) == 0 {
		return sb, nil
	}
	info, err := g.bridge.info(ctx)
	if err != nil {
		return nil, err
	}
	for key, addr := range sb.Endpoints {
		if addr == "" {
			continue
		}
		port, err := strconv.Atoi(key)
		if err != nil {
			port = info.Ports[key]
		}
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("gateway endpoint %q has no valid guest port", key)
		}
		local, err := g.bridge.endpoint(nativeID, port)
		if err != nil {
			return nil, err
		}
		sb.Endpoints[key] = local
	}
	return sb, nil
}

func (g *GatewayClient) listGateways(ctx context.Context, path string) ([]GWSandbox, error) {
	var result []GWSandbox
	seen := map[string]bool{}
	for _, client := range g.connections() {
		// A named connection may point at the legacy gateway during migration.
		// Fetch it once and distinguish old/new records using the create label.
		if seen[client.baseURL] {
			continue
		}
		seen[client.baseURL] = true
		var out struct {
			Sandboxes []GWSandbox `json:"sandboxes"`
		}
		if err := client.doDirect(ctx, http.MethodGet, path, nil, &out); err != nil {
			return nil, err // incomplete lists must never drive orphan cleanup
		}
		for i := range out.Sandboxes {
			sb := &out.Sandboxes[i]
			owner := client
			if binding := sb.Labels[gatewayBindingLabel]; binding != "" {
				var ok bool
				owner, ok = g.gateways[binding]
				if !ok || owner.baseURL != client.baseURL {
					return nil, fmt.Errorf("sandbox has unavailable gateway binding %q", binding)
				}
			}
			adapted, err := owner.adaptSandbox(ctx, sb)
			if err != nil {
				return nil, err
			}
			result = append(result, *adapted)
		}
	}
	return result, nil
}

// UsesGatewayTransport allows persisted endpoint consumers to resolve fresh
// local listeners after restart rather than dialing a stale saved port.
func (g *GatewayClient) UsesGatewayTransport(id string) bool {
	client, _, err := g.resolveID(id)
	// An unavailable binding must fail closed instead of dialing a persisted
	// loopback port that may now belong to a different sandbox.
	return (err != nil && strings.HasPrefix(id, gatewayIDPrefix)) || (err == nil && client.bridge != nil)
}

func (g *GatewayClient) endpointForPort(ctx context.Context, id string, port int, fallback string) (string, error) {
	if port < 1 || port > 65535 {
		return "", errors.New("invalid sandbox guest port")
	}
	client, native, err := g.resolveID(id)
	if err != nil {
		return "", err
	}
	if client.bridge == nil || fallback == "" {
		return fallback, nil
	}
	if _, err := client.bridge.info(ctx); err != nil {
		return "", err
	}
	return client.bridge.endpoint(native, port)
}

// WaitTransportReady refuses old gateways instead of falling back to direct
// dialing with credentials. This runs before startup reconciliation.
func (g *GatewayClient) WaitTransportReady(ctx context.Context) error {
	for _, client := range g.connections() {
		if client.bridge == nil {
			continue
		}
		var lastErr error
		for {
			if _, err := client.bridge.info(ctx); err == nil {
				break
			} else {
				lastErr = err
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("sandbox gateway %q transport not ready (%v): %w", client.id, lastErr, ctx.Err())
			case <-time.After(time.Second):
			}
		}
	}
	return nil
}
