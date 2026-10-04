package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// GatewayClient is a thin REST client for the sandbox-gateway control plane
// (https://.../api/v1/sandboxes). The gateway owns the container/pod lifecycle
// and image; approving no longer talks to Docker directly. Data-plane traffic
// (ACP WebSocket, SSH exec/files) goes straight to the addresses the gateway
// reports in a sandbox's `endpoints` map — never through the gateway.
type GatewayClient struct {
	baseURL string // e.g. http://127.0.0.1:8899
	apiKey  string // Authorization: Bearer <key>; empty = no auth header
	http    *http.Client
}

// NewGatewayClient builds a client for baseURL. A trailing slash is trimmed.
func NewGatewayClient(baseURL, apiKey string) *GatewayClient {
	return &GatewayClient{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:  strings.TrimSpace(apiKey),
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// BaseURL returns the configured gateway base URL.
func (g *GatewayClient) BaseURL() string { return g.baseURL }

// GWSandbox mirrors the gateway's sandboxDTO (see gateway internal/api/handlers.go).
// Endpoints maps both named keys (session/ide/ssh/cdp/novnc) and raw port
// numbers ("8765", "3000", …) to a reachable "host:port" address.
type GWSandbox struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Status    string            `json:"status"`
	Image     string            `json:"image"`
	Namespace string            `json:"namespace,omitempty"`
	Error     string            `json:"error,omitempty"`
	Endpoints map[string]string `json:"endpoints"`
	Labels    map[string]string `json:"labels,omitempty"`
	Resources *GWResources      `json:"resources,omitempty"`
}

// GWResources is the optional per-sandbox resource limit block on create/get.
type GWResources struct {
	CPUCores float64 `json:"cpuCores,omitempty"`
	MemoryMB int64   `json:"memoryMB,omitempty"`
	DiskGi   int64   `json:"diskGi,omitempty"`
}

// Endpoint returns the reachable address for a named or numeric port key.
func (s *GWSandbox) Endpoint(key string) string {
	if s == nil {
		return ""
	}
	return s.Endpoints[key]
}

// hostPort splits an "host:port" endpoint into its parts. Returns ("",0) when
// absent or malformed.
func hostPortOf(addr string) (string, int) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", 0
	}
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		return addr, 0
	}
	host := addr[:i]
	port, _ := strconv.Atoi(addr[i+1:])
	if host == "" {
		host = "127.0.0.1"
	}
	return host, port
}

