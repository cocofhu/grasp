package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Capabilities is the sandbox capability descriptor (Workflow Sandbox Protocol)
// returned by GET /api/capabilities. The platform fetches it for pre-session
// negotiation: which paths to use, whether change reporting / token usage are
// available, etc. Missing fields degrade safely.
type Capabilities struct {
	Protocol string `json:"protocol"`
	Agent    struct {
		Runtime string `json:"runtime"`
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"agent"`
	Session struct {
		WS         string `json:"ws"`
		Events     string `json:"events"`
		TokenUsage bool   `json:"tokenUsage"`
	} `json:"session"`
	Changes struct {
		Endpoint string `json:"endpoint"`
		VCS      string `json:"vcs"`
	} `json:"changes"`
	IDE struct {
		CodeServer bool `json:"codeServer"`
		Port       int  `json:"port"`
	} `json:"ide"`
	// Preview declares the optional in-sandbox VNC desktop (CDP + websockify)
	// used by app_preview. Missing / vnc=false → platform degrades noVNC.
	Preview struct {
		VNC            bool   `json:"vnc"`
		CDPPort        int    `json:"cdpPort"`
		WebsockifyPort int    `json:"websockifyPort"`
		EnableEnv      string `json:"enableEnv"`
	} `json:"preview"`
	// Config declares where MCP / rules / skills / env are injected. Raw so the
	// platform can read declared paths without coupling to a fixed layout.
	Config map[string]json.RawMessage `json:"config"`
}

// SupportsChanges reports whether the sandbox declares a change-reporting
// endpoint. A nil descriptor (older image without /api/capabilities) returns
// false here; callers should still attempt the endpoint best-effort.
//
// Unwired protocol stub: not yet called from Manager / live session paths;
// retained for Workflow Sandbox Protocol completeness and unit tests.
func (c *Capabilities) supportsChanges() bool {
	return c != nil && c.Changes.Endpoint != ""
}

// SupportsPreview reports whether the sandbox declares an in-container VNC
// preview desktop (CDP + websockify).
//
// Unwired protocol stub: not yet called from Manager / live session paths;
// retained for Workflow Sandbox Protocol completeness and unit tests.
func (c *Capabilities) supportsPreview() bool {
	return c != nil && c.Preview.VNC
}

// FetchCapabilities reads a live sandbox's capability descriptor. Best-effort:
// a nil/error result means "descriptor unavailable" and the caller proceeds
// with safe defaults.
//
// Unwired protocol stub: written against GET /api/capabilities but not yet
// wired into Manager session setup; keep in-tree, do not treat as dead code.
func fetchCapabilities(ctx context.Context, host string, port int) (*Capabilities, error) {
	if host == "" {
		host = "127.0.0.1"
	}
	url := fmt.Sprintf("http://%s:%d/api/capabilities", host, port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("capabilities GET: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("capabilities %d", resp.StatusCode)
	}
	var caps Capabilities
	if err := json.NewDecoder(resp.Body).Decode(&caps); err != nil {
		return nil, fmt.Errorf("capabilities decode: %w", err)
	}
	return &caps, nil
}
