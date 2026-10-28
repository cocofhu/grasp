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
	_, err := exec.LookPath(name)
	return err
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