// GWCreateRequest is the POST /api/v1/sandboxes body.
type GWCreateRequest struct {
	Image        string            `json:"image,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	WorkspaceDir string            `json:"workspaceDir,omitempty"`
	Ports        []int             `json:"ports,omitempty"`
	Mounts       []string          `json:"mounts,omitempty"`
	Config       *GWConfigInject   `json:"config,omitempty"`
	Resources    *GWResources      `json:"resources,omitempty"`
}

// GWConfigInject mirrors the gateway create `config` block.
// Prefer BundleURL (+ Headers) so startup.sh extracts into ConfigRoot before
// services start (remote K8s). HostPath is docker same-host bind-mount only
// and must not be used for remote pods.
type GWConfigInject struct {
	ConfigRoot string `json:"configRoot,omitempty"`
	HostPath   string `json:"hostPath,omitempty"`
	BundleURL  string `json:"bundleUrl,omitempty"`
	Headers    string `json:"headers,omitempty"` // e.g. "Authorization: Bearer <token>"
}

func (g *GatewayClient) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.baseURL+path, rdr)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if g.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+g.apiKey)
	}
	resp, err := g.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read %s %s response: %w", method, path, err)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("gateway %s %s: %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode %s %s response: %w", method, path, err)
		}
	}
	return nil
}

// Create provisions a sandbox and returns the initial record (status usually
// "creating"; poll WaitRunning for endpoints).
func (g *GatewayClient) Create(ctx context.Context, req GWCreateRequest) (*GWSandbox, error) {
	var sb GWSandbox
	if err := g.do(ctx, http.MethodPost, "/api/v1/sandboxes", req, &sb); err != nil {
		return nil, err
	}
	return &sb, nil
}

// Get returns one sandbox by gateway id.
func (g *GatewayClient) Get(ctx context.Context, id string) (*GWSandbox, error) {
	var sb GWSandbox
	if err := g.do(ctx, http.MethodGet, "/api/v1/sandboxes/"+id, nil, &sb); err != nil {
		return nil, err
	}
	return &sb, nil
}

// List returns sandboxes the gateway knows about. Optional labelFilters are
// "key:value" pairs (AND) forwarded as repeated ?label= query params.
func (g *GatewayClient) List(ctx context.Context, labelFilters ...string) ([]GWSandbox, error) {
	path := "/api/v1/sandboxes"
	if len(labelFilters) > 0 {
		q := url.Values{}
		for _, f := range labelFilters {
			f = strings.TrimSpace(f)
			if f == "" {
				continue
			}
			q.Add("label", f)
		}
		if enc := q.Encode(); enc != "" {
			path += "?" + enc
		}
	}
	var out struct {
		Sandboxes []GWSandbox `json:"sandboxes"`
	}
	if err := g.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Sandboxes, nil
}

// Destroy deletes a sandbox by gateway id.
func (g *GatewayClient) Destroy(ctx context.Context, id string) error {
	return g.do(ctx, http.MethodDelete, "/api/v1/sandboxes/"+id, nil, nil)
}

// Stop stops (but retains) a sandbox by gateway id.
func (g *GatewayClient) Stop(ctx context.Context, id string) error {
	return g.do(ctx, http.MethodPost, "/api/v1/sandboxes/"+id+"/stop", nil, nil)
}

// Start (re)starts a stopped sandbox.
func (g *GatewayClient) Start(ctx context.Context, id string) error {
	return g.do(ctx, http.MethodPost, "/api/v1/sandboxes/"+id+"/start", nil, nil)
}

// LiveStatus returns the gateway's live driver status ("running"/"stopped"/
// "pending"/"not_found"/"error").
func (g *GatewayClient) LiveStatus(ctx context.Context, id string) (string, error) {
	var out struct {
		Status string `json:"status"`
	}
	if err := g.do(ctx, http.MethodGet, "/api/v1/sandboxes/"+id+"/status", nil, &out); err != nil {
		return "", err
	}
	return out.Status, nil
}

// Logs fetches PID1 combined stdout/stderr from the gateway control plane
// (GET /api/v1/sandboxes/:id/logs?tail=). Non-follow; empty content is a
// successful read with no output yet.
func (g *GatewayClient) Logs(ctx context.Context, id string, tail int) (string, error) {
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("sandbox id required")
	}
	if tail <= 0 {
		tail = 5000
	}
	path := fmt.Sprintf("/api/v1/sandboxes/%s/logs?tail=%d", url.PathEscape(id), tail)
	var out struct {
		Content string `json:"content"`
	}
	if err := g.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return "", err
	}
	return out.Content, nil
}

// WaitRunning polls Get until the record is "running" with a resolved session
// endpoint, or the timeout/context elapses. An "error" status is returned as an
// error carrying the gateway's message.
func (g *GatewayClient) WaitRunning(ctx context.Context, id string, timeout time.Duration) (*GWSandbox, error) {
	deadline := time.Now().Add(timeout)
	var last *GWSandbox
	for {
		sb, err := g.Get(ctx, id)
		if err == nil {
			last = sb
			switch sb.Status {
			case "running":
				if sb.Endpoint("session") != "" {
					return sb, nil
				}
			case "error":
				return sb, fmt.Errorf("sandbox %s error: %s", id, sb.Error)
			}
		} else {
			log.Debug().Err(err).Str("id", id).Msg("gateway poll get failed (retrying)")
		}
		if time.Now().After(deadline) {
			if last != nil {
				return last, fmt.Errorf("sandbox %s not running after %s (status=%s)", id, timeout, last.Status)
			}
			return nil, fmt.Errorf("sandbox %s not running after %s", id, timeout)
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
