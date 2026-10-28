package kimicode

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// ConfigRoot is the official Kimi Code data directory.
	ConfigRoot = "/root/.kimi-code"
	// APIKeyEnv is the process variable named by config.toml. The key itself
	// is never written into that file.
	APIKeyEnv = "KIMI_CODE_API_KEY"
	// RegionEnv is the Agent site selection. It is not a secret.
	RegionEnv = "GRASP_KIMI_CODE_REGION"

	cnBase   = "https://api.moonshot.cn/v1"
	intlBase = "https://api.moonshot.ai/v1"
)

// BaseURL maps a saved site onto the open-platform root. Empty means the
// international default. An unrecognized site is refused so a typo cannot
// silently send the key to the other platform.
func BaseURL(region string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(region)) {
	case "", "intl", "international", "public", "global", "ai":
		return intlBase, nil
	case "cn", "china", "domestic", "internal":
		return cnBase, nil
	default:
		return "", fmt.Errorf("未知的 Kimi Code 服务站点")
	}
}

// LaunchArgv is the ACP process. kimi acp does not accept -m; a chosen model
// is written into config instead, and an empty model is omitted entirely.
func LaunchArgv(model string) []string {
	_ = model
	return []string{"kimi", "acp"}
}

// RenderConfig builds config.toml. The document names APIKeyEnv and never
// contains the key. An empty model leaves default_model unset so the CLI
// keeps its own default.
func RenderConfig(baseURL, model string) string {
	model = strings.TrimSpace(model)
	if model == "auto" {
		model = ""
	}
	var b strings.Builder
	b.WriteString("telemetry = false\n")
	b.WriteString("default_permission_mode = \"auto\"\n")
	if model != "" {
		b.WriteString("default_model = " + tomlString(model) + "\n")
	}
	b.WriteString("\n[providers.kimi]\n")
	b.WriteString("type = \"kimi\"\n")
	b.WriteString("base_url = " + tomlString(baseURL) + "\n")
	b.WriteString("api_key_env = " + tomlString(APIKeyEnv) + "\n")
	if model != "" {
		b.WriteString("\n[models." + tomlTableKey(model) + "]\n")
		b.WriteString("provider = \"kimi\"\n")
		b.WriteString("model = " + tomlString(model) + "\n")
		b.WriteString("max_context_size = " + strconv.Itoa(contextTokens(model)) + "\n")
	}
	return b.String()
}

// WriteConfig replaces config.toml under dir. The file mode keeps it private
// even though the body has no secret. dir is the server-chosen config home,
// never a model id; reject anything that is not an absolute cleaned path.
func WriteConfig(dir, baseURL, model string) error {
	dir = filepath.Clean(strings.TrimSpace(dir))
	if !strings.HasPrefix(dir, "/") || strings.Contains(dir, "..") {
		return fmt.Errorf("kimi config directory must be an absolute path")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, "config.toml")
	return os.WriteFile(path, []byte(RenderConfig(baseURL, model)), 0o600)
}

func contextTokens(model string) int {
	switch model {
	case "kimi-k3.1-pro", "kimi-k3.1-pro-highspeed", "kimi-k3":
		return 1_000_000
	default:
		return 262_144
	}
}

func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func tomlTableKey(s string) string {
	if s == "" || strings.ContainsAny(s, ".\"\\ \t#[]='") {
		return tomlString(s)
	}
	for _, r := range s {
		if r < 0x21 || r > 0x7e {
			return tomlString(s)
		}
	}
	return s
}
