package sandbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// seedLocalAuthConfigHome links host CLI login/config files into configRoot
// when running in local execution mode without overwriting files already
// materialized by Grasp (such as mcp.json, rules/, skills/, CLAUDE.md, AGENTS.md).
func seedLocalAuthConfigHome(provider, configRoot string) {
	configRoot = strings.TrimSpace(configRoot)
	if configRoot == "" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return
	}
	var hostDirs []string
	switch strings.TrimSpace(provider) {
	case "cursor":
		hostDirs = []string{filepath.Join(home, ".cursor")}
	case "claude_code":
		hostDirs = []string{filepath.Join(home, ".claude")}
	case "codex":
		hostDirs = []string{filepath.Join(home, ".codex")}
	case "codebuddy":
		hostDirs = []string{filepath.Join(home, ".codebuddy")}
	case "trae":
		hostDirs = []string{filepath.Join(home, ".trae")}
	case "opencode":
		hostDirs = []string{filepath.Join(home, ".config", "opencode")}
	case "deepseek_harness":
		hostDirs = []string{filepath.Join(home, ".dsh")}
	case "kimi_code":
		hostDirs = []string{filepath.Join(home, ".kimi"), filepath.Join(home, ".kimi-code")}
	}
	for _, srcDir := range hostDirs {
		if filepath.Clean(srcDir) == filepath.Clean(configRoot) {
			continue
		}
		entries, err := os.ReadDir(srcDir)
		if err != nil {
			continue
		}
		_ = os.MkdirAll(configRoot, 0o755)
		for _, entry := range entries {
			name := entry.Name()
			src := filepath.Join(srcDir, name)
			dst := filepath.Join(configRoot, name)
			if _, err := os.Lstat(dst); err == nil {
				switch name {
				case "opencode.json", "settings.json":
					mergeLocalJSONFile(src, dst)
				case "config.toml":
					mergeLocalTOMLFile(src, dst)
				}
				continue
			}
			_ = os.Symlink(src, dst)
		}
	}
}

func mergeLocalJSONFile(hostPath, dstPath string) {
	hostBytes, err := os.ReadFile(hostPath)
	if err != nil {
		return
	}
	dstBytes, err := os.ReadFile(dstPath)
	if err != nil {
		return
	}
	var hostMap, dstMap map[string]any
	if json.Unmarshal(hostBytes, &hostMap) != nil || json.Unmarshal(dstBytes, &dstMap) != nil {
		return
	}
	merged := mergeSettingsMaps(hostMap, dstMap)
	if b, err := json.MarshalIndent(merged, "", "  "); err == nil {
		_ = os.WriteFile(dstPath, b, 0o644)
	}
}

func mergeLocalTOMLFile(hostPath, dstPath string) {
	hostBytes, err := os.ReadFile(hostPath)
	if err != nil {
		return
	}
	dstBytes, err := os.ReadFile(dstPath)
	if err != nil {
		return
	}
	var hostMap, dstMap map[string]any
	if toml.Unmarshal(hostBytes, &hostMap) != nil || toml.Unmarshal(dstBytes, &dstMap) != nil {
		return
	}
	merged := mergeSettingsMaps(hostMap, dstMap)
	if b, err := toml.Marshal(merged); err == nil {
		_ = os.WriteFile(dstPath, b, 0o600)
	}
}
