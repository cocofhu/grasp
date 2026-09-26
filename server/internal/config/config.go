// Package config holds the typed runtime configuration, loaded from a single
// YAML file (CONFIG_PATH, default "config.yaml") and supporting hot-reload.
//
// Config is a single YAML file. In Kubernetes it is typically mounted read-only
// at CONFIG_PATH from a ConfigMap; the process watches the file via fsnotify
// (see watcher.go). **Do not bake config into the image.**
//
// Precedence: explicit env > config file > code defaults.
// High-sensitivity secrets (e.g. cursor_api_key) should be injected via K8s
// Secret env overrides, not ConfigMap / image. Git host credentials are not
// platform config; they live on Agent meta env / connectors.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

// Config is the resolved server configuration.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Engine   EngineConfig   `yaml:"engine"`
	Sandbox  SandboxConfig  `yaml:"sandbox"`
	Browser  BrowserConfig  `yaml:"browser"`
	Auth     AuthConfig     `yaml:"auth"`
	Security SecurityConfig `yaml:"security"`
	Storage  StorageConfig  `yaml:"storage"`
}

// StorageConfig selects how attachment bytes are persisted (DB keeps refs only).
type StorageConfig struct {
	// Driver is "local" (default) or "cos" (reserved; not implemented yet).
	Driver string `yaml:"driver"`
	// BlobsRoot is the LocalFS directory for blob:{id} objects.
	BlobsRoot string `yaml:"blobs_root"`
}

// SecurityConfig holds cross-cutting secrets. SecretsKey is the master key used
// to encrypt at-rest credentials (external channel app_secret) — treat it like
// a fixed salt: set it once and keep it stable, since rotating it makes existing
// ciphertext undecryptable. Inject via K8s Secret env in production rather than
// committing it to git.
type SecurityConfig struct {
	// SecretsKey is a base64-encoded 32-byte AES-256 key. Empty disables
	// credential encryption (channel app_secret cannot be saved).
	SecretsKey string `yaml:"secrets_key"`
}

// SecretsKey returns the configured at-rest encryption key (base64, 32 bytes),
// trimmed. Empty when unset.
func (c *Config) SecretsKey() string {
	return strings.TrimSpace(c.Security.SecretsKey)
}

// BrowserConfig configures the server-side VNC preview path. Each app_preview
// sandbox embeds Xvfb+Chromium+x11vnc+websockify; the platform dials that
// sandbox over CDP/websockify (no global browser pool). See internal/browser.
type BrowserConfig struct {
	// Enabled is deprecated: VNC preview is always available when the service
	// starts. Kept for config compatibility only.
	Enabled bool `yaml:"enabled"`
	// MaxTabs caps globally concurrent preview tabs (one per viewer session).
	MaxTabs int `yaml:"max_tabs"`
	// MaxTabsPerContainer caps tabs per sandbox desktop; VNC shows one X display
	// per sandbox, so the default is 1.
	MaxTabsPerContainer int `yaml:"max_tabs_per_container"`
	// TabIdleTTLSeconds frees a tab with no activity for this long. 0 = 300.
	TabIdleTTLSeconds int `yaml:"tab_idle_ttl_seconds"`
	// ContainerIdleTTLSeconds drops a cached CDP attachment after the sandbox
	// has held zero tabs for this long (does not destroy the sandbox). 0 = 600.
	ContainerIdleTTLSeconds int `yaml:"container_idle_ttl_seconds"`
}

// TabIdleTTL returns the per-tab idle lifetime before it is freed.
func (c *Config) TabIdleTTL() time.Duration {
	return time.Duration(c.Browser.TabIdleTTLSeconds) * time.Second
}

// ContainerIdleTTL returns how long an empty Chromium container is kept.
func (c *Config) ContainerIdleTTL() time.Duration {
	return time.Duration(c.Browser.ContainerIdleTTLSeconds) * time.Second
}

// AuthUser is one static login account (username + bcrypt password hash).
type AuthUser struct {
	Username     string `yaml:"username"`
	PasswordHash string `yaml:"password_hash"`
	IsAdmin      bool   `yaml:"is_admin"`
}

