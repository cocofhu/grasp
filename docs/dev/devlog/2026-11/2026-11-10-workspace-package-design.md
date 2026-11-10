# Workspace 子包拆分：接口设计与文件归位

- 日期：2026-11-10
- 范围：`docs/dev/workspace-package-design.md`、`server/internal/services` 三个文件改名

## 做了什么

- 用 AST 工具测量 `workspace_*` 与包内其余代码的依赖：核心 38 个文件依赖 sandbox 租约、ACP 连接、Solo 消息落库等私有能力，反向又被 `SandboxService`、`PreviewService`、`solo_queue.go` 直接调用，存在双向依赖，不能直接移包。
- 写出接口设计：`SandboxRuntime`、`SoloSessions` 两个端口，加上反向的 `Guards` 注入，并分五个阶段推进。
- 第 0 阶段：`workspace_credentials.go` → `credential_service.go`、`workspace_token_stats.go` → `token_stats_buckets.go`、`workspace_mcp_apikey.go` → `mcp_api_key.go`（含测试），只改文件名。

## 为什么

不先设计接口就移包，需要一次性导出约 60 个私有标识符，还要打破循环导入，风险与审查成本都过高。按端口逐步收敛后，最后的移包只剩机械改名。

## 如何验证

- 纯文件改名，`go build ./...`、`go vet ./internal/services` 通过；仓库内没有按旧文件名引用的脚本或文档。
