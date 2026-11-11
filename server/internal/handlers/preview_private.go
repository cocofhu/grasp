package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/cocofhu/grasp/internal/mcp"
)

// privateEndpoints reports whether sandbox endpoints are reachable only from
// this server (gateway transport or private E2B). The path proxies then add
// the checks a published loopback port needs.
func (h *Handlers) privateEndpoints() bool {
	return h.MCP != nil && h.MCP.PrivateEndpoints()
}

// privatePreviewBound rejects registrations from an older sandbox generation:
// a loopback port can be republished to another sandbox after a restart.
func (h *Handlers) privatePreviewBound(reg mcp.PreviewPort) bool {
	if h.Preview == nil || reg.SandboxName == "" {
		return false
	}
	name, ok := h.Preview.SandboxForRunNode(reg.RunID, reg.NodeID)
	return ok && name == reg.SandboxName
}

// checkPrivateInjectorUpstream verifies a switched injector still serves the
// registered app. It can fall back to PREVIEW_PORT when the app stops; the
// fixed runtime routes are served by the injector itself.
func checkPrivateInjectorUpstream(reg mcp.PreviewPort, path string, resp *http.Response) error {
	if reg.InjectorPort <= 0 {
		return nil
	}
	if privatePreviewRuntimePath(path) && resp.StatusCode != http.StatusSwitchingProtocols {
		return nil
	}
	if privatePreviewUpstreamHeader(resp.Header) != strconv.Itoa(reg.Port) {
		return errors.New("preview upstream does not match registration")
	}
	return nil
}

func privatePreviewUpstreamHeader(header http.Header) string {
	values := header.Values("X-Grasp-Upstream")
	if len(values) != 1 {
		return ""
	}
	port, err := strconv.Atoi(values[0])
	if err != nil || port < 1 || port > 65535 || strconv.Itoa(port) != values[0] {
		return ""
	}
	return values[0]
}

func privatePreviewRuntimePath(path string) bool {
	switch path {
	case "/__grasp/preview-pick.js", "/__grasp/page-control.js", "/__grasp/live-overlay.js", "/__grasp/embed-origin":
		return true
	}
	return false
}

// stripPrivateReservedHeaders keeps provider routing credentials out of both
// directions of a private endpoint proxy.
func stripPrivateReservedHeaders(header http.Header) {
	for key := range header {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "e2b-") || strings.HasPrefix(lower, "x-e2b-") || lower == "proxy-authorization" {
			header.Del(key)
		}
	}
}