// AuthConfig holds static-account login settings. password_hash values are
// secrets — inject via K8s Secret env in production, not git.
type AuthConfig struct {
	Users        []AuthUser `yaml:"users"`
	SessionTTL   string     `yaml:"session_ttl"`   // e.g. "168h" or "7d"
	MaxFailures  int        `yaml:"max_failures"`  // login failures before IP lock
	LockDuration string     `yaml:"lock_duration"` // e.g. "5m"
}

// SessionTTLDuration returns the fixed session lifetime (default 7 days).
func (a AuthConfig) SessionTTLDuration() time.Duration {
	if d := parseDuration(a.SessionTTL, 0); d > 0 {
		return d
	}
	return 7 * 24 * time.Hour
}

// LockDurationDuration returns the IP lock duration after too many failures.
func (a AuthConfig) LockDurationDuration() time.Duration {
	if d := parseDuration(a.LockDuration, 0); d > 0 {
		return d
	}
	return 5 * time.Minute
}

type ServerConfig struct {
	Port int `yaml:"port"`
	// DeploymentMode describes the trust boundary. "local-demo" is restricted
	// to loopback development; other values trigger warnings when auth is empty.
	DeploymentMode string `yaml:"deployment_mode"`
	// MCPAdvertise is the base URL the in-container cursor-agent uses to reach
	// the run-scoped artifact-store MCP. Empty defaults to
	// http://host.docker.internal:<port>.
	MCPAdvertise string `yaml:"mcp_advertise"`
	// PublicAdvertise is the browser-facing base URL for preview proxy links
	// and Run→QQ notification deep links (/runs/{id}). Empty defaults to
	// http://localhost:<port>; QQ clients cannot open relative /runs paths.
	PublicAdvertise string `yaml:"public_advertise"`
}

type DatabaseConfig struct {
	// Driver selects the storage backend: "sqlite" (default) or "mysql".
	// When empty it is inferred: a non-empty DSN implies mysql, else sqlite.
	Driver string `yaml:"driver"`
	// Path is the SQLite file; ":memory:" is honored for tests.
	Path string `yaml:"path"`
	// DSN is the MySQL data source name, e.g.
	// "user:pass@tcp(host:3306)/dbname?charset=utf8mb4&parseTime=True&loc=Local".
	// Used only when Driver == "mysql".
	DSN string `yaml:"dsn"`
}

type EngineConfig struct {
	// ExecProvider selects the agent/sandbox execution backend. The product
	// ships a single Docker-sandbox backend; the neutral name is "sandbox"
	// (default), with "cursor" accepted as a compatibility alias. Other values
	// fall back to the sandbox backend.
	ExecProvider string `yaml:"exec_provider"`
	// MaxConcurrentRuns caps simultaneously executing runs.
	MaxConcurrentRuns int `yaml:"max_concurrent_runs"`
	// ProfilesRoot is where skill profiles (rules) are stored.
	ProfilesRoot string `yaml:"profiles_root"`
	// PlatformRulesRoot is where global platform rule defaults are stored.
	PlatformRulesRoot string `yaml:"platform_rules_root"`
	// NodeAutoRetryMax caps how many times a node that fails with a transient /
	// contract-style fault (structured-product contract miss, plan-incomplete,
	// agent/sandbox execution error) and has no explicit failure/rollback edge
	// is auto-retried from its failure position before the run is failed. 0
	// disables auto-retry. Default 3 (see setDefaults). Set via the settings
	// page (DB override) can also express an explicit 0 to disable.
	NodeAutoRetryMax int `yaml:"node_auto_retry_max"`
}

