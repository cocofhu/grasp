# Grasp

**[English](README.md) | 简体中文**
近两年，随着大模型能力持续增强，我开始尝试多方面的探索，大约做过 150+ 个个人项目。在开发过程中，我遇到最大问题是：

1、多项目切换成本高——IDE 来回跳，运行状态、上下文也难管齐；
2、并行开发读不懂 Agent——模型总是一大段一大段往外倒，真正关键的信息反而被淹没，需要花大量的时间理解。

于是我做了 Grasp：把多项目收进同一个平台管理，用可视化的需求澄清，把 Agent 冗长输出收成你能一眼看懂的表达，从而抬高「人」这一侧的吞吐。我们也支持接入多种 Agent 后端，例如 Cursor、CodeBuddy、Claude Code 等。



[项目站](https://www.approving-ai.com/) · [快速开始](https://www.approving-ai.com/guide/quick-start/) · [贡献指南](CONTRIBUTING.md) · [配置](server/CONFIGURATION.md)



[![CI Server](https://github.com/cocofhu/grasp/actions/workflows/ci-server.yml/badge.svg)](https://github.com/cocofhu/grasp/actions/workflows/ci-server.yml)
[![CI Web](https://github.com/cocofhu/grasp/actions/workflows/ci-web.yml/badge.svg)](https://github.com/cocofhu/grasp/actions/workflows/ci-web.yml)
[![CI Sandbox](https://github.com/cocofhu/grasp/actions/workflows/ci-sandbox.yml/badge.svg)](https://github.com/cocofhu/grasp/actions/workflows/ci-sandbox.yml)
[![CI Gateway](https://github.com/cocofhu/grasp/actions/workflows/ci-gateway.yml/badge.svg)](https://github.com/cocofhu/grasp/actions/workflows/ci-gateway.yml)
[![Commits](https://img.shields.io/github/commit-activity/t/cocofhu/grasp)](https://github.com/cocofhu/grasp/commits/main)

[![coverage-web](https://img.shields.io/endpoint?url=https%3A%2F%2Fraw.githubusercontent.com%2Fcocofhu%2Fgrasp%2Fcoverage-badges%2Fcoverage-web.json)](https://github.com/cocofhu/grasp/actions/workflows/ci-web.yml)
[![coverage-sandbox](https://img.shields.io/endpoint?url=https%3A%2F%2Fraw.githubusercontent.com%2Fcocofhu%2Fgrasp%2Fcoverage-badges%2Fcoverage-sandbox.json)](https://github.com/cocofhu/grasp/actions/workflows/ci-sandbox.yml)
[![coverage-server](https://img.shields.io/endpoint?url=https%3A%2F%2Fraw.githubusercontent.com%2Fcocofhu%2Fgrasp%2Fcoverage-badges%2Fcoverage-server.json)](https://github.com/cocofhu/grasp/actions/workflows/ci-server.yml)
[![coverage-gateway](https://img.shields.io/endpoint?url=https%3A%2F%2Fraw.githubusercontent.com%2Fcocofhu%2Fgrasp%2Fcoverage-badges%2Fcoverage-gateway.json)](https://github.com/cocofhu/grasp/actions/workflows/ci-gateway.yml)

## 演示

https://github.com/user-attachments/assets/47728d1f-54a1-485e-967e-28d8c716ed36

## 核心能力

| 能力 | 在 FSM 里的位置 |
|---|---|
| 可视化画布 | 节点 + 成功 / 失败 / 回滚 + `when` + checkpoint |
| 可视化澄清 | Grasp 节点 → 规格 + 计划 + 可选 `page.html` |
| 人工门禁 | 收件箱、运行详情、可分享的临时链接 |
| 并行 run | 多台机器同时跑；人在一个收件箱里审批 |
| Artifact MCP | 按 run 隔离；必要产物卡住转移 |
| Git 交付 | 沙箱内 `gh` / `glab` / SSH |
| 可观测 | 时间线、沙箱日志、产物、Token |

仓库内提供 Clarify、Visual、Research、Proposal、Plan、Implement、Test、Preview、Review 等角色包。用 `agents/pack.sh` 打包后导入 Agent Studio。

## 典型工作流

短的开发前闭环：

```text
一句话 → Grasp（澄清 / 计划 / page.html）→ 人工门禁 → 开工
```

更完整的交付机器：

```text
需求澄清 → 技术调研 → 方案设计 → 人工门禁
        → 执行计划 → 代码实现 → 测试验证 → 代码评审
        → 人工确认 → PR / MR
```

失败边和回滚边画在同一张画布上。下一次失败应该走你已经设计好的路径。

## 快速开始

### 前置条件

- Linux 主机
- Git
- Docker 与 Docker Compose

### 启动

默认路径直接拉取已发布的 GHCR 镜像，无需本地构建：

```bash
git clone https://github.com/cocofhu/grasp.git
cd grasp
./start.sh -d
```

启动后访问：

- UI / API：<http://localhost:8080>
- API 健康检查：<http://localhost:8080/api/health>
- Gateway 健康检查：<http://localhost:8899/healthz>
- 本地演示账号：`admin` / `demo1234`

> 沙箱 runtime 在首次创建沙箱时按需拉取（待办 / 运行页会显示拉取 loading）。可用 `./start.sh pull` 提前预热。

常用命令：

```bash
./start.sh logs          # 查看日志
./start.sh down          # 停止服务
./start.sh pull          # 刷新 GHCR 镜像
./start.sh dev -d        # 源码开发栈：Go + Vite HMR
```

镜像 tag / digest 可在 `.env` 中覆盖，参见 [`.env.example`](.env.example)。

## 四步创建第一个工作流

1. 使用本地演示账号登录。全新安装默认是空项目，不会自动创建样例流水线。
2. 在 **Agent Studio** 创建 Agent，选择 `cursor`、`claude_code`、`codebuddy`、`trae` 或 `opencode`，并配置对应 API Key。
3. 打开画布：把 Grasp 接到开始节点，再接 Visual / 门禁 / 实现。画出成功、失败与回滚，并在该重入的地方标 checkpoint。
4. 发布并启动 run（也可从**首页**用一句话启动）。观察状态轨迹、`page.html` 预览，以及停在门禁上的收件箱项。

后端鉴权和 Agent env 配置详见 [`server/README.md`](server/README.md)。

## 系统架构

```text
┌──────────────────┐     ┌──────────────────┐     ┌──────────────────┐
│ Vue 3 + Vue Flow │────▶│ Go 后端          │────▶│ sandbox-gateway  │
│ FSM 画布         │◀────│ 引擎 + API + MCP │◀────│ 控制面           │
└──────────────────┘     └────────┬─────────┘     └────────┬─────────┘
                                  │                        │
                                  │                        ▼
                                  │               ┌──────────────────┐
                                  └──────────────▶│ Docker 沙箱      │
                                    产物           │ ACP 后端         │
                                                  └──────────────────┘
```

- `web/`：Vue 3 + Vue Flow，负责画布、首页澄清、运行详情、收件箱与 Agent Studio。
- `server/`：Go FSM 引擎、API、SQLite、artifact MCP、调度与审计。
- `sandbox-gateway/gateway/`：沙箱生命周期控制面。
- `sandbox-gateway/sandbox/`：通用沙箱镜像与 ACP bridge。
- `agents/`：可导入的角色 Agent 工作区。
- `docs/`：项目站和中英文帮助文档。

配置优先级为：显式环境变量 > 挂载配置文件 > 代码默认值。完整配置见 [`server/CONFIGURATION.md`](server/CONFIGURATION.md)，网关契约见 [`GATEWAY.md`](GATEWAY.md)。

## 开发与质量

**开发前置：** Go、Node.js、Docker Compose；运行沙箱需要 Linux 宿主。

```bash
./start.sh dev -d
```

各模块的 lint、测试、覆盖率和 E2E 命令见 [`AGENTS.md`](AGENTS.md) 与 [`CONTRIBUTING.md`](CONTRIBUTING.md)。安全工作流在 push / PR 时运行 CodeQL、Web `npm audit` 和 gitleaks。

## 部署与安全提示

- 默认账号仅用于本地演示；共享或生产环境必须配置自己的鉴权用户。
- ACP API Key 与 Git 凭据应配置在项目或 Agent env，不应提交到仓库。
- 发布环境建议使用 digest 固定镜像，参考 [Release images and smoke](CONTRIBUTING.md#release-images-and-smoke)。
- 项目仍处于 Beta 阶段，请在实际环境中完成安全评估、备份和容量验证。
- **反向代理 Host：** 临时审批分享链接按本请求的 `Host` 铸造（不信任客户端 `X-Forwarded-Host`）。代理须保留浏览器原始 Host（如 nginx `proxy_set_header Host $host`）；TLS 终止时正确转发 `X-Forwarded-Proto`。详见 [`SECURITY.md`](SECURITY.md)。
- **数据库与附件同生命周期：** 发布 Compose 把 SQLite（`./.localdata/db`）和附件（`./.localdata/app-data`）分开挂载。备份和清理要成对进行（若自定义了 `GRASP_BLOBS_ROOT` 也要一起带上）；否则 Run 输入可能仍引用 `blob:`，而 `GET /api/blobs/:id` 返回 404。历史孤儿只在 UI 里显示为永久占位，本版本不提供孤儿扫描。详见 [快速开始 · 数据库与附件](docs/content/guide/quick-start.md#数据库与附件同生命周期备份--清理)。

## 文档

- [核心概念](docs/content/guide/concepts.md)
- [快速开始](docs/content/guide/quick-start.md)
- [完整配置](server/CONFIGURATION.md)
- [Gateway 契约](GATEWAY.md)
- [贡献指南](CONTRIBUTING.md)
- [安全策略](SECURITY.md)
- [支持渠道](SUPPORT.md)

## 参与贡献

欢迎提交 Issue 和 Pull Request。请先阅读 [`CONTRIBUTING.md`](CONTRIBUTING.md)、[`AGENTS.md`](AGENTS.md) 与 [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md)。

## 许可证

[MIT](LICENSE) © 2026 cocofhu
