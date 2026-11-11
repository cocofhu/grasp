package sandbox

import (
	"encoding/json"
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
	// used by set_preview. Missing / vnc=false → platform degrades noVNC.
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
