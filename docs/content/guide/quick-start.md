---
title: 启动 Grasp
navTitle: 快速开始
description: 用 Docker Compose 启动本地演示，并检查 Grasp 和沙箱网关是否就绪。
section: start
order: 10
keywords: [安装, 自托管, start.sh, Docker, 登录, doctor]
---

## 准备主机 {#prerequisites}

需要一台安装了 Git、Docker 和 Docker Compose 的 Linux 主机。默认脚本会拉取发布镜像，并在本机启动 Grasp 和 Docker 网关。在共享主机、生产环境或 Kubernetes、E2B 上部署时，先阅读[部署与配置](/help/configuration/)。

## 启动发布镜像 {#start}

```bash
git clone https://github.com/cocofhu/grasp.git
cd grasp
./start.sh -d
```

打开 [http://localhost:8080](http://localhost:8080)。本地演示登录为 `admin` / `demo1234`。该模式绑定在 loopback，适合在主机上试用。正式部署请配置独立用户和凭据加密密钥。

在主机上检查两个服务。

```bash
curl -fsS http://localhost:8080/api/health
curl -fsS http://localhost:8899/healthz
```

第一次创建沙箱时，网关按需拉取沙箱镜像。想在首次运行前预热，可以执行 `./start.sh pull`。准备好后，按[首次运行指南](/guide/first-run/)完成模型和 Agent 配置。

## 日常命令 {#commands}

| 命令 | 效果 |
| --- | --- |
| `./start.sh logs` | 跟随服务日志。 |
| `./start.sh restart` | 重建发布栈服务并保留 `.localdata`。 |
| `./start.sh down` | 停止发布栈服务并保留 `.localdata`。 |
| `./start.sh pull` | 刷新发布栈镜像并预热默认沙箱镜像。 |
| `./start.sh dev -d` | 使用本地源码启动开发栈，前端支持热更新。 |

## 备份应用数据和附件 {#backup}

发布栈使用三个持久化位置。将数据库、应用数据、网关数据和部署密钥放入同一套备份，确保恢复后的附件引用、沙箱记录和凭据仍能使用。

| 位置 | 保存的内容 |
| --- | --- |
| `.localdata/db` | SQLite 数据库及其相关文件。 |
| `.localdata/app-data` | Agent 配置和默认附件存储。如果使用 `GRASP_BLOBS_ROOT`，备份自定义路径。 |
| `.localdata/gateway` | 网关记录。 |
| 部署配置和原 `GRASP_SECRETS_KEY` | 恢复配置并解密已有凭据。 |

沙箱工作文件使用自己的[保存和恢复策略](/help/sandbox-lifecycle/)。备份应用数据时暂停写入或使用一致性快照。如果迁移后附图显示 `blob:` 或 404，先检查数据库与附件目录是否来自同一个备份。
