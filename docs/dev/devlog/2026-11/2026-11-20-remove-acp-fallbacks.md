# 2026-11-20 移除 `*_acp` 回退

- 日期：2026-11-20
- 范围：`sandbox-gateway/sandbox/internal/agents`、`internal/provider`（`acpx`、`modelconnection`）、新增 `internal/envutil`，删除 `internal/backend`；`sandbox-gateway/sandbox/scripts`、`sandbox-gateway/scripts/cover-check-sandbox.sh`
- 做了什么：
  - 删除 `cursor_acp` / `claude_code_acp` / `codebuddy_acp` 三个 provider 及其 `modelconnection` 分支、安装脚本里的 `@zed-industries/claude-code-acp`、启动 / reprime 脚本里的别名。
  - 删除 `internal/backend`（`common.Backend` 接口、四个 backend 包）和 `acpx.FromBackend`：`OnEvent` 钩子没有任何实现覆盖，剩下的 `trae` 改为普通 `acpx.Spec`（`agents/trae.go`）。
  - 环境变量切片工具从 `backend/common` 移到 `internal/envutil`（`FirstNonEmpty` / `Get` / `SetIfEmpty` / `Upsert`）。
  - 只有 `codebuddy_acp` 读取的 `ACP_CODEBUDDY_REGION` 一并移除；服务端本就把 `GRASP_CODEBUDDY_REGION` 写成 `CODEBUDDY_INTERNET_ENVIRONMENT`。refusal 提示改为指向官方变量。
- 为什么：这三个 CLI 的主通道已是 stream-json（Claude Code / CodeBuddy 支持原生引导，会话可接回），ACP 回退没有镜像、服务端也从不下发，只剩一套平行的 backend 抽象要维护。#1311 第四阶段的一部分。
- 如何验证：沙箱模块 `go test -race ./...`、golangci-lint、覆盖率门槛 97.8%；`bash -n` 检查三个脚本。
- 后续：ACP `session/load` 接回会话、OpenCode serve、上下文交接组件，见 #1311。
