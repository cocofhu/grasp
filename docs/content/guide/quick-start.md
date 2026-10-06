---
title: 快速开始
description: 在 Linux 主机上用 Docker Compose 拉起 Grasp。
---

## 前提

- Linux 主机
- 已安装 Git、Docker 与 Docker Compose

## 克隆仓库

```bash
git clone https://github.com/cocofhu/approving.git
cd approving
```

## 启动

默认路径从 GHCR 拉取已发布镜像（无需本地构建镜像）：

```bash
./start.sh -d
```

然后打开：

- UI / API：http://localhost:8080
- API health：http://localhost:8080/api/health
- Gateway health：http://localhost:8899/healthz
- 默认登录：`admin` / `demo1234`（local-demo）

默认 `./start.sh` / `-d` / `restart` **不会**预拉 sandbox runtime：首次创建沙箱时 Gateway 再按需拉取（可能数 GB），待办 starting / 运行页会显示「正在拉取镜像」。需要提前预热用 `./start.sh pull`。

Agent / workspace 与 SQLite 持久在仓库根 `.localdata` 宿主机目录（bind mount：`gateway` / `db` / `app-data`）。`./start.sh restart` 与 `./start.sh down` 会保留该目录。清空数据：`./start.sh down && rm -rf .localdata`。

### 数据库与附件同生命周期（备份 / 清理）

正式栈（`compose.release.yaml`）把 **SQLite** 挂在 `./.localdata/db`，把 **应用数据（含默认附件 blobs，相对 `WORKDIR` 的 `data/blobs`）** 挂在 `./.localdata/app-data`。复合变量附图在库/Run 输出里只保留 `blob:{id}` 引用，字节落在 blobs 目录；若只备份或只清理其中一侧，会出现「库里还有引用、GET `/api/blobs/:id` 却 404」的孤儿引用，Run 详情里附图会显示为「无法显示 / 附件不可用」。

运维约束：

- **成对备份**：同一备份集须同时包含 `./.localdata/db` 与 `./.localdata/app-data`（或整棵 `.localdata`）。
- **成对清理 / 迁移 / 升级**：不要只搬 SQLite 或只删附件目录；自定义 `GRASP_BLOBS_ROOT` 时，须把该路径与数据库一并纳入同一生命周期。
- **历史孤儿**：已损坏的引用不保证可从附件存储找回；界面仅展示永久失败占位。本次交付**不做**孤儿巡检台、批量扫描页或启动/健康检查告警。

## 常用命令

```bash
./start.sh logs
./start.sh down
./start.sh pull          # 刷新 compose 镜像并预热 sandbox runtime
./start.sh restart       # down + up -d（保留 .localdata）
./start.sh dev -d        # 源码栈：go run + Vite HMR
```

镜像 tag / digest 可在 `.env` 覆盖 — 见仓库根目录 [`.env.example`](https://github.com/cocofhu/approving/blob/main/.env.example)。默认一张 `universal-sandbox`（六个 CLI 预装，含 Codex；Codex 使用登录文件而不是 API Key，运行时按 Agent 后端切换）。发布与 smoke 见 [Contributing](https://github.com/cocofhu/approving/blob/main/CONTRIBUTING.md)。

## 下一步

- 登录后进入「默认项目」时，按第一次安装引导在项目凭据 UI 配置 **ACP 后端、API Token、Git 凭据**（Git 可跳过）。运行时密钥只来自项目凭据。完成后生成 **综合项目组** 与已发布的 **默认工作流**（仓库 URL 在启动 Run 时填写）。引导只针对默认项目。
- [核心概念](../concepts/) — FSM、gate、sandbox、artifact
- [配置摘要](../../help/configuration/) — 指向完整 `CONFIGURATION.md`
- [网关摘要](../../help/gateway/) — 指向 `GATEWAY.md`
