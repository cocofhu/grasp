# 2026-11-23 准备 4.0.0 发布候选

- 日期：2026-11-23
- 范围：`CHANGELOG.md`、发布镜像默认值、Docker/Kubernetes 部署示例、gateway 配置测试
- 做了什么：对比 `v3.3.0` 与当前 `HEAD` 的 135 个提交，将已有 Unreleased 变更固化为 `4.0.0`，并把 Grasp、sandbox-gateway 与 universal-sandbox 的部署默认标签统一到 `4.0.0`。同步 Kubernetes 示例及其配置加载测试。
- 为什么：当前变更包含 MCP 改名、执行模式移除、gateway 配置拆分、仓库授权归属调整、Cursor SDK 与 `IMAGE_LEVEL=3` 等不兼容变化，适合按新的主版本发布。所有部署入口需要指向同一组经过验收的镜像。
- 如何验证：服务端、gateway、sandbox 的清理环境 Go 全量测试通过；Web lint、类型检查、Vitest 覆盖率与生产构建通过；docs 构建和官方 npm registry high/critical audit 通过。GitHub exact `HEAD` 的 `go-race(server-handlers)` 仍是取消状态，发布前需要重新跑完整 CI，并完成三张 GHCR 镜像和 release smoke 验收。
