# E2B 独立部署

E2B 是 sandbox-gateway 的第三种显式驱动，适用于 E2B Cloud 或自托管 E2B/Embed。使用仓库根目录的 [compose.e2b.yaml](../../../compose.e2b.yaml) 启动；原有 Docker、Kubernetes、`start.sh` 和其他 Compose 配置不读取本目录，默认行为不变。

当前完整真机验收针对自托管 E2B/Embed。Cloud 的控制面与私有流量协议有接口回归覆盖，尚未使用 Cloud 账号做端到端验收；接入 Cloud 时仍须执行下述相同的产品验收。

本标准部署要求 **Linux amd64/x86-64** 宿主机与同架构的 OCI/E2B 模板。服务端镜像构建的 runtime bundle 必须与 guest 架构一致；不宣称此 Compose 在 Arm 宿主机开箱即用。定制架构部署须自行提供与 guest 匹配的运行时包并完成同等验收。

运行时 gateway 使用原生 Go 客户端，不依赖宿主机 Python SDK。Python 仅用于这里的模板构建与依赖探针。已有兼容性实测及上游限制见 [方案 issue #1111](https://github.com/cocofhu/grasp/issues/1111)；它不代替当前部署的验收。

## 部署边界

- Grasp 和 gateway 必须在同一 Linux 宿主机共享网络命名空间。此 Compose 使用 `network_mode: host`；guest 的 SSH、ACP、IDE、CDP、noVNC、应用端口均由 gateway 转成宿主机 `127.0.0.1` TCP 端口。它们不能直接作为用户浏览器地址，也不能直接拆到另一台宿主机。
- E2B 控制面 API key、envd token、traffic token、relay key 留在 gateway；浏览器只拿到注册预览的 Grasp 随机域名。guest 保留现有按作用域注入的 MCP、模型和 Git 凭据。
- guest 到 Grasp 的专用回连入口只转发 `/mcp/`、`/sandbox-inject/`，以及 **GET `/embed-api/origin/<runID>`**。最后一条只提供按 run/node 和短期 ticket 校验的嵌入聊天来源查询，不能扩展成整个 `/embed-api/`。运行时包和 ConfigHome 通过受保护的 `/sandbox-inject/` 下发。该入口不转发 `/api/`、登录页或整个 Grasp UI；网络策略只放行这一受控入口。provider 默认禁止内网访问时，由运维为该入口配置所需的精确规则，不要开放整个私有网段。
- 用户平台来源、guest 回连来源和预览来源分开配置。例如 `https://grasp.example.com`、`https://callback.internal.example.com` 和 `https://preview.example.net`。生产模式要求预览与平台属于**不同可注册站点**；`grasp.example.com` 与 `preview.example.com` 仍共享 Cookie 的站点边界，配置会拒绝。
- `*.preview.example.net` 的 DNS 和通配符 TLS 证书指向同一个 Grasp listener。反向代理须保留原始 `Host`、查询参数与 WebSocket Upgrade，直接转发完整根路径，不加 `/preview/` 前缀。每个随机子域名只服务其注册的应用；未知子域名不能回落到平台 API、静态文件或登录页。
- 只有边缘代理应能访问平台 HTTP listener。用宿主防火墙保护 Grasp 原始端口，gateway 控制面仅监听 loopback；不要把 E2B 管理端口、relay、CDP 或动态本地端口开放到公网。

独立来源保留应用的 `/`、`fetch('/api/...')`、`/__grasp/*`、Vite HMR 和 WebSocket 路径。现有嵌入聊天仍使用 Grasp 自身的短期 ticket、bearer 与严格 `postMessage` 来源检查，不把 E2B 认证头交给页面。生产环境还需实际浏览器验收第三方 Cookie 限制下的 ticket 流程；该流程不应依赖 iframe 携带平台登录 Cookie。

## 准备环境模板

模板必须来自可审计的 Grasp **环境 OCI 镜像**，使用 `@sha256:` 固定内容。模板包含系统包、真实 Agent CLI、SSH、code-server、Chromium、原始 `grasp-bootstrap.sh` 和 `IMAGE_LEVEL`；Grasp 的 backend、页面脚本等运行时逻辑仍由服务端在创建时下发，更新逻辑无需重建环境模板。

在仓库根目录执行。API key 通过操作者环境提供，不写入配置样例、命令参数或结果文件。自托管环境按实际部署设置 `E2B_API_URL` 与 `E2B_SANDBOX_URL`；Cloud 可使用 SDK 的默认域名。

```bash
python3 -m venv /tmp/grasp-e2b-builder
/tmp/grasp-e2b-builder/bin/python -m pip install -r sandbox-gateway/deploy/e2b/requirements-build.txt

# 先设置 E2B_API_KEY，以及 SANDBOX_OCI_IMAGE=仓库/镜像@sha256:完整摘要。
# 自托管环境还需 E2B_API_URL、E2B_SANDBOX_URL；不要输出这些凭据。
/tmp/grasp-e2b-builder/bin/python sandbox-gateway/deploy/e2b/prepare-template.py \
  --image "${SANDBOX_OCI_IMAGE:?Set an immutable OCI image with sha256 digest}" \
  --alias grasp-production-v1 \
  --logical-image universal-sandbox:e2b \
  --cpu-count 2 \
  --memory-mb 3072 \
  --minimum-workspace-gib 20 \
  --output /tmp/grasp-e2b-template.json
```

使用新的版本化 alias，避免无意更新另一部署引用的 alias。工具只上传当前 checkout 的原始 bootstrap 和 `IMAGE_LEVEL`，保留公开的 `PATH` 与 `PLAYWRIGHT_BROWSERS_PATH=/ms-playwright` 约定，并校验以下真实依赖：

- `cursor-agent`、`claude`、`codebuddy`、`traecli`、`opencode`、`codex`、`dsh`、`kimi`。
- Python、Node/npm、Go、Git、curl/tar、sshd、Docker、code-server、Xvfb、x11vnc、websockify、Chromium 和 `chrome-devtools-mcp`。

不复制 E2E wire fixture，不创建替代 vendor CLI，不调用真实模型，也不将缺失 CLI 视为成功。具备可执行文件不代表已经完成 Cursor、Trae、Kimi 等厂商账号授权；实际使用仍遵守原有 workspace 凭据配置。

构建完成后，工具用返回的**实际 templateID** 创建一个禁止公网入站、禁止出网、300 秒到期销毁的临时探针 guest，确认 templateID、CPU/RAM、bootstrap 摘要、IMAGE_LEVEL、依赖与实际可用工作区空间，然后删除这个 guest。成功 JSON 包含 `templateID`、`buildID`、测得的 `workspaceAvailableBytes` 和可复制到 gateway YAML 的 `gatewayProfile`。失败不会输出 provider 原始错误体或凭据；清理失败会保留本次探针的 `probeSandboxID` 并返回失败，须由操作者检查这一个 guest。

`diskGi: 20` 表示创建时 `/root/workspace` 所在文件系统**至少有 20 GiB 可用空间**。它不是 PVC 大小、磁盘配额，也不是每次创建都能调整的资源。CPU、RAM 和磁盘空间均须由预构建模板满足；不匹配的请求会被拒绝。工具不显式设置或依赖 `min_free_disk_mb`，也不据此声称 Embed 已扩容；该 SDK 参数在部分 Embed 部署中受服务端功能开关影响。空间不足时，调整实际 provider/builder 的受支持磁盘配置、重新从固定 OCI 构建，并重跑探针。gateway 每次新建 guest 还会重新检查可用容量。

只校验计划可加 `--dry-run`：它不导入 SDK、不联系 provider、不构建模板、不创建 guest。离线单测也不依赖 SDK：

```bash
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover \
  -s sandbox-gateway/deploy/e2b -p 'test_*.py' -v
```

## 配置 gateway 与 Grasp

复制 [gateway.example.yaml](gateway.example.yaml) 到 checkout 外的部署目录，填入成功结果的 `gatewayProfile`，并让 `image.ref` 与该 profile 的 `image` 相同。示例是 2 CPU、3072 MiB、初始可用 20 GiB。不要把逻辑 `image` 当作在 create 时临时转换 OCI 的指令；只有显式登记的准备好模板可以使用。部署引用实际 templateID，alias 用于构建管理。

`e2b.ownerID` / `E2B_GATEWAY_OWNER_ID` 是该部署稳定且唯一的身份。gateway 只操作匹配其保留 owner/id/driver metadata 的 guest。不要在重启时更换身份，或让两个同时运行的 gateway 共用同一个身份与恢复目录。

Compose 读取的变量如下；敏感值请由 Secret 管理或受保护的环境文件提供，文件放在 checkout 外：

| 变量 | 用途 |
| --- | --- |
| `E2B_GATEWAY_CONFIG` | 已填实际模板 profile 的 gateway YAML **绝对路径** |
| `E2B_GATEWAY_OWNER_ID` | 稳定且唯一的部署身份 |
| `E2B_API_KEY` | E2B 控制面凭据；仅交给 gateway |
| `E2B_API_URL`、`E2B_SANDBOX_URL`、`E2B_DOMAIN` | 可选 provider 地址；自托管使用实际控制/私有流量来源，Cloud 默认 `e2b.app` |
| `SANDBOX_GATEWAY_API_KEY` | Grasp→gateway 的独立 Bearer token |
| `GRASP_PUBLIC_ADVERTISE` | 用户访问的平台 HTTPS 来源，例如 `https://grasp.example.com` |
| `GRASP_MCP_ADVERTISE` | guest 能访问的受控回连来源，只开放上述精确回连路径 |
| `GRASP_PREVIEW_ADVERTISE` | 独立可注册站点的预览基址，例如 `https://preview.example.net`，须有通配符 DNS/TLS |
| `GRASP_AUTH_USERS`、`GRASP_SECRETS_KEY` | 原有平台账户散列配置与固定加密主密钥 |
| `GRASP_SANDBOX_IMAGE` | 与 profile 相同；默认 `universal-sandbox:e2b` |
| `GRASP_SANDBOX_MEMORY_MB` | 与 profile 相同；默认 `3072` |
| `GRASP_DATA_DIR` | 原有 Grasp 数据父目录，内部包含 `profiles/`；默认 `./data` |

Compose 固定启用 `GRASP_SANDBOX_PRIVATE_ENDPOINTS=true`，将 gateway 配置映射为 `/etc/sandbox-gateway/config.yaml`，设置 `SBGW_E2B_STATE_DIR=/data/e2b` 并共享宿主机 loopback。直接部署二进制时，对应变量使用 `SBGW_E2B_API_KEY`、`SBGW_E2B_OWNER_ID`、`SBGW_E2B_API_URL`、`SBGW_E2B_SANDBOX_URL`、`SBGW_E2B_DOMAIN`、`SBGW_E2B_STATE_DIR` 和 `SBGW_API_KEYS`；模板 profile 保持在 YAML 中。

Compose 按 gateway 健康检查安排首次启动顺序。显式 private-endpoints 模式下，Grasp 自身还会在打开数据库和执行沙箱 reconcile 前，等待 gateway `/healthz` 确认 E2B 驱动已就绪，最长等待 `sandbox.sandbox_create_timeout_seconds`（`GRASP_SANDBOX_CREATE_TIMEOUT_SEC`）；超时则退出。这个检查也覆盖 Docker 宿主重启和直接启动二进制，防止 gateway 尚未恢复本地端口时把仍然存活的沙箱当作已消失。

```bash
# --env-file 指向 checkout 外、权限受保护的部署环境文件。
docker compose --env-file /absolute/private/grasp-e2b.env -f compose.e2b.yaml config --quiet
docker compose --env-file /absolute/private/grasp-e2b.env -f compose.e2b.yaml up -d --build
```

不要使用会展开输出全部环境变量的 `docker compose config` 作为公开诊断。首次部署须通过新建会话实测 SSH/终端、文件、真实 Agent/MCP 往返、IDE HTTP/WS、预览根路径/HMR、Pick/Page Control/Live、noVNC/CDP，并确认浏览器响应和服务日志没有 E2B 流量凭据。测试完成后销毁自建 guest 并核对 provider 列表。

## 状态与生命周期

`e2b-gateway-data` 保存 gateway DB 与 `/data/e2b` 私有恢复记录；`e2b-server-data` 保存 Grasp DB。恢复记录包含连接 private guest 所需的 envd、traffic 和 relay 凭据，目录权限是 `0700`、记录是 `0600`，须作为凭据备份与保护。不要公开挂载、提交到仓库、输出内容，或用 `docker compose down -v` 删除后期待恢复。gateway 重启会恢复同一批 loopback 端口；端口被其他进程占用时会报错，不会悄悄换端口。恢复卷、稳定 ownerID 和平台 DB 应一并保留。

gateway 恢复 guest、连接凭据和本地端口，Grasp 继续使用原有冷会话机制。`per_node` 模式下，server 重启后，等待人工输入的 workflow 可以仍为 `waiting_human`，原 guest 与预览注册也可以保留，但进程内的 Agent 热会话不会因此自动重连；下一次标准回复可能按原有冷恢复路径新建 guest。不能据此宣称 Agent 热会话无缝恢复或原工作区自动继承，跨重启的会话与工作区行为须单独验收。

gateway 的显式 stop/start 对应 E2B 全内存 pause/resume；自动暂停和自动恢复未启用。运行中的 guest 由 gateway 定期续租，gateway 长时间不可用时仍受 provider lease 上限约束。**Grasp 当前“停止/删除沙箱”继续走 Destroy，不因选择 E2B 自动变成暂停**；现有 reconcile/TTL 语义也未改写。

续租只延长 provider 允许范围内的到期时间，不能突破 provider 对单次 guest 生命周期的硬上限；这与 Grasp 的空闲回收期限是两项独立策略。运维必须将目标 provider 的最长运行时长配置为足以覆盖预期的长任务和 Solo 会话，并在目标部署核对续租后实际返回的到期时间。续租请求成功不代表到期时间一定继续延后，本模式不承诺 guest 无限期运行。

pause/resume 仍需在目标 provider 版本上单独验收。既有实测包含完整服务恢复成功，也出现过 provider 已运行但 private 路由尚未恢复的超时；“provider running”不能单独证明应用、SSH、IDE、CDP 和 Docker 都可访问。失败应保留安全状态证据并处理本部署拥有的 guest，不自动降级成新建空白 guest。

不支持 `preserveData` 的 reinstall；需要保留数据的重装必须先有明确的快照/迁移流程。普通无保留 reinstall 会销毁后重建。此模式也不接受宿主路径挂载，应使用原有 bundle 注入机制；不要把此限制包装成与 PVC 相同的持久化保证。
