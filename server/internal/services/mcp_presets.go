package services

// Built-in MCP preset ids. The MCP tab offers each preset as a quick-add
// entry; DefaultPlatformMCP and onboarding read the same definitions.
const (
	MCPPresetGraspTool = "grasp-tool"
	MCPPresetBrowser   = "browser"
	MCPPresetDesktop   = "desktop"
	MCPPresetBlender   = "blender"
)

// GraspDesktopBin is where the runtime bundle installs grasp-desktop inside a
// sandbox. Absolute so CLIs started without the profile PATH still find it.
const GraspDesktopBin = "/opt/grasp-runtime/current/bin/grasp-desktop"

// BlenderMCPPort is the Blender MCP add-on port inside the desktop image
// (reserved desktop range 6090–6099).
const BlenderMCPPort = "6092"

// MCPPreset is one built-in MCP server template.
type MCPPreset struct {
	ID string `json:"id"`
	// RequiresDesktop marks presets that only work on the desktop image.
	RequiresDesktop bool      `json:"requiresDesktop"`
	Server          MCPServer `json:"server"`
}

// MCPPresets returns fresh copies of every built-in MCP template.
func MCPPresets() []MCPPreset {
	return []MCPPreset{
		{
			ID: MCPPresetGraspTool,
			Server: MCPServer{
				Name:    GraspToolMCP,
				URL:     "${GRASP_ARTIFACT_URL}",
				Headers: map[string]string{"Authorization": "Bearer ${GRASP_ARTIFACT_TOKEN}"},
			},
		},
		{
			ID: MCPPresetBrowser,
			Server: MCPServer{
				Name:    "chrome-devtools",
				Command: "chrome-devtools-mcp",
				Args:    []string{"--browser-url=http://127.0.0.1:9222"},
			},
		},
		{
			ID:              MCPPresetDesktop,
			RequiresDesktop: true,
			Server: MCPServer{
				Name:    "desktop",
				Command: GraspDesktopBin,
				Args:    []string{"mcp"},
			},
		},
		{
			ID:              MCPPresetBlender,
			RequiresDesktop: true,
			Server: MCPServer{
				Name:    "blender",
				Command: "/usr/local/bin/blender-mcp",
				Env:     map[string]string{"BLENDER_MCP_HOST": "127.0.0.1", "BLENDER_MCP_PORT": BlenderMCPPort},
			},
		},
	}
}

// MCPPresetServer returns the server of the preset with this id.
func MCPPresetServer(id string) (MCPServer, bool) {
	for _, p := range MCPPresets() {
		if p.ID == id {
			return p.Server, true
		}
	}
	return MCPServer{}, false
}
