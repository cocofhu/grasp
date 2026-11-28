---
title: 部署与配置 Grasp
navTitle: 部署配置
description: 选择 Docker、Kubernetes 或 E2B 后端，掌握配置入口、覆盖顺序和生效时机。
section: deploy
order: 10
keywords: [配置, YAML, env, .env, CONFIG_PATH, Compose, SBGW_CONFIG, 生产部署, Docker, Kubernetes, E2B]
---

## 选择配置入口 {#where-to-configure}

| 想改什么 | 去哪里改 | 如何生效 |
| --- | --- | --- |
| API 账号和工具平台授权 | 「设置 → 凭据」 | Agent 运行解析已绑定的凭据。更新后按提示重开受影响的会话。 |
| 模型、编码工具和推理 | 「设置 → 凭据 → 模型组」，然后在智能体里绑定 | 会话内选择从下一轮生效。自动运行采用默认组。 |
| Git 仓库、克隆授权、提交身份 | 「设置 → 仓库」 | 下次启动或恢复沙箱时解析。 |
| 沙箱容量、空闲、内存和 Agent 无动作时限 | 「设置 → 常规」 | 保存后由运行时应用，具体时机见[常规设置](/help/settings/)。 |
| 端口、数据库、用户、网关连接 | Grasp YAML 或服务进程环境变量 | 改完部署配置后重启相应服务。 |
| 镜像、模板、CPU、内存、磁盘和网络 | 对应后端的 Gateway YAML | 重启网关并用新沙箱验证。 |
| 节点目标、总时限和人工门禁 | 工作流画布 | 保存并发布，适用于后续启动的运行。 |

Grasp 通过 `CONFIG_PATH` 指定 YAML，默认路径为 `config.yaml`。Gateway 使用 `-config` 或 `SBGW_CONFIG`。部署时准备一份平台配置和一份与后端匹配的网关配置。

## 选择沙箱后端 {#backends}

| 后端 | 适用场景 | 网关配置 |
| --- | --- | --- |
| Docker | 单机演示或自托管服务 | Docker 镜像、资源限制和网络。未指定资源时共享宿主机 CPU 与内存。 |
| Kubernetes | 通过现有集群管理沙箱 | 集群访问、命名空间、调度、存储类和资源。默认每沙箱 2 CPU、4096 MiB、25 GiB。 |
| E2B | 使用模板与快照的环境 | API 地址和已构建的模板 ID，规格由实际模板决定。 |

按你的后端使用最新版部署示例。[Docker](https://github.com/cocofhu/grasp/blob/main/sandbox-gateway/deploy/docker/README.md) · [Kubernetes](https://github.com/cocofhu/grasp/blob/main/sandbox-gateway/deploy/k8s/README.md) · [E2B](https://github.com/cocofhu/grasp/blob/main/sandbox-gateway/deploy/e2b/README.md)

镜像和模板在 Gateway 配置。发布 Compose 的 `SANDBOX_IMAGE` 会映射到 Docker 网关镜像配置。Grasp 自身的 `sandbox.image` 和 `GRASP_SANDBOX_IMAGE` 已废弃。Docker 和 Kubernetes 的沙箱会保留到手动停止、关闭或取消。E2B 空闲回收默认 360 分钟，见[沙箱生命周期](/help/sandbox-lifecycle/)。

## 判断哪个值生效 {#precedence}

- 服务端普通项按**进程环境变量 → YAML → 内置默认值**取值。
- 支持在「常规」中修改的字段按**进程环境变量 → 界面保存值 → YAML → 默认值**取值。环境变量可锁定对应的界面字段。
- Compose 根目录的 `.env` 提供编排变量。服务通过 `environment` 或配置挂载获得对应值。新增键时核对实际注入位置。
- Grasp 的内存覆盖默认 `0`，沿用网关或模板。旧安装曾在界面保存覆盖时，将其改回 `0` 后新沙箱会跟随网关。

## 区分四类时限 {#time-limits}

| 参数 | 起算点与效果 |
| --- | --- |
| `sandbox.idle_ttl_minutes`，默认 360 分钟 | E2B 的模型回合结束且队列为空后，计时到期触发保存和回收。 |
| `sandbox.chat_idle_timeout_seconds`，默认 1200 秒 | 对话中持续没有输出和活动时，用于检测本轮是否卡住。 |
| 画布节点总时限，空值回退 `sandbox.agent_node_hard_cap_hours`，默认 24 小时 | 约束一个 Agent 节点的总执行时间。 |
| `sandbox.sandbox_create_timeout_seconds`，默认 1200 秒 | 等待网关报告沙箱就绪，包含冷启动准备。与网关的 finalize 时限配合。 |

## 凭据、入口和备份 {#secrets}

生产部署时设置明确的用户和 `GRASP_SECRETS_KEY`。密钥用于解密已保存的凭据，与部署配置和数据库成套备份。配置平台代理与沙箱回连地址时，确认浏览器与沙箱分别可以访问对应端点。

如需 GitHub 设备登录，设置 `GRASP_GITHUB_OAUTH_CLIENT_ID` 并重启。模型凭据与仓库 Token 通过应用的凭据管理添加。启用本地 IDE 连接时，参阅[SSH 网关](/help/local-ide/)的配置和链路。

## 检查是否生效 {#verify}

1. 检查 Grasp `/api/health` 和 Gateway `/healthz`，然后核对启动日志中的后端和配置来源。
2. 打开「设置 → 常规」，核对实际值、来源和锁定状态。
3. 用同一个 Agent 启动测试环境，核对网关、模板、资源和预览入口。使用[首次运行指南](/guide/first-run/)检查模型和仓库。
4. 查看[配置索引](/help/configuration-reference/)以按字段查找 YAML 路径、环境变量与缺省值。完整网关字段见[部署参数参考](https://github.com/cocofhu/grasp/blob/main/CONFIGURATION.md)。
