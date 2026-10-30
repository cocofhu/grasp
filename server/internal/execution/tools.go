package execution

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// lookPath is replaced in tests.
var lookPath = defaultLookPath

func defaultLookPath(name string) error {
	if _, err := exec.LookPath(name); err == nil {
		return nil
	} else if !Local() {
		return err
	}
	for _, dir := range localCLIDirs() {
		cand := filepath.Join(dir, name)
		if st, err := os.Stat(cand); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return nil
		}
	}
	return exec.ErrNotFound
}

// Keep discovery and the launched backend's PATH on the same host directories.
func localCLIDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".cursor", "bin"),
		filepath.Join(home, ".kimi", "bin"),
		filepath.Join(home, ".npm-global", "bin"),
	}
}

func appendLocalCLIPath(env map[string]string) {
	if env == nil {
		return
	}
	path, configured := env["PATH"]
	if !configured {
		path = os.Getenv("PATH")
	}
	dirs := filepath.SplitList(path)
	seen := make(map[string]bool, len(dirs))
	for _, dir := range dirs {
		seen[dir] = true
	}
	for _, dir := range localCLIDirs() {
		if !seen[dir] {
			dirs = append(dirs, dir)
			seen[dir] = true
		}
	}
	env["PATH"] = strings.Join(dirs, string(os.PathListSeparator))
}

// KnownProviders is the canonical list of supported Agent CLI providers.
var KnownProviders = []string{
	"claude_code",
	"codex",
	"cursor",
	"opencode",
	"deepseek_harness",
	"kimi_code",
	"codebuddy",
	"trae",
}

// InstalledProviders returns the subset of KnownProviders whose CLI binary is installed locally.
func InstalledProviders() []string {
	out := make([]string, 0, len(KnownProviders))
	for _, p := range KnownProviders {
		bin, ok := CLIForProvider(p)
		if !ok {
			continue
		}
		if err := lookPath(bin); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// HostGitIdentity reads the host user's global git user.name and user.email when running in Local mode.
func HostGitIdentity() (name, email string) {
	if !Local() {
		return "", ""
	}
	if out, err := exec.Command("git", "config", "--global", "--get", "user.name").Output(); err == nil {
		name = strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("git", "config", "--global", "--get", "user.email").Output(); err == nil {
		email = strings.TrimSpace(string(out))
	}
	return name, email
}

// SetLookPathForTest replaces CLI lookup. The returned function restores it.
func SetLookPathForTest(fn func(string) error) func() {
	prev := lookPath
	if fn == nil {
		lookPath = defaultLookPath
	} else {
		lookPath = fn
	}
	return func() { lookPath = prev }
}

// CLIForProvider is the executable a provider must have inside the worktree image.
func CLIForProvider(provider string) (string, bool) {
	switch strings.TrimSpace(provider) {
	case "cursor", "cursor_acp":
		return "cursor-agent", true
	case "claude_code", "claude_code_acp":
		return "claude", true
	case "codebuddy", "codebuddy_acp":
		return "codebuddy", true
	case "trae":
		return "traecli", true
	case "opencode":
		return "opencode", true
	case "codex":
		return "codex", true
	case "deepseek_harness":
		return "dsh", true
	case "kimi_code":
		return "kimi", true
	default:
		return "", false
	}
}

// RequireTools fails when the selected CLI, or a preview component the launch
// asked for, is not on PATH. The caller must not create a sandbox container.
func RequireTools(provider string, env map[string]string) error {
	name := strings.TrimSpace(provider)
	if name == "" {
		name = "cursor"
	}
	bin, ok := CLIForProvider(name)
	if !ok {
		return fmt.Errorf("工作树镜像不支持工具 %s，无法启动。不会改用沙箱容器", name)
	}
	if err := lookPath(bin); err != nil {
		return fmt.Errorf("工作树镜像缺少 CLI %s（%s），无法启动。不会改用沙箱容器", bin, name)
	}
	if Local() {
		appendLocalCLIPath(env)
		return nil
	}
	if !previewRequested(env) {
		return nil
	}
	if err := lookPath("chrome-devtools-mcp"); err != nil {
		return fmt.Errorf("工作树镜像缺少本地预览组件 chrome-devtools-mcp，无法启动。不会改用沙箱容器")
	}
	if !hasChromium() {
		return fmt.Errorf("工作树镜像缺少 Playwright Chromium，无法启动。不会改用沙箱容器")
	}
	for _, bin := range []string{"Xvfb", "x11vnc", "websockify", "socat"} {
		if err := lookPath(bin); err != nil {
			return fmt.Errorf("工作树镜像缺少本地预览组件 %s，无法启动。不会改用沙箱容器", bin)
		}
	}
	return nil
}

// PreviewAvailable reports whether the local VNC/browser preview toolchain is installed.
func PreviewAvailable() bool {
	if err := lookPath("chrome-devtools-mcp"); err != nil {
		return false
	}
	if !hasChromium() {
		return false
	}
	for _, bin := range []string{"Xvfb", "x11vnc", "websockify", "socat"} {
		if err := lookPath(bin); err != nil {
			return false
		}
	}
	return true
}

// PreviewRequested reports whether env enables any browser/VNC preview.
func PreviewRequested(env map[string]string) bool { return previewRequested(env) }

func previewRequested(env map[string]string) bool {
	for _, key := range []string{"BROWSER_MCP", "VNC_PREVIEW", "PREVIEW_DIRECT"} {
		switch strings.TrimSpace(env[key]) {
		case "1", "true", "TRUE", "True":
			return true
		}
	}
	return false
}

func hasChromium() bool {
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"} {
		if err := lookPath(name); err == nil {
			return true
		}
	}
	root := strings.TrimSpace(os.Getenv("PLAYWRIGHT_BROWSERS_PATH"))
	if root == "" {
		root = "/ms-playwright"
	}
	matches, err := filepath.Glob(filepath.Join(root, "*", "chrome-linux*", "chrome"))
	return err == nil && len(matches) > 0
}
