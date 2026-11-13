# Grasp 与沙箱配置

一次部署维护两份配置：**Grasp 的平台配置**和**所选沙箱后端的 gateway 配置**。E2B 和 Kubernetes 各有独立示例，只复制正在使用的后端；不要把它们写进同一份 gateway 文件。

Grasp 默认跟随网关：镜像为空、内存覆盖为 `0`。镜像、模板和资源在 gateway 配置一次即可。本文对应当前源码；新配置需要与本次更新后的 Grasp / gateway 二进制一起使用，不适用于尚未包含该变更的发布镜像。

## 先选配置文件

| 文件 | 配置什么 | 示例 |
| --- | --- | --- |
| `grasp.yaml` | 平台地址、数据库、鉴权、网关连接、会话保留策略 | [Grasp 完整示例](server/config.example.yaml)；[E2B Compose 配套示例](sandbox-gateway/deploy/e2b/grasp.example.yaml) |
| `gateway.docker.yaml` | Docker 镜像、网络、资源 | [Docker 示例](sandbox-gateway/gateway/config.docker.example.yaml) |
| `gateway.kubernetes.yaml` | K8s 镜像、集群、PVC、资源与调度 | [Kubernetes 示例](sandbox-gateway/gateway/config.kubernetes.example.yaml) |
| `gateway.e2b.yaml` | E2B API、沙箱代理地址、准备好的模板 | [E2B 示例](sandbox-gateway/deploy/e2b/gateway.example.yaml) |

后三份 **三选一**。Grasp 不保存 E2B API key、templateID、kubeconfig 或 StorageClass，这些属于 gateway。沙箱内部的 Agent、SSH、IDE 和浏览器运行配置由 Grasp 下发。

## 配置放在哪里、谁覆盖谁

- Grasp：`CONFIG_PATH` 指定 YAML，默认 `config.yaml`；首个命令行参数可覆盖路径。文件不存在时使用环境变量和程序默认值。
- Gateway：`-config` 优先于 `SBGW_CONFIG`。两者都未指定时，若存在镜像约定路径 `/etc/sandbox-gateway/config.yaml` 则加载它，否则使用 Docker 默认值及环境变量（启动日志会提示）；指定的文件必须存在。文件只接受一个 YAML 文档和一个匹配 `driver` 的后端段。
- 普通配置：非空环境变量 > YAML > 程序默认值。Gateway 会拒绝未知字段、未知 `SBGW_*` 变量及其他后端的配置；无效数字/布尔值也会报错，不会静默忽略。
- 设置页管理的参数：环境变量 > 数据库保存值 > YAML > 默认值。环境变量会锁定相应字段；已有内存覆盖需要在设置页改回 `0` 才会跟随网关。
- Compose `.env` 只是编排输入，须由 `environment` 或挂载文件传入程序。模板和资源只支持 YAML，密钥建议通过环境变量注入。修改 gateway 配置或进程环境后重启服务。

## 默认配置是什么

| 项目 | 程序默认值 | 说明 |
| --- | --- | --- |
| Grasp 监听 | `8080` | 单独 gateway 默认 `:8080`，同机运行需改端口 |
| Grasp gateway URL | `http://127.0.0.1:8899` | 与下方成对示例一致 |
| Grasp transport | `direct` | `gateway` 模式通过鉴权网关转发数据，远程部署可选 |
| Grasp image | 空 | 使用网关默认镜像或模板 |
| Grasp 内存覆盖 | `0` | 使用所选环境的内存 |
| Docker/K8s 镜像 | `universal-sandbox:local` | 发布栈通过 `SANDBOX_IMAGE` 给 Docker 网关指定镜像 |
| Docker/K8s 默认资源 | `2 CPU / 4096 MiB / 25 GiB` | 各自后端独立配置 |
| Docker/K8s 资源上限 | `8 CPU / 16384 MiB / 500 GiB` | Docker 的磁盘字段不建立配额；K8s 用于 PVC |
| E2B 模板与资源 | **无内置模板或规格** | 必须填实际构建结果；示例的 `2 / 3072 / 20` 只是示例 |
| E2B API 地址 | **无默认值，必填** | 自建填自己的 API；云服务显式填 `https://api.e2b.app` |

