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
		{Env: "GRASP_FRAME_ANCESTORS", YAML: "server.frame_ancestors", Type: "origin list", Default: "", ZH: "允许用 iframe 嵌入工作区页面的来源（空格或逗号分隔，如 https://portal.example.com）；留空禁止嵌入", EN: "Origins allowed to embed workspace pages in an iframe (space or comma separated, e.g. https://portal.example.com); empty forbids embedding"},
		{Env: "GRASP_DB", YAML: "database.path", Type: "path", Default: "grasp.db", ZH: "SQLite 数据库文件", EN: "SQLite database file"},
		{Env: "GRASP_DB_DRIVER", YAML: "database.driver", Type: "enum", Default: "sqlite", ZH: "数据库驱动：sqlite 或 mysql", EN: "Database driver: sqlite or mysql"},
		{Env: "GRASP_DB_DSN", YAML: "database.dsn", Type: "string", Sensitive: true, ZH: "MySQL DSN", EN: "MySQL DSN"},
		{Env: "GRASP_MAX_RUNS", YAML: "engine.max_concurrent_runs", Type: "integer", Default: "5", ZH: "最大并发运行数", EN: "Maximum concurrent runs"},
		{Env: "GRASP_PROFILES_ROOT", YAML: "engine.profiles_root", Type: "path", Default: "data/profiles", ZH: "Agent profile 根目录", EN: "Agent profile root"},
		{Env: "GRASP_NODE_AUTO_RETRY", YAML: "engine.node_auto_retry_max", Type: "integer", Default: "3", ZH: "节点自动重试上限", EN: "Node automatic retry limit"},
		{Env: "GRASP_SANDBOX_IMAGE", YAML: "sandbox.image", Type: "image", Default: "", ZH: "沙箱镜像覆盖；空时跟随网关（一张图预装全部编程工具 CLI，含 Codex、DeepSeek Harness 和 Kimi Code。Codex 平台模式使用登录文件，也可选择 Responses 模型连接，运行时按 Agent 后端切换）", EN: "Optional sandbox image override; empty follows the gateway (one image ships every coding-tool CLI, including DeepSeek Harness, Kimi Code, and Codex (ChatGPT login or a Responses model connection); runtime switches by Agent backend)"},
		{Env: "GRASP_SANDBOX_IMAGE_CURSOR", YAML: "sandbox.images.cursor", Type: "image", Default: "", ZH: "可选：仅覆盖 cursor 后端镜像；留空用 GRASP_SANDBOX_IMAGE / 网关默认", EN: "Optional cursor-only image override; empty uses GRASP_SANDBOX_IMAGE / the gateway default"},
		{Env: "GRASP_SANDBOX_IMAGE_CLAUDE_CODE", YAML: "sandbox.images.claude_code", Type: "image", Default: "", ZH: "可选：仅覆盖 claude_code 后端镜像；留空用 GRASP_SANDBOX_IMAGE / 网关默认", EN: "Optional claude_code-only image override; empty uses GRASP_SANDBOX_IMAGE / the gateway default"},
		{Env: "GRASP_SANDBOX_IMAGE_CODEBUDDY", YAML: "sandbox.images.codebuddy", Type: "image", Default: "", ZH: "可选：仅覆盖 codebuddy 后端镜像；留空用 GRASP_SANDBOX_IMAGE / 网关默认", EN: "Optional codebuddy-only image override; empty uses GRASP_SANDBOX_IMAGE / the gateway default"},
		{Env: "GRASP_SANDBOX_IMAGE_TRAE", YAML: "sandbox.images.trae", Type: "image", Default: "", ZH: "可选：仅覆盖 trae 后端镜像；留空用 GRASP_SANDBOX_IMAGE / 网关默认", EN: "Optional trae-only image override; empty uses GRASP_SANDBOX_IMAGE / the gateway default"},
		{Env: "GRASP_SANDBOX_IMAGE_DEEPSEEK_HARNESS", YAML: "sandbox.images.deepseek_harness", Type: "image", Default: "", ZH: "可选：仅覆盖 DeepSeek Harness 后端镜像；留空用 GRASP_SANDBOX_IMAGE / 网关默认", EN: "Optional DeepSeek Harness-only image override; empty uses GRASP_SANDBOX_IMAGE / the gateway default"},
		{Env: "GRASP_SANDBOX_IMAGE_KIMI_CODE", YAML: "sandbox.images.kimi_code", Type: "image", Default: "", ZH: "可选：仅覆盖 Kimi Code 后端镜像；留空用 GRASP_SANDBOX_IMAGE / 网关默认", EN: "Optional Kimi Code-only image override; empty uses GRASP_SANDBOX_IMAGE / the gateway default"},
		{Env: "GRASP_SANDBOX_IMAGE_OPENCODE", YAML: "sandbox.images.opencode", Type: "image", Default: "", ZH: "可选：仅覆盖 opencode 后端镜像；留空用 GRASP_SANDBOX_IMAGE / 网关默认", EN: "Optional opencode-only image override; empty uses GRASP_SANDBOX_IMAGE / the gateway default"},
		{Env: "GRASP_SANDBOX_GATEWAY_URL", YAML: "sandbox.gateway_url", Type: "URL", Default: "http://127.0.0.1:8899", ZH: "sandbox-gateway 控制面地址", EN: "sandbox-gateway control-plane URL"},
		{Env: "GRASP_SANDBOX_GATEWAY_API_KEY", YAML: "sandbox.gateway_api_key", Type: "string", Sensitive: true, ZH: "gateway Bearer token", EN: "Gateway bearer token"},
		{Env: "GRASP_SANDBOX_GATEWAY_TRANSPORT", YAML: "sandbox.gateway_transport", Type: "enum", Default: "direct", ZH: "沙箱连接方式：direct 直连或 gateway 认证通道；gateway 要求 API key，预览走现有路径代理，无需额外预览域名", EN: "Sandbox transport: direct or authenticated gateway; gateway requires an API key and uses the existing preview path proxy without an extra preview domain"},
		{Env: "GRASP_SANDBOX_DEFAULT_GATEWAY", YAML: "sandbox.default_gateway", Type: "string", Default: "", ZH: "新沙箱使用的命名网关；已有沙箱保持原绑定", EN: "Named gateway for new sandboxes; existing sandboxes retain their binding"},
		{Env: "GRASP_SANDBOX_PRIVATE_ENDPOINTS", YAML: "sandbox.private_endpoints", Type: "boolean", Default: "false", ZH: "沙箱端点仅服务端可达，预览走 Grasp 路径代理；gateway 通道自动启用，默认直连保持兼容", EN: "Sandbox endpoints are server-only and previews use the Grasp path proxy; gateway transport enables this automatically; legacy direct defaults remain compatible"},
		{Env: "GRASP_OPENCODE_CATALOG_URL", YAML: "sandbox.opencode_catalog_url", Type: "URL", Default: "https://models.dev/api.json", ZH: "OpenCode 模型目录地址；出网受限时改指镜像", EN: "OpenCode model catalog URL; point at a mirror when egress is restricted"},
		{Env: "GRASP_SANDBOX_ENV", YAML: "sandbox.env", Type: "key-value list", Sensitive: true, ZH: "注入所有沙箱的通用环境变量", EN: "Generic environment injected into every sandbox"},
		{Env: "GRASP_AGENT_TIMEOUT_SEC", YAML: "sandbox.agent_chat_timeout_seconds", Type: "integer", Default: "", ZH: "已废弃并忽略：Agent 回合不再有单轮总时限；节点总时限在画布节点上配置", EN: "Deprecated and ignored: Agent turns no longer have a per-turn limit; set the node time limit on the canvas"},
		{Env: "GRASP_CHAT_IDLE_SEC", YAML: "sandbox.chat_idle_timeout_seconds", Type: "integer", Default: "1200", ZH: "Agent 无动作时限秒数：既无输出也无 CPU/IO 活动这么久，先续跑一次，仍无活动则中断本轮；设置后设置页只读", EN: "Agent no-activity limit in seconds: after this long with no output, CPU or IO the turn is resumed once, then stopped; locks the settings page value"},
		{Env: "GRASP_AGENT_NODE_HARD_CAP_HOURS", YAML: "sandbox.agent_node_hard_cap_hours", Type: "integer", Default: "24", ZH: "未在画布配置总时限的 Agent 节点的兜底上限（小时）", EN: "Total limit in hours for an Agent node whose canvas time limit is empty"},
		{Env: "GRASP_SANDBOX_MAX_ATTEMPTS", YAML: "sandbox.sandbox_max_attempts", Type: "integer", Default: "3", ZH: "可重试沙箱故障的最大尝试次数", EN: "Maximum attempts for retryable sandbox faults"},
		{Env: "GRASP_SANDBOX_RETRY_BACKOFF_SEC", YAML: "sandbox.sandbox_retry_backoff_seconds", Type: "integer", Default: "2", ZH: "沙箱重试基础退避秒数", EN: "Base sandbox retry backoff in seconds"},
		{Env: "GRASP_SANDBOX_CREATE_TIMEOUT_SEC", YAML: "sandbox.sandbox_create_timeout_seconds", Type: "integer", Default: "1200", ZH: "等待沙箱就绪的超时秒数", EN: "Timeout waiting for sandbox readiness in seconds"},
		{Env: "GRASP_SANDBOX_MEMORY_MB", YAML: "sandbox.sandbox_memory_mb", Type: "integer", Default: "0", ZH: "沙箱内存覆盖（MiB）；0 跟随网关/模板，非零至少 1024，E2B 必须匹配模板；环境变量覆盖并锁定设置页", EN: "Sandbox memory override in MiB; 0 follows the gateway/template, otherwise at least 1024 and E2B must match its template; env overrides and locks the settings page"},
		{Env: "GRASP_SANDBOX_WORK_DIR", YAML: "sandbox.work_dir", Type: "path", ZH: "ConfigHome 宿主工作目录", EN: "Host work directory for ConfigHome"},
		{Env: "GRASP_SANDBOX_RUNTIME_BUNDLE", YAML: "sandbox.runtime_bundle", Type: "path", Default: "/app/sandbox-runtime/sandbox-runtime.tgz", ZH: "下发给沙箱的运行时包（scripts/build-sandbox-runtime.sh 构建）", EN: "Runtime bundle served to sandboxes (built by scripts/build-sandbox-runtime.sh)"},
		{Env: "GRASP_AUTH_SESSION_TTL", YAML: "auth.session_ttl", Type: "duration", Default: "168h", ZH: "会话有效期", EN: "Session lifetime"},
		{Env: "GRASP_AUTH_USERS", YAML: "auth.users", Type: "YAML/JSON", Sensitive: true, ZH: "静态账号数组；非本地部署必须显式配置", EN: "Static user array; required explicitly outside local mode"},
		{Env: "GRASP_SECRETS_KEY", YAML: "security.secrets_key", Type: "string", Sensitive: true, ZH: "凭据加密主密钥（base64 32 字节）；视作固定盐、请勿轮换。未设置时非 production 模式在 SQLite 库旁自动生成 secrets.key", EN: "Master AES key for encrypting credentials at rest (base64 32 bytes); treat as a fixed salt, do not rotate. When unset, non-production modes generate secrets.key next to the SQLite database"},
		{Env: "GRASP_STORAGE_DRIVER", YAML: "storage.driver", Type: "enum", Default: "local", ZH: "附件存储驱动：local（预留 cos）", EN: "Attachment storage driver: local (cos reserved)"},
		{Env: "GRASP_MAX_FILE_BYTES", YAML: "storage.max_file_bytes", Type: "int", Default: "134217728", ZH: "Agent 单文件导入大小上限（字节）", EN: "Maximum bytes per Agent file import"},
		{Env: "GRASP_BLOBS_ROOT", YAML: "storage.blobs_root", Type: "path", Default: "data/blobs", ZH: "本地附件 blob 根目录", EN: "Local attachment blob root directory"},
		{Env: "GRASP_SOLO_BACKUP_DIR", YAML: "solo_backup.local_dir", Type: "path", Default: "data/solo-backups", ZH: "未配置 S3 时，停止 Solo 会话的工作区备份目录", EN: "Directory for stopped Solo workspace archives when S3 is not configured"},
		{Env: "GRASP_SOLO_BACKUP_MAX_BYTES", YAML: "solo_backup.max_bytes", Type: "int", Default: "4294967296", ZH: "单个 Solo 工作区压缩备份上限（字节）；超出则不能停止", EN: "Maximum bytes of one compressed Solo workspace archive; larger workspaces cannot stop"},
		{Env: "GRASP_SOLO_BACKUP_KEEP", YAML: "solo_backup.keep", Type: "int", Default: "5", ZH: "每个 Solo 会话保留的工作区备份份数，更早的自动删除", EN: "Workspace archives kept per Solo session, newest first; older ones are deleted"},
		{Env: "GRASP_SOLO_BACKUP_INTERVAL_MINUTES", YAML: "solo_backup.interval_minutes", Type: "int", Default: "360", ZH: "运行中且有新对话的 Solo 会话距上次备份满该分钟数后自动备份；负数关闭", EN: "Archive a running Solo session with new activity once this long has passed since its last archive; negative disables"},
		{Env: "GRASP_SOLO_BACKUP_S3_ENDPOINT", YAML: "solo_backup.s3.endpoint", Type: "URL", Default: "", ZH: "S3 兼容服务地址；留空使用 region 对应的 AWS 地址", EN: "S3-compatible endpoint origin; empty uses the AWS endpoint for the region"},
		{Env: "GRASP_SOLO_BACKUP_S3_REGION", YAML: "solo_backup.s3.region", Type: "string", Default: "us-east-1", ZH: "S3 区域", EN: "S3 region"},
		{Env: "GRASP_SOLO_BACKUP_S3_BUCKET", YAML: "solo_backup.s3.bucket", Type: "string", Default: "", ZH: "设置后 Solo 工作区备份写入该 S3 桶，否则写本地目录", EN: "When set, Solo workspace archives go to this S3 bucket instead of the local directory"},
		{Env: "GRASP_SOLO_BACKUP_S3_PREFIX", YAML: "solo_backup.s3.prefix", Type: "string", Default: "", ZH: "S3 对象键前缀", EN: "S3 object key prefix"},
		{Env: "GRASP_SOLO_BACKUP_S3_ACCESS_KEY_ID", YAML: "solo_backup.s3.access_key_id", Type: "string", Sensitive: true, ZH: "S3 访问密钥 ID", EN: "S3 access key ID"},
		{Env: "GRASP_SOLO_BACKUP_S3_SECRET_ACCESS_KEY", YAML: "solo_backup.s3.secret_access_key", Type: "string", Sensitive: true, ZH: "S3 访问密钥", EN: "S3 secret access key"},
		{Env: "GRASP_SOLO_BACKUP_S3_PATH_STYLE", YAML: "solo_backup.s3.path_style", Type: "boolean", Default: "false", ZH: "使用路径式桶地址（MinIO 等自托管服务通常需要）", EN: "Use path-style bucket addressing (usually required by MinIO and other self-hosted servers)"},
		{Env: "GRASP_GITHUB_OAUTH_CLIENT_ID", YAML: "github.oauth_client_id", Type: "string", Default: "", ZH: "GitHub OAuth App 的 Client ID（需勾选 Enable Device Flow）；设置后工作区可用「用 GitHub 登录」添加 github.com 凭据，不需要 client secret", EN: "Client ID of a GitHub OAuth App with Enable Device Flow checked; enables \"Sign in with GitHub\" for github.com workspace credentials (no client secret needed)"},
		{Env: "GRASP_CODEX_LOGIN_REFRESH_LEAD", YAML: "codex_login.refresh_lead", Type: "duration", Default: "96h", ZH: "Codex ChatGPT 登录在 access token 到期前多久由平台续期", EN: "How long before the access token expires the platform renews a Codex ChatGPT login"},
		{Env: "GRASP_CODEX_LOGIN_CHECK_INTERVAL", YAML: "codex_login.check_interval", Type: "duration", Default: "10m", ZH: "平台检查 Codex 登录是否需要续期的间隔", EN: "How often the platform checks whether Codex logins need renewal"},
		{Env: "GRASP_CODEX_LOGIN_AUTH_BASE_URL", YAML: "codex_login.auth_base_url", Type: "URL", Default: "https://auth.openai.com", ZH: "OpenAI 登录服务地址，用于 Codex 登录续期和设备码登录；请求遵循 HTTPS_PROXY", EN: "OpenAI auth server for Codex login renewal and device-code sign-in; requests honor HTTPS_PROXY"},
	}
}
