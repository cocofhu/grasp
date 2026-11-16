// Package desktop implements the desktop image variant's in-sandbox pieces:
// mode detection, the app catalog, the user/agent control state, the
// launcher behind the launch page, and the desktop MCP server.
package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Desktop-internal services listen on loopback inside the reserved range
// 6090–6099; the platform refuses app previews on that range.
const (
	LauncherPort = 6091
	BlenderPort  = 6092
)

// Mode values reported by Status.
const (
	ModeDesktop = "desktop"
	ModeBrowser = "browser"
)

// Paths locates the files shared by the session script, the backend and the
// MCP server. Tests point them at a temp dir.
type Paths struct {
	// Marker exists only on the desktop image.
	Marker string
	// RunDir holds state (written by desktop-session.sh) and control.
	RunDir string
	// Apps is the image's app catalog.
	Apps string
	// Share is the runtime bundle's share/desktop; its icons/<id>.svg
	// replaces the image icon on the launch page. Empty: image icons only.
	Share string
}

// DefaultPaths honours GRASP_DESKTOP_MARKER / GRASP_DESKTOP_RUN_DIR /
// GRASP_DESKTOP_APPS / GRASP_DESKTOP_SHARE so the scripts and binaries agree
// in tests.
func DefaultPaths() Paths {
	return Paths{
		Marker: envOr("GRASP_DESKTOP_MARKER", "/etc/grasp-desktop"),
		RunDir: envOr("GRASP_DESKTOP_RUN_DIR", "/run/grasp-desktop"),
		Apps:   envOr("GRASP_DESKTOP_APPS", "/opt/grasp-desktop/apps.json"),
		Share:  envOr("GRASP_DESKTOP_SHARE", ""),
	}
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// Available reports whether this is the desktop image.
func (p Paths) Available() bool {
	_, err := os.Stat(p.Marker)
	return err == nil
}

func (p Paths) statePath() string   { return filepath.Join(p.RunDir, "state") }
func (p Paths) controlPath() string { return filepath.Join(p.RunDir, "control") }

// SessionEnv is the desktop session environment written by desktop-session.sh.
func (p Paths) SessionEnv() string { return filepath.Join(p.RunDir, "session.env") }

// Shell is the desktop shell state written by desktop-session.sh.
type Shell struct {
	Mode           string `json:"mode"`
	ShellReady     bool   `json:"shellReady"`
	FallbackReason string `json:"fallbackReason,omitempty"`
}

// ReadShell returns the shell state. Without the marker the mode is browser;
// with the marker but no state yet the shell is still starting.
func (p Paths) ReadShell() Shell {
	if !p.Available() {
		return Shell{Mode: ModeBrowser}
	}
	b, err := os.ReadFile(p.statePath())
	if err != nil {
		return Shell{Mode: ModeDesktop}
	}
	var s Shell
	if json.Unmarshal(b, &s) != nil || (s.Mode != ModeDesktop && s.Mode != ModeBrowser) {
		return Shell{Mode: ModeDesktop}
	}
	if s.Mode == ModeBrowser {
		s.ShellReady = false
	}
	return s
}

// Status is the GET /api/desktop/status body.
type Status struct {
	Mode           string   `json:"mode"`
	ShellReady     bool     `json:"shellReady"`
	AgentActive    bool     `json:"agentActive"`
	Holder         string   `json:"holder"`
	ExpiresAt      string   `json:"expiresAt,omitempty"`
	FallbackReason string   `json:"fallbackReason,omitempty"`
	Apps           []string `json:"apps"`
}

// BuildStatus assembles the status from the shell, control and catalog.
func BuildStatus(p Paths, ctl Control, agentActive bool, lookPath func(string) (string, error)) Status {
	sh := p.ReadShell()
	st := Status{
		Mode:           sh.Mode,
		ShellReady:     sh.ShellReady,
		AgentActive:    agentActive,
		Holder:         ctl.Holder,
		FallbackReason: sh.FallbackReason,
		Apps:           []string{},
	}
	if !ctl.ExpiresAt.IsZero() {
		st.ExpiresAt = ctl.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	if sh.Mode == ModeDesktop {
		for _, a := range InstalledApps(LoadApps(p.Apps), lookPath) {
			st.Apps = append(st.Apps, a.ID)
		}
	}
	return st
}