type SandboxConfig struct {
	// Image, when non-empty, is the sandbox image for every acpBackend.
	// Empty falls through to Images[backend] then DefaultSandboxImage.
	Image string `yaml:"image"`
	// Images optionally overrides the image for one acpBackend. Empty entries
	// fall back to DefaultSandboxImage (universal-sandbox:local).
	Images map[string]string `yaml:"images"`
	// GatewayURL is the sandbox-gateway control-plane base URL. approving calls
	// it to create/manage sandboxes instead of driving Docker directly. Empty
	// defaults to http://127.0.0.1:8899 (the start.sh-managed local gateway).
	GatewayURL string `yaml:"gateway_url"`
	// GatewayAPIKey is the optional bearer token for the gateway (empty when
	// the gateway runs with auth disabled).
	GatewayAPIKey string `yaml:"gateway_api_key"`
	// OpenCodeCatalogURL overrides where the OpenCode model catalog is read from
	// (models.dev by default). Point it at a mirror when egress is restricted.
	OpenCodeCatalogURL string `yaml:"opencode_catalog_url"`
	// Env is the vendor-neutral set of environment variables injected into
	// every sandbox container. ACP API keys belong in Agent env or acp_env, not here.
	Env map[string]string `yaml:"env"`
	// AcpEnv is the vendor-neutral map of ACP backend secrets and options merged
	// into every sandbox container's environment (after Env). Per-backend keys
	// keep their vendor semantics (e.g. GRASP_CURSOR_API_KEY, ANTHROPIC_API_KEY).
	AcpEnv map[string]string `yaml:"acp_env"`
	// CursorAPIKey is deprecated: use sandbox.acp_env or Agent env instead.
	// When set, a deprecation warning is logged and the value is NOT injected.
	CursorAPIKey string `yaml:"cursor_api_key"`
	// CursorAuthPath is deprecated: reference-implementation only. When set, a
	// host dir is mounted read-only at /root/.config/cursor for the cursor CLI.
	CursorAuthPath string `yaml:"cursor_auth_path"`
	// AgentChatTimeoutSeconds bounds a single agent/react turn (hard wall-clock
	// cap). A slow-but-productive turn is bounded by this; a stuck one is caught
	// sooner by ChatIdleTimeoutSeconds below.
	AgentChatTimeoutSeconds int `yaml:"agent_chat_timeout_seconds"`
	// ChatIdleTimeoutSeconds aborts a turn when no ACP event arrives within the
	// window (agent/sandbox presumed stuck). 0 = default 720 — above the
	// sandbox bridge watchdog (SANDBOX_TURN_IDLE_TIMEOUT, 10m) so the bridge,
	// which can actually kill the turn, fires first.
	ChatIdleTimeoutSeconds int `yaml:"chat_idle_timeout_seconds"`
	// MaxAttempts caps how many times a node is (re)attempted on a retryable
	// sandbox/ACP fault (create/ACP-ready/connect/mid-turn crash/idle). 0 = 3.
	MaxAttempts int `yaml:"sandbox_max_attempts"`
	// RetryBackoffSeconds is the base backoff between sandbox retries (grows
	// exponentially, capped). 0 = default 2.
	RetryBackoffSeconds int `yaml:"sandbox_retry_backoff_seconds"`
	// CreateTimeoutSeconds bounds how long approving waits for the gateway to
	// report a running sandbox (cold-start: image pull + PVC attach + git clone
	// + boot). Must stay >= the gateway's FinalizeTimeout, else approving gives
	// up before provisioning finishes. 0 = default 1200 (20 min).
	CreateTimeoutSeconds int `yaml:"sandbox_create_timeout_seconds"`
	// TestSandboxTTLMinutes is the idle lifetime of interactive (chat-test)
	// sandboxes before the sweeper reclaims them. 0 = default 30.
	TestSandboxTTLMinutes int `yaml:"test_sandbox_ttl_minutes"`
	// RunSandboxTTLMinutes is how long a per-run workflow node sandbox is kept
	// alive AFTER its node/run finishes, so it can be inspected (terminal / IDE
	// / ACP / container logs) for debugging before the sweeper reclaims it.
	// 0 = default 30. Set to a small value to reclaim sooner on tight hosts.
	RunSandboxTTLMinutes int `yaml:"run_sandbox_ttl_minutes"`
	// MaxTestSandboxes caps concurrently live interactive sandboxes. 0 = 5.
	MaxTestSandboxes int `yaml:"max_test_sandboxes"`
	// WorkDir is the host base directory under which per-sandbox ConfigHome
	// trees (rules/skills/mcp.json) are staged before gateway bundleUrl / SSH
	// inject into the sandbox. Empty = OS temp dir.
	WorkDir string `yaml:"work_dir"`
}

// TestSandboxTTL returns the interactive sandbox idle lifetime.
func (c *Config) TestSandboxTTL() time.Duration {
	return time.Duration(c.Sandbox.TestSandboxTTLMinutes) * time.Minute
}

