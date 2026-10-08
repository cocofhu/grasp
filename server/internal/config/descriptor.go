package config

// OptionDescriptor is the public, machine-readable configuration contract used
// by runtime validation and generated documentation.
type OptionDescriptor struct {
	Env       string
	YAML      string
	Type      string
	Default   string
	Sensitive bool
	ZH        string
	EN        string
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
		{Env: "GRASP_MAX_RUNS", YAML: "engine.max_concurrent_runs", Type: "integer", Default: "5", ZH: "最大并发运行数", EN: "Maximum concurrent runs"},
		{Env: "GRASP_PROFILES_ROOT", YAML: "engine.profiles_root", Type: "path", Default: "data/profiles", ZH: "Agent profile 根目录", EN: "Agent profile root"},
		{Env: "GRASP_NODE_AUTO_RETRY", YAML: "engine.node_auto_retry_max", Type: "integer", Default: "3", ZH: "节点自动重试上限", EN: "Node automatic retry limit"},
		{Env: "GRASP_SANDBOX_IMAGE", YAML: "sandbox.image", Type: "image", Default: "", ZH: "沙箱镜像（推荐；一张图预装六个 CLI，含 Codex。Codex 使用登录文件而不是 API Key，运行时按 Agent 后端切换）", EN: "Sandbox image (recommended; one image ships six CLIs including Codex, which uses a ChatGPT login file rather than an API key; runtime switches by Agent backend)"},
		{Env: "GRASP_SANDBOX_IMAGE_CURSOR", YAML: "sandbox.images.cursor", Type: "image", Default: "", ZH: "可选：仅覆盖 cursor 后端镜像；留空用 GRASP_SANDBOX_IMAGE / 内置默认", EN: "Optional cursor-only image override; empty uses GRASP_SANDBOX_IMAGE / the built-in default"},
		{Env: "GRASP_SANDBOX_IMAGE_CLAUDE_CODE", YAML: "sandbox.images.claude_code", Type: "image", Default: "", ZH: "可选：仅覆盖 claude_code 后端镜像；留空用 GRASP_SANDBOX_IMAGE / 内置默认", EN: "Optional claude_code-only image override; empty uses GRASP_SANDBOX_IMAGE / the built-in default"},
		{Env: "GRASP_SANDBOX_IMAGE_CODEBUDDY", YAML: "sandbox.images.codebuddy", Type: "image", Default: "", ZH: "可选：仅覆盖 codebuddy 后端镜像；留空用 GRASP_SANDBOX_IMAGE / 内置默认", EN: "Optional codebuddy-only image override; empty uses GRASP_SANDBOX_IMAGE / the built-in default"},
		{Env: "GRASP_SANDBOX_IMAGE_TRAE", YAML: "sandbox.images.trae", Type: "image", Default: "", ZH: "可选：仅覆盖 trae 后端镜像；留空用 GRASP_SANDBOX_IMAGE / 内置默认", EN: "Optional trae-only image override; empty uses GRASP_SANDBOX_IMAGE / the built-in default"},
		{Env: "GRASP_SANDBOX_IMAGE_OPENCODE", YAML: "sandbox.images.opencode", Type: "image", Default: "", ZH: "可选：仅覆盖 opencode 后端镜像；留空用 GRASP_SANDBOX_IMAGE / 内置默认", EN: "Optional opencode-only image override; empty uses GRASP_SANDBOX_IMAGE / the built-in default"},
		{Env: "GRASP_SANDBOX_GATEWAY_URL", YAML: "sandbox.gateway_url", Type: "URL", Default: "http://127.0.0.1:8899", ZH: "sandbox-gateway 控制面地址", EN: "sandbox-gateway control-plane URL"},
		{Env: "GRASP_SANDBOX_GATEWAY_API_KEY", YAML: "sandbox.gateway_api_key", Type: "string", Sensitive: true, ZH: "gateway Bearer token", EN: "Gateway bearer token"},
		{Env: "GRASP_OPENCODE_CATALOG_URL", YAML: "sandbox.opencode_catalog_url", Type: "URL", Default: "https://models.dev/api.json", ZH: "OpenCode 模型目录地址；出网受限时改指镜像", EN: "OpenCode model catalog URL; point at a mirror when egress is restricted"},
		{Env: "GRASP_SANDBOX_ENV", YAML: "sandbox.env", Type: "key-value list", Sensitive: true, ZH: "注入所有沙箱的通用环境变量", EN: "Generic environment injected into every sandbox"},
		{Env: "GRASP_AGENT_TIMEOUT_SEC", YAML: "sandbox.agent_chat_timeout_seconds", Type: "integer", Default: "", ZH: "已废弃并忽略：Agent 回合不再有单轮总时限；节点总时限在画布节点上配置", EN: "Deprecated and ignored: Agent turns no longer have a per-turn limit; set the node time limit on the canvas"},
		{Env: "GRASP_CHAT_IDLE_SEC", YAML: "sandbox.chat_idle_timeout_seconds", Type: "integer", Default: "1200", ZH: "Agent 无动作时限秒数：既无输出也无 CPU/IO 活动这么久，先续跑一次，仍无活动则中断本轮；设置后设置页只读", EN: "Agent no-activity limit in seconds: after this long with no output, CPU or IO the turn is resumed once, then stopped; locks the settings page value"},
		{Env: "GRASP_AGENT_NODE_HARD_CAP_HOURS", YAML: "sandbox.agent_node_hard_cap_hours", Type: "integer", Default: "24", ZH: "未在画布配置总时限的 Agent 节点的兜底上限（小时）", EN: "Total limit in hours for an Agent node whose canvas time limit is empty"},
		{Env: "GRASP_SANDBOX_MAX_ATTEMPTS", YAML: "sandbox.sandbox_max_attempts", Type: "integer", Default: "3", ZH: "可重试沙箱故障的最大尝试次数", EN: "Maximum attempts for retryable sandbox faults"},
		{Env: "GRASP_SANDBOX_RETRY_BACKOFF_SEC", YAML: "sandbox.sandbox_retry_backoff_seconds", Type: "integer", Default: "2", ZH: "沙箱重试基础退避秒数", EN: "Base sandbox retry backoff in seconds"},
		{Env: "GRASP_SANDBOX_CREATE_TIMEOUT_SEC", YAML: "sandbox.sandbox_create_timeout_seconds", Type: "integer", Default: "1200", ZH: "等待沙箱就绪的超时秒数", EN: "Timeout waiting for sandbox readiness in seconds"},
		{Env: "GRASP_SANDBOX_MEMORY_MB", YAML: "sandbox.sandbox_memory_mb", Type: "integer", Default: "8192", ZH: "每个沙箱的内存上限（MiB）；设置页可改，设置后页面只读", EN: "Memory limit per sandbox in MiB; editable on the settings page unless this is set"},
		{Env: "GRASP_SANDBOX_WORK_DIR", YAML: "sandbox.work_dir", Type: "path", ZH: "ConfigHome 宿主工作目录", EN: "Host work directory for ConfigHome"},
		{Env: "GRASP_SANDBOX_RUNTIME_BUNDLE", YAML: "sandbox.runtime_bundle", Type: "path", Default: "/app/sandbox-runtime/sandbox-runtime.tgz", ZH: "下发给沙箱的运行时包（scripts/build-sandbox-runtime.sh 构建）", EN: "Runtime bundle served to sandboxes (built by scripts/build-sandbox-runtime.sh)"},
		{Env: "GRASP_AUTH_SESSION_TTL", YAML: "auth.session_ttl", Type: "duration", Default: "168h", ZH: "会话有效期", EN: "Session lifetime"},
		{Env: "GRASP_AUTH_USERS", YAML: "auth.users", Type: "YAML/JSON", Sensitive: true, ZH: "静态账号数组；非本地部署必须显式配置", EN: "Static user array; required explicitly outside local mode"},
		{Env: "GRASP_SECRETS_KEY", YAML: "security.secrets_key", Type: "string", Sensitive: true, ZH: "凭据加密主密钥（base64 32 字节）；视作固定盐、请勿轮换。未设置时非 production 模式在 SQLite 库旁自动生成 secrets.key", EN: "Master AES key for encrypting credentials at rest (base64 32 bytes); treat as a fixed salt, do not rotate. When unset, non-production modes generate secrets.key next to the SQLite database"},
		{Env: "GRASP_STORAGE_DRIVER", YAML: "storage.driver", Type: "enum", Default: "local", ZH: "附件存储驱动：local（预留 cos）", EN: "Attachment storage driver: local (cos reserved)"},
		{Env: "GRASP_BLOBS_ROOT", YAML: "storage.blobs_root", Type: "path", Default: "data/blobs", ZH: "本地附件 blob 根目录", EN: "Local attachment blob root directory"},
	}
}
