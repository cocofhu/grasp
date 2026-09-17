package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/rs/zerolog/log"
)

// AcpBackend identifies which ACP CLI bridge a agent_profile binds to.
type AcpBackend string

const (
	BackendCursor     AcpBackend = "cursor"
	BackendClaudeCode AcpBackend = "claude_code"
	BackendCodeBuddy  AcpBackend = "codebuddy"
	BackendTrae       AcpBackend = "trae"
	BackendOpenCode   AcpBackend = "opencode"
	BackendCodex      AcpBackend = "codex"
)

// Region / site env keys written by Agent Studio or set manually.
const (
	EnvCodeBuddyRegion = "GRASP_CODEBUDDY_REGION"
	EnvTraeRegion      = "GRASP_TRAE_REGION"

	EnvCodeBuddyInternet = "CODEBUDDY_INTERNET_ENVIRONMENT"
	EnvCodeBuddyBaseURL  = "CODEBUDDY_BASE_URL"
	EnvTraeCLIHost       = "TRAECLI_HOST"
	EnvTraeCLIToken      = "TRAECLI_PERSONAL_ACCESS_TOKEN"

	CodeBuddyStagingEndpoint = "https://staging-codebuddy.tencent.com"
	TraeIntlHost             = "https://www.trae.ai"
)

// CodeBuddySettingsForEnv returns settings.json contents for CodeBuddy when the
// agent targets staging (envRouteMode+endpoint). Nil means no settings file.
// Non-CodeBuddy backends always return nil so stray REGION env cannot pollute
// cursor/claude/trae config homes.
func CodeBuddySettingsForEnv(backend AcpBackend, env map[string]string) map[string]any {
	if NormalizeBackend(string(backend)) != BackendCodeBuddy {
		return nil
	}
	region := strings.ToLower(strings.TrimSpace(env[EnvCodeBuddyRegion]))
	if region != "staging" {
		return nil
	}
	endpoint := strings.TrimSpace(env[EnvCodeBuddyBaseURL])
	if endpoint == "" {
		endpoint = CodeBuddyStagingEndpoint
	}
	return map[string]any{
		"envRouteMode": "staging",
		"endpoint":     endpoint,
		"env": map[string]string{
			EnvCodeBuddyInternet: "public",
		},
	}
}

// DefaultConfigRoot returns the protocol default config root for a backend.
func DefaultConfigRoot(b AcpBackend) string {
	switch NormalizeBackend(string(b)) {
	case BackendCodex:
		return "/root/.codex"
	case BackendClaudeCode:
		return "/root/.claude"
	case BackendCodeBuddy:
		return "/root/.codebuddy"
	case BackendTrae:
		return "/root/.trae"
	case BackendOpenCode:
		return "/root/.config/opencode"
	default:
		return "/root/.cursor"
	}
}

// NormalizeBackend coerces unknown/empty values to cursor for backward compat.
func NormalizeBackend(raw string) AcpBackend {
	switch AcpBackend(strings.TrimSpace(raw)) {
	case BackendCursor, BackendClaudeCode, BackendCodeBuddy, BackendTrae, BackendOpenCode, BackendCodex:
		return AcpBackend(strings.TrimSpace(raw))
	default:
		return BackendCursor
	}
}

// ResolveConfigRoot applies backend default when layout configRoot is empty.
func ResolveConfigRoot(backend AcpBackend, layoutConfigRoot string) string {
	if r := strings.TrimSpace(layoutConfigRoot); r != "" {
		return r
	}
	return DefaultConfigRoot(backend)
}

// authSpec describes how agent env keys map to in-container CLI env.
type authSpec struct {
	agentKeys []string // GRASP_* aliases accepted in agent.json env
	cliKey    string   // env var the bridge CLI reads
}