// RunSandboxTTL returns how long a finished run's node sandbox is retained for
// debugging before the sweeper reclaims it.
func (c *Config) RunSandboxTTL() time.Duration {
	return time.Duration(c.Sandbox.RunSandboxTTLMinutes) * time.Minute
}

// AgentChatTimeout returns the per-turn budget as a duration.
func (c *Config) AgentChatTimeout() time.Duration {
	return time.Duration(c.Sandbox.AgentChatTimeoutSeconds) * time.Second
}

// ChatIdleTimeout returns the per-turn idle (no-activity) window as a duration.
func (c *Config) ChatIdleTimeout() time.Duration {
	return time.Duration(c.Sandbox.ChatIdleTimeoutSeconds) * time.Second
}

// SandboxRetryBackoff returns the base backoff between sandbox retries.
func (c *Config) SandboxRetryBackoff() time.Duration {
	return time.Duration(c.Sandbox.RetryBackoffSeconds) * time.Second
}

// SandboxCreateTimeout returns how long Create waits for the gateway to report
// a running sandbox.
func (c *Config) SandboxCreateTimeout() time.Duration {
	return time.Duration(c.Sandbox.CreateTimeoutSeconds) * time.Second
}

// appConfig holds the current configuration atomically for concurrent-safe
// reload (ConfigMap / fsnotify). All reads go through GetConfig().
var appConfig atomic.Pointer[Config]

// GetConfig returns the current configuration snapshot. Safe for concurrent use.
func GetConfig() *Config { return appConfig.Load() }

// StoreConfig atomically replaces the live configuration. Used by Load(),
// Reload(), and tests.
func StoreConfig(c *Config) { appConfig.Store(c) }

// Load reads, resolves and stores the configuration from path.
func Load(path string) error {
	c, err := parse(path)
	if err != nil {
		return err
	}
	StoreConfig(c)
	return nil
}

// Reload re-reads the config file, re-applies env overrides and defaults, and
// atomically swaps the live config. Returns the new config so callers (e.g.
// the watcher) can diff against the old one.
func Reload(path string) (*Config, error) {
	c, err := parse(path)
	if err != nil {
		return nil, err
	}
	StoreConfig(c)
	return c, nil
}

// parse reads the YAML file, then applies env overrides and defaults so the
// final priority is env > file > default. A missing file is NOT an error: the
// server boots from env/defaults with zero external dependencies (e.g. local
// dev, or before the ConfigMap is mounted). A present-but-malformed
// file is a hard error.
func parse(path string) (*Config, error) {
	c := &Config{}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(data, c); err != nil {
			return nil, fmt.Errorf("parse config %q: %w", path, err)
		}
	case os.IsNotExist(err):
		log.Info().Str("path", path).Msg("config: file not found; using env/defaults")
	default:
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	applyEnvOverrides(c)
	setDefaults(c)
	return c, nil
}