## 可直接复制的默认示例

下面是同机运行 Grasp 与 Docker gateway 的两份最小 YAML。完整部署还需准备 runtime bundle、登录账号及稳定加密密钥，见 [开发指南](CONTRIBUTING.md) 与 [Grasp 完整示例](server/config.example.yaml)。

`grasp.yaml`：

```yaml
server:
  port: 8080
  mcp_advertise: http://host.docker.internal:8080
sandbox:
  gateway_url: http://127.0.0.1:8899
  sandbox_memory_mb: 0
  # image 不填，跟随网关。
```

`gateway.docker.yaml`：

```yaml
driver: docker
server:
  listen: "127.0.0.1:8899"
docker:
  image: universal-sandbox:local
  resources:
    default: {cpuCores: 2, memoryMB: 4096, diskGi: 25}
    max: {cpuCores: 8, memoryMB: 16384, diskGi: 500}
```

使用编译好的程序，分别运行 `grasp /absolute/path/grasp.yaml` 和 `sandbox-gateway -config /absolute/path/gateway.docker.yaml`。两端鉴权 key 分别通过 `GRASP_SANDBOX_GATEWAY_API_KEY` 与 `SBGW_API_KEYS` 注入相同值。这组配置创建的沙箱采用网关的 **2 CPU、4096 MiB**。

完整部署步骤见 [Docker](sandbox-gateway/deploy/docker/README.md)、[Kubernetes](sandbox-gateway/deploy/k8s/README.md) 和 [E2B](sandbox-gateway/deploy/e2b/README.md) 指南。K8s 使用 [独立 K8s 文件](sandbox-gateway/gateway/config.kubernetes.example.yaml)，Grasp 仍只连接 gateway。

## Grasp 平台配置

以下是常用部署项。全部已登记的环境变量、YAML 路径与类型见由代码生成的 [server/CONFIGURATION.md](server/CONFIGURATION.md)；配置示例见 [server/config.example.yaml](server/config.example.yaml)。

### 平台入口与存储

| YAML 路径 | 环境变量 | 程序默认值 | 用途 |
| --- | --- | --- | --- |
| `server.port` | `GRASP_PORT` | `8080` | Grasp HTTP 监听端口 |
| `server.deployment_mode` | `GRASP_DEPLOYMENT_MODE` | `development` | 部署模式；发布脚本使用 `local-demo`，共享部署配置 `production` |
| `server.public_advertise` | `GRASP_PUBLIC_ADVERTISE` | `http://localhost:<port>` | 用户浏览器可访问的平台来源 |
| `server.mcp_advertise` | `GRASP_MCP_ADVERTISE` | 沙箱模式为 `http://host.docker.internal:<port>`；worktree 模式为 `http://127.0.0.1:<port>` | 沙箱回连 MCP、下载运行时与配置包的 API 基址，必须从沙箱内可达 |
| `server.frame_ancestors` | `GRASP_FRAME_ANCESTORS` | 空 | 允许嵌入平台页面的门户来源；空表示不允许嵌入 |
| `database.driver` | `GRASP_DB_DRIVER` | `sqlite`；未指定 driver 但配置 DSN 时推导为 `mysql` | Grasp 数据库类型 |
| `database.path` | `GRASP_DB` | `grasp.db` | SQLite 文件；容器部署通常覆盖为 `/data/grasp.db` |
| `database.dsn` | `GRASP_DB_DSN` | 空 | MySQL 连接串 |
| `auth.users` | `GRASP_AUTH_USERS` | 空 | 平台登录账号与密码散列；生产部署显式提供 |
| `auth.session_ttl` | `GRASP_AUTH_SESSION_TTL` | `168h` | 登录会话有效期 |
| `security.secrets_key` | `GRASP_SECRETS_KEY` | 空 | 固定的凭据加密主密钥；非 production 可在数据库旁生成，production 显式提供并随数据库备份 |
| `engine.profiles_root` | `GRASP_PROFILES_ROOT` | `data/profiles` | Agent 配置目录，与 gateway 的环境 `profiles` 是不同概念 |
| `storage.driver` | `GRASP_STORAGE_DRIVER` | `local` | 附件存储类型 |
| `storage.blobs_root` | `GRASP_BLOBS_ROOT` | `data/blobs` | 附件文件目录，与数据库一起备份 |
| `storage.max_file_bytes` | `GRASP_MAX_FILE_BYTES` | `134217728`（128 MiB） | 单个文件导入上限 |
| `solo_backup.local_dir` | `GRASP_SOLO_BACKUP_DIR` | `data/solo-backups` | Solo 工作区备份目录；S3 配置见完整环境变量表 |
| `solo_backup.max_bytes` | `GRASP_SOLO_BACKUP_MAX_BYTES` | `4294967296`（4 GiB） | 单个压缩备份上限 |

