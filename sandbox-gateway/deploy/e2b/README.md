# E2B 独立部署与配置

E2B 是 sandbox-gateway 的一种独立驱动，适用于 E2B Cloud 或自托管 E2B/Embed。使用仓库根目录的 [compose.e2b.yaml](../../../compose.e2b.yaml) 启动，并分别准备本目录的 Grasp 与 E2B gateway 配置。Docker 和 Kubernetes 使用各自的独立后端示例，配置不能混用。

当前完整真机验收针对自托管 E2B/Embed。Cloud 的控制面与私有流量协议有接口回归覆盖，尚未使用 Cloud 账号做端到端验收；接入 Cloud 时仍须执行下述相同的产品验收。

本标准部署要求 **Linux amd64/x86-64** 宿主机与同架构的 OCI/E2B 模板。服务端镜像构建的 runtime bundle 必须与 guest 架构一致；不宣称此 Compose 在 Arm 宿主机开箱即用。定制架构部署须自行提供与 guest 匹配的运行时包并完成同等验收。

运行时 gateway 使用原生 Go 客户端，不依赖宿主机 Python SDK。Python 仅用于这里的模板构建与依赖探针。已有兼容性实测及上游限制见 [方案 issue #1111](https://github.com/cocofhu/grasp/issues/1111)；它不代替当前部署的验收。

## 部署边界

- Grasp 和 gateway 必须在同一 Linux 宿主机共享网络命名空间。此 Compose 使用 `network_mode: host`；guest 的 SSH、ACP、IDE、CDP、noVNC、应用端口均由 gateway 转成宿主机 `127.0.0.1` TCP 端口。它们不能直接作为用户浏览器地址，也不能直接拆到另一台宿主机。
- E2B 控制面 API key、envd token、traffic token、relay key 留在 gateway；浏览器只通过 Grasp 的 `/preview/...` 路径代理访问注册预览。guest 保留现有按作用域注入的 MCP、模型和 Git 凭据。
- guest 到 Grasp 的专用回连入口只转发 `/mcp/`、`/sandbox-inject/`，以及 **GET `/embed-api/origin/<runID>`**。最后一条只提供按 run/node 和短期 ticket 校验的嵌入聊天来源查询，不能扩展成整个 `/embed-api/`。运行时包和 ConfigHome 通过受保护的 `/sandbox-inject/` 下发。该入口不转发 `/api/`、登录页或整个 Grasp UI；网络策略只放行这一受控入口。provider 默认禁止内网访问时，按下文[内网访问](#内网访问)在 provider 上配置所需的精确规则。
- 用户平台来源与 guest 回连来源分开配置，例如 `https://grasp.example.com` 和 `https://callback.internal.example.com`。预览不需要额外域名或通配符证书。
- 只有边缘代理应能访问平台 HTTP listener。用宿主防火墙保护 Grasp 原始端口，gateway 控制面仅监听 loopback；不要把 E2B 管理端口、relay、CDP 或动态本地端口开放到公网。

预览与 Docker/Kubernetes 一样走路径代理：应用页面在 CSP sandbox 的不透明来源中运行，代理只服务当前沙箱代次的注册端口，并在双向剥离 E2B 路由头。路径代理会改写 HTML 中的根路径引用，但应用脚本里写死的根路径请求（例如 `fetch('/api/...')`）和 Vite HMR 的 WebSocket 地址需要应用自行使用相对路径，这与现有 Docker/Kubernetes 预览一致。

## 内网访问

E2B 的沙箱出站规则与 gateway 所在网络分开。宿主机或 gateway 能访问内网 Git、模型服务、DNS 或 Grasp 回连入口，不代表 guest 也能访问。E2B 的默认规则拦截 `10.0.0.0/8`、`172.16.0.0/12`、`192.168.0.0/16` 等地址；网关创建 guest 时设置的 `allow_internet_access: true` 不会解除这项限制，`network.allowPublicTraffic: false` 则只控制沙箱公网入站。

支持 `ALLOW_SANDBOX_INTERNAL_CIDRS` 的自托管 E2B/Embed，可在 **provider 的 `orchestrator` 进程环境**中配置逗号分隔的 IP/CIDR 例外。规则顺序是 provider 内建放行、内建内网拦截，再到用户出站规则；仅设置普通 `network.allowOut` 不能覆盖内建拦截。该例外影响节点上的所有沙箱，应使用目标服务的 `/32` 地址，并保留已有例外。它只解除对应防火墙限制，不会配置 DNS 或创建路由。

部署前核对所用 E2B 版本是否支持该变量及其规则顺序，参考上游[配置定义](https://github.com/e2b-dev/runtime/blob/main/packages/orchestrator/pkg/sandbox/network/pool.go)、[默认拦截范围](https://github.com/e2b-dev/runtime/blob/main/packages/shared/pkg/sandbox-network/firewall.go)和[防火墙实现](https://github.com/e2b-dev/runtime/blob/main/packages/orchestrator/pkg/sandbox/network/firewall.go)。以下步骤用于自托管 provider；E2B Cloud 的底层网络由服务商管理，不能通过 Grasp 的 Compose 设置这个变量。

### 在哪里配置

先从 gateway 的 `e2b.apiURL` / `e2b.sandboxURL` 确认实际 provider 部署。**修改 E2B 本体的部署配置**：此变量不属于 gateway YAML、`SBGW_*`、Grasp 的 `compose.e2b.yaml` 或 guest 的环境变量。

Docker Compose 部署可在 **E2B 宿主机**用以下只读命令定位实际文件。容器名取自该机器的 `docker ps`，`e2b-orchestrator-1` 仅为示例：

```bash
e2b_orchestrator_container=e2b-orchestrator-1
docker inspect "$e2b_orchestrator_container" --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}'
docker inspect "$e2b_orchestrator_container" --format '{{index .Config.Labels "com.docker.compose.project.config_files"}}'
docker exec "$e2b_orchestrator_container" printenv ALLOW_SANDBOX_INTERNAL_CIDRS
```

例如标签显示 `/opt/e2b/compose.yaml,/opt/e2b/compose.override.yaml`，就在该部署的 `compose.override.yaml` 中合并 `orchestrator.environment`。下面两项是示例地址；实际编辑时保留已有白名单，再追加所需的 Git、模型或受控回连入口 IP。若使用内网 DNS，其服务器 IP 也需要相应放行。

```yaml
services:
  orchestrator:
    environment:
      ALLOW_SANDBOX_INTERNAL_CIDRS: "10.20.30.10/32,10.20.30.11/32"
```

Kubernetes 部署则在 **E2B provider 的 orchestrator 工作负载**中设置同名环境变量；由配置管理或 Helm 管理时修改其源配置。不要把变量加到 Grasp 或 sandbox-gateway 的 Deployment。

### 生效与验证

先安排 provider 维护窗口并按其运维流程保护正在运行的任务。更改 Compose 的环境变量后，需要重建 `orchestrator` 容器才能载入新值，单独执行 `docker compose restart` 不会载入新的容器配置。沿用原来的 Compose 文件、项目名与环境文件；上述目录和文件名的示例对应：

```bash
cd /opt/e2b
docker compose -f compose.yaml -f compose.override.yaml config --quiet
docker compose -f compose.yaml -f compose.override.yaml up -d --no-deps orchestrator
```

不要用删除卷或销毁整个部署来应用白名单。provider 重启和既有 guest 网络规则的更新行为取决于版本，不能把容器环境变量已更新当作所有沙箱立即生效。

1. 再次只读取 `ALLOW_SANDBOX_INTERNAL_CIDRS`，确认保留原有地址且新增项已进入运行中的 orchestrator。
2. 在 provider 宿主机核对目标 guest 的实际防火墙规则。使用 `slot-firewall` 的版本中，`filtered_always_allowlist` 应包含所需目标，且其放行规则位于 `filtered_always_denylist` 的拦截规则之前。核对既有 guest，也用新建 guest 验收；仅查看容器环境或空闲网络命名空间不足以证明目标 guest 已生效。
3. 分别从 provider 宿主机和 **guest 内部**检查 DNS 与应用响应，例如在替换示例域名后运行：

   ```bash
   getent ahostsv4 git.internal.example.test
   curl --connect-timeout 5 --max-time 15 -I https://git.internal.example.test/
   ```

   需要区分 DNS 与连通性时，可用 `curl --resolve git.internal.example.test:443:10.20.30.10` 配合同一 URL 保留域名及证书校验。以目标服务的 HTTP 响应或 TLS 握手为依据，不只看 `ping`、`nc` 或 TCP 端口是否打开；确认响应来自目标服务后，HTTP 401/403 应继续检查认证权限。若地址已放行仍失败，继续核对 guest 的 DNS、provider 宿主机路由及目标服务的防火墙。

## 准备环境模板

模板必须来自可审计的 Grasp **环境 OCI 镜像**，使用 `@sha256:` 固定内容。模板包含系统包、真实 Agent CLI、SSH、code-server、Chromium、原始 `grasp-bootstrap.sh` 和 `IMAGE_LEVEL`；Grasp 的 backend、页面脚本等运行时逻辑仍由服务端在创建时下发，更新逻辑无需重建环境模板。

构建命令中的 `--image` 是实际 OCI 镜像引用；`--name` 是 gateway 配置中的模板名称，会作为输出 `gatewayTemplates` 的键。`--alias` 用于 E2B 构建管理，实际 `templateID` 由 E2B 返回。这几个名字的用途不同，配置时按下文的对应关系填写。

在仓库根目录执行。API key 通过操作者环境提供，不写入配置样例、命令参数或结果文件。自托管环境按实际部署设置 `E2B_API_URL` 与 `E2B_SANDBOX_URL`；Cloud 可使用 SDK 的默认域名。

```bash
python3 -m venv /tmp/grasp-e2b-builder
/tmp/grasp-e2b-builder/bin/python -m pip install -r sandbox-gateway/deploy/e2b/requirements-build.txt

# 先设置 E2B_API_KEY，以及 SANDBOX_OCI_IMAGE=仓库/镜像@sha256:完整摘要。
# 自托管环境还需 E2B_API_URL、E2B_SANDBOX_URL；不要输出这些凭据。
/tmp/grasp-e2b-builder/bin/python sandbox-gateway/deploy/e2b/prepare-template.py \
  --image "${SANDBOX_OCI_IMAGE:?Set an immutable OCI image with sha256 digest}" \
  --alias grasp-production-v1 \
  --name standard \
  --cpu-count 2 \
  --memory-mb 3072 \
  --minimum-workspace-gib 20 \
  --output /tmp/grasp-e2b-template.json
```

使用新的版本化 alias，避免无意更新另一部署引用的 alias。工具直接使用固定 digest 镜像中已有的 bootstrap 和 `IMAGE_LEVEL`，校验其与当前 checkout 的摘要和级别一致；镜像不匹配时须选择对应镜像或重新构建环境镜像。模板准备不再上传或覆盖这两个文件，因此无需 provider 的构建文件上传接口。工具保留公开的 `PATH` 与 `PLAYWRIGHT_BROWSERS_PATH=/ms-playwright` 约定，并校验以下真实依赖：

- `cursor-agent`、`claude`、`codebuddy`、`traecli`、`opencode`、`codex`、`dsh`、`kimi`。
- Python、Node/npm、Go、Git、curl/tar、sshd、Docker、code-server、Xvfb、x11vnc、websockify、Chromium 和 `chrome-devtools-mcp`。

不复制 E2E wire fixture，不创建替代 vendor CLI，不调用真实模型，也不将缺失 CLI 视为成功。具备可执行文件不代表已经完成 Cursor、Trae、Kimi 等厂商账号授权；实际使用仍遵守原有 workspace 凭据配置。

构建完成后，工具用返回的**实际 templateID** 创建一个禁止公网入站、禁止出网、300 秒到期销毁的临时探针 guest，确认 templateID、CPU/RAM、bootstrap 摘要、IMAGE_LEVEL、依赖与实际可用工作区空间，然后删除这个 guest。成功 JSON 包含 `templateID`、`buildID`、测得的 `workspaceAvailableBytes` 和可复制到 gateway YAML 的 `gatewayTemplates`。失败不会输出 provider 原始错误体或凭据；清理失败会保留本次探针的 `probeSandboxID` 并返回失败，须由操作者检查这一个 guest。

`diskGi: 20` 表示创建时 `/root/workspace` 所在文件系统**至少有 20 GiB 可用空间**。它不是 PVC 大小、磁盘配额，也不是每次创建都能调整的资源。CPU、RAM 和磁盘空间均须由预构建模板满足；不匹配的请求会被拒绝。需要在构建时申请空间增长时，可显式传 `--min-free-disk-mb`，工具会原样传给 SDK 的 `min_free_disk_mb`，并在结果中记录 `requestedMinFreeDiskMB`；不传则继续使用 provider 的默认策略，传 `0` 表示不请求增长。该参数要求 provider 支持且满足团队限额，部分 Embed 部署还需要开启相应功能。空间增长是尽力而为，文件系统元数据可能消耗少量空间；可为申请值预留余量，但验收仍以 `--minimum-workspace-gib` 和 guest 实测为准。例如要求 50 GiB 可用空间时，可传 `--minimum-workspace-gib 50 --min-free-disk-mb 52224`（额外申请 1 GiB）。provider 忽略申请或实际空间不足时，探针仍失败、清理临时 guest，不输出可发布模板配置。gateway 每次新建 guest 还会重新检查可用容量。

只校验计划可加 `--dry-run`：它不导入 SDK、不联系 provider、不构建模板、不创建 guest。离线单测也不依赖 SDK：

```bash
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover \
  -s sandbox-gateway/deploy/e2b -p 'test_*.py' -v
```

### 桌面模板（可选）

桌面镜像（`sandbox-gateway/sandbox/Dockerfile.desktop`，发布为 `ghcr.io/cocofhu/universal-sandbox:<版本>-desktop`）可另建一份模板，与标准模板并存。用该镜像的 `@sha256:` 摘要构建，并加 `--desktop`：

```bash
/tmp/grasp-e2b-builder/bin/python sandbox-gateway/deploy/e2b/prepare-template.py \
  --image "${SANDBOX_DESKTOP_OCI_IMAGE:?Set the desktop image with sha256 digest}" \
  --alias grasp-desktop-v1 \
  --name desktop \
  --desktop \
  --cpu-count 4 \
  --memory-mb 8192 \
  --minimum-workspace-gib 30 \
  --output /tmp/grasp-e2b-desktop-template.json
```

`--desktop` 要求至少 4 CPU、8192 MiB 内存、30 GiB 可用工作区，并在探针中额外确认 `openbox`、`plank`、`fcitx5`、`xdotool`、`wmctrl`、`import`、`blender`、`blender-mcp`，标记文件 `/etc/grasp-desktop` 与非空应用目录 `/opt/grasp-desktop/apps.json`。结果的 `gatewayTemplates.desktop` 填入 gateway YAML（见 [gateway.example.yaml](gateway.example.yaml) 中注释的 `desktop` 条目）。

选用方式：只要桌面时把它设为唯一模板或 `defaultTemplate: desktop`；两份并存时，在 Grasp 的命名连接中写 `profile: desktop`，使用该连接的沙箱即为桌面镜像。桌面 MCP 与 Blender MCP 需在 Agent 的 MCP 页签「快捷添加」。

#### 用户验收清单（桌面模板）

本仓库不连接真实 E2B 测试，以下由部署方在自己的 E2B 上验收：

1. `prepare-template.py --desktop` 成功，结果 `desktop` 为 `true`，`gatewayTemplates.desktop` 规格与命令行一致。
2. 用 `profile: desktop` 的连接创建沙箱，沙箱控制台的「电脑」页签出现 Openbox 桌面、底部 Dock 与「我的电脑」启动台；中文输入可用（Ctrl+Space 切换拼音）。
3. 启动台打开 Mousepad，输入一行文字保存到 `/root/workspace/desktop-check.txt`；终端中能读到该文件。
4. 为 Agent 快捷添加「桌面控制」，让它截图并列出窗口；在控制台点「接管」后让它点击，返回「用户正在控制桌面」；点「交还」后再次点击成功。
5. 快捷添加「Blender」，打开 Blender 后让 Agent 通过 Blender MCP 新建一个立方体并读回场景对象列表。
6. 同一 gateway 的标准模板沙箱仍为浏览器模式（页签名为「浏览器」），浏览器 MCP 正常。

## 两份配置文件

只使用本目录的 E2B 示例，不复制 Docker 或 Kubernetes 的配置：

- [grasp.example.yaml](grasp.example.yaml) → `/absolute/private/grasp.yaml`：平台服务、网关连接与沙箱保留策略。
- [gateway.example.yaml](gateway.example.yaml) → `/absolute/private/gateway.e2b.yaml`：E2B 地址、owner、持久化目录和模板。

### 镜像如何变成模板

构建工具的 `--image` 是不可变 OCI 镜像输入；E2B 以它构建环境模板，并返回 `templateID`。`--alias grasp-production-v1` 是 E2B 构建管理名称；`--name standard` 是你在本 gateway 中给模板取的名字。`standard` 不会被拉取或构建成 Docker 镜像。

构建成功的结果包含：

```json
{
  "gatewayTemplates": {
    "standard": {
      "templateID": "实际构建返回的模板ID",
      "cpuCores": 2,
      "memoryMB": 3072,
      "diskGi": 20
    }
  }
}
```

将 `gatewayTemplates` 的内容填写到 gateway YAML 的 `e2b.templates`。实际 ID 与规格来自成功构建和探针验证；示例的 `2/3072/20` 不是 E2B 程序默认值。

```yaml
driver: e2b
e2b:
  apiURL: https://api.e2b.example.com
  sandboxURL: https://sandbox.e2b.example.com
  ownerID: my-grasp-deployment
  stateDir: /data/e2b
  templates:
    standard:
      templateID: replace-with-resolved-template-id
      cpuCores: 2
      memoryMB: 3072
      diskGi: 20
```

一份模板自动成为默认；有多份时填写 `defaultTemplate: standard`。模板名自动作为 gateway profile 发布，Grasp 的命名连接可以通过 `profile: standard` 指定它。

Grasp **不填写 `image`，内存覆盖保持 `sandbox_memory_mb: 0`**。这样 gateway 使用选中模板的 CPU、内存和初始可用工作区，无须在 Grasp 复制模板名与内存。普通创建 API 仍支持 `image`：E2B 下它表示上述模板名称。显式资源覆盖必须与选中模板完全一致，不匹配时创建前报错；选中另一模板时，省略值自动取那一份模板的规格。

### 自建地址与云服务地址

| 字段 | 自建 E2B / Embed | E2B Cloud |
| --- | --- | --- |
| `e2b.apiURL` | 你的 E2B 控制 API 来源，例如 `https://api.e2b.example.com` | 显式填 `https://api.e2b.app` |
| `e2b.sandboxURL` | 你的沙箱私有流量代理来源，例如 `https://sandbox.e2b.example.com` | 通常留空，使用 provider 返回的沙箱域名 |
| `e2b.apiKey` | 自建服务颁发的 API key | 云服务 API key |

`apiURL` 必填，没有隐式云服务目标。`sandboxURL` 留空但 provider 又不返回有效域名时会报错。两项 URL 均为 HTTP(S) 来源，不带业务路径、query 或账号密码。Grasp 网页的 `public_advertise` 和沙箱回连的 `mcp_advertise` 单独设置，不是 E2B 地址。

### Compose 启动

在 checkout 外保存权限受保护的环境文件，例如 `/absolute/private/grasp-e2b.env`：

```dotenv
GRASP_CONFIG=/absolute/private/grasp.yaml
E2B_GATEWAY_CONFIG=/absolute/private/gateway.e2b.yaml
E2B_API_KEY=replace-with-provider-key
E2B_GATEWAY_OWNER_ID=my-grasp-deployment
SANDBOX_GATEWAY_API_KEY=replace-with-shared-gateway-key
GRASP_AUTH_USERS=admin:replace-with-password-hash
GRASP_SECRETS_KEY=replace-with-stable-encryption-key
GRASP_PUBLIC_ADVERTISE=https://grasp.example.com
GRASP_MCP_ADVERTISE=https://grasp.example.com
```

在 gateway YAML 填好实际 `apiURL`、`sandboxURL` 和模板。Compose 可通过 `E2B_API_URL` / `E2B_SANDBOX_URL` 覆盖文件地址；不设时保留 YAML。上述两个 key 用途不同：`E2B_API_KEY` 用于 gateway 调用 provider，`SANDBOX_GATEWAY_API_KEY` 是 Grasp 和 gateway 共享的鉴权 key。密码散列与加密密钥的生成见 [Grasp 配置示例](../../../server/config.example.yaml)。不要提交部署文件或密钥。

Compose 固定使用 host network，网关监听 `127.0.0.1:8899`，Grasp 使用 `direct` transport 和 private endpoints。`GRASP_PORT` 默认 `8080`，`GRASP_GATEWAY_PORT` 默认 `8899`，`GRASP_DATA_DIR` 默认 `./data`。`/data/e2b` 与两个服务的数据库由命名卷持久化。沙箱模板只由网关的 `e2b.defaultTemplate` / `e2b.templates` 决定，Grasp 不再读取 `GRASP_SANDBOX_IMAGE`；`GRASP_SANDBOX_MEMORY_MB` 仅作为可选显式覆盖。

```bash
docker compose --env-file /absolute/private/grasp-e2b.env -f compose.e2b.yaml config --quiet
docker compose --env-file /absolute/private/grasp-e2b.env -f compose.e2b.yaml up -d --build
```

直接部署二进制时，使用 `-config gateway.e2b.yaml` 和 `CONFIG_PATH=grasp.yaml`。provider 环境变量改用 `SBGW_E2B_API_KEY`、`SBGW_E2B_API_URL`、`SBGW_E2B_SANDBOX_URL`、`SBGW_E2B_OWNER_ID`、`SBGW_E2B_STATE_DIR`；完整字段、默认值和优先级见 [配置总览](../../../CONFIGURATION.md#e2b-字段与默认值)。

首次部署应在目标 provider 上验证创建、暂停/恢复、SSH、文件、Agent/MCP 往返、IDE、预览及 CDP/noVNC。模板构建探针不替代这些运行验证。

## 状态与生命周期

`e2b-gateway-data` 保存 gateway DB 与 `/data/e2b` 私有恢复记录；`e2b-server-data` 保存 Grasp DB。恢复记录包含连接 private guest 所需的 envd、traffic 和 relay 凭据，目录权限是 `0700`、记录是 `0600`，须作为凭据备份与保护。不要公开挂载、提交到仓库、输出内容，或用 `docker compose down -v` 删除后期待恢复。gateway 重启会恢复同一批 loopback 端口；端口被其他进程占用时会报错，不会悄悄换端口。恢复卷、稳定 ownerID 和平台 DB 应一并保留。

gateway 恢复 guest、连接凭据和本地端口，Grasp 保留原沙箱记录。等待人工的 workflow 可以释放空闲环境，回复时先恢复最新成功状态再继续；不会因为空闲策略改变工作流的 `waiting_human` 状态。内存快照不等于网络连接快照，终端、模型请求和 Agent 控制连接仍需重建，不承诺正在进行的网络请求无缝继续。

Grasp 的空闲回收与手动「停止环境」使用同一条保存流程，E2B 不另做文件备份。`sandbox.idle_ttl_minutes`（默认 60）、`sandbox.max_sandboxes`（默认 15）和 `solo_backup.keep`（默认 1）继续作用于所有用途/后端，不增加 E2B 专用策略配置。详见 [生命周期说明](../../../docs/content/help/sandbox-lifecycle.md)。

### 标准 E2B 接口

- `keep: 1`：调用官方 `POST /sandboxes/{id}/pause`，请求 `memory: true`、`requireHealthyGuest: true`；恢复调用原 ID 的 `connect`。不创建额外独立快照。
- `keep > 1`：空闲保存用官方 `POST /sandboxes/{id}/snapshots` 创建具名内存快照，保存成功后删除源 guest 以释放计算资源；这份快照就是本次保存的确切状态，不再紧接着生成另一份暂停快照。保留最近 N 个独立快照，超出部分用 `DELETE /templates/{id}` 删除。磁盘上的实际对象数不等于 N。从独立快照恢复后若切回原生暂停，仍被该 guest 引用的基础模板保留为依赖，取消 guest 后继续清理，不作为可选择的历史恢复点。多份保留会增加保存时间和存储占用；provider 必须支持官方独立快照 API 及相应 envd 版本。
- 原生暂停恢复使用原 guest；独立快照保存完成后从该确切快照新建 provider guest。原 guest 必须明确不存在，且最新快照可用，才能执行后者；gateway 的业务 ID 保持不变，私密连接凭据和端口转发随新 provider ID 更新。不从更旧的快照或空白模板悄悄恢复。保存成功到恢复之间的外部网络连接仍需重建。
- 快照名称、不可变模板 ID、创建/恢复/删除意图保存在 gateway 私有 journal。请求超时后先查询并认领结果，不能无条件再创建。无法确认的操作保留重试记录；尤其快照已发送但尚无可查询别名时，取消不会伪报全部清理完成。
- 取消运行删除所属 guest 和 gateway 创建的独立快照，接口失败继续重试。完成表示标准 API 已接受删除且 guest 查询不存在，**不表示宿主机磁盘立刻腾空**；共享层与物理 GC 由 provider 负责。Grasp 不再要求 `snapshotKeep` 或 `snapshot-status` 私有扩展，也不读取物理回收字节数。

官方暂停的可恢复性和持久化时机由 provider 合同负责；Grasp 不额外承诺对象存储上传或底层进程退出凭证。上传故障、断电保护与物理 GC 应在部署层监控。自建增强版可以在标准接口内部维护这些保障，但不能成为通用客户端的必要条件。

### 存量兼容和部署顺序

已有原生暂停 guest 继续按原 ID 恢复；升级不会批量转换、暂停或删除它们。旧 provider 私有账本中的历史恢复点仍由该部署的回收机制管理，不伪装成 gateway 新建的官方独立快照。私有 journal 读取 v1，后续写入 v2；旧 gateway 不理解 v2，回退需保留全部 journal 并使用支持该版本的程序，不能删除新字段规避校验。

标准 E2B 用户无需私有迁移或定制 Dashboard。自建增强部署应先应用配套 API/migrator，让标准暂停和模板删除进入已有安全 GC，再更新 gateway 和 Grasp；该 API 升级不要求重启已具备存储维护能力的 orchestrator。官方 Dashboard 使用同一官方发布清单中的配套 API/节点版本，不能只升级页面镜像。更换 provider 节点仍须单独安排维护窗口，不得为了更新中断业务沙箱。

新建 guest 仍默认 `autoPause=true`，provider 租约是 gateway 不可用时的兜底，不是 Grasp 的空闲计时。运行任务期间 gateway 持续续租，自建部署须另行配置足够的最长运行时长或滚动租约策略。provider 自行暂停的历史 guest 直接走原生恢复，不重复调用暂停制造新恢复点。恢复保留原有端口，现存 TCP 连接会断开后重建。

pause/resume 仍需在目标 provider 版本上单独验收。gateway 对恢复阶段的 provider 5xx、临时控制面传输失败和 private relay 发布竞态，会在同一个 provider guest 上做有界重试；启动恢复与显式 start 共用这条路径。仍必须在目标部署检查应用、SSH、IDE、CDP 和 Docker，而不能只看 “provider running”。失败应保留安全状态证据并处理本部署拥有的 guest，不自动降级成新建空白 guest。

不支持 `preserveData` 的 reinstall；需要保留数据的重装必须先有明确的快照/迁移流程。普通无保留 reinstall 会销毁后重建。此模式也不接受宿主路径挂载，应使用原有 bundle 注入机制；不要把此限制包装成与 PVC 相同的持久化保证。
