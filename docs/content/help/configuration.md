---
title: 配置
description: 配置要点摘要；完整说明见源码 CONFIGURATION.md。
---

Grasp 平台服务配置以 YAML / 环境变量为主（本地示例见 `server/config.example.yaml` 与根目录 `.env.example`）。ACP、Git 等运行时凭据只在项目详情的项目凭据 UI 中管理。

## 完整文档

请以仓库内权威文档为准（避免本站与源码双份漂移）：

- [server/CONFIGURATION.md](https://github.com/cocofhu/approving/blob/main/server/CONFIGURATION.md)

该文档由 `go run ./cmd/gen-configdoc` 生成/校验，CI 会 `-check`。

## 本地快速路径

```bash
./start.sh -d          # 发布镜像栈
./start.sh dev -d      # 源码 + HMR
```

镜像 tag / digest、网关与沙箱相关变量见 `.env.example`。Agent 侧 API key、GitHub / GitLab Token、SSH 等只能保存到项目凭据 UI；写进项目共享、Agent meta 或平台 `sandbox.env` 会被拒绝。站点、模型等非敏感项仍可放在 Agent meta env（值可引用 `${vars.<name>}`）。

## 数据库与附件同生命周期

正式栈中 SQLite（`./.localdata/db`）与应用数据/默认 blobs（`./.localdata/app-data`，或自定义 `GRASP_BLOBS_ROOT`）必须**成对备份与成对清理**；迁移/升级勿只搬库。否则复合变量附图会出现孤儿 `blob:` 引用（GET `/api/blobs/:id` → 404）。历史孤儿仅界面永久失败占位，本次不做巡检台。详见 [快速开始](../guide/quick-start.md#数据库与附件同生命周期备份--清理)。

## 可选的 Parallel 网页搜索（OpenCode）

[Parallel Search MCP](https://docs.parallel.ai/integrations/mcp/search-mcp) 通过 Streamable HTTP 提供 `web_search` 和 `web_fetch`，无需 Parallel API Key。匿名档可免费用于轻量请求，有速率限制。

在 Agent Studio 中选择使用 **OpenCode** 后端的 Agent，打开 **MCP** 面板。新增自定义服务 `parallel-search`，选择 **HTTP (url)**，URL 填 `https://search.parallel.ai/mcp`，添加请求头 `User-Agent: grasp/parallel-search-example`。保存后启动新的沙箱会话，使新配置生效。保留原有 MCP 条目，包括 Agent 需要的平台工具。

也可以在 MCP 面板的 **原始 JSON** 数组中追加以下对象：

```json
{
  "name": "parallel-search",
  "url": "https://search.parallel.ai/mcp",
  "headers": {
    "User-Agent": "grasp/parallel-search-example"
  }
}
```

面板接收条目数组，请追加对象，不要用 `mcpServers` 包装替换原数组。Grasp 会将该条目转换为 OpenCode 的远程 MCP 配置；沙箱内不需要额外安装 MCP 包，也不需要保存 Parallel 凭据。Agent 仍需原有模型配置。

可以试试：“使用 parallel-search 查找 go.dev 上的 Go 发布说明，再抓取 https://go.dev/doc/devel/release，附来源 URL 总结受支持的版本。” OpenCode 暴露的工具名带服务名前缀（`parallel-search_web_search` 和 `parallel-search_web_fetch`）。搜索参数为 `objective`、`search_queries`；抓取参数为 `urls` 和可选的 `objective`。优先使用摘录，避免输出过长。

本示例需主动配置，不会改变后端或为其他 Agent 启用搜索。删除时只移除 `parallel-search` 条目，再启动新的沙箱会话。若工具返回限流，请按服务端的重试间隔等待后再调用。

## 相关

- [网关](../gateway/)
- [贡献](../contributing/)