func authSpecFor(b AcpBackend) authSpec {
	switch b {
	case BackendCodex:
		return authSpec{agentKeys: []string{"OPENAI_API_KEY", "CODEX_API_KEY"}, cliKey: "OPENAI_API_KEY"}
	case BackendClaudeCode:
		return authSpec{
			agentKeys: []string{"GRASP_CLAUDE_API_KEY", "ANTHROPIC_API_KEY"},
			cliKey:    "ANTHROPIC_API_KEY",
		}
	case BackendCodeBuddy:
		return authSpec{
			agentKeys: []string{"GRASP_CODEBUDDY_API_KEY", "CODEBUDDY_API_KEY"},
			cliKey:    "CODEBUDDY_API_KEY",
		}
	case BackendTrae:
		// Official traecli headless auth uses TRAECLI_PERSONAL_ACCESS_TOKEN;
		// keep TRAE_API_KEY / GRASP_TRAE_API_KEY as aliases.
		return authSpec{
			agentKeys: []string{
				"GRASP_TRAE_API_KEY",
				"TRAE_API_KEY",
				EnvTraeCLIToken,
			},
			cliKey: EnvTraeCLIToken,
		}
	case BackendOpenCode:
		return authSpec{
			agentKeys: []string{EnvGraspOpenCodeAPIKey, EnvOpenCodeAPIKey},
			cliKey:    EnvOpenCodeAPIKey,
		}
	default:
		return authSpec{
			agentKeys: []string{"GRASP_CURSOR_API_KEY", "CURSOR_API_KEY"},
			cliKey:    "CURSOR_API_KEY",
		}
	}
}

// MergeAuthEnv maps agent-configured API keys into CLI env names and applies
// CodeBuddy / Trae region (intl vs CN) normalization.
func MergeAuthEnv(backend AcpBackend, env map[string]string) (map[string]string, error) {
	return mergeAuthEnv(backend, env, true)
}

// SettingsFileExists reports whether settings.json is present at the root of the
// given workspace. Auth gate treats file presence as sufficient; content
// is not validated or rewritten by the gate.
func SettingsFileExists(workDirSrc string) bool {
	if workDirSrc == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(workDirSrc, "settings.json"))
	return err == nil
}

func OpenCodeConfigFileExists(workDirSrc string) bool {
	if workDirSrc == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(workDirSrc, "opencode.json"))
	return err == nil
}

// AuthConfigFileExists reports whether a backend-specific auth config file is
// present (settings.json for all backends, plus opencode.json for OpenCode).
func AuthConfigFileExists(workDirSrc string, backend AcpBackend) bool {
	if SettingsFileExists(workDirSrc) {
		return true
	}
	if NormalizeBackend(string(backend)) == BackendOpenCode {
		return OpenCodeConfigFileExists(workDirSrc)
	}
	return false
}

// ResolveSettingsWorkDir picks which workspace root's settings.json wins for the
// auth gate, matching BuildConfigHome layering: Agent overlay if present, else
// project-shared extend. Empty sharedWorkDir falls back to the Agent directory only.
func ResolveSettingsWorkDir(agentWorkDir, sharedWorkDir string) string {
	return ResolveAuthWorkDir(agentWorkDir, sharedWorkDir, BackendCursor)
}

// ResolveAuthWorkDir is ResolveSettingsWorkDir with backend-aware config files.
func ResolveAuthWorkDir(agentWorkDir, sharedWorkDir string, backend AcpBackend) string {
	if AuthConfigFileExists(agentWorkDir, backend) {
		return agentWorkDir
	}
	if sharedWorkDir != "" && AuthConfigFileExists(sharedWorkDir, backend) {
		return sharedWorkDir
	}
	if agentWorkDir != "" {
		return agentWorkDir
	}
	return sharedWorkDir
}

