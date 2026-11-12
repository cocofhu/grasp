# Docker 部署

本指南使用根目录的 [compose.release.yaml](../../../compose.release.yaml)，在同一台 Linux 主机运行 Grasp 与 Docker 驱动 gateway。沙箱由 gateway 在宿主 Docker daemon 上创建。标准发布镜像与 runtime bundle 使用 Linux amd64/x86-64；定制架构需提供匹配的镜像和运行时包。

本地体验见[快速开始](../../../docs/content/guide/quick-start.md)。下文补齐共享或生产部署的配置、网络、持久化和验收步骤。其他驱动参见 [Kubernetes 指南](../k8s/README.md)与 [E2B 指南](../e2b/README.md)，不要把不同驱动的网络选项混用。

## 前提与拓扑

- 安装 Git、Docker Engine、Compose v2；部署账号能访问 Docker socket。准备域名、TLS 反向代理及有足够空间的持久磁盘。密码哈希可在安装了项目所需 Go 的管理机上生成。
- Grasp 和 gateway 使用 `network_mode: host`。`GRASP_PORT` 默认 8080，gateway 默认 8899；端口不能与已有进程冲突。这里不使用 Compose `ports:` 映射。
- gateway 挂载 `/var/run/docker.sock`，沙箱支持 privileged / DinD。主机应专用于受信任的工作负载；Docker socket 权限等同宿主管理权限。
- 本指南启用带鉴权的 `gateway` transport，浏览器通过 Grasp 使用终端、IDE、预览和 noVNC。gateway 与沙箱的宿主映射监听 loopback；CDP/noVNC 保留在容器网络中。

## 1. 准备独立配置

在仓库根执行，真实配置保存在仓库外：

```bash
grasp_deploy_dir="$HOME/.config/grasp-deploy"
install -d -m 700 "$grasp_deploy_dir"
cp sandbox-gateway/deploy/docker/compose.production.example.yaml \
  "$grasp_deploy_dir/compose.production.yaml"
umask 077
```

首次安装生成一个 gateway key、一个固定的加密主密钥及自有账号的 bcrypt 哈希。已有部署须沿用原 `GRASP_SECRETS_KEY`，不要重新生成：

```bash
openssl rand -hex 32       # SANDBOX_GATEWAY_API_KEY
openssl rand -base64 32    # GRASP_SECRETS_KEY，32 字节密钥的 base64 表示
./server/scripts/gen-auth-hash.sh --username admin  # 交互输入密码
```

在 `$grasp_deploy_dir/production.env` 写入以下内容，替换所有占位值。三个镜像应使用同一次经过验收的发布，生产固定到实际 digest；下列 tag 是示例，不代表自动升级到最新版本。

```dotenv
GRASP_IMAGE=ghcr.io/cocofhu/grasp:3.3.0
SANDBOX_GATEWAY_IMAGE=ghcr.io/cocofhu/sandbox-gateway:3.3.0
SANDBOX_IMAGE=ghcr.io/cocofhu/universal-sandbox:3.3.0
GRASP_PORT=8080
GRASP_GATEWAY_PORT=8899
SANDBOX_GATEWAY_API_KEY=REPLACE_WITH_GENERATED_GATEWAY_KEY
GRASP_SECRETS_KEY=REPLACE_WITH_GENERATED_BASE64_KEY
GRASP_AUTH_USERS='[{"username":"admin","password_hash":"REPLACE_WITH_BCRYPT_HASH","is_admin":true}]'
GRASP_PUBLIC_ADVERTISE=https://grasp.example.com
GRASP_MCP_ADVERTISE=http://10.20.30.10:8080
```

`GRASP_AUTH_USERS` 外层单引号必须保留，避免 Compose 将 bcrypt 中的 `$` 当作插值。不要使用示例密码或把该文件提交到 Git。限制读取权限：

```bash
chmod 600 "$grasp_deploy_dir/production.env"
```

