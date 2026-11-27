---
title: 连接沙箱网关
navTitle: 网关与预览
description: 核对网关连接、后端选择与预览策略，连接本地 IDE 或排查启动失败。
section: deploy
order: 30
keywords: [Gateway, 网关, preview, 预览, direct, subdomain, private_endpoints, E2B, healthz]
---

## 连接 Grasp 与 Gateway {#connect}

1. 用对应示例配置 Docker、Kubernetes 或 E2B 网关。网关负责镜像、模板和资源规格。
2. 在 Grasp 配置 `sandbox.gateway_url`，并提供与网关匹配的 API Key。访问 `/healthz` 确认网关服务已就绪。
3. 选择 `sandbox.gateway_transport`。默认 `direct` 需要客户端和服务端访问对应端点。`gateway` 通过鉴权后的网关传输，适合端点只在服务端可达的部署。命名连接允许为不同工作流或会话选用不同的环境。

完整连接、资源和后端字段见[部署参数参考](https://github.com/cocofhu/grasp/blob/main/CONFIGURATION.md)和[网关契约](https://github.com/cocofhu/grasp/blob/main/GATEWAY.md)。

## 配置应用预览 {#preview}

| 策略 | 效果 |
| --- | --- |
| `auto`，默认 | 公开可用的直连环境采用直连。私有端点或鉴权网关在配置子域名基址时用子域名，否则隐藏这类预览入口。 |
| `subdomain` | 使用 `preview.expose.subdomain.base_url` 和通配 DNS 为每个预览提供独立子域名。 |
| `none` | 关闭此预览暴露方式。 |
| `direct` | 在使用直连传输的命名连接里单独配置。 |

在 production 使用子域名模式时，配置 HTTPS 及与平台不同的根域。将预览链接按访问凭证处理。CDP 与 noVNC 使用平台代理，保持原始端口在服务侧。要从桌面 IDE 访问沙箱，参阅[本地 IDE](/help/local-ide/)。

## 核对网络可达性 {#network}

分别从 Grasp 和从沙箱验证访问。Docker 检查容器 DNS、宿主转发和防火墙。Kubernetes 检查 Pod DNS、网络策略和目标 ACL。E2B 对内网访问有独立策略，按[部署文档](https://github.com/cocofhu/grasp/blob/main/sandbox-gateway/deploy/e2b/README.md)配置必要的目标范围。

## 检查创建和恢复 {#diagnose}

先看运行详情或 Solo 的沙箱状态，并对照 Gateway 日志。缺失模板、资源不匹配、仓库授权失败和等待容量都可能让环境无法就绪。用新建测试沙箱核对当前配置。空闲回收与恢复行为见[沙箱生命周期](/help/sandbox-lifecycle/)，常见失败和下一步见[排查](/help/troubleshooting/)。
