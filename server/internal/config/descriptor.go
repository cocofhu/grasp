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
		{Env: "GRASP_WORKFLOWS_ENABLED", YAML: "features.workflows_enabled", Type: "boolean", Default: "true", ZH: "允许新建工作流；关闭后现有任务仍可管理", EN: "Enable new workflow runs; existing runs remain manageable when disabled"},
		{Env: "GRASP_MAX_ACTIVE_WORKFLOWS", YAML: "features.max_active_workflows", Type: "integer", Default: "1000", ZH: "运行中与排队工作流总数上限", EN: "Maximum total active and queued workflow runs"},
		{Env: "GRASP_PORT", YAML: "server.port", Type: "integer", Default: "8080", ZH: "HTTP 监听端口", EN: "HTTP listen port"},
		{Env: "GRASP_DEPLOYMENT_MODE", YAML: "server.deployment_mode", Type: "string", Default: "development", ZH: "部署信任边界；local-demo 仅限 loopback", EN: "Deployment trust boundary; local-demo is loopback-only"},
		{Env: "GRASP_MCP_ADVERTISE", YAML: "server.mcp_advertise", Type: "URL", Default: "derived", ZH: "沙箱回连 artifact-store MCP 的 API 基址", EN: "API base URL used by sandboxes for artifact-store MCP"},
		{Env: "GRASP_PUBLIC_ADVERTISE", YAML: "server.public_advertise", Type: "URL", Default: "derived", ZH: "浏览器预览代理与 Run→QQ 深链的公开基址（不用于门禁分享铸造）", EN: "Public base URL for browser preview proxy and Run→QQ deep links (not used to mint gate share URLs)"},
		{Env: "GRASP_PREVIEW_ADVERTISE", YAML: "server.preview_advertise", Type: "URL", Default: "", ZH: "私有端点预览的独立域名基址；通配符子域名须转发到 Grasp，production 须与平台使用不同可注册站点", EN: "Separate wildcard origin for private previews routed to Grasp; production requires a different registrable site from the platform"},
		{Env: "GRASP_FRAME_ANCESTORS", YAML: "server.frame_ancestors", Type: "origin list", Default: "", ZH: "允许用 iframe 嵌入工作区页面的来源（空格或逗号分隔，如 https://portal.example.com）；留空禁止嵌入", EN: "Origins allowed to embed workspace pages in an iframe (space or comma separated, e.g. https://portal.example.com); empty forbids embedding"},
		{Env: "GRASP_DB", YAML: "database.path", Type: "path", Default: "grasp.db", ZH: "SQLite 数据库文件", EN: "SQLite database file"},
		{Env: "GRASP_DB_DRIVER", YAML: "database.driver", Type: "enum", Default: "sqlite", ZH: "数据库驱动：sqlite 或 mysql", EN: "Database driver: sqlite or mysql"},
		{Env: "GRASP_DB_DSN", YAML: "database.dsn", Type: "string", Sensitive: true, ZH: "MySQL DSN", EN: "MySQL DSN"},
		{Env: "GRASP_MAX_RUNS", YAML: "engine.max_concurrent_runs", Type: "integer", Default: "5", ZH: "最大并发运行数", EN: "Maximum concurrent runs"},
		{Env: "GRASP_PROFILES_ROOT", YAML: "engine.profiles_root", Type: "path", Default: "data/profiles", ZH: "Agent profile 根目录", EN: "Agent profile root"},
		{Env: "GRASP_NODE_AUTO_RETRY", YAML: "engine.node_auto_retry_max", Type: "integer", Default: "3", ZH: "节点自动重试上限", EN: "Node automatic retry limit"},
		{Env: "GRASP_SANDBOX_IMAGE", YAML: "sandbox.image", Type: "image", Default: "", ZH: "沙箱镜像（推荐；一张图预装全部编程工具 CLI，含 Codex、DeepSeek Harness 和 Kimi Code。Codex 平台模式使用登录文件，也可选择 Responses 模型连接，运行时按 Agent 后端切换）", EN: "Sandbox image (recommended; one image ships every coding-tool CLI, including DeepSeek Harness, Kimi Code, and Codex (ChatGPT login or a Responses model connection); runtime switches by Agent backend)"},
		{Env: "GRASP_SANDBOX_IMAGE_CURSOR", YAML: "sandbox.images.cursor", Type: "image", Default: "", ZH: "可选：仅覆盖 cursor 后端镜像；留空用 GRASP_SANDBOX_IMAGE / 内置默认", EN: "Optional cursor-only image override; empty uses GRASP_SANDBOX_IMAGE / the built-in default"},
		{Env: "GRASP_SANDBOX_IMAGE_CLAUDE_CODE", YAML: "sandbox.images.claude_code", Type: "image", Default: "", ZH: "可选：仅覆盖 claude_code 后端镜像；留空用 GRASP_SANDBOX_IMAGE / 内置默认", EN: "Optional claude_code-only image override; empty uses GRASP_SANDBOX_IMAGE / the built-in default"},
		{Env: "GRASP_SANDBOX_IMAGE_CODEBUDDY", YAML: "sandbox.images.codebuddy", Type: "image", Default: "", ZH: "可选：仅覆盖 codebuddy 后端镜像；留空用 GRASP_SANDBOX_IMAGE / 内置默认", EN: "Optional codebuddy-only image override; empty uses GRASP_SANDBOX_IMAGE / the built-in default"},
		{Env: "GRASP_SANDBOX_IMAGE_TRAE", YAML: "sandbox.images.trae", Type: "image", Default: "", ZH: "可选：仅覆盖 trae 后端镜像；留空用 GRASP_SANDBOX_IMAGE / 内置默认", EN: "Optional trae-only image override; empty uses GRASP_SANDBOX_IMAGE / the built-in default"},
		{Env: "GRASP_SANDBOX_IMAGE_DEEPSEEK_HARNESS", YAML: "sandbox.images.deepseek_harness", Type: "image", Default: "", ZH: "可选：仅覆盖 DeepSeek Harness 后端镜像；留空用 GRASP_SANDBOX_IMAGE / 内置默认", EN: "Optional DeepSeek Harness-only image override; empty uses GRASP_SANDBOX_IMAGE / the built-in default"},
		{Env: "GRASP_SANDBOX_IMAGE_KIMI_CODE", YAML: "sandbox.images.kimi_code", Type: "image", Default: "", ZH: "可选：仅覆盖 Kimi Code 后端镜像；留空用 GRASP_SANDBOX_IMAGE / 内置默认", EN: "Optional Kimi Code-only image override; empty uses GRASP_SANDBOX_IMAGE / the built-in default"},
		{Env: "GRASP_SANDBOX_IMAGE_OPENCODE", YAML: "sandbox.images.opencode", Type: "image", Default: "", ZH: "可选：仅覆盖 opencode 后端镜像；留空用 GRASP_SANDBOX_IMAGE / 内置默认", EN: "Optional opencode-only image override; empty uses GRASP_SANDBOX_IMAGE / the built-in default"},
		{Env: "GRASP_SANDBOX_GATEWAY_URL", YAML: "sandbox.gateway_url", Type: "URL", Default: "http://127.0.0.1:8899", ZH: "sandbox-gateway 控制面地址", EN: "sandbox-gateway control-plane URL"},
		{Env: "GRASP_SANDBOX_GATEWAY_API_KEY", YAML: "sandbox.gateway_api_key", Type: "string", Sensitive: true, ZH: "gateway Bearer token", EN: "Gateway bearer token"},
		{Env: "GRASP_SANDBOX_GATEWAY_TRANSPORT", YAML: "sandbox.gateway_transport", Type: "enum", Default: "direct", ZH: "沙箱连接方式：direct 直连或 gateway 认证通道；gateway 要求独立预览域名", EN: "Sandbox transport: direct or authenticated gateway; gateway requires an isolated preview origin"},
		{Env: "GRASP_SANDBOX_DEFAULT_GATEWAY", YAML: "sandbox.default_gateway", Type: "string", Default: "", ZH: "新沙箱使用的命名网关；已有沙箱保持原绑定", EN: "Named gateway for new sandboxes; existing sandboxes retain their binding"},
		{Env: "GRASP_SANDBOX_PRIVATE_ENDPOINTS", YAML: "sandbox.private_endpoints", Type: "boolean", Default: "false", ZH: "私有沙箱端点使用独立 Grasp 预览域名；gateway 通道自动启用，默认直连保持兼容", EN: "Use isolated Grasp preview origins for private endpoints; gateway transport enables this automatically; legacy direct defaults remain compatible"},
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
		{Env: "GRASP_STORAGE_DRIVER", YAML: "storage.driver", Type: "enum", Default: "local", ZH: "正文与附件存储驱动：local / s3 / cos", EN: "Payload and attachment storage driver: local / s3 / cos"},
		{Env: "GRASP_S3_ENDPOINT", YAML: "storage.s3.endpoint", Type: "URL", ZH: "S3 兼容对象存储地址（HTTP(S) origin）", EN: "S3-compatible object storage HTTP(S) origin"},
		{Env: "GRASP_S3_REGION", YAML: "storage.s3.region", Type: "string", Default: "us-east-1", ZH: "对象存储地域；COS 填实际 bucket 地域", EN: "Object storage region; use the actual bucket region for COS"},
		{Env: "GRASP_S3_BUCKET", YAML: "storage.s3.bucket", Type: "string", ZH: "已创建的私有对象存储 bucket", EN: "Existing private object storage bucket"},
		{Env: "GRASP_S3_PREFIX", YAML: "storage.s3.prefix", Type: "string", ZH: "当前部署的对象前缀；共享 bucket 时须隔离", EN: "Object prefix for this deployment; isolate independent deployments in a shared bucket"},
		{Env: "GRASP_S3_ACCESS_KEY_ID", YAML: "storage.s3.access_key_id", Type: "string", Sensitive: true, ZH: "对象存储访问凭据 ID，建议通过 Secret 注入", EN: "Object storage access key ID; inject through a Secret"},
		{Env: "GRASP_S3_SECRET_ACCESS_KEY", YAML: "storage.s3.secret_access_key", Type: "string", Sensitive: true, ZH: "对象存储访问密钥，通过 Secret 注入", EN: "Object storage secret key; inject through a Secret"},
		{Env: "GRASP_S3_SESSION_TOKEN", YAML: "storage.s3.session_token", Type: "string", Sensitive: true, ZH: "对象存储临时会话令牌（可选）", EN: "Optional object storage temporary session token"},
		{Env: "GRASP_S3_PATH_STYLE", YAML: "storage.s3.path_style", Type: "boolean", ZH: "路径式寻址；s3 默认 true，cos 默认 false", EN: "Path-style addressing; defaults to true for s3 and false for cos"},
		{Env: "GRASP_MAX_FILE_BYTES", YAML: "storage.max_file_bytes", Type: "int", Default: "134217728", ZH: "Agent 单文件导入大小上限（字节）", EN: "Maximum bytes per Agent file import"},
		{Env: "GRASP_BLOBS_ROOT", YAML: "storage.blobs_root", Type: "path", Default: "data/blobs", ZH: "本地附件 blob 根目录", EN: "Local attachment blob root directory"},
		{Env: "GRASP_EXECUTION_CONTROL_DRIVER", YAML: "execution_control.driver", Type: "enum", ZH: "独立执行连续性存储：local / s3 / cos；未配置时只读历史，须显式离线初始化", EN: "Independent execution continuity store: local / s3 / cos; unset keeps history readable and execution disabled until explicit offline initialization"},
		{Env: "GRASP_EXECUTION_CONTROL_BLOBS_ROOT", YAML: "execution_control.blobs_root", Type: "path", ZH: "独立持久控制目录，不得与 SQL、正文、备份目录重叠", EN: "Independent persistent control directory, disjoint from SQL, payload and backup directories"},
		{Env: "GRASP_EXECUTION_CONTROL_S3_ENDPOINT", YAML: "execution_control.s3.endpoint", Type: "URL", ZH: "独立控制存储的 S3 HTTP(S) origin，必须支持真实条件写", EN: "Independent control S3 HTTP(S) origin; real conditional writes are required"},
		{Env: "GRASP_EXECUTION_CONTROL_S3_REGION", YAML: "execution_control.s3.region", Type: "string", Default: "us-east-1", ZH: "独立控制对象存储地域", EN: "Independent control object storage region"},
		{Env: "GRASP_EXECUTION_CONTROL_S3_BUCKET", YAML: "execution_control.s3.bucket", Type: "string", ZH: "独立控制 bucket，或与正文逻辑前缀严格分离的 bucket", EN: "Independent control bucket, or a bucket with strictly disjoint payload/control prefixes"},
		{Env: "GRASP_EXECUTION_CONTROL_S3_PREFIX", YAML: "execution_control.s3.prefix", Type: "string", ZH: "控制前缀不得包含于正文、复制目标或其父级；控制数据不随 SQL 恢复", EN: "Control prefix must be disjoint from payload and replica roots; control data must not roll back with SQL"},
		{Env: "GRASP_EXECUTION_CONTROL_S3_ACCESS_KEY_ID", YAML: "execution_control.s3.access_key_id", Type: "string", Sensitive: true, ZH: "独立控制存储凭据 ID，通过 Secret 注入", EN: "Independent control access key ID; inject through a Secret"},
		{Env: "GRASP_EXECUTION_CONTROL_S3_SECRET_ACCESS_KEY", YAML: "execution_control.s3.secret_access_key", Type: "string", Sensitive: true, ZH: "独立控制存储密钥，通过 Secret 注入", EN: "Independent control secret key; inject through a Secret"},
		{Env: "GRASP_EXECUTION_CONTROL_S3_SESSION_TOKEN", YAML: "execution_control.s3.session_token", Type: "string", Sensitive: true, ZH: "独立控制存储临时令牌（可选）", EN: "Optional independent control temporary session token"},
		{Env: "GRASP_EXECUTION_CONTROL_S3_PATH_STYLE", YAML: "execution_control.s3.path_style", Type: "boolean", ZH: "控制存储路径式寻址；s3 默认 true，cos 默认 false", EN: "Control path-style addressing; defaults to true for s3 and false for cos"},
		{Env: "GRASP_GITHUB_OAUTH_CLIENT_ID", YAML: "github.oauth_client_id", Type: "string", Default: "", ZH: "GitHub OAuth App 的 Client ID（需勾选 Enable Device Flow）；设置后工作区可用「用 GitHub 登录」添加 github.com 凭据，不需要 client secret", EN: "Client ID of a GitHub OAuth App with Enable Device Flow checked; enables \"Sign in with GitHub\" for github.com workspace credentials (no client secret needed)"},
		{Env: "GRASP_CODEX_LOGIN_REFRESH_LEAD", YAML: "codex_login.refresh_lead", Type: "duration", Default: "96h", ZH: "Codex ChatGPT 登录在 access token 到期前多久由平台续期", EN: "How long before the access token expires the platform renews a Codex ChatGPT login"},
		{Env: "GRASP_CODEX_LOGIN_CHECK_INTERVAL", YAML: "codex_login.check_interval", Type: "duration", Default: "10m", ZH: "平台检查 Codex 登录是否需要续期的间隔", EN: "How often the platform checks whether Codex logins need renewal"},
		{Env: "GRASP_CODEX_LOGIN_AUTH_BASE_URL", YAML: "codex_login.auth_base_url", Type: "URL", Default: "https://auth.openai.com", ZH: "OpenAI 登录服务地址，用于 Codex 登录续期和设备码登录；请求遵循 HTTPS_PROXY", EN: "OpenAI auth server for Codex login renewal and device-code sign-in; requests honor HTTPS_PROXY"},
	}
}
