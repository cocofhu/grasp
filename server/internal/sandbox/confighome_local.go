package sandbox

import (
	"crypto/rand"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/pelletier/go-toml/v2"
)

// seedLocalAuthConfigHome snapshots host CLI login/config files into configRoot
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
	var names []string
	switch strings.TrimSpace(provider) {
	case "cursor":
		hostDirs = []string{filepath.Join(home, ".cursor")}
		names = []string{"auth.json", "cli-config.json", "agent-cli-state.json", "settings.json"}
	case "claude_code":
		hostDirs = []string{filepath.Join(home, ".claude")}
		names = []string{".credentials.json", "settings.json", "settings.local.json", "hooks"}
	case "codex":
		hostDirs = []string{filepath.Join(home, ".codex")}
		names = []string{"auth.json", "config.toml"}
	case "codebuddy":
		hostDirs = []string{filepath.Join(home, ".codebuddy")}
		names = []string{"auth.json", ".credentials.json", "settings.json", "models.json", "user-state.json", "local_storage"}
	case "trae":
		hostDirs = []string{filepath.Join(home, ".trae")}
		names = []string{"auth.json", "credentials.json", "config.json", "config.toml", "settings.json", "config.yaml", "tokens.json"}
	case "opencode":
		hostDirs = []string{filepath.Join(home, ".config", "opencode")}
		names = []string{"auth.json", "opencode.json", "opencode.jsonc", "config.json"}
	case "deepseek_harness":
		hostDirs = []string{filepath.Join(home, ".dsh")}
		names = []string{".credentials.yaml", "settings.yaml", "settings.yaml.imported", "cordis.patch.yml", "profiles"}
	case "kimi_code":
		hostDirs = []string{filepath.Join(home, ".kimi"), filepath.Join(home, ".kimi-code")}
		names = []string{"auth.json", "credentials.json", "credentials", "config.toml", "config.json"}
	}
	for _, srcDir := range hostDirs {
		if filepath.Clean(srcDir) != filepath.Clean(configRoot) {
			snapshotLocalConfigEntries(srcDir, configRoot, true, names)
		}
	}
}

// Never retain host symlinks, including nested links in auth directories. Both
// roots confine file operations if a directory entry changes while copying.
// The snapshot is writable by the CLI without changing the host's login state.
func snapshotLocalConfig(source, destination string, merge bool) {
	snapshotLocalConfigEntries(source, destination, merge, nil)
}

func snapshotLocalConfigEntries(source, destination string, merge bool, names []string) {
	info, err := os.Lstat(source)
	if err != nil || !info.IsDir() {
		return
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return
	}
	info, err = os.Lstat(destination)
	if err != nil || !info.IsDir() {
		return
	}
	sourcePath, err := filepath.EvalSymlinks(source)
	if err != nil {
		return
	}
	destinationPath, err := filepath.EvalSymlinks(destination)
	if err != nil {
		return
	}
	rel, err := filepath.Rel(sourcePath, destinationPath)
	if err != nil || rel == "." {
		return
	}
	// A managed config home can live below a host CLI worktree directory.
	// Never import the source branch containing our own destination.
	excluded := ""
	if filepath.IsLocal(rel) {
		excluded, _, _ = strings.Cut(rel, string(filepath.Separator))
	}
	src, err := os.OpenRoot(source)
	if err != nil {
		return
	}
	defer func() { _ = src.Close() }()
	dst, err := os.OpenRoot(destination)
	if err != nil {
		return
	}
	defer func() { _ = dst.Close() }()
	entries, err := fs.ReadDir(src.FS(), ".")
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == excluded || names != nil && !slices.Contains(names, name) {
			continue
		}
		if merge {
			if info, err := dst.Lstat(name); err == nil {
				if info.Mode().IsRegular() {
					mergeLocalConfigFile(src, dst, name)
				}
				continue
			}
		}
		copyLocalConfigEntry(src, dst, name)
	}
}

func copyLocalConfigEntry(src, dst *os.Root, name string) {
	info, err := src.Lstat(name)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return
	}
	if info.IsDir() {
		if err := dst.Mkdir(name, 0o700); err != nil {
			return
		}
		sub, err := src.OpenRoot(name)
		if err != nil {
			return
		}
		defer func() { _ = sub.Close() }()
		entries, err := fs.ReadDir(sub.FS(), ".")
		if err != nil {
			return
		}
		for _, entry := range entries {
			copyLocalConfigEntry(src, dst, filepath.Join(name, entry.Name()))
		}
		return
	}
	if !info.Mode().IsRegular() {
		return
	}
	in, err := src.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return
	}
	defer func() { _ = in.Close() }()
	openedInfo, err := in.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() {
		return
	}
	// Auth remains private while executable hooks and SSH proxies stay usable.
	mode := os.FileMode(0o600) | openedInfo.Mode().Perm()&0o111
	out, err := dst.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		_ = dst.Remove(name)
	}
}

func mergeLocalConfigFile(src, dst *os.Root, name string) {
	switch name {
	case "opencode.json", "settings.json", "config.toml":
	default:
		return
	}
	info, err := src.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	hostBytes, err := src.ReadFile(name)
	if err != nil {
		return
	}
	dstBytes, err := dst.ReadFile(name)
	if err != nil {
		return
	}
	var hostMap, dstMap map[string]any
	var result []byte
	if name == "config.toml" {
		if toml.Unmarshal(hostBytes, &hostMap) != nil || toml.Unmarshal(dstBytes, &dstMap) != nil {
			return
		}
		result, err = toml.Marshal(mergeSettingsMaps(hostMap, dstMap))
	} else {
		if json.Unmarshal(hostBytes, &hostMap) != nil || json.Unmarshal(dstBytes, &dstMap) != nil {
			return
		}
		result, err = json.MarshalIndent(mergeSettingsMaps(hostMap, dstMap), "", "  ")
	}
	if err != nil {
		return
	}
	// Replace the directory entry, rather than truncate it, so even an existing
	// hardlink cannot write back to a host settings/auth file.
	temp := ".grasp-local-" + rand.Text()
	file, err := dst.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = dst.Remove(temp) }()
	_, writeErr := file.Write(result)
	closeErr := file.Close()
	if writeErr == nil && closeErr == nil {
		_ = dst.Rename(temp, name)
	}
}