`.env` / `--env-file` 只是 Compose 的变量输入，**不会自动把所有变量传进容器**。附带的 override 显式转发回连地址、公开地址、鉴权和 transport，并让 gateway 与 Grasp 使用同一把 key；仅在原 `.env` 中添加 `GRASP_MCP_ADVERTISE` 不会改变原 release Compose 的服务环境。确认当前 shell 没有 export 同名旧值，以免覆盖环境文件。其他配置也须添加到服务 `environment`，或挂载配置文件并设置 `CONFIG_PATH`（Grasp）/ `SBGW_CONFIG`（gateway）。应用实际配置优先级为进程环境 > 配置文件 > 默认值。

## 2. 配置网络与入口

| 配置/方向 | 必须满足 |
|---|---|
| `GRASP_PUBLIC_ADVERTISE` | 浏览器能访问的 Grasp 基址，含实际协议和非默认端口，不带 `/api` 后缀 |
| `GRASP_MCP_ADVERTISE` | **沙箱内部**能解析并访问的 Grasp API 基址，用于 runtime/config 下载、MCP、模型和 Git 代理等回连；不能填仅托管 SPA 的前端地址 |
| Grasp → gateway | 同机 `http://127.0.0.1:8899`，控制面 key 必须匹配 |
| gateway → sandbox | 宿主机能访问沙箱 bridge IP 和本地映射端口 |
| sandbox → 内网/外网 | 容器 DNS、Docker 网络路由、宿主 FORWARD/NAT、防火墙和目标服务 ACL 都允许对应连接 |

`10.20.30.10` 只是示例，替换为沙箱可达的宿主内网地址或受控回连代理地址。容器内 `127.0.0.1` 指向容器自身；宿主 `curl` 成功不能证明沙箱连通。Docker 没有 E2B 的 `ALLOW_SANDBOX_INTERNAL_CIDRS`，内网访问应检查 Docker daemon/DNS、路由、防火墙与 VPN 的转发规则。自定义 Docker network 要先创建，并在 override 中给 gateway 设置 `SBGW_DOCKER_NETWORK`。

