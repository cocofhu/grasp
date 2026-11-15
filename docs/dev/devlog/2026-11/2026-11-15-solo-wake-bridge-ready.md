# 2026-11-15（Solo 唤醒后等会话服务就绪，唤醒失败时重试）

- 日期：2026-11-15
- 范围：`server/internal/services/sandbox_solo_recovery.go`、`server/internal/services/solo.go`、`server/internal/services/sandbox.go`、`server/internal/sandbox/sandboxtest/fakegw.go`、相关测试
- 做了什么：
  - `EnsureSoloRunning` 从 exited/stopped 恢复、或等待 pending/creating 的常驻沙箱后，先用 `WaitForACPReady` 登录 bridge 并完成一次 WebSocket 握手，成功后才记为已恢复；超时按恢复失败处理，工作区保留。已在运行的沙箱不做这一步。
  - `waitSandbox` 唤醒失败时间隔 3 秒重试，每轮最多 3 次，仍在原有 20 分钟准备时间内；会话关闭、停止或沙箱不存在时照常立即结束。
  - 假网关新增 `FailStartTimes`；新增唤醒等待/重试单测和暂停后首轮的端到端测试（`solo_pause_e2e_test.go`）。
- 为什么：用户反馈沙箱暂停后发的第一条消息必然失败。原先网关报告 running 就算唤醒完成，而 E2B 只检查 relay 看到的端口在监听，Docker 端口映射在容器启动时就能连通，二者都早于 bridge 真正可用；新建沙箱有就绪检查，唤醒路径缺了。另外网关 `/start` 的 HTTP 超时为 60 秒，E2B 恢复较慢时单次唤醒失败会直接让这一轮失败。
- 如何验证：`go test ./internal/services/ ./internal/sandbox/...`、`go vet`。把重试次数改回 1 可复现"唤醒失败一次即整轮失败"。端到端测试中，去掉就绪等待后也能通过（ACP `Connect` 本身会重试登录约 90 秒），旧版本 `ce7c098` 同样通过，因此尚未证实这是线上"必现"的根因；未在真实 E2B 环境验证。