// applyEnvOverrides lets explicit env vars win over the file. The cursor_api_key
// secret is injected here from K8s Secret-backed env.
func applyEnvOverrides(c *Config) {
	for _, option := range OptionDescriptors() {
		if option.Deprecated && os.Getenv(option.Env) != "" {
			log.Warn().Str("env", option.Env).Msg("deprecated configuration option is set")
		}
	}
	if v := envInt("GRASP_PORT"); v != 0 {
		c.Server.Port = v
	}
	if v := env("GRASP_DEPLOYMENT_MODE"); v != "" {
		c.Server.DeploymentMode = v
	}
	if v := env("GRASP_MCP_ADVERTISE"); v != "" {
		c.Server.MCPAdvertise = v
	}
	if v := env("GRASP_PUBLIC_ADVERTISE"); v != "" {
		c.Server.PublicAdvertise = v
	}
	if v := env("GRASP_DB"); v != "" {
		c.Database.Path = v
	}
	if v := env("GRASP_DB_DRIVER"); v != "" {
		c.Database.Driver = v
	}
	if v := env("GRASP_DB_DSN"); v != "" {
		c.Database.DSN = v
	}
	if v := env("GRASP_EXEC_PROVIDER"); v != "" {
		c.Engine.ExecProvider = v
	}
	if v := envInt("GRASP_MAX_RUNS"); v != 0 {
		c.Engine.MaxConcurrentRuns = v
	}
	if v := env("GRASP_PROFILES_ROOT"); v != "" {
		c.Engine.ProfilesRoot = v
	}
	if v := envInt("GRASP_NODE_AUTO_RETRY"); v != 0 {
		c.Engine.NodeAutoRetryMax = v
	}
	if v := env("GRASP_SANDBOX_IMAGE"); v != "" {
		c.Sandbox.Image = v
	}
	applySandboxImageEnv(c)
	if v := env("GRASP_SANDBOX_GATEWAY_URL"); v != "" {
		c.Sandbox.GatewayURL = v
	}
	if v := env("GRASP_SANDBOX_GATEWAY_API_KEY"); v != "" {
		c.Sandbox.GatewayAPIKey = v
	}
	if v := env("GRASP_OPENCODE_CATALOG_URL"); v != "" {
		c.Sandbox.OpenCodeCatalogURL = v
	}
	if v := env("GRASP_BROWSER_ENABLED"); v != "" {
		lv := strings.ToLower(v)
		c.Browser.Enabled = lv == "1" || lv == "true" || lv == "yes"
	}
	// Accept both the GRASP_-prefixed and bare names for the secrets.
	if v := first(env("GRASP_CURSOR_API_KEY"), env("CURSOR_API_KEY")); v != "" {
		c.Sandbox.CursorAPIKey = v
	}
	if v := env("GRASP_CURSOR_AUTH"); v != "" {
		c.Sandbox.CursorAuthPath = v
	}
	if v := env("GRASP_SANDBOX_ENV"); v != "" {
		mergeEnvList(c, v)
	}
	if v := envInt("GRASP_AGENT_TIMEOUT_SEC"); v != 0 {
		c.Sandbox.AgentChatTimeoutSeconds = v
	}
	if v := envInt("GRASP_CHAT_IDLE_SEC"); v != 0 {
		c.Sandbox.ChatIdleTimeoutSeconds = v
	}
	if v := envInt("GRASP_SANDBOX_MAX_ATTEMPTS"); v != 0 {
		c.Sandbox.MaxAttempts = v
	}
	if v := envInt("GRASP_SANDBOX_RETRY_BACKOFF_SEC"); v != 0 {
		c.Sandbox.RetryBackoffSeconds = v
	}
	if v := envInt("GRASP_SANDBOX_CREATE_TIMEOUT_SEC"); v != 0 {
		c.Sandbox.CreateTimeoutSeconds = v
	}
	if v := env("GRASP_SANDBOX_WORK_DIR"); v != "" {
		c.Sandbox.WorkDir = v
	}
	if v := envInt("GRASP_AUTH_MAX_FAILURES"); v != 0 {
		c.Auth.MaxFailures = v
	}
	if v := env("GRASP_AUTH_LOCK_DURATION"); v != "" {
		c.Auth.LockDuration = v
	}
	if v := env("GRASP_AUTH_SESSION_TTL"); v != "" {
		c.Auth.SessionTTL = v
	}
	if v := env("GRASP_AUTH_USERS"); v != "" {
		var users []AuthUser
		if err := yaml.Unmarshal([]byte(v), &users); err == nil && len(users) > 0 {
			c.Auth.Users = users
		}
	}
	if v := env("GRASP_SECRETS_KEY"); v != "" {
		c.Security.SecretsKey = v
	}
	if v := env("GRASP_STORAGE_DRIVER"); v != "" {
		c.Storage.Driver = v
	}
	if v := env("GRASP_BLOBS_ROOT"); v != "" {
		c.Storage.BlobsRoot = v
	}
}

