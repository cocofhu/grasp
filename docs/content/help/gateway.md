---
title: 网关
description: sandbox-gateway 契约摘要；完整说明见 GATEWAY.md。
---

Grasp 通过 vendored 的 **sandbox-gateway** 控制平面调度通用沙箱镜像。Web UI 走 Grasp API；Agent 节点经 gateway 在容器中执行。

## 直连与平台代理

默认保持旧版直连。配置 `sandbox.gateway_transport: gateway` 后，Docker、Kubernetes、E2B 的 SSH、聊天、IDE 和浏览器连接均可经过带鉴权的网关，Grasp 不必与沙箱处于同一网络。浏览器继续使用原有 Grasp 入口。此模式要求网关 API key 和独立预览域名；命名网关的默认值仅影响新沙箱，已有沙箱保留原绑定。配置和迁移步骤见下方完整文档。

- **可直连（已鉴权）**：`session`（会话密码）、`ide`（IDE 密码）、`ssh`（`ROOT_PASSWORD` 或 `SSH_KEY`）。
- **不可直连**：CDP `:9222`、noVNC `:6080` 无应用层鉴权，不发布到宿主/LB，详情页不展示/不复制直连地址。
- **用户入口**：仅平台代理 `/sandbox-vnc/:sandboxId/ws`，每个沙箱一个桌面，桌面上就是 Agent 操作的浏览器（启用平台 Auth 时须 Session；仅校验登录有效，不校验沙箱/跑步归属）。「打开预览」进入沙箱控台 noVNC，不直连 websockify。
- **存量窗口**：Docker 已运行容器的 `-p` 须 TTL/Reinstall 才收敛；K8s 存量 `*-lb` 在网关启动调和 / Start / Reinstall 完成前仍可能对外暴露 `:9222` / `:6080`。

## 环境与文件保留

工作流的调试保留期到期后只停止计算，保留环境身份和工作区。启动调和或网关扫描发现 Solo / 工作流环境缺失记录、停止或暂时不可用时，不以此自动删除唯一文件副本；未归属资源由管理员恢复控制面记录或核对后处理。物理释放是独立的删除操作。

## 完整文档

- [GATEWAY.md](https://github.com/cocofhu/approving/blob/main/GATEWAY.md)

## 健康检查（默认本地栈）

- Gateway：http://localhost:8899/healthz
- Grasp API：http://localhost:8080/api/health

## 源码位置

- 控制平面：`sandbox-gateway/gateway/`
- 沙箱镜像与脚本：`sandbox-gateway/sandbox/`、`sandbox-gateway/scripts/`

## 相关

- [配置](../configuration/)
- [核心概念](../../guide/concepts/)