// PrepareAuthEnv merges auth keys from workspace settings.json.env into env
// (explicit env wins, including empty overrides), then normalizes via mergeAuthEnv.
// Optional sharedWorkDir is the project-shared Agent workspace (extend layer);
// when the Agent directory has no settings.json, the shared root is used instead.
// When settings.json exists in either layer, the auth gate passes without requiring
// Env keys; content is left to the backend/CLI.
func PrepareAuthEnv(backend AcpBackend, env map[string]string, workDirSrc string, sharedWorkDir ...string) (map[string]string, error) {
	if backend == BackendCodex && strings.TrimSpace(os.Getenv("GRASP_CODEX_AUTH_FILE")) != "" {
		if _, err := sandbox.ReadCodexLocalAuth(); err != nil {
			return nil, err
		}
		return mergeAuthEnv(backend, env, false)
	}
	base := ""
	if len(sharedWorkDir) > 0 {
		base = sharedWorkDir[0]
	}
	settingsDir := ResolveAuthWorkDir(workDirSrc, base, backend)
	settingsAuth := ReadSettingsAuthEnv(settingsDir, backend)
	merged := mergeSettingsAuthIntoEnv(env, settingsAuth)
	requireAuth := !AuthConfigFileExists(settingsDir, backend)
	out, err := mergeAuthEnv(backend, merged, requireAuth)
	if err != nil {
		return out, err
	}
	spec := authSpecFor(backend)
	if strings.TrimSpace(out[spec.cliKey]) == "" && !requireAuth {
		hint := "settings.json"
		if AuthConfigFileExists(settingsDir, backend) {
			if OpenCodeConfigFileExists(settingsDir) {
				hint = "opencode.json"
			}
		}
		log.Warn().
			Str("backend", string(backend)).
			Strs("tried_keys", spec.agentKeys).
			Str("cli_key", spec.cliKey).
			Str("auth_dir", settingsDir).
			Str("gate_file", hint).
			Msg("auth keys empty; gate skipped because a backend config file exists")
	}
	return out, nil
}

