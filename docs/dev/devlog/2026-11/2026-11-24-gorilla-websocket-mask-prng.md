# 2026-11-24（升级 gorilla/websocket 修复 GO-2026-6278）

- 日期：2026-11-24
- 范围：`server/go.mod`、`server/go.sum`、`sandbox-gateway/gateway/go.mod`、`sandbox-gateway/gateway/go.sum`、`sandbox-gateway/sandbox/go.mod`、`sandbox-gateway/sandbox/go.sum`、`CHANGELOG.md`
- 做了什么：三个 Go 模块的 `github.com/gorilla/websocket` 从 `v1.5.3` 升到修复提交 `v1.5.4-0.20240701034025-d67f41855da4`。
- 为什么：govulncheck 新收录 GO-2026-6278（客户端 WebSocket 掩码密钥使用弱随机数），三个模块都有调用路径（ACP 就绪探测、VNC 预览、终端等）。上游在 `v1.5.3` 之后没有发布正式版本，只能固定到修复提交的伪版本；后续出正式版时再切回 tag。
- 如何验证：`./.github/scripts/govulncheck-check.sh` 不再报 GO-2026-6278；`go build ./...` 三个模块通过；`go test ./internal/handlers/... ./internal/sandbox/...`（server）通过。