`GRASP_PUBLIC_ADVERTISE` 与 `GRASP_MCP_ADVERTISE` 可以不同：前者给浏览器使用，后者给沙箱使用。选择 gateway transport 不会替代沙箱到 Grasp 的 MCP / 运行时下载回连路径。

### 连接 gateway 与选择环境

| YAML 路径 | 环境变量 | 程序默认值 | 用途 |
| --- | --- | --- | --- |
| `sandbox.gateway_url` | `GRASP_SANDBOX_GATEWAY_URL` | `http://127.0.0.1:8899` | 单网关连接，及已有未带命名绑定的沙箱所用连接 |
| `sandbox.gateway_api_key` | `GRASP_SANDBOX_GATEWAY_API_KEY` | 空 | 调用该网关的 Bearer key，须匹配 gateway 的 `auth.apiKeys` |
| `sandbox.gateway_transport` | `GRASP_SANDBOX_GATEWAY_TRANSPORT` | `direct` | `direct` 直接连接返回的沙箱端点；`gateway` 通过认证的 gateway 通道连接 |
| `sandbox.private_endpoints` | `GRASP_SANDBOX_PRIVATE_ENDPOINTS` | `false`；配置 gateway transport 时自动启用 | 端点仅服务端使用，浏览器预览走 Grasp 路径代理 |
| `sandbox.gateways` | — | 空列表 | 命名网关连接列表，适用于多个部署 |
| `sandbox.default_gateway` | `GRASP_SANDBOX_DEFAULT_GATEWAY` | 空，使用单网关连接 | 新建沙箱使用哪个命名连接；已有沙箱保留原绑定 |
| `sandbox.image` | `GRASP_SANDBOX_IMAGE` | 空 | 所有 Agent 后端的镜像 / 逻辑环境覆盖值；空时继续下面的解析顺序 |
| `sandbox.images[backend]` | `GRASP_SANDBOX_IMAGE_<BACKEND>` | 空 | 单独为某个 Agent 后端指定镜像，例如 `_CLAUDE_CODE`、`_CODEX`、`_KIMI_CODE` |
| `sandbox.env` | `GRASP_SANDBOX_ENV` | 空 map | 传给沙箱的非敏感环境选项；环境变量入口使用 `K=V,K2=V2` 格式 |
| `sandbox.acp_env` | — | 空 map | Agent 的非敏感环境选项；配置加载末尾合并到 `sandbox.env`，同名时 `acp_env` 覆盖已有值，建议避免重复键 |
| `sandbox.work_dir` | `GRASP_SANDBOX_WORK_DIR` | 空，使用系统临时目录 | Grasp 宿主侧暂存配置包的目录，不是沙箱内工作目录 |
| `sandbox.runtime_bundle` | `GRASP_SANDBOX_RUNTIME_BUNDLE` | `/app/sandbox-runtime/sandbox-runtime.tgz` | Grasp 下发的运行时包；服务端镜像已包含该文件，直接运行二进制时要提供实际构建产物 |