// ReadSettingsAuthEnv reads backend-relevant auth keys from settings.json under
// workDirSrc. Missing file, invalid JSON, or absent keys return nil (not an error).
func ReadSettingsAuthEnv(workDirSrc string, backend AcpBackend) map[string]string {
	if workDirSrc == "" {
		return nil
	}
	path := filepath.Join(workDirSrc, "settings.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil
	}
	settingsEnv := settingsEnvFromDoc(doc)
	if len(settingsEnv) == 0 {
		return nil
	}
	spec := authSpecFor(backend)
	keys := append([]string{}, spec.agentKeys...)
	keys = append(keys, spec.cliKey)
	out := map[string]string{}
	for _, k := range keys {
		if v, ok := settingsEnv[k]; ok && strings.TrimSpace(v) != "" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func settingsEnvFromDoc(doc map[string]any) map[string]string {
	raw, ok := doc["env"]
	if !ok {
		return nil
	}
	switch m := raw.(type) {
	case map[string]any:
		out := make(map[string]string, len(m))
		for k, v := range m {
			if s, ok := v.(string); ok {
				out[k] = s
			}
		}
		return out
	case map[string]string:
		return m
	default:
		return nil
	}
}

// mergeSettingsAuthIntoEnv overlays settings auth keys only where env lacks the key.
func mergeSettingsAuthIntoEnv(env, settingsAuth map[string]string) map[string]string {
	if len(settingsAuth) == 0 {
		return env
	}
	out := map[string]string{}
	for k, v := range env {
		out[k] = v
	}
	for k, v := range settingsAuth {
		if _, exists := out[k]; !exists {
			out[k] = v
		}
	}
	return out
}

// by the sandbox bridge. When requireAuth is false (workspace settings.json
// exists), missing keys do not error; region normalization still applies.
func mergeAuthEnv(backend AcpBackend, env map[string]string, requireAuth bool) (map[string]string, error) {
	spec := authSpecFor(backend)
	out := map[string]string{}
	for k, v := range env {
		out[k] = v
	}
	var val string
	for _, k := range spec.agentKeys {
		if v := strings.TrimSpace(out[k]); v != "" {
			val = v
			break
		}
	}
	if val == "" {
		if v := strings.TrimSpace(out[spec.cliKey]); v != "" {
			val = v
		}
	}
	if val == "" {
		if requireAuth {
			cfgHint := "settings.json"
			if backend == BackendOpenCode {
				cfgHint = "opencode.json 或 settings.json"
			}
			return out, fmt.Errorf(
				"鉴权未配置:请在项目共享 Agent 工作目录或该 Agent 工作目录添加 %s，或在项目沙箱 env、Agent 环境变量中设置 %s",
				cfgHint,
				strings.Join(spec.agentKeys, " 或 "),
			)
		}
		mergeRegionEnv(backend, out)
		return out, nil
	}
	out[spec.cliKey] = val
	mergeRegionEnv(backend, out)
	return out, nil
}

// mergeRegionEnv normalizes intl/CN (and CodeBuddy staging) site settings into
// the env vars each CLI actually reads. Explicit official vars win over region aliases.
func mergeRegionEnv(backend AcpBackend, env map[string]string) {
	switch backend {
	case BackendCodeBuddy:
		mergeCodeBuddyRegion(env)
	case BackendTrae:
		mergeTraeRegion(env)
	case BackendOpenCode:
		mergeOpenCodeVendorEnv(env)
	}
}

func mergeCodeBuddyRegion(env map[string]string) {
	region := strings.ToLower(strings.TrimSpace(firstNonEmpty(
		env[EnvCodeBuddyRegion],
		env[EnvCodeBuddyInternet],
	)))
	explicitInternet := strings.TrimSpace(env[EnvCodeBuddyInternet]) != ""
	explicitBase := strings.TrimSpace(env[EnvCodeBuddyBaseURL]) != ""

	switch region {
	case "", "public", "intl", "international":
		if !explicitInternet {
			env[EnvCodeBuddyInternet] = "public"
		}
	case "internal", "cn", "china":
		if !explicitInternet {
			env[EnvCodeBuddyInternet] = "internal"
		}
	case "ioa":
		if !explicitInternet {
			env[EnvCodeBuddyInternet] = "ioa"
		}
	case "staging":
		// Staging needs settings.json envRouteMode+endpoint (BASE_URL alone
		// hits the wrong chat path). Mark via region; config-home writer applies it.
		if !explicitInternet {
			env[EnvCodeBuddyInternet] = "public"
		}
		_ = explicitBase // keep any user BASE_URL; do not invent one for staging
		env[EnvCodeBuddyRegion] = "staging"
	default:
		// Unknown region alias: leave as-is; if it looks like an official
		// INTERNET_ENVIRONMENT value already present, keep it.
		if !explicitInternet && region != "" {
			env[EnvCodeBuddyInternet] = region
		}
	}
}

func mergeTraeRegion(env map[string]string) {
	region := strings.ToLower(strings.TrimSpace(firstNonEmpty(
		env[EnvTraeRegion],
	)))
	explicitHost := strings.TrimSpace(env[EnvTraeCLIHost]) != ""
	switch region {
	case "intl", "international", "public", "ai":
		if !explicitHost {
			env[EnvTraeCLIHost] = TraeIntlHost
		}
	case "", "cn", "china", "internal":
		// CN is the default for the sandbox Trae install (docs.trae.cn); do not
		// force TRAECLI_HOST so enterprise custom domains remain unsettable.
	default:
		// Unknown: no host mutation.
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// AgentRuntimeLabel is the capabilities.agent.runtime string for logging.
func AgentRuntimeLabel(b AcpBackend) string {
	switch b {
	case BackendCodex:
		return "codex-cli"
	case BackendClaudeCode:
		return "claude-code-acp"
	case BackendCodeBuddy:
		return "codebuddy-acp"
	case BackendTrae:
		return "trae-acp"
	case BackendOpenCode:
		return "opencode-json"
	default:
		return "cursor-agent"
	}
}

// WarnDeprecatedExecProvider logs when GRASP_EXEC_PROVIDER is set but ignored.
func WarnDeprecatedExecProvider(name string) {
	if n := strings.TrimSpace(name); n != "" && n != "sandbox" && n != "cursor" {
		log.Warn().Str("GRASP_EXEC_PROVIDER", n).
			Msg("GRASP_EXEC_PROVIDER is deprecated and ignored; route agents via agent_profile acpBackend")
	}
}