// setDefaults fills any still-empty fields with the built-in defaults. Runs
// after env+file so a default (MCPAdvertise) can derive from the final port.
func setDefaults(c *Config) {
	if c.Server.Port == 0 {
		c.Server.Port = 8080
	}
	if c.Server.DeploymentMode == "" {
		c.Server.DeploymentMode = "development"
	}
	// Resolve the DB driver: an explicit driver wins; otherwise a non-empty
	// DSN implies mysql, else sqlite. The sqlite file path only defaults when
	// actually running sqlite so a mysql deployment isn't forced to set it.
	c.Database.Driver = strings.ToLower(strings.TrimSpace(c.Database.Driver))
	if c.Database.Driver == "" {
		if strings.TrimSpace(c.Database.DSN) != "" {
			c.Database.Driver = "mysql"
		} else {
			c.Database.Driver = "sqlite"
		}
	}
	if c.Database.Driver == "sqlite" && c.Database.Path == "" {
		c.Database.Path = "grasp.db"
	}
	if c.Engine.ExecProvider == "" {
		c.Engine.ExecProvider = "sandbox"
	}
	c.Engine.ExecProvider = strings.ToLower(c.Engine.ExecProvider)
	if c.Engine.MaxConcurrentRuns == 0 {
		c.Engine.MaxConcurrentRuns = 5
	}
	if c.Engine.ProfilesRoot == "" {
		c.Engine.ProfilesRoot = "data/profiles"
	}
	if c.Storage.Driver == "" {
		c.Storage.Driver = "local"
	}
	c.Storage.Driver = strings.ToLower(strings.TrimSpace(c.Storage.Driver))
	if c.Storage.BlobsRoot == "" {
		c.Storage.BlobsRoot = "data/blobs"
	}
	if c.Engine.PlatformRulesRoot == "" {
		c.Engine.PlatformRulesRoot = "data/platform-rules"
	}
	if c.Engine.NodeAutoRetryMax == 0 {
		c.Engine.NodeAutoRetryMax = 3
	}
	// Image intentionally has no compiled-in default: empty means
	// Images[backend] / DefaultSandboxImage (universal-sandbox:local).
	// Release compose sets GRASP_SANDBOX_IMAGE to the published GHCR pin.
	if c.Sandbox.Images == nil {
		c.Sandbox.Images = map[string]string{}
	}
	if c.Sandbox.GatewayURL == "" {
		c.Sandbox.GatewayURL = "http://127.0.0.1:8899"
	}
	if c.Sandbox.CursorAPIKey != "" {
		log.Warn().Msg("sandbox.cursor_api_key / GRASP_CURSOR_API_KEY is deprecated; use sandbox.acp_env or Agent env instead")
	}
	if c.Sandbox.CursorAuthPath != "" {
		log.Warn().Msg("sandbox.cursor_auth_path is deprecated; configure auth per Agent/backend via acp_env")
	}
	mergeAcpEnv(c)
	if c.Sandbox.AgentChatTimeoutSeconds == 0 {
		c.Sandbox.AgentChatTimeoutSeconds = 600
	}
	if c.Sandbox.ChatIdleTimeoutSeconds == 0 {
		c.Sandbox.ChatIdleTimeoutSeconds = 720
	}
	if c.Sandbox.MaxAttempts == 0 {
		c.Sandbox.MaxAttempts = 3
	}
	if c.Sandbox.RetryBackoffSeconds == 0 {
		c.Sandbox.RetryBackoffSeconds = 2
	}
	if c.Sandbox.CreateTimeoutSeconds == 0 {
		c.Sandbox.CreateTimeoutSeconds = 1200
	}
	if c.Sandbox.TestSandboxTTLMinutes == 0 {
		c.Sandbox.TestSandboxTTLMinutes = 30
	}
	if c.Sandbox.RunSandboxTTLMinutes == 0 {
		c.Sandbox.RunSandboxTTLMinutes = 30
	}
	if c.Sandbox.MaxTestSandboxes == 0 {
		c.Sandbox.MaxTestSandboxes = 5
	}
	if c.Browser.MaxTabs == 0 {
		c.Browser.MaxTabs = 16
	}
	if c.Browser.MaxTabsPerContainer == 0 {
		// One X desktop per sandbox; multiple viewers on the same sandbox share it.
		c.Browser.MaxTabsPerContainer = 1
	}
	if c.Browser.TabIdleTTLSeconds == 0 {
		c.Browser.TabIdleTTLSeconds = 300
	}
	if c.Browser.ContainerIdleTTLSeconds == 0 {
		c.Browser.ContainerIdleTTLSeconds = 600
	}
	if c.Server.MCPAdvertise == "" {
		c.Server.MCPAdvertise = fmt.Sprintf("http://host.docker.internal:%d", c.Server.Port)
	} else {
		c.Server.MCPAdvertise = RewriteMisconfiguredMCPAdvertise(c.Server.MCPAdvertise)
	}
	if c.Server.PublicAdvertise == "" {
		c.Server.PublicAdvertise = fmt.Sprintf("http://localhost:%d", c.Server.Port)
	}
	if c.Auth.MaxFailures == 0 {
		c.Auth.MaxFailures = 5
	}
	warnUnsafeAuth(c)
}