`direct` 和 `gateway` 只决定数据面连接方式，两者都通过 gateway 创建沙箱。`gateway` 要求网关 API key，并使用现有 `/preview/...` 路径代理，无需额外预览域名。命名连接的字段如下：

| `sandbox.gateways[]` 字段 | 默认值 / 要求 | 含义 |
| --- | --- | --- |
| `id` | 必填，部署内唯一 | 稳定连接标识；已有沙箱使用期间保留 |
| `url` | 必填 | 该 gateway 的 HTTP(S) 地址 |
| `api_key` / `api_key_env` | 空；gateway transport 或 profile 发现必须提供 key | 直接设置 key，或指定从哪个环境变量取 key；只选一种来源 |
| `transport` | `gateway` | 与单网关 `gateway_transport` 默认 `direct` 不同 |
| `profile` | 空 | 可选环境名，Docker/K8s 在各自的 `profiles` 中声明；E2B 直接使用 `templates` 下的名称 |

连接列表和默认连接修改后重启 Grasp。完整迁移示例见 [GATEWAY.md](GATEWAY.md#gateway-transport-and-multiple-deployments)。

**镜像解析顺序**：非空 `sandbox.image` > `sandbox.images[backend]` > 网关默认镜像或模板。命名连接指定 `profile` 时，由网关选择该环境。普通部署保持 `image` 为空即可，不需要把模板名称再写到 Grasp。

### 资源、并发与超时

| YAML 路径 | 环境变量 | 程序默认值 | 含义 / 设置页 |
| --- | --- | --- | --- |
| `sandbox.sandbox_memory_mb` | `GRASP_SANDBOX_MEMORY_MB` | `0` | 跟随网关/模板；非零覆盖至少 `1024 MiB`，环境变量会锁定设置页 |
| `engine.max_concurrent_runs` | `GRASP_MAX_RUNS` | `5` | 最大并发运行数；设置页可改 |
| `engine.node_auto_retry_max` | `GRASP_NODE_AUTO_RETRY` | `3` | 节点自动重试次数；设置页可改 |
| `sandbox.max_test_sandboxes` | — | `5` | 测试沙箱数量上限；设置页可改 |
| `sandbox.max_solo_sandboxes` | — | `5` | Solo 沙箱数量上限；设置页可改 |
| `sandbox.test_sandbox_ttl_minutes` | — | `30 分钟` | 测试沙箱空闲保留时长；设置页可改 |
| `sandbox.run_sandbox_ttl_minutes` | — | `30 分钟` | 工作流节点结束后沙箱保留时长；设置页可改 |
| `sandbox.run_sandbox_max_retention_hours` | — | `24 小时` | 工作流沙箱延长保留的硬上限；设置页可改，范围 `1–168` |
| `sandbox.chat_idle_timeout_seconds` | `GRASP_CHAT_IDLE_SEC` | `1200 秒` | Agent 无输出且无 CPU/IO 活动的时限；设置页以分钟显示，默认 `20 分钟` |
| `sandbox.agent_node_hard_cap_hours` | `GRASP_AGENT_NODE_HARD_CAP_HOURS` | `24 小时` | 画布未设节点总时限时的兜底上限 |
| `sandbox.sandbox_create_timeout_seconds` | `GRASP_SANDBOX_CREATE_TIMEOUT_SEC` | `1200 秒` | Grasp 等待沙箱创建就绪的时限，应不小于 gateway 的 finalize 时限 |
| `sandbox.sandbox_max_attempts` | `GRASP_SANDBOX_MAX_ATTEMPTS` | `3` | 可重试沙箱故障的最大尝试次数 |
| `sandbox.sandbox_retry_backoff_seconds` | `GRASP_SANDBOX_RETRY_BACKOFF_SEC` | `2 秒` | 沙箱重试基础退避时长 |
| `sandbox.agent_chat_timeout_seconds` | `GRASP_AGENT_TIMEOUT_SEC` | 已废弃并忽略 | 不再控制单轮总时长；使用节点总时限和 Agent 无动作时限 |

Grasp 默认不覆盖 CPU、内存或磁盘。设置页将内存填回 `0` 会清除覆盖，后续创建跟随网关；已存在的沙箱不会调整规格。E2B 的非零内存必须等于选中模板的规格，Docker/K8s 则受各自的 `resources.max` 限制。

### 浏览器与其他平台选项

以下浏览器参数只支持 YAML：

| YAML 路径 | 程序默认值 | 用途 |
| --- | --- | --- |
| `browser.max_tabs` | `16` | 浏览器预览 tab 总量上限 |
| `browser.max_tabs_per_container` | `1` | 每个沙箱桌面的 tab 数；同一沙箱的访问者共享桌面 |
| `browser.tab_idle_ttl_seconds` | `300 秒` | 空闲 tab 保留时间 |
| `browser.container_idle_ttl_seconds` | `600 秒` | 没有 tab 时清理缓存的 CDP 连接，不销毁沙箱 |
| `browser.desktop_idle_ttl_seconds` | `0` | 没人观看时释放预览页面的时限；平台打开的页面关闭，Agent 自己的页面只断开连接；`0` 表示随沙箱保留 |

GitHub 设备码登录（`github.oauth_client_id`，默认空）、Codex 登录续期（`codex_login`）和 Solo S3 备份（`solo_backup.s3`）的字段与默认值见 [完整环境变量表](server/CONFIGURATION.md)。OpenCode 模型目录由 `sandbox.opencode_catalog_url` / `GRASP_OPENCODE_CATALOG_URL` 指定，未配置时使用 `https://models.dev/api.json`。

## Gateway 公共配置

这些字段可用于任何后端。资源与镜像字段分别位于 `docker`、`kubernetes` 或 `e2b`，不放在公共段。

| YAML 路径 | 环境变量 | 默认值 | 用途 |
| --- | --- | --- | --- |
| `driver` | `SBGW_DRIVER` | `docker` | `docker` / `kubernetes` / `e2b`；环境变量与文件不能冲突 |
| `server.listen` | `SBGW_LISTEN` | `:8080` | 网关 API 监听 |
| `server.finalizeTimeoutSeconds` | `SBGW_FINALIZE_TIMEOUT_SEC` | `1200` | 创建/重装就绪期限 |
| `database.driver` | `SBGW_DB_DRIVER` | `sqlite` | `sqlite` 或 `mysql` |
| `database.path` | `SBGW_DB_PATH` | `gateway.db` | SQLite 持久化文件 |
| `database.dsn` | `SBGW_DB_DSN` | 空 | MySQL DSN；优先于分项 |
| `database.host` / `port` | `SBGW_DB_HOST` / `SBGW_DB_PORT` | 空 / `0`（连接时用 `3306`） | MySQL 主机和端口 |
| `database.user` / `password` / `name` | `SBGW_DB_USER` / `SBGW_DB_PASSWORD` / `SBGW_DB_NAME` | 空 | MySQL 账号及数据库 |
| `database.params` | — | 空 | MySQL 默认补 `charset=utf8mb4&parseTime=True&loc=Local` |
| `auth.apiKeys` | `SBGW_API_KEYS` | `[]` | Bearer key，环境变量用逗号分隔；空禁用 v1 鉴权且不能使用 v2 transport |
| `orphanGC.enabled` | `SBGW_ORPHAN_GC_ENABLED` | `false` | 清理无数据库记录的本部署工作负载 |
| `orphanGC.intervalSeconds` | `SBGW_ORPHAN_GC_INTERVAL_SEC` | `300` | 扫描间隔 |
| `orphanGC.minAgeSeconds` | `SBGW_ORPHAN_GC_MIN_AGE_SEC` | `600` | 最小清理年龄；`0` 无年龄限制 |
| `ports.session` / `codeServer` / `ssh` | — | `8765` / `8744` / `22` | 会话、IDE、SSH 端口 |
| `ports.cdp` / `novnc` | — | `9222` / `6080` | 内部端口，不发布到 Docker 主机 / K8s LB |
| `ports.app` | — | 见各后端示例 | 额外应用端口 |

日志通过 `SBGW_LOG_LEVEL`（默认 `info`）和 `SBGW_LOG_PRETTY`（默认 `false`）设置，无 YAML 字段。时间单位为秒，内存 MiB，磁盘 GiB。

## Docker 配置

只在 `driver: docker` 时使用 [Docker 文件](sandbox-gateway/gateway/config.docker.example.yaml)。

| `docker` 字段 | 环境变量 | 默认值 | 用途 |
| --- | --- | --- | --- |
| `image` | `SBGW_DOCKER_IMAGE` | `universal-sandbox:local` | 真实 OCI 镜像 |
| `bindIP` | `SBGW_DOCKER_BIND_IP` | `127.0.0.1` | 发布端口的宿主地址 |
| `network` | `SBGW_DOCKER_NETWORK` | 空 | Docker 网络 |
| `namePrefix` / `shmSize` | — | `sbx-` / `1g` | 容器名称前缀及共享内存 |
| `resources.default` | — | CPU `2`、memory `4096`、disk `25` | 请求省略资源时采用；字段为 `cpuCores/memoryMB/diskGi` |
| `resources.max` | — | CPU `8`、memory `16384`、disk `500` | API 请求上限；不等于 Docker 磁盘配额 |
| `profiles` | — | `{}` | 可选 `名称: {image: OCI镜像}`，供 Grasp 命名连接选择 |

## Kubernetes 配置

只在 `driver: kubernetes` 时使用 [K8s 文件](sandbox-gateway/gateway/config.kubernetes.example.yaml)。资源由自己的 `kubernetes.resources` 定义，与 Docker 无关。

| `kubernetes` 字段 | 环境变量 | 默认值 | 用途 |
| --- | --- | --- | --- |
| `image` | `SBGW_K8S_IMAGE` | `universal-sandbox:local` | 节点可获取的 OCI 镜像 |
| `inCluster` | `SBGW_K8S_IN_CLUSTER` | `false` | 使用 Pod service account |
| `kubeconfig` | `SBGW_K8S_KUBECONFIG` | 空 | 留空使用标准 kubeconfig / in-cluster 发现 |
| `namespace` | `SBGW_K8S_NAMESPACE` | `sandboxes` | 沙箱所在 namespace |
| `namePrefix` | — | `sbx-` | 工作负载名称前缀 |
| `storageClass` | `SBGW_K8S_STORAGE_CLASS` | 空 | 使用集群默认 StorageClass |
| `pvcAnnotations` | — | `{}` | 新建 PVC 的注解 |
| `imagePullSecret` | — | 空 | 私有镜像仓库 Secret |
| `imagePullPolicy` | `SBGW_K8S_IMAGE_PULL_POLICY` | `IfNotPresent` | `Always` / `IfNotPresent` / `Never` |
| `enableLoadBalancer` | `SBGW_K8S_ENABLE_LB` | `true` | 是否创建公开端口的 LB Service |
| `resources.default` / `resources.max` | — | `2/4096/25` / `8/16384/500` | CPU、内存、PVC 容量；字段为 `cpuCores/memoryMB/diskGi` |
| `cpuRequestCores` / `memoryRequestMB` | — | `0.25` / `512` | 调度 request，固定值优先；填 `0` 才使用对应比例 |
| `cpuRequestRatio` / `memoryRequestRatio` | — | `0.25` / `0.25` | `request = limit × ratio`，范围 `(0,1]`，驱动应用最小 request 保护 |
| `profiles` | — | `{}` | 可选环境名到 OCI 镜像映射 |

## E2B 模板与连接配置

只在 `driver: e2b` 时使用 [E2B 文件](sandbox-gateway/deploy/e2b/gateway.example.yaml)。**没有 Docker 镜像字段、K8s 资源字段或隐式云服务域名。**

```yaml
driver: e2b
e2b:
  apiURL: https://api.e2b.example.com
  sandboxURL: https://sandbox.e2b.example.com
  ownerID: my-grasp-deployment
  stateDir: /data/e2b
  templates:
    standard:
      templateID: replace-with-build-result
      cpuCores: 2
      memoryMB: 3072
      diskGi: 20
```

另通过 `SBGW_E2B_API_KEY` 提供该 E2B 服务的 API key；这是 provider key，与 Grasp 调用 gateway 的 key 不同。

### 模板、镜像与名称

| 名称 | 从哪里来 | 用途 |
| --- | --- | --- |
| OCI 镜像 | 仓库的 sandbox Dockerfile 构建，固定为 `仓库/镜像@sha256:摘要` | E2B 模板构建的输入，包含系统环境、CLI 和 bootstrap |
| E2B 模板 | `prepare-template.py` 调用 E2B SDK 构建并验证 | 创建 guest 的准备好环境，CPU/内存在构建时指定 |
| `templateID` | E2B 构建返回 | gateway 真正提交给 E2B API 的模板标识 |
| `standard` | 你给网关取的模板名称，构建命令 `--name standard` | `templates` 的键，不是镜像仓库名；可换成任何符合规则的名称 |
| 构建 `--alias` | 你给 E2B 构建取的版本名 | 构建管理用途；部署配置引用构建返回的 `templateID` |

运行过程：**Grasp 不覆盖环境 → gateway 选择默认模板 → 用该条 templateID 创建沙箱，并采用该条资源规格**。单模板自动选择；多模板设置 `e2b.defaultTemplate`。通过 Grasp 命名连接的 `profile: standard` 可以选择指定模板，E2B 模板名自动作为 profile 发布，不再维护第二份映射。

统一创建 API 仍可接受 `image` 作为显式覆盖：Docker/K8s 是 OCI 镜像，E2B 是模板名称。普通 Grasp 配置无需填写它，也无需与 gateway 重复保持三个名称一致。切换模板时，未覆盖的资源取自新模板，显式覆盖不匹配则在创建前返回错误。

### E2B 字段与默认值

| `e2b` 字段 | 环境变量 | 默认值 / 要求 | 含义 |
| --- | --- | --- | --- |
| `apiURL` | `SBGW_E2B_API_URL` | 必填，无默认 | 你的 E2B 控制 API；自建填写自建地址，云服务填写 `https://api.e2b.app` |
| `sandboxURL` | `SBGW_E2B_SANDBOX_URL` | 空 | 自建沙箱代理来源；留空使用 provider 返回的每个沙箱域名，返回为空则报错 |
| `apiKey` | `SBGW_E2B_API_KEY` | 必填 | E2B API key，建议仅环境变量 |
| `ownerID` | `SBGW_E2B_OWNER_ID` | 必填 | 稳定、唯一的部署标识，限制管理范围 |
| `stateDir` | `SBGW_E2B_STATE_DIR` | 必填，绝对路径且不能为 `/` | 私有连接恢复记录，须持久化 |
| `bootTimeoutSeconds` | `SBGW_E2B_BOOT_TIMEOUT_SEC` | `600` | guest 启动及私有连接准备期限 |
| `leaseSeconds` | `SBGW_E2B_LEASE_SEC` | `3600` | 请求的 provider 租约，至少 `30` |
| `renewIntervalSeconds` | `SBGW_E2B_RENEW_INTERVAL_SEC` | `300` | 必须大于 0 且小于租约一半 |
| `autoPause` | `SBGW_E2B_AUTO_PAUSE` | `true` | 新 guest 到期自动暂停；显式删除仍销毁 |
| `templates` | — | 必填，无默认模板 | `名称 → {templateID,cpuCores,memoryMB,diskGi}` |
| `defaultTemplate` | — | 单模板自动选择，多模板必填 | 必须是 `templates` 中的名称 |

CPU 为正整数核数，内存为 MiB，`diskGi` 是构建探针验证的最低初始可用工作区，不是动态扩容或磁盘配额。规格直接复制构建输出的 `gatewayTemplates`，无需再写 Docker/K8s 风格的资源默认值或上限。

自建配置直接填写 `apiURL` 与 `sandboxURL`；它们分别用于 E2B 控制请求和沙箱私有流量。Grasp 的网页入口由 `public_advertise` 设置，沙箱回连 Grasp 使用 `mcp_advertise`，与这两个 provider 地址用途不同。完整构建和 Compose 环境变量见 [E2B 部署指南](sandbox-gateway/deploy/e2b/README.md)。

## 沙箱内部配置

下面是箱内程序的默认值，Grasp 创建沙箱时可能注入覆盖值。完整列表与用途见 [sandbox/README.md 的环境变量](sandbox-gateway/sandbox/README.md#环境变量)。不要把箱内变量与 Grasp / gateway 进程的环境变量混用。

| 箱内变量 | 独立运行时默认值 | Grasp 管理沙箱时的来源 / 用途 |
| --- | --- | --- |
| `GRASP_RUNTIME_URL` | 无，启动必填 | Grasp 自动提供运行时包下载地址 |
| `SANDBOX_INJECT` / `SANDBOX_INJECT_HEADERS` | 空 | Grasp 提供配置包与有作用域的下载凭据 |
| `WORKSPACE_DIR` | `/root/workspace` | 沙箱内的代码目录；与 Grasp 的 `sandbox.work_dir` 不同 |
| `ROOT_PASSWORD` | `toor` | Grasp 为沙箱注入密码；不要按独立脚本默认值推断平台沙箱密码 |
| `SSH_KEY` | 空 | 可选 SSH 公钥 |
| `CODE_SERVER_PORT` | `8744` | IDE 监听端口 |
| `AGENT_PROVIDER` | `cursor` | Grasp 按所选编码工具设置，与 gateway `driver` 无关 |
| `ACP_BRIDGE_PORT` | `8765` | Agent 会话服务端口 |
| `ACP_BRIDGE_PASSWORD` | 空 | Grasp 创建时注入会话鉴权口令 |
| `SKIP_INNER_DOCKER` | 按 `0` 处理，启动内层 Docker | 设置 `1` 时跳过内层 dockerd |
| `INNER_DOCKER_STORAGE_DRIVER` | `vfs` | 内层 Docker 存储驱动 |
| `VNC_PREVIEW` / `BROWSER_MCP` | 关闭 | Grasp 可按 Agent 的预览能力启用浏览器与 MCP |
| `PREVIEW_DIRECT` / `PREVIEW_PORT` | 空 | 直连预览注入开关与应用端口，由平台预览配置决定 |

模型 API key、工具登录文件、Git Token 和 SSH 私钥在工作区「凭据」中管理。Git 凭据由仓库绑定，模型凭据由具名模型配置选择；它们不属于这份平台部署配置。

镜像负责系统依赖与原始 `grasp-bootstrap.sh`，运行逻辑由 Grasp 发布的 runtime bundle 提供。热更新不会重跑首次启动中的凭据、clone、dockerd、code-server 初始化；详见 [镜像与运行时包](sandbox-gateway/sandbox/README.md#镜像与运行时包)。

## 配置不生效时检查什么

| 现象 | 检查项 |
| --- | --- |
| 改了 YAML 内存仍不生效 | 先检查环境变量，再看设置页的数据库覆盖；改回 `0` 跟随网关 |
| gateway 启动报配置错误 | 检查 `driver`、文件是否存在、是否混入其他后端字段或未知变量；按独立示例重写 |
| E2B 模板规格不匹配 | 清除 Grasp 镜像/内存覆盖；需要覆盖时必须匹配选中模板 |
| 多模板不知选哪个 | 填 `e2b.defaultTemplate`，或在 Grasp 命名连接中选 `profile` |
| `.env` 改了容器没变化 | 确认 Compose 传入该变量，并重新创建容器 |
| 创建成功但 runtime / MCP 失败 | 从沙箱检查 `mcp_advertise` 可达性，数据通道不会替代回连路径 |

默认值依据 [Grasp 配置](server/internal/config/config.go)、[gateway 配置](sandbox-gateway/gateway/internal/config/config.go) 与 [后端字段定义](sandbox-gateway/gateway/internal/config/types.go)。完整 Grasp 环境变量表由生成器维护。
