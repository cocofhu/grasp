# Kubernetes 部署

本目录提供 **Grasp + Kubernetes 驱动 gateway + 独立沙箱命名空间**的新安装示例。单个 gateway 只运行一种驱动。已有 Docker/E2B 部署接入第二个网关时，使用 [GATEWAY.md 的命名连接](../../../GATEWAY.md#gateway-transport-and-multiple-deployments)，不要直接覆盖已有沙箱绑定的网关地址。

`role-pods-log.yaml` / `rolebinding-pods-log.yaml` 是给已有安装补充读取日志权限的增量补丁，**不是完整部署清单**。新安装使用下列文件，复制到仓库外再按环境修改：

| 文件 | 用途 |
|---|---|
| [rbac.example.yaml](rbac.example.yaml) | 两个命名空间、ServiceAccount、完整且限定作用域的 RBAC |
| [stack.example.yaml](stack.example.yaml) | 两个服务的 ConfigMap、PVC、Deployment 和 ClusterIP Service；密钥引用单独创建的 Secret |
| [ingress.example.yaml](ingress.example.yaml) | 可选的浏览器 HTTPS 入口；需已有 Ingress controller 与证书 |

这是单副本 SQLite 部署基线，使用 `Recreate` 避免滚动期间两个进程同时写同一个库；它不提供高可用。不能只提高 replicas 或把 SQLite 放到共享 NFS 就宣称支持多副本。资源与容量是示例，应根据并发、构建任务和存储性能调整。

## 1. 集群与存储前提

- 管理机具有相应管理权限的 `kubectl`、Git、OpenSSL；生成账号哈希的机器具备项目所需的 Go。
- 发布镜像/runtime bundle 使用 Linux amd64。示例为控制面 Pod 指定此架构；gateway 当前创建的沙箱 Pod 不携带 nodeSelector，因此沙箱的可调度节点也须使用匹配架构，混合集群需另行配置调度准入策略。
- 集群 DNS、CNI 和动态存储可用。两个控制面 PVC 和每个沙箱 PVC 都需要支持 `ReadWriteOnce` 的 StorageClass。将两个 PVC 的 `spec.storageClassName` 及 gateway 的 `kubernetes.storageClass` 占位值替换为集群实际名称。当前 gateway 配置加载器会把空值替换成代码中的预设存储类，**留空不表示使用集群默认类**。
- 沙箱执行 privileged 容器，支持 DinD。`grasp-sandboxes` 必须允许 privileged，节点/准入策略也必须允许；不能用它承诺互不信任租户的强隔离。控制面在另一个 `grasp-system` 命名空间。
- gateway 使用自己的 ServiceAccount 访问 Kubernetes API。Grasp 和沙箱默认 ServiceAccount 不自动挂载 API token，也不授予管理权限。PSA 与 RBAC 机制见 [Kubernetes 命名空间安全标签](https://kubernetes.io/docs/tasks/configure-pod-container/enforce-standards-namespace-labels/)和 [RBAC 文档](https://kubernetes.io/docs/reference/access-authn-authz/rbac/)。

先检查集群实际资源：

```bash
kubectl get nodes -L kubernetes.io/arch
kubectl get storageclass
kubectl get ingressclass
```

不要照搬旧配置示例中某一环境的存储类或 PVC 删除注解。确认 CSI 的回收策略、快照/恢复能力和 DinD 对文件系统的要求。使用私有镜像仓库时，在 `grasp-system` 为两个控制面 Pod 配置 `imagePullSecrets`；在 `grasp-sandboxes` 单独创建拉取 Secret，并填写 gateway 的 `kubernetes.imagePullSecret`。两个命名空间的 Secret 不能相互引用。

## 2. 准备配置、权限和密钥

以下命令从仓库根执行。复制后替换域名、Ingress class、镜像版本/digest、存储类和容量；若改命名空间，还要同步 RBAC subject/resourceNames、Service DNS、Secret 和所有清单中的 namespace。

```bash
grasp_k8s_dir="$HOME/.config/grasp-k8s"
install -d -m 700 "$grasp_k8s_dir"
cp sandbox-gateway/deploy/k8s/*.example.yaml "$grasp_k8s_dir/"
```

`stack.example.yaml` 的三个关键地址/选项：

| 配置 | 本示例值与含义 |
|---|---|
| Grasp `server.public_advertise` | `https://grasp.example.com`，改成浏览器实际访问的入口 |
| Grasp `server.mcp_advertise` | `http://grasp.grasp-system.svc.cluster.local:8080`，沙箱 Pod 回连 Grasp；自定义 cluster domain 时修改后缀 |
| Grasp `sandbox.gateway_url` | `http://sandbox-gateway.grasp-system.svc.cluster.local:8080`，控制面调用入口 |
| Grasp `sandbox.gateway_transport` | `gateway`，由 gateway 转发沙箱 TCP，不要求用户或集群外 Grasp 直连沙箱 IP |
| gateway `kubernetes.enableLoadBalancer` | `false`，沙箱只需要 ClusterIP；此拓扑不依赖 MetalLB，也不为每个沙箱申请外部 IP |

gateway 配置通过 `SBGW_CONFIG` 加载，Grasp 配置通过 `CONFIG_PATH` 加载，不能交换变量名。公共配置放 ConfigMap；账号哈希、gateway key 和加密密钥只通过 Secret 注入。两端引用同一个 `gateway-api-key`，避免 key 不一致。

由集群管理员先创建命名空间和权限：

```bash
kubectl apply -f "$grasp_k8s_dir/rbac.example.yaml"
kubectl auth can-i create deployments.apps -n grasp-sandboxes \
  --as=system:serviceaccount:grasp-system:sandbox-gateway
kubectl auth can-i get pods --subresource=log -n grasp-sandboxes \
  --as=system:serviceaccount:grasp-system:sandbox-gateway
kubectl auth can-i get namespaces/grasp-sandboxes \
  --as=system:serviceaccount:grasp-system:sandbox-gateway
```

后三项应返回 `yes`（调用者需要 impersonate 权限）。RBAC 已包含 Deployment/Service/Secret/PVC 的创建、更新或删除，以及 Pod 状态与日志读取。ClusterRole 仅允许读取/修正指定沙箱命名空间；管理员已预建该命名空间，所以不授予创建任意命名空间的权限。不要给 gateway `cluster-admin`。已有安装只需日志权限时，改好两个旧增量文件的命名空间与 SA subject 后另行 apply，不要用单条日志 Role 覆盖原完整 Role。

首次安装在仓库外生成密钥；以下子 shell 禁止覆盖已有文件。迁移时从备份恢复原加密主密钥，不能重新生成。

```bash
(
  set -eC
  umask 077
  openssl rand -hex 32 > "$grasp_k8s_dir/gateway-api-key"
  openssl rand -base64 32 > "$grasp_k8s_dir/secrets-key"
)
./server/scripts/gen-auth-hash.sh --username admin
```

将生成的 bcrypt 哈希写到 `$grasp_k8s_dir/auth-users.json`，内容为 JSON 数组，替换占位值：

```json
[{"username":"admin","password_hash":"REPLACE_WITH_BCRYPT_HASH","is_admin":true}]
```

创建 Secret（文件末尾的换行在导入前去掉，避免 API key 带换行）：

```bash
chmod 600 "$grasp_k8s_dir/auth-users.json"
for key_file in gateway-api-key secrets-key; do
  tr -d '\r\n' < "$grasp_k8s_dir/$key_file" > "$grasp_k8s_dir/$key_file.tmp"
  chmod 600 "$grasp_k8s_dir/$key_file.tmp"
  mv "$grasp_k8s_dir/$key_file.tmp" "$grasp_k8s_dir/$key_file"
done
kubectl -n grasp-system create secret generic grasp-secrets \
  --from-file=gateway-api-key="$grasp_k8s_dir/gateway-api-key" \
  --from-file=secrets-key="$grasp_k8s_dir/secrets-key" \
  --from-file=auth-users="$grasp_k8s_dir/auth-users.json"
```

该命令用于首次创建，已存在会失败而不覆盖。后续凭据更新按本地 Secret 管理流程执行，保持 `secrets-key` 稳定，并重启相关 Deployment 以加载环境变量。Secret 的 base64 表示不等于加密，应使用集群既有的 Secret 访问控制与备份机制。

## 3. 启动与配置浏览器入口

```bash
kubectl apply --dry-run=server -f "$grasp_k8s_dir/stack.example.yaml"
kubectl apply -f "$grasp_k8s_dir/stack.example.yaml"
kubectl -n grasp-system rollout status deployment/sandbox-gateway --timeout=5m
kubectl -n grasp-system rollout status deployment/grasp --timeout=5m
kubectl -n grasp-system get pods,pvc,services
```

两个控制面 PVC 应为 `Bound`。尚无沙箱 PVC 是正常的，首次创建沙箱时才申请。镜像首次下载较慢时先看 Pod Events，不要因等待超时反复删除卷。

浏览器入口由现有 Ingress controller 提供。准备 `grasp-system/grasp-tls` TLS Secret，将 DNS 指向入口地址，修改 Ingress 的 host/class；它应与 `server.public_advertise` 一致，再执行：

```bash
kubectl apply --dry-run=server -f "$grasp_k8s_dir/ingress.example.yaml"
kubectl apply -f "$grasp_k8s_dir/ingress.example.yaml"
curl --fail --silent https://grasp.example.com/api/health
```

按所用 controller 配置 WebSocket Upgrade、长连接超时、附件大小，并保留含端口的 Host；不要照抄其他 controller 的 annotations。Ingress 只暴露 Grasp，gateway、沙箱 CDP/noVNC 和动态端口保持内部访问。预览走 Grasp `/preview/...`，无需额外预览域名。

临时排障可执行 `kubectl -n grasp-system port-forward service/grasp 8080:8080`，访问 `http://localhost:8080/api/health`。端口转发只用于检查；正式登录/预览仍使用配置的公开来源，沙箱回连继续使用集群 Service DNS。

## 4. 网络、内网访问与验收

| 流量方向 | 必须允许的连接 |
|---|---|
| 入口 controller → Grasp | TCP 8080，HTTP 与 WebSocket |
| Grasp → gateway | TCP 8080，控制 API 与 v2 WebSocket |
| gateway → Kubernetes API | 集群 API 的实际地址/端口（通常 443）和 ServiceAccount 认证 |
| gateway → 沙箱 | Pod/Service 上的 SSH 22、session 8765、IDE 8744、CDP 9222、noVNC 6080 及注册应用端口 |
| 沙箱 → Grasp | `mcp_advertise` 对应的 8080；下载 runtime/config、MCP 及代理回连 |
| 各 Pod → DNS/依赖 | 集群 DNS 的 UDP/TCP 53；所需 Git、模型、代理和制品服务 |

若集群存在默认拒绝的 NetworkPolicy，按这张表在源和目标命名空间补齐入站/出站规则，并确认 CNI 真正支持所用策略。节点容器运行时还需拉取镜像仓库。示例没有附加一套会覆盖集群现有边界的通用放行策略。

Kubernetes 访问内网取决于 **Pod DNS、CNI 路由/SNAT、NetworkPolicy、节点防火墙及目标 ACL**，不使用 E2B 的 `ALLOW_SANDBOX_INTERNAL_CIDRS`。节点能访问某服务不代表 Pod 能访问；`svc.cluster.local` 也不能直接供集群外 Grasp/沙箱使用。若 Grasp 在集群外，需分别提供它可达的鉴权 gateway 入口，以及沙箱可达的 Grasp 回连入口；gateway transport 不替代回连路由。

登录 Grasp 后配置工作区模型和 Git 凭据，通过 UI 创建测试会话。当前镜像只有环境和 bootstrap，需要 Grasp 注入 `GRASP_RUNTIME_URL` 与下载鉴权；不能仅凭手工 `AGENT_PROVIDER` create 请求验收。检查沙箱 PVC `Bound`、Pod `Running`，然后实测终端、文件、Agent/MCP 往返、IDE WebSocket、注册应用预览与 noVNC。

```bash
kubectl -n grasp-sandboxes get deployments,pods,pvc,services
kubectl -n grasp-system logs deployment/sandbox-gateway --tail=100
```

再进入新建沙箱的 Grasp 终端执行，替换示例目标：

```bash
getent hosts git.internal.example.com
curl --connect-timeout 5 --max-time 15 -I https://git.internal.example.com
```

| 现象 | 优先检查 |
|---|---|
| `forbidden` 创建资源/读取日志 | 报错资源、命名空间、gateway SA 与 RoleBinding subject；日志补丁不代替完整 RBAC |
| PVC `Pending` | StorageClass/default class、CSI Events、容量/配额、节点拓扑、RWO 挂载冲突 |
| Pod 被准入拒绝 | 沙箱命名空间 PSA 标签、额外准入策略是否允许 privileged |
| `ImagePullBackOff` / `exec format error` | 仓库连通性、同命名空间拉取 Secret、镜像与节点架构 |
| bootstrap 超时/退出 | 沙箱到 Grasp Service 的 DNS/路由/策略，运行时下载日志与鉴权；不输出整个 Secret 或 Pod 环境 |
| `LoadBalancer` 地址一直 Pending | 本示例应为 `enableLoadBalancer: false`；只有旧直连拓扑才需要配置 LB controller/地址池并选择 true |
| 停止一直等待 | Deployment 缩容是否被观察到、Pod 是否退出、终止挂载/节点状态 |
| UI 通但终端/预览失败 | Ingress WebSocket/Host/超时、gateway 到沙箱端口和应用端口注册 |

## 5. 数据生命周期、升级和恢复

| 数据 | 本示例持久化位置 |
|---|---|
| Grasp SQLite、附件、Agent/工作区配置 | `grasp-system/grasp-data` PVC 的 `db` 与 `app-data` 子目录 |
| gateway 清单与连接元数据 | `grasp-system/sandbox-gateway-data` PVC |
| 加密主密钥、账号哈希、gateway key | `grasp-system/grasp-secrets`；独立保存受保护的恢复副本 |
| 每个沙箱的 workspace、工具缓存、DinD/BuildKit 和 `/tmp` | `grasp-sandboxes` 中 gateway 创建的沙箱 PVC；不包含整个容器文件系统 |

gateway **Stop** 会缩容至 0 并等待 Pod 退出，保留 PVC；Start 冷启动，不保存内存。**Destroy 会删除该沙箱的 Deployment、Service、Secret 和 PVC**；底层 PV 是否保留由 StorageClass 回收策略决定。Reinstall 只有明确选择保留数据时才保留 PVC。Grasp 停止/TTL 回收可能调用 Destroy，不能将 UI 停止等同于 gateway Stop。删除命名空间会连同 PVC 一起删除，不能用作日常重启或备份步骤。

升级前等待活动任务结束，停止新任务，在维护窗口暂停写入，对 Grasp/gateway 数据和需要保留的沙箱卷做一致备份，并保存 ConfigMap、原 Secret、镜像 digest 和命名空间绑定。先在独立环境演练恢复，确认附件引用及加密凭据可读。不要只备份数据库却丢掉 blobs 或 `GRASP_SECRETS_KEY`。

更新镜像或配置后 apply 本地清单。Secret/ConfigMap 变化不能保证进程重载，需对受影响的 Deployment 执行 `kubectl rollout restart` 并等待就绪；本示例是单副本，期间会有维护中断。启用 v2 transport 前先升级 gateway；更换依赖新 runtime 的沙箱环境镜像前先升级 Grasp 服务端。已有沙箱的环境镜像不会因修改默认值自动更新，见[镜像与 runtime 契约](../../sandbox/README.md)。

恢复时使用兼容版本和同一备份集，还原原加密密钥与 gateway 清单，再核对实际沙箱资源。保持 `orphanGC.enabled: false`，直到确认 DB 与资源一致，避免把未恢复元数据的沙箱误作孤儿删除。数据库升级后的回滚应恢复兼容备份，不能仅回滚 Deployment 镜像。完成真实会话验收后再恢复业务。