func warnUnsafeAuth(c *Config) {
	if len(c.Auth.Users) > 0 {
		return
	}
	mode := strings.ToLower(strings.TrimSpace(c.Server.DeploymentMode))
	publicHost := ""
	if u, err := url.Parse(c.Server.PublicAdvertise); err == nil {
		publicHost = strings.ToLower(u.Hostname())
	}
	loopback := publicHost == "" || publicHost == "localhost" || publicHost == "127.0.0.1" || publicHost == "::1"
	if mode != "local-demo" && (mode == "production" || !loopback) {
		log.Warn().
			Str("deployment_mode", c.Server.DeploymentMode).
			Str("public_advertise", c.Server.PublicAdvertise).
			Msg("no auth users configured for a non-local deployment; set GRASP_AUTH_USERS before exposing Grasp")
	}
}

// RewriteMisconfiguredMCPAdvertise normalizes mcp_advertise input. Historically
// this rewrote deployment-specific SPA hosts to an API ingress; that map is
// empty for the public tree, so the value is returned trimmed and unchanged.
// Callers should set mcp_advertise to a URL that actually serves /mcp/runs/:id
// (for local compose, typically host.docker.internal or the server listen address).
func RewriteMisconfiguredMCPAdvertise(raw string) string {
	return strings.TrimSpace(raw)
}

// EffectiveMCPAdvertise returns the live mcp_advertise base URL (no trailing
// slash). Prefer this at sandbox-injection time so config reloads take effect
// without relying on a frozen Options snapshot.
func EffectiveMCPAdvertise() string {
	cfg := GetConfig()
	if cfg == nil {
		return ""
	}
	return strings.TrimRight(RewriteMisconfiguredMCPAdvertise(cfg.Server.MCPAdvertise), "/")
}

// ResolveMCPAdvertise returns the base URL for sandbox MCP injection:
// live EffectiveMCPAdvertise when set, otherwise the boot-time Options fallback.
func ResolveMCPAdvertise(fallback string) string {
	if base := EffectiveMCPAdvertise(); base != "" {
		return base
	}
	return strings.TrimRight(RewriteMisconfiguredMCPAdvertise(fallback), "/")
}

// applySandboxImageEnv merges GRASP_SANDBOX_IMAGE_<BACKEND> into Images.
func applySandboxImageEnv(c *Config) {
	if c.Sandbox.Images == nil {
		c.Sandbox.Images = map[string]string{}
	}
	for _, b := range knownSandboxBackends {
		envKey := "GRASP_SANDBOX_IMAGE_" + strings.ToUpper(strings.ReplaceAll(b, "-", "_"))
		if v := env(envKey); v != "" {
			c.Sandbox.Images[b] = v
		}
	}
}

func parseDuration(s string, fallback time.Duration) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err == nil && n > 0 {
			return time.Duration(n) * 24 * time.Hour
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}

// mergeEnvList parses a "K=V,K2=V2" list and merges it into Sandbox.Env (the
// generic, vendor-neutral env injected into every sandbox).
func mergeAcpEnv(c *Config) {
	if c.Sandbox.AcpEnv == nil {
		return
	}
	if c.Sandbox.Env == nil {
		c.Sandbox.Env = map[string]string{}
	}
	for k, v := range c.Sandbox.AcpEnv {
		if strings.TrimSpace(k) == "" {
			continue
		}
		c.Sandbox.Env[k] = v
	}
}

func mergeEnvList(c *Config, list string) {
	for _, pair := range strings.Split(list, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		k, v, ok := strings.Cut(pair, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			continue
		}
		if c.Sandbox.Env == nil {
			c.Sandbox.Env = map[string]string{}
		}
		c.Sandbox.Env[k] = strings.TrimSpace(v)
	}
}

func env(key string) string { return os.Getenv(key) }

func envInt(key string) int {
	if v := env(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 0
}

func first(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