反向代理对用户提供 HTTPS，转发到 Grasp 的 8080，保留原始 Host（含端口，例如 nginx `proxy_set_header Host $http_host`）和 WebSocket Upgrade，按长连接配置超时。所有 Grasp UI/API/终端/IDE/预览路径应正确转发；预览走 `/preview/...`，不需要单独预览域名。若另设只供沙箱回连的代理，仅允许 `/mcp/`、`/sandbox-inject/` 与 GET `/embed-api/origin/<runID>`，具体边界见 [E2B 指南中的回连说明](../e2b/README.md#部署边界)。

由于 host 网络下 Grasp 监听宿主端口，须用宿主防火墙限制 8080 的来源为边缘代理及所需的沙箱回连网络。只开放用户 HTTPS 入口；不要为排障公开 Docker socket、gateway、CDP 或 noVNC。

## 3. 校验、拉取与启动

在仓库根使用 Bash 数组保存完整的 Compose 参数，后续命令使用同一项目名、配置文件和数据路径：

```bash
grasp_compose=(docker compose --project-name grasp \
  --env-file "$grasp_deploy_dir/production.env" \
  -f compose.release.yaml -f "$grasp_deploy_dir/compose.production.yaml")
"${grasp_compose[@]}" config --quiet
"${grasp_compose[@]}" pull
# 预热沙箱镜像；改为 production.env 中实际选择的 tag 或 digest。
docker pull ghcr.io/cocofhu/universal-sandbox:3.3.0
"${grasp_compose[@]}" up -d
"${grasp_compose[@]}" ps
curl --fail --silent http://127.0.0.1:8080/api/health
curl --fail --silent http://127.0.0.1:8899/healthz
```

如改过端口，替换最后两个地址。`pull` 只拉 Compose 服务镜像，沙箱镜像可预拉或首次创建时按需拉取。已有栈须沿用原项目名，避免同时启动两套服务抢占端口和数据。

Compose 按 `-f` 顺序合并，所有相对路径相对于第一个文件，因此数据仍在仓库根 `.localdata`。规则见 [Docker 官方说明](https://docs.docker.com/compose/how-tos/multiple-compose-files/merge/)。`config --quiet` 校验但不打印展开后的密钥。此生产流程直接调用 Compose；后续不要改用未携带这个 override 的 `./start.sh` 命令管理它。

## 4. 验收与排障

健康检查仅证明进程响应。还需在 Grasp 登录，配置工作区模型/Git 凭据，创建一个测试会话，验证沙箱启动、终端命令、文件保留、真实 Agent/MCP 往返、IDE HTTP/WebSocket、注册应用预览及 noVNC。通过 Grasp 创建时会注入 `GRASP_RUNTIME_URL` 和有作用域的下载鉴权；直接向 gateway 发一个只有 `AGENT_PROVIDER` 的 create 请求无法启动当前环境镜像。

从该沙箱的 Grasp 终端检查目标 DNS 和 HTTP 响应，例如（替换示例目标）：

```bash
getent hosts git.internal.example.com
curl --connect-timeout 5 --max-time 15 -I https://git.internal.example.com
```

| 现象 | 检查位置 |
|---|---|
| gateway 返回 401 / 通道失败 | 两端 API key、`gateway` transport、网关版本是否支持 v2 |
| 容器反复退出 / bootstrap 下载失败 | gateway 日志、沙箱日志、沙箱到 `GRASP_MCP_ADVERTISE` 的 DNS/路由/TLS；不要打印含 token 的完整环境 |
| Git/模型回连超时 | 在沙箱内检查目标；再核对宿主转发、Docker DNS、代理和目标 ACL |
| UI 正常，终端/IDE/noVNC 断开 | 反向代理的 WebSocket/超时和 Host；gateway 到容器网络 |
| 预览空白 | 应用绑定 `0.0.0.0`、端口已注册、路径代理下使用相对 URL；Vite HMR 单独验收 |

## 5. 持久化、升级与恢复

| 内容 | release Compose 默认位置 |
|---|---|
| Grasp SQLite | `.localdata/db/` |
| 附件 blobs、Agent/工作区配置等应用数据 | `.localdata/app-data/` |
| gateway 沙箱清单、连接状态 | `.localdata/gateway/` |
| 账号哈希、gateway key、固定加密主密钥和部署参数 | 仓库外 `production.env` 及 override；`start.sh` 部署则还包括原 `.env` |
| 沙箱内未提交文件 | 默认在沙箱容器可写层，**不在上述 `.localdata` 中** |

备份前让活动任务结束并阻止新任务，在维护窗口暂停 Grasp/gateway 写入，再成套备份三个数据目录和部署配置。只复制正在写入的 SQLite 单文件不保证一致；也可使用经过验证的 SQLite 在线备份或存储快照流程。自定义附件目录一并备份。`GRASP_SECRETS_KEY` 丢失或改变会使已有加密凭据无法解密；本地非生产模式自动生成的 `secrets.key` 也要保留。

沙箱文件另行通过 Git、文件下载或已配置的备份导出。Docker 驱动的 Stop 保留容器文件层，Destroy/重装可能移除它；Grasp 的停止/回收流程可能调用 Destroy，不能将 UI 停止当成备份。删除 `.localdata` 也不会自动清理所有 gateway 创建的沙箱。

升级前保存可恢复备份及三张镜像的旧 digest。更新配置后执行同一组 `pull` / `up -d`；新增环境变量需重建容器，`docker compose restart` 不会加载新环境。引入新 runtime 时先升级 Grasp 服务端，再切换新的沙箱环境镜像；仅 runtime 逻辑变化通常只发布服务端，见[镜像与运行时契约](../../sandbox/README.md)。gateway transport 升级则先确保 gateway 支持 v2，再启用 Grasp 配置。

恢复时先还原同一备份集的数据与原加密密钥，再启动匹配版本、核对 gateway 清单和沙箱实际状态。数据库有 schema 变化时不能只降镜像 tag；回滚应同时恢复兼容的数据备份。保留原部署目录、网关绑定和容器清单，完成上述产品验收后再恢复流量。
