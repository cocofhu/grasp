---
title: 网关
description: sandbox-gateway 契约摘要；完整说明见 GATEWAY.md。
---

Grasp 通过 vendored 的 **sandbox-gateway** 控制平面调度通用沙箱镜像。Web UI 走 Grasp API；Agent 节点经 gateway 在容器中执行。

## 直连与平台代理

- **可直连（已鉴权）**：`session`（会话密码）、`ide`（IDE 密码）、`ssh`（`ROOT_PASSWORD` 或 `SSH_KEY`）。
- **不可直连**：CDP `:9222`、noVNC `:6080` 无应用层鉴权，不发布到宿主/LB，详情页不展示/不复制直连地址。
- **用户入口**：仅平台代理 `/sandbox-vnc/:sandboxId/ws` 与 `/preview-vnc/:runId/:nodeId/:port/ws`（启用平台 Auth 时须 Session；仅校验登录有效，不校验沙箱/跑步归属）。「打开预览」进入沙箱控台 noVNC，不直连 websockify。
- **存量窗口**：Docker 已运行容器的 `-p` 须 TTL/Reinstall 才收敛；K8s 存量 `*-lb` 在网关启动调和 / Start / Reinstall 完成前仍可能对外暴露 `:9222` / `:6080`。

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
