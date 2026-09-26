package config

// OptionDescriptor is the public, machine-readable configuration contract used
// by runtime validation and generated documentation.
type OptionDescriptor struct {
	Env        string
	YAML       string
	Type       string
	Default    string
	Sensitive  bool
	Deprecated bool
	ZH         string
	EN         string
}

// OptionDescriptors returns all environment variables accepted by the runtime.
// Keep this list in the same change as applyEnvOverrides.
func OptionDescriptors() []OptionDescriptor {
	return []OptionDescriptor{
		{Env: "GRASP_PORT", YAML: "server.port", Type: "integer", Default: "8080", ZH: "HTTP 监听端口", EN: "HTTP listen port"},
		{Env: "GRASP_DEPLOYMENT_MODE", YAML: "server.deployment_mode", Type: "string", Default: "development", ZH: "部署信任边界；local-demo 仅限 loopback", EN: "Deployment trust boundary; local-demo is loopback-only"},
		{Env: "GRASP_MCP_ADVERTISE", YAML: "server.mcp_advertise", Type: "URL", Default: "derived", ZH: "沙箱回连 artifact-store MCP 的 API 基址", EN: "API base URL used by sandboxes for artifact-store MCP"},
		{Env: "GRASP_PUBLIC_ADVERTISE", YAML: "server.public_advertise", Type: "URL", Default: "derived", ZH: "浏览器预览代理与 Run→QQ 深链的公开基址（不用于门禁分享铸造）", EN: "Public base URL for browser preview proxy and Run→QQ deep links (not used to mint gate share URLs)"},
		{Env: "GRASP_DB", YAML: "database.path", Type: "path", Default: "grasp.db", ZH: "SQLite 数据库文件", EN: "SQLite database file"},
		{Env: "GRASP_DB_DRIVER", YAML: "database.driver", Type: "enum", Default: "sqlite", ZH: "数据库驱动：sqlite 或 mysql", EN: "Database driver: sqlite or mysql"},
		{Env: "GRASP_DB_DSN", YAML: "database.dsn", Type: "string", Sensitive: true, ZH: "MySQL DSN", EN: "MySQL DSN"},
		{Env: "GRASP_EXEC_PROVIDER", YAML: "engine.exec_provider", Type: "string", Default: "sandbox", Deprecated: true, ZH: "已弃用；执行后端由 Agent acpBackend 决定", EN: "Deprecated; Agent acpBackend selects the execution backend"},
		{Env: "GRASP_MAX_RUNS", YAML: "engine.max_concurrent_runs", Type: "integer", Default: "5", ZH: "最大并发运行数", EN: "Maximum concurrent runs"},
		{Env: "GRASP_PROFILES_ROOT", YAML: "engine.profiles_root", Type: "path", Default: "data/profiles", ZH: "Agent profile 根目录", EN: "Agent profile root"},
		{Env: "GRASP_NODE_AUTO_RETRY", YAML: "engine.node_auto_retry_max", Type: "integer", Default: "3", ZH: "节点自动重试上限", EN: "Node automatic retry limit"},
		{Env: "GRASP_SANDBOX_IMAGE", YAML: "sandbox.image", Type: "image", Default: "", ZH: "沙箱镜像（推荐；一张图预装五个 CLI，运行时按 Agent 后端切换）", EN: "Sandbox image (recommended; one image ships all five CLIs, runtime switches by Agent backend)"},
		{Env: "GRASP_SANDBOX_IMAGE_CURSOR", YAML: "sandbox.images.cursor", Type: "image", Default: "", ZH: "可选：仅覆盖 cursor 后端镜像；留空用 GRASP_SANDBOX_IMAGE / 内置默认", EN: "Optional cursor-only image override; empty uses GRASP_SANDBOX_IMAGE / the built-in default"},
		{Env: "GRASP_SANDBOX_IMAGE_CLAUDE_CODE", YAML: "sandbox.images.claude_code", Type: "image", Default: "", ZH: "可选：仅覆盖 claude_code 后端镜像；留空用 GRASP_SANDBOX_IMAGE / 内置默认", EN: "Optional claude_code-only image override; empty uses GRASP_SANDBOX_IMAGE / the built-in default"},
		{Env: "GRASP_SANDBOX_IMAGE_CODEBUDDY", YAML: "sandbox.images.codebuddy", Type: "image", Default: "", ZH: "可选：仅覆盖 codebuddy 后端镜像；留空用 GRASP_SANDBOX_IMAGE / 内置默认", EN: "Optional codebuddy-only image override; empty uses GRASP_SANDBOX_IMAGE / the built-in default"},
		{Env: "GRASP_SANDBOX_IMAGE_TRAE", YAML: "sandbox.images.trae", Type: "image", Default: "", ZH: "可选：仅覆盖 trae 后端镜像；留空用 GRASP_SANDBOX_IMAGE / 内置默认", EN: "Optional trae-only image override; empty uses GRASP_SANDBOX_IMAGE / the built-in default"},
		{Env: "GRASP_SANDBOX_IMAGE_OPENCODE", YAML: "sandbox.images.opencode", Type: "image", Default: "", ZH: "可选：仅覆盖 opencode 后端镜像；留空用 GRASP_SANDBOX_IMAGE / 内置默认", EN: "Optional opencode-only image override; empty uses GRASP_SANDBOX_IMAGE / the built-in default"},
		{Env: "GRASP_SANDBOX_GATEWAY_URL", YAML: "sandbox.gateway_url", Type: "URL", Default: "http://127.0.0.1:8899", ZH: "sandbox-gateway 控制面地址", EN: "sandbox-gateway control-plane URL"},
		{Env: "GRASP_SANDBOX_GATEWAY_API_KEY", YAML: "sandbox.gateway_api_key", Type: "string", Sensitive: true, ZH: "gateway Bearer token", EN: "Gateway bearer token"},
		{Env: "GRASP_OPENCODE_CATALOG_URL", YAML: "sandbox.opencode_catalog_url", Type: "URL", Default: "https://models.dev/api.json", ZH: "OpenCode 模型目录地址；出网受限时改指镜像", EN: "OpenCode model catalog URL; point at a mirror when egress is restricted"},
		{Env: "GRASP_BROWSER_ENABLED", YAML: "browser.enabled", Type: "boolean", Deprecated: true, ZH: "兼容字段；VNC 预览始终可用", EN: "Compatibility field; VNC preview is always available"},
		{Env: "GRASP_CURSOR_API_KEY", YAML: "sandbox.cursor_api_key", Type: "string", Sensitive: true, Deprecated: true, ZH: "已弃用；改用 Agent env", EN: "Deprecated; use agent env"},
		{Env: "CURSOR_API_KEY", YAML: "sandbox.cursor_api_key", Type: "string", Sensitive: true, Deprecated: true, ZH: "已弃用别名；改用 Agent env", EN: "Deprecated alias; use agent env"},
		{Env: "GRASP_CURSOR_AUTH", YAML: "sandbox.cursor_auth_path", Type: "path", Sensitive: true, Deprecated: true, ZH: "已弃用的 Cursor 认证目录", EN: "Deprecated Cursor authentication directory"},
		{Env: "GRASP_SANDBOX_ENV", YAML: "sandbox.env", Type: "key-value list", Sensitive: true, ZH: "注入所有沙箱的通用环境变量", EN: "Generic environment injected into every sandbox"},
		{Env: "GRASP_AGENT_TIMEOUT_SEC", YAML: "sandbox.agent_chat_timeout_seconds", Type: "integer", Default: "600", ZH: "单次 Agent turn 总超时秒数", EN: "Overall timeout for one agent turn in seconds"},
		{Env: "GRASP_CHAT_IDLE_SEC", YAML: "sandbox.chat_idle_timeout_seconds", Type: "integer", Default: "720", ZH: "无 ACP 事件的空闲超时秒数", EN: "Idle timeout without ACP events in seconds"},
		{Env: "GRASP_SANDBOX_MAX_ATTEMPTS", YAML: "sandbox.sandbox_max_attempts", Type: "integer", Default: "3", ZH: "可重试沙箱故障的最大尝试次数", EN: "Maximum attempts for retryable sandbox faults"},
		{Env: "GRASP_SANDBOX_RETRY_BACKOFF_SEC", YAML: "sandbox.sandbox_retry_backoff_seconds", Type: "integer", Default: "2", ZH: "沙箱重试基础退避秒数", EN: "Base sandbox retry backoff in seconds"},
		{Env: "GRASP_SANDBOX_CREATE_TIMEOUT_SEC", YAML: "sandbox.sandbox_create_timeout_seconds", Type: "integer", Default: "1200", ZH: "等待沙箱就绪的超时秒数", EN: "Timeout waiting for sandbox readiness in seconds"},
		{Env: "GRASP_SANDBOX_WORK_DIR", YAML: "sandbox.work_dir", Type: "path", ZH: "ConfigHome 宿主工作目录", EN: "Host work directory for ConfigHome"},
		{Env: "GRASP_AUTH_MAX_FAILURES", YAML: "auth.max_failures", Type: "integer", Default: "5", ZH: "IP 锁定前的登录失败次数", EN: "Login failures before IP lock"},
		{Env: "GRASP_AUTH_LOCK_DURATION", YAML: "auth.lock_duration", Type: "duration", Default: "5m", ZH: "登录失败锁定时长", EN: "Login failure lock duration"},
		{Env: "GRASP_AUTH_SESSION_TTL", YAML: "auth.session_ttl", Type: "duration", Default: "168h", ZH: "会话有效期", EN: "Session lifetime"},
		{Env: "GRASP_AUTH_USERS", YAML: "auth.users", Type: "YAML/JSON", Sensitive: true, ZH: "静态账号数组；非本地部署必须显式配置", EN: "Static user array; required explicitly outside local mode"},
		{Env: "GRASP_SECRETS_KEY", YAML: "security.secrets_key", Type: "string", Sensitive: true, ZH: "外部渠道凭据加密主密钥（base64 32 字节）；用于加密存储渠道 app_secret，视作固定盐、请勿轮换", EN: "Master AES key for encrypting channel credentials at rest (base64 32 bytes); treat as a fixed salt, do not rotate"},
		{Env: "GRASP_STORAGE_DRIVER", YAML: "storage.driver", Type: "enum", Default: "local", ZH: "附件存储驱动：local（预留 cos）", EN: "Attachment storage driver: local (cos reserved)"},
		{Env: "GRASP_BLOBS_ROOT", YAML: "storage.blobs_root", Type: "path", Default: "data/blobs", ZH: "本地附件 blob 根目录", EN: "Local attachment blob root directory"},
	}
}
