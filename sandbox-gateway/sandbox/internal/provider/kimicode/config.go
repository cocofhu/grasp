package kimicode

import (
	"crypto/sha256"
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

	cnBase       = "https://api.kimi.com/coding/v1"
	intlBase     = "https://api.kimi.ai/coding/v1"
	defaultModel = "k3.1-pro"
)

// BaseURL maps a saved site onto the Coding Plan API. Empty means the
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
	return renderConfig(baseURL, model, "")
}

// canonicalModel preserves the three saved K3 aliases while sending only the
// exact IDs supported by the Coding Plan API. Unknown IDs must not reach the
// vendor, which can silently fall back to a different model.
func canonicalModel(model string) (string, bool) {
	switch strings.TrimSpace(model) {
	case "k3.1-pro", "kimi-k3.1-pro":
		return "k3.1-pro", true
	case "k3.1-pro-highspeed", "kimi-k3.1-pro-highspeed":
		return "k3.1-pro-highspeed", true
	case "k3", "kimi-k3":
		return "k3", true
	case "k3-256k", "kimi-for-coding", "kimi-for-coding-highspeed":
		return strings.TrimSpace(model), true
	default:
		return "", false
	}
}

// ReasoningEfforts is the exact set supported by Coding Plan models. The
// K2.7 Highspeed model has no separate effort control.
func ReasoningEfforts(model string) []string {
	wireModel, _ := canonicalModel(model)
	switch wireModel {
	case "k3", "k3.1-pro", "k3.1-pro-highspeed", "k3-256k", "kimi-for-coding":
		return []string{"low", "high", "max"}
	default:
		return nil
	}
}

func validateReasoningEffort(model, effort string) error {
	if effort == "" {
		return nil
	}
	for _, value := range ReasoningEfforts(model) {
		if value == effort {
			return nil
		}
	}
	return fmt.Errorf("Kimi Code: unsupported reasoning effort for the selected model")
}

func defaultReasoningEffort(model string) string {
	wireModel, _ := canonicalModel(model)
	if wireModel == "kimi-for-coding" {
		return "max"
	}
	return "high"
}

func renderConfig(baseURL, model, effort string) string {
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
	if effort != "" {
		b.WriteString("\n[thinking]\nenabled = true\neffort = " + tomlString(effort) + "\n")
	}
	b.WriteString("\n[providers.kimi]\n")
	b.WriteString("type = \"kimi\"\n")
	b.WriteString("base_url = " + tomlString(baseURL) + "\n")
	b.WriteString("api_key_env = " + tomlString(APIKeyEnv) + "\n")
	if model != "" {
		wireModel, _ := canonicalModel(model)
		b.WriteString("\n[models." + tomlTableKey(model) + "]\n")
		b.WriteString("provider = \"kimi\"\n")
		b.WriteString("model = " + tomlString(wireModel) + "\n")
		b.WriteString("max_context_size = " + strconv.Itoa(contextTokens(model)) + "\n")
		// K2.7 Highspeed still thinks; it has a boolean native control,
		// rather than K3/K2.8's separate strength settings. Omitting this
		// capability disables thinking and can trigger a vendor fallback.
		b.WriteString("capabilities = [\"image_in\", \"thinking\", \"tool_use\"]\n")
		if len(ReasoningEfforts(model)) > 0 {
			// Kimi Code 2.4.0 uses these declarations to expose its ACP
			// thought_level selector and validate the native model request.
			b.WriteString("support_efforts = [\"low\", \"high\", \"max\"]\n")
			b.WriteString("default_effort = " + tomlString(defaultReasoningEffort(model)) + "\n")
		}
	}
	return b.String()
}

// WriteConfig replaces config.toml under dir. The file mode keeps it private
// even though the body has no secret. dir is the server-chosen config home,
// never a model id; reject anything that is not an absolute cleaned path.
func WriteConfig(dir, baseURL, model string) error {
	return writeConfig(dir, baseURL, model, "")
}

func writeConfig(dir, baseURL, model, effort string) error {
	model = strings.TrimSpace(model)
	if model == "auto" {
		model = ""
	}
	if _, supported := canonicalModel(model); model != "" && !supported {
		return fmt.Errorf("%s", MsgModelUnavailable)
	}
	if err := validateReasoningEffort(model, effort); err != nil {
		return err
	}
	dir = filepath.Clean(strings.TrimSpace(dir))
	if !strings.HasPrefix(dir, "/") || strings.Contains(dir, "..") {
		return fmt.Errorf("kimi config directory must be an absolute path")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, "config.toml")
	body := renderConfig(baseURL, model, effort)
	if existing, err := os.ReadFile(path); err == nil && string(existing) == body {
		return nil
	}
	// Homes are reused by concurrent processes selecting the same route.
	// Never expose a truncated file to native startup or config watchers.
	temporary, err := os.CreateTemp(dir, ".config-*.toml")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(temporary.Name()) }()
	if _, err := temporary.WriteString(body); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), path)
}

// isolatedHome reuses an immutable home per site/model/effort combination.
// The finite model/effort catalog bounds these directories. Retaining their
// symlink shells keeps native absolute session-index paths valid after exit,
// while all histories, process locks and blobs share the configured home.
// Credentials and provider settings are excluded; configs name an env var.
func isolatedHome(root, baseURL, model, effort string) (string, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("kimi config directory must be an absolute path")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	identity := sha256.Sum256([]byte(baseURL + "\x00" + model + "\x00" + effort))
	dir := filepath.Join(root, ".grasp-configs", fmt.Sprintf("%x", identity[:12]))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	link := func(entry string) error {
		shared := filepath.Join(root, entry)
		path := filepath.Join(dir, entry)
		if err := os.Symlink(shared, path); err != nil {
			if target, readErr := os.Readlink(path); readErr != nil || target != shared {
				return err
			}
		}
		return nil
	}
	for _, entry := range []string{"sessions", "blobs", "store", "cache", "logs", "locks"} {
		shared := filepath.Join(root, entry)
		if err := os.MkdirAll(shared, 0o700); err != nil {
			return "", err
		}
		if err := link(entry); err != nil {
			return "", err
		}
	}
	index, err := os.OpenFile(filepath.Join(root, "session_index.jsonl"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return "", err
	}
	if err := index.Close(); err != nil {
		return "", err
	}
	if err := link("session_index.jsonl"); err != nil {
		return "", err
	}
	for _, entry := range []string{"skills", "agents", "rules", "plugins", "mcp.json", ".mcp.json", "AGENTS.md"} {
		shared := filepath.Join(root, entry)
		if _, err := os.Stat(shared); os.IsNotExist(err) {
			continue
		}
		if err := link(entry); err != nil {
			return "", err
		}
	}
	return dir, nil
}

func contextTokens(model string) int {
	wireModel, _ := canonicalModel(model)
	switch wireModel {
	case "k3.1-pro", "k3.1-pro-highspeed", "k3", "kimi-for-coding":
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
