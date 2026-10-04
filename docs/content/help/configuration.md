---
title: 配置
description: 配置要点摘要；完整说明见源码 CONFIGURATION.md。
---

Grasp 平台服务配置以 YAML / 环境变量为主（本地示例见 `server/config.example.yaml` 与根目录 `.env.example`）。ACP、Git 等运行时凭据优先在项目详情的项目凭据 UI 中管理；兼容的项目/Agent 环境变量仅作为回退。

## 完整文档

请以仓库内权威文档为准（避免本站与源码双份漂移）：

- [server/CONFIGURATION.md](https://github.com/cocofhu/approving/blob/main/server/CONFIGURATION.md)

该文档由 `go run ./cmd/gen-configdoc` 生成/校验，CI 会 `-check`。

## 本地快速路径

```bash
./start.sh -d          # 发布镜像栈
./start.sh dev -d      # 源码 + HMR
```

镜像 tag / digest、网关与沙箱相关变量见 `.env.example`。Agent 侧 API key、`GITHUB_*` / `GITLAB_*` / SSH 等应先保存到项目凭据 UI；兼容回退仍可放在项目共享或 Agent meta env（值可引用 `${vars.<name>}`）。

## 数据库与附件同生命周期

正式栈中 SQLite（`./.localdata/db`）与应用数据/默认 blobs（`./.localdata/app-data`，或自定义 `GRASP_BLOBS_ROOT`）必须**成对备份与成对清理**；迁移/升级勿只搬库。否则复合变量附图会出现孤儿 `blob:` 引用（GET `/api/blobs/:id` → 404）。历史孤儿仅界面永久失败占位，本次不做巡检台。详见 [快速开始](../guide/quick-start.md#数据库与附件同生命周期备份--清理)。

## 相关

- [网关](../gateway/)
- [贡献](../contributing/)
