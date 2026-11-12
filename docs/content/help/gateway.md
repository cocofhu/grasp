---
title: 网关
description: sandbox-gateway 契约摘要；完整说明见 GATEWAY.md。
---

Grasp 通过 vendored 的 **sandbox-gateway** 控制平面调度通用沙箱镜像。Web UI 走 Grasp API；Agent 节点经 gateway 在容器中执行。

## 直连与平台代理

默认保持旧版直连。配置 `sandbox.gateway_transport: gateway` 后，Docker、Kubernetes、E2B 的 SSH、聊天、IDE 和浏览器连接均可经过带鉴权的网关，Grasp 不必与沙箱处于同一网络。此模式要求网关 API key；浏览器继续使用 Grasp 入口，预览走 `/preview/...`，无需独立预览域名。沙箱仍须能访问 `server.mcp_advertise`，用于 runtime/config 下载与 MCP 等回连。命名网关的默认值仅影响新沙箱，已有沙箱保留原绑定。

## Docker 与 Kubernetes 部署

- [Docker 完整指南](https://github.com/cocofhu/grasp/blob/main/sandbox-gateway/deploy/docker/README.md)：生产 Compose 覆盖示例、账号与密钥、公开入口/沙箱回连、内网排障、备份和升级。`.env` 不会自动转发所有变量，新增配置须显式写入服务环境或挂载配置文件。
- [Kubernetes 完整指南与清单](https://github.com/cocofhu/grasp/blob/main/sandbox-gateway/deploy/k8s/README.md)：独立命名空间、完整 RBAC、Secret、PVC、Deployment、Service 与 Ingress。采用 gateway transport 时可使用 ClusterIP，无需每个沙箱一个 LoadBalancer。旧 `pods/log` 文件只是增量补丁。

Docker 内网访问检查容器 DNS、宿主转发和防火墙；Kubernetes 检查 Pod DNS、CNI/NetworkPolicy 与目标 ACL。两者不使用 E2B 的内网白名单变量，连通性须从沙箱内验证。

- **可直连（已鉴权）**：`session`（会话密码）、`ide`（IDE 密码）、`ssh`（`ROOT_PASSWORD` 或 `SSH_KEY`）。
- **不可直连**：CDP `:9222`、noVNC `:6080` 无应用层鉴权，不发布到宿主/LB，详情页不展示/不复制直连地址。
- **用户入口**：仅平台代理 `/sandbox-vnc/:sandboxId/ws`，每个沙箱一个桌面，桌面上就是 Agent 操作的浏览器（启用平台 Auth 时须 Session；仅校验登录有效，不校验沙箱/跑步归属）。「打开预览」进入沙箱控台 noVNC，不直连 websockify。
- **存量窗口**：Docker 已运行容器的 `-p` 须 TTL/Reinstall 才收敛；K8s 存量 `*-lb` 在网关启动调和 / Start / Reinstall 完成前仍可能对外暴露 `:9222` / `:6080`。

## Kubernetes 停止确认

Kubernetes 的停止请求会等待控制器确认缩容和实际 Pod 退出，保留 PVC 后冷启动服务。等待期间不报告已经停止；观察失败或被后续启动请求取代时返回错误，可核对状态后重试。该方式不保存进程内存。

## E2B 访问内网

E2B 沙箱默认限制内网出站；gateway 或宿主机能访问某个内网服务，不代表沙箱也能访问。支持该能力的自托管 E2B/Embed 需要在 **provider 的 orchestrator** 上配置 `ALLOW_SANDBOX_INTERNAL_CIDRS`，保留已有地址，再追加所需目标。该例外影响节点上的所有沙箱，不属于 Grasp 或 gateway 的配置。

配置文件定位、Compose 示例、生效步骤与沙箱内验证见 [E2B 部署文档：内网访问](https://github.com/cocofhu/grasp/blob/main/sandbox-gateway/deploy/e2b/README.md#内网访问)。

## 完整文档

- [GATEWAY.md](https://github.com/cocofhu/grasp/blob/main/GATEWAY.md)

## 健康检查（默认本地栈）

- Gateway：http://localhost:8899/healthz
- Grasp API：http://localhost:8080/api/health

## 源码位置

- 控制平面：`sandbox-gateway/gateway/`
- 沙箱镜像与脚本：`sandbox-gateway/sandbox/`、`sandbox-gateway/scripts/`

## 相关

- [配置](../configuration/)
- [核心概念](../../guide/concepts/)
